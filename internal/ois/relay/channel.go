package relay

import (
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const (
	statePreparing int32 = iota + 1
	stateOpen
	stateClosing
	stateClosed
)

var stateNames = map[int32]string{
	statePreparing: protocol.StatePreparing,
	stateOpen:      protocol.StateOpen,
	stateClosing:   protocol.StateClosing,
	stateClosed:    protocol.StateClosed,
}

// channel is guarded by Hub.mu except st, which leg writers read.
type channel struct {
	id, exposureID, accessID, revision, subject, tokenID string
	admission, readiness, reqAdmission, requestID        string
	epoch, gen                                           int64

	state  int32
	st     atomic.Int32
	reason protocol.Code

	created, deadline, opened, lastData, closedAt, forceAt time.Time

	ticketHash [2][32]byte
	used       [2]bool
	legs       [2]*leg
	fin        [2]bool
	quotaHeld  bool
}

func (c *channel) setState(s int32) {
	c.state = s
	c.st.Store(s)
}

func (c *channel) view() protocol.ChannelView {
	return protocol.ChannelView{
		ChannelID: c.id, State: stateNames[c.state], CloseReason: c.reason,
		ExposureID: c.exposureID, Revision: c.revision,
	}
}

func (c *channel) liveLegs() int {
	n := 0
	for _, l := range c.legs {
		if l != nil && !l.dead {
			n++
		}
	}
	return n
}

// quota counts Preparing, Open and Closing channels: a channel holds its
// share until it is Closed (B1 §6 releases quota on Closed).
type quota struct {
	total      int
	perAccess  map[string]int
	perSubject map[string]int
}

func newQuota() quota {
	return quota{perAccess: map[string]int{}, perSubject: map[string]int{}}
}

func (q *quota) admit(l limits.Limits, accessID, subject string) bool {
	return q.total < l.NOIS && q.perAccess[accessID] < l.NAccess && q.perSubject[subject] < l.NSubject
}

func (q *quota) take(c *channel) {
	q.total++
	q.perAccess[c.accessID]++
	q.perSubject[c.subject]++
	c.quotaHeld = true
}

func (q *quota) release(c *channel) {
	if !c.quotaHeld {
		return
	}
	c.quotaHeld = false
	q.total--
	if q.perAccess[c.accessID]--; q.perAccess[c.accessID] == 0 {
		delete(q.perAccess, c.accessID)
	}
	if q.perSubject[c.subject]--; q.perSubject[c.subject] == 0 {
		delete(q.perSubject, c.subject)
	}
}

// terminate moves a Preparing channel to Closed or an Open channel to
// Closing with reason: attached legs receive ABORT and the OIC receives
// close_channel. Other states are left unchanged. Caller holds h.mu.
func (h *Hub) terminate(ch *channel, reason protocol.Code, now time.Time) {
	if ch.state != statePreparing && ch.state != stateOpen {
		return
	}
	wasPreparing := ch.state == statePreparing
	ch.reason = reason
	ch.forceAt = now.Add(limits.D(h.lim.CloseMS))
	if a := h.accesses[ch.accessID]; a != nil && a.sess != nil && a.sess.gen == ch.gen {
		a.sess.send(&protocol.CloseChannel{ChannelID: ch.id, Code: reason})
	}
	for _, l := range ch.legs {
		if l != nil {
			l.abort(reason)
		}
	}
	if wasPreparing || ch.liveLegs() == 0 {
		h.finalize(ch, now)
		return
	}
	ch.setState(stateClosing)
	h.log.Info("closing", "channel", ch.id, "reason", reason)
}

// finalize marks the channel Closed and releases its quota.
func (h *Hub) finalize(ch *channel, now time.Time) {
	ch.setState(stateClosed)
	ch.closedAt = now
	h.quota.release(ch)
	h.log.Info("closed", "channel", ch.id, "reason", ch.reason)
}

// tryCommit runs the B1 §6 commit check once both legs are attached and
// queues READY to the provider leg, then the consumer leg.
func (h *Hub) tryCommit(ch *channel, now time.Time) {
	if ch.legs[Consumer] == nil || ch.legs[Provider] == nil {
		return
	}
	if code := h.commitCheck(ch, now); code != "" {
		h.terminate(ch, code, now)
		return
	}
	ch.setState(stateOpen)
	ch.opened = now
	ch.lastData = now
	// The consumer's READY is written only after the provider's (B1 §5).
	providerReady := &readyGate{ch: make(chan struct{})}
	ch.legs[Provider].enqueue(outMsg{frame: protocol.ReadyFrame(), signal: providerReady})
	ch.legs[Consumer].enqueue(outMsg{frame: protocol.ReadyFrame(), after: providerReady})
	h.log.Info("ready", "channel", ch.id, "post_to_ready_ms", ms(now.Sub(ch.created)))
}

// commitCheck returns the close reason of the first failing condition.
// P1 has no probes, so the readiness item ⑥ cannot fail here:
// require_ready was refused at precheck and NotReady never occurs.
func (h *Hub) commitCheck(ch *channel, now time.Time) protocol.Code {
	if !now.Before(ch.deadline) {
		return protocol.CodePrepareTimeout
	}
	if !h.permitted(ch.subject, ch.exposureID) {
		return protocol.CodeRevoked
	}
	if code := h.tokenProblem(ch, now); code != "" {
		return code
	}
	exp := h.exposures[ch.exposureID]
	if exp == nil || exp.rec.Revision != ch.revision || exp.rec.AccessID != ch.accessID || exp.gen != ch.gen {
		return protocol.CodeRevisionChanged
	}
	a := h.accesses[ch.accessID]
	if a == nil || !a.rec.Enabled {
		return protocol.CodeRevoked
	}
	if !a.online() || a.sess.gen != ch.gen {
		return protocol.CodeAccessOffline
	}
	return ""
}

func (h *Hub) tokenProblem(ch *channel, now time.Time) protocol.Code {
	tok, ok := h.tokens[ch.tokenID]
	if !ok || tok.RevokedAt != nil {
		return protocol.CodeRevoked
	}
	if !now.Before(tok.ExpiresAt) {
		return protocol.CodeUnauthenticated
	}
	return ""
}

// sweep is the janitor pass: prepare deadlines, token expiry, idle
// timeouts, T_close force-close and RETAIN cleanup.
func (h *Hub) sweep(now time.Time) {
	var force []*leg
	h.mu.Lock()
	for id, ch := range h.channels {
		switch ch.state {
		case statePreparing:
			if !now.Before(ch.deadline) {
				h.terminate(ch, protocol.CodePrepareTimeout, now)
			} else if code := h.tokenProblem(ch, now); code == protocol.CodeUnauthenticated {
				h.terminate(ch, code, now)
			}
		case stateOpen:
			if code := h.tokenProblem(ch, now); code == protocol.CodeUnauthenticated {
				h.terminate(ch, code, now)
			} else if now.Sub(ch.lastData) >= limits.D(h.lim.IdleMS) {
				h.terminate(ch, protocol.CodeIdleTimeout, now)
			}
		}
		if (ch.state == stateClosing || ch.state == stateClosed) && !now.Before(ch.forceAt) {
			for _, l := range ch.legs {
				if l != nil && !l.dead {
					force = append(force, l)
				}
			}
			if ch.state == stateClosing {
				h.finalize(ch, now)
			}
		}
		if ch.state == stateClosed && now.Sub(ch.closedAt) >= limits.D(h.lim.RetainMS) {
			delete(h.channels, id)
			if ch.requestID != "" {
				delete(h.idem, idemKey{ch.subject, ch.requestID})
			}
		}
	}
	h.mu.Unlock()
	for _, l := range force {
		_ = l.conn.CloseNow()
	}
}
