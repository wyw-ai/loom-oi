package relay

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type sessCause int

const (
	causeLost       sessCause = iota // connection ended or protocol error
	causeSuperseded                  // a newer connection of the Access arrived
	causeRevoked                     // Access deleted or disabled, or pin removed
	causeOffline                     // heartbeat acknowledgements missing
	causeShutdown
)

func (c sessCause) String() string {
	return [...]string{"lost", "superseded", "revoked", "offline", "shutdown"}[c]
}

// ctrlQueue bounds queued control messages; an OIC that stops reading its
// control connection is disconnected rather than buffered without limit.
const ctrlQueue = 256

// maxPendingAudits bounds registry audit records kept while the store is
// unavailable; the newest records are kept.
const maxPendingAudits = 1024

type ctrlOut struct {
	data  []byte
	close bool
}

// session is one oi.control.v1 connection. Fields after out are guarded
// by Hub.mu.
type session struct {
	h          *Hub
	conn       Conn
	accessID   string
	pin        string
	gen, epoch int64
	out        chan ctrlOut

	seq        uint64
	registered bool
	ended      bool
	hbSeq      uint64
}

// push queues m and returns its seq, or 0 when the queue is full.
func (s *session) push(m protocol.Message) uint64 {
	s.seq++
	select {
	case s.out <- ctrlOut{data: protocol.EncodeControl(m, s.seq)}:
		return s.seq
	default:
		return 0
	}
}

// send queues m on a live session; a full queue ends the session. Caller
// holds h.mu.
func (s *session) send(m protocol.Message) uint64 {
	if s.ended {
		return 0
	}
	seq := s.push(m)
	if seq == 0 {
		s.h.endSession(s, causeLost, "", "")
	}
	return seq
}

func (s *session) writeLoop() {
	defer s.h.loops.Done()
	timeout := limits.D(s.h.lim.HeartbeatMS)
	for m := range s.out {
		if m.close {
			_ = s.conn.Close("")
			return
		}
		ctx, cancel := context.WithTimeout(s.h.ctx, timeout)
		err := s.conn.Write(ctx, true, m.data)
		cancel()
		if err != nil {
			_ = s.conn.CloseNow()
			return
		}
	}
}

func messageOf(err error) string {
	if pe, ok := err.(*protocol.Error); ok {
		return pe.Message
	}
	return err.Error()
}

// ServeControl runs one control session for an mTLS-authenticated Access
// until its connection ends (B1 §4).
func (h *Hub) ServeControl(conn Conn, accessID, pin string) {
	ctx, cancel := context.WithTimeout(h.ctx, limits.D(h.lim.PrepareMS))
	text, data, err := conn.Read(ctx)
	cancel()
	if err != nil {
		_ = conn.CloseNow()
		return
	}
	var hello *protocol.Hello
	if text {
		if m, derr := protocol.DecodeControl(data); derr == nil {
			hello, _ = m.(*protocol.Hello)
		}
	}
	if hello == nil {
		h.reject(conn, protocol.CodeProtocolError, "the first control message must be hello")
		return
	}
	s, err := h.startSession(conn, accessID, pin, hello)
	if err != nil {
		h.reject(conn, protocol.CodeOf(err), messageOf(err))
		return
	}
	defer h.conns.Done()
	go s.writeLoop()
	s.readLoop()
	h.mu.Lock()
	h.endSession(s, causeLost, "", "")
	h.mu.Unlock()
}

func (h *Hub) reject(conn Conn, code protocol.Code, detail string) {
	ctx, cancel := context.WithTimeout(h.ctx, limits.D(h.lim.CloseMS))
	defer cancel()
	_ = conn.Write(ctx, true, protocol.EncodeControl(&protocol.ControlError{Code: code, Detail: detail}, 1))
	_ = conn.Close(string(code))
}

func (h *Hub) startSession(conn Conn, accessID, pin string, hello *protocol.Hello) (*session, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.shutting {
		return nil, protocol.Errorf(protocol.CodeShutdown, "OIS is stopping")
	}
	a := h.accesses[accessID]
	if a == nil || !a.rec.Enabled || !slices.Contains(a.rec.SPKIPins, pin) {
		return nil, protocol.Errorf(protocol.CodeRevoked, "access is disabled or the certificate is not pinned")
	}
	// A raise is bounded so one Access cannot push the epoch to where OIS
	// could no longer move past it (B3 §6); epochs OIS issued itself are
	// accepted whatever their size.
	if hello.LastEpochSeen > h.epoch && hello.LastEpochSeen >= protocol.MaxEpoch {
		return nil, protocol.Errorf(protocol.CodeProtocolError, "last_epoch_seen %d exceeds the epoch bound", hello.LastEpochSeen)
	}
	now := h.now()
	if a.sess != nil {
		h.endSession(a.sess, causeSuperseded, protocol.CodeSuperseded, "a newer control connection replaced this one")
	}
	// A new generation aborts every channel of older generations (D1-f).
	for _, ch := range h.channels {
		if ch.accessID == accessID {
			h.terminate(ch, protocol.CodeChannelInterrupted, now)
		}
	}
	if hello.LastEpochSeen > h.epoch {
		h.raiseEpoch(accessID, hello.LastEpochSeen, now)
	}
	a.gen++
	s := &session{h: h, conn: conn, accessID: accessID, pin: pin, gen: a.gen, epoch: h.epoch,
		out: make(chan ctrlOut, ctrlQueue)}
	a.sess, a.missed, a.offline = s, 0, false
	s.send(&protocol.Welcome{
		Epoch: s.epoch, Generation: s.gen, RunID: h.runID,
		HeartbeatIntervalMS: h.lim.HeartbeatMS, LicenseTTLMS: h.lim.LicenseTTLMS,
		Limits: protocol.WelcomeLimits{
			DataMax: h.lim.DataMax, CloseGraceMS: h.lim.CloseMS, DescMax: h.lim.DescMax,
			MaxExposures: min(a.rec.MaxExposures, h.lim.MaxExposures),
		},
	})
	h.conns.Add(1)
	h.loops.Add(1)
	h.log.Info("control session started", "access", accessID, "epoch", s.epoch, "generation", s.gen,
		"oic_version", hello.OICVersion)
	return s, nil
}

// raiseEpoch moves the epoch past a value an OIC has already seen: the
// database was replaced by an older copy (B3 §6). Caller holds h.mu.
func (h *Hub) raiseEpoch(accessID string, seen int64, now time.Time) {
	prev := h.epoch
	h.epoch = seen + 1
	h.epochDirty = true
	if !h.rollbackAudited {
		h.rollbackAudited = true
		detail, _ := json.Marshal(map[string]any{"access_id": accessID, "last_epoch_seen": seen,
			"store_epoch": prev, "raised_to": h.epoch})
		h.epochAudit = &store.AuditEntry{At: now, Actor: "access:" + accessID, Action: "epoch_rollback_detected",
			ObjectType: "meta", ObjectID: "epoch", Detail: detail}
	}
	h.log.Warn("epoch rollback detected", "access", accessID, "last_epoch_seen", seen, "store_epoch", prev, "raised_to", h.epoch)
	h.kickPersist()
}

// endSession closes a control session once. code, when set, is sent in an
// error message first. Caller holds h.mu.
func (h *Hub) endSession(s *session, cause sessCause, code protocol.Code, detail string) {
	if s.ended {
		return
	}
	s.ended = true
	if code != "" {
		s.push(&protocol.ControlError{Code: code, Detail: detail})
	}
	select {
	case s.out <- ctrlOut{close: true}:
	default:
		_ = s.conn.CloseNow()
	}
	if a := h.accesses[s.accessID]; a != nil && a.sess == s {
		a.sess = nil
	}
	now := h.now()
	for _, ch := range h.channels {
		if ch.accessID != s.accessID || ch.gen != s.gen {
			continue
		}
		switch cause {
		case causeRevoked:
			h.terminate(ch, protocol.CodeRevoked, now)
		case causeSuperseded:
			h.terminate(ch, protocol.CodeChannelInterrupted, now)
		case causeLost:
			// Without a control path a Preparing channel cannot commit; Open
			// channels last until the Access is judged offline (B2 §5).
			if ch.state == statePreparing {
				h.terminate(ch, protocol.CodeAccessOffline, now)
			}
		}
	}
	h.log.Info("control session ended", "access", s.accessID, "generation", s.gen, "cause", cause.String())
}

func (s *session) readLoop() {
	for {
		text, data, err := s.conn.Read(s.h.ctx)
		if err != nil {
			return
		}
		var m protocol.Message
		if text {
			m, err = protocol.DecodeControl(data)
		} else {
			err = protocol.Errorf(protocol.CodeProtocolError, "control messages must be text")
		}
		if !s.h.handle(s, m, err) {
			return
		}
	}
}

func (h *Hub) handle(s *session, m protocol.Message, err error) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.ended {
		return false
	}
	if err != nil {
		h.endSession(s, causeLost, protocol.CodeProtocolError, messageOf(err))
		return false
	}
	now := h.now()
	switch v := m.(type) {
	case *protocol.Register:
		h.onRegister(s, v, now)
	case *protocol.PrepareFailed:
		h.onPrepareFailed(s, v, now)
	case *protocol.HeartbeatAck:
		if v.AckSeq == s.hbSeq {
			s.hbSeq = 0
		}
	case *protocol.ControlError:
		h.endSession(s, causeLost, "", "")
		return false
	default:
		h.endSession(s, causeLost, protocol.CodeProtocolError, "unexpected control message from OIC")
		return false
	}
	return true
}

func (h *Hub) checkEntry(accessID string, e protocol.RegisterExposure, accepted map[string]*exposure, limit int) protocol.Code {
	owner, _, ok := protocol.SplitExposureID(e.ExposureID)
	switch {
	case !ok || accepted[e.ExposureID] != nil:
		return protocol.CodeInvalidRequest
	case owner != accessID:
		return protocol.CodeDenied
	// JSON decoding already replaced invalid UTF-8 in the description.
	case !protocol.ValidRevision(e.Revision) || !protocol.ValidProtocol(e.Protocol) ||
		e.TransportProfile != protocol.TransportTCP || !protocol.ValidAdmission(e.AdmissionMode):
		return protocol.CodeInvalidRequest
	case len(e.Description) > h.lim.DescMax:
		return protocol.CodePayloadTooLarge
	case len(accepted) >= limit:
		return protocol.CodeResourceExhausted
	}
	return ""
}

func sameEntry(a, b store.Exposure) bool {
	return a.Revision == b.Revision && a.Protocol == b.Protocol && a.TransportProfile == b.TransportProfile &&
		a.AdmissionMode == b.AdmissionMode && a.Description == b.Description
}

func regAudit(now time.Time, accessID, action, exposureID, from, to string) store.AuditEntry {
	detail, _ := json.Marshal(map[string]string{"from": from, "to": to})
	return store.AuditEntry{At: now, Actor: "access:" + accessID, Action: action,
		ObjectType: "exposure", ObjectID: exposureID, Detail: detail}
}

// onRegister replaces the Access's registry with the accepted entries of a
// full snapshot (B1 §4, B2 §4). Channels bound to a removed or changed
// revision end with RevisionChanged. Caller holds h.mu.
func (h *Hub) onRegister(s *session, m *protocol.Register, now time.Time) {
	a := h.accesses[s.accessID]
	limit := min(a.rec.MaxExposures, h.lim.MaxExposures)
	ack := &protocol.RegisterAck{Accepted: []string{}, Rejected: []protocol.Rejection{}}
	next := map[string]*exposure{}
	for _, e := range m.Exposures {
		if code := h.checkEntry(s.accessID, e, next, limit); code != "" {
			ack.Rejected = append(ack.Rejected, protocol.Rejection{ExposureID: e.ExposureID, Code: code})
			continue
		}
		next[e.ExposureID] = &exposure{gen: s.gen, rec: store.Exposure{
			ExposureID: e.ExposureID, AccessID: s.accessID, Revision: e.Revision, Protocol: e.Protocol,
			TransportProfile: e.TransportProfile, AdmissionMode: e.AdmissionMode, Description: e.Description,
			UpdatedAt: now.UTC().Truncate(time.Microsecond),
		}}
		ack.Accepted = append(ack.Accepted, e.ExposureID)
	}
	old := map[string]*exposure{}
	for id, e := range h.exposures {
		if e.rec.AccessID == s.accessID {
			old[id] = e
		}
	}
	var audits []store.AuditEntry
	changed := false
	for id, o := range old {
		n := next[id]
		switch {
		case n == nil:
			audits = append(audits, regAudit(now, s.accessID, "exposure_removed", id, o.rec.Revision, ""))
		case n.rec.Revision != o.rec.Revision:
			audits = append(audits, regAudit(now, s.accessID, "exposure_revision_changed", id, o.rec.Revision, n.rec.Revision))
		case sameEntry(n.rec, o.rec):
			n.rec.UpdatedAt = o.rec.UpdatedAt
			continue
		}
		changed = true
	}
	for id, n := range next {
		if old[id] == nil {
			audits = append(audits, regAudit(now, s.accessID, "exposure_added", id, "", n.rec.Revision))
			changed = true
		}
	}
	for _, ch := range h.channels {
		if ch.accessID != s.accessID {
			continue
		}
		if n := next[ch.exposureID]; n == nil || n.rec.Revision != ch.revision {
			h.terminate(ch, protocol.CodeRevisionChanged, now)
		}
	}
	for id := range old {
		delete(h.exposures, id)
	}
	for id, n := range next {
		h.exposures[id] = n
	}
	if changed {
		h.regSeq++
		p := h.regDirty[s.accessID]
		if p == nil {
			p = &regPending{}
			h.regDirty[s.accessID] = p
		}
		p.seq = h.regSeq
		p.audits = append(p.audits, audits...)
		if over := len(p.audits) - maxPendingAudits; over > 0 {
			p.audits = p.audits[over:]
		}
		h.kickPersist()
	}
	s.registered = true
	s.send(ack)
}

func (h *Hub) onPrepareFailed(s *session, m *protocol.PrepareFailed, now time.Time) {
	ch := h.channels[m.ChannelID]
	if ch == nil || ch.accessID != s.accessID || ch.gen != s.gen || ch.state != statePreparing {
		return
	}
	code := m.Code
	if _, ok := protocol.AbortReason(code); !ok {
		code = protocol.CodeProtocolError
	}
	h.terminate(ch, code, now)
}

// heartbeatTick sends heartbeats and judges Accesses offline after
// OFFLINE_MISSES unacknowledged periods (B2 §5).
func (h *Hub) heartbeatTick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for _, a := range h.accesses {
		if a.gen == 0 || a.offline {
			continue
		}
		s := a.sess
		if s != nil && s.hbSeq == 0 {
			a.missed = 0
		} else {
			a.missed++
		}
		if a.missed >= h.lim.OfflineMisses {
			a.offline = true
			if s != nil {
				h.endSession(s, causeOffline, "", "")
			}
			for _, ch := range h.channels {
				if ch.accessID == a.rec.AccessID {
					h.terminate(ch, protocol.CodeAccessOffline, now)
				}
			}
			h.log.Warn("access offline", "access", a.rec.AccessID, "generation", a.gen)
			continue
		}
		if s != nil {
			s.hbSeq = s.send(&protocol.Heartbeat{LicenseTTLMS: h.lim.LicenseTTLMS})
		}
	}
}
