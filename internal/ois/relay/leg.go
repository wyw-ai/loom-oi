package relay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/wsio"
)

// legQueue bounds a leg's ordered write queue. At most READY, one DATA
// (forwarding is synchronous), FIN, ABORT and the close marker are ever
// queued at once.
const legQueue = 8

type outMsg struct {
	frame []byte
	close bool
	done  chan struct{} // DATA only: closed once written or dropped
	n     int
	owner *wsio.DataOwner // destination DATA from enqueue through finish
	// after and signal order the two READY frames (B1 §5).
	after, signal *readyGate
}

// readyGate holds the consumer's READY until the provider's READY was
// written, and drops it when the provider's READY was not.
type readyGate struct {
	ch      chan struct{}
	written atomic.Bool
}

// leg is one attached data WebSocket. dead, abortSent, peerAborted and
// closeQueued are guarded by Hub.mu.
type leg struct {
	h       *Hub
	ch      *channel
	role    Role
	conn    Conn
	out     chan outMsg
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	control *wsio.Control

	dead        bool
	abortSent   bool
	peerAborted bool
	closeQueued bool

	activity    atomic.Int64
	forwarding  atomic.Bool
	finWritten  atomic.Bool // diagnostic: FIN successfully written to this leg
	diagnostics legDiagnostics
}

// enqueue adds m to the ordered write queue. Caller holds h.mu.
func (l *leg) enqueue(m outMsg) {
	if l.dead {
		l.finish(m, false)
		return
	}
	select {
	case l.out <- m:
	default:
		// Unreachable while the queue bound above holds; fail closed.
		l.finish(m, false)
		l.closeNow("queue_full")
	}
}

// abort queues ABORT (unless this leg sent one) and the close marker.
// Caller holds h.mu.
func (l *leg) abort(reason protocol.Code) {
	if !l.abortSent && !l.peerAborted {
		l.abortSent = true
		l.enqueue(outMsg{frame: protocol.AbortFrame(reason, "")})
	}
	l.closeAfterQueue()
}

func (l *leg) closeAfterQueue() {
	if !l.closeQueued {
		l.closeQueued = true
		l.enqueue(outMsg{close: true})
	}
}

func (l *leg) finish(m outMsg, written bool) {
	if m.signal != nil {
		m.signal.written.Store(written)
		close(m.signal.ch)
	}
	if m.done == nil {
		return
	}
	if written {
		l.control.Data()
	}
	m.owner.End()
	l.h.inflight.Add(-int64(m.n))
	if written {
		l.h.relayBytes.Add(int64(m.n))
	}
	close(m.done)
}

func (l *leg) drain() {
	for {
		select {
		case m := <-l.out:
			l.finish(m, false)
		default:
			return
		}
	}
}

// AttachRequest describes an attach attempt after the server checked the
// subprotocol, the query string and the caller's credential.
type AttachRequest struct {
	ChannelID string
	Ticket    string
	Role      Role
	Principal Principal // consumer legs
	AccessID  string    // provider legs: the Access of the mTLS pin
}

// Reservation is a consumed ticket waiting for its WebSocket.
type Reservation struct {
	ch   *channel
	role Role
}

var errTicketInvalid = protocol.Errorf(protocol.CodeTicketInvalid, "ticket is not valid for this channel and role")

// ReserveAttach validates and consumes a ticket atomically (B2 §3).
func (h *Hub) ReserveAttach(req AttachRequest) (*Reservation, error) {
	if !strings.HasPrefix(req.Ticket, "tk_") || len(req.Ticket) > 128 {
		return nil, errTicketInvalid
	}
	sum := sha256.Sum256([]byte(req.Ticket))
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	ch := h.channels[req.ChannelID]
	if ch == nil || subtle.ConstantTimeCompare(sum[:], ch.ticketHash[req.Role][:]) != 1 {
		return nil, errTicketInvalid
	}
	if ch.state == statePreparing && !now.Before(ch.deadline) {
		h.terminate(ch, protocol.CodePrepareTimeout, now)
	}
	if ch.state == stateClosing || ch.state == stateClosed {
		return nil, protocol.Errorf(protocol.CodeChannelGone, "channel has terminated")
	}
	if ch.used[req.Role] {
		return nil, protocol.Errorf(protocol.CodeTicketConsumed, "ticket was already consumed")
	}
	if req.Role == Consumer {
		if req.Principal.Subject != ch.subject || req.Principal.TokenID != ch.tokenID {
			return nil, errTicketInvalid
		}
	} else {
		a := h.accesses[ch.accessID]
		if req.AccessID != ch.accessID || a == nil || a.sess == nil || a.sess.gen != ch.gen {
			return nil, errTicketInvalid
		}
	}
	ch.used[req.Role] = true
	return &Reservation{ch: ch, role: req.Role}, nil
}

// CancelAttach handles a reservation whose WebSocket upgrade failed.
func (h *Hub) CancelAttach(res *Reservation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminate(res.ch, protocol.CodeChannelInterrupted, h.now())
}

// Attach runs an upgraded leg until it ends.
func (h *Hub) Attach(res *Reservation, conn Conn) {
	control := conn.DataControl()
	h.mu.Lock()
	ch := res.ch
	if ch.state != statePreparing {
		reason := ch.reason
		h.mu.Unlock()
		ctx, cancel := context.WithTimeout(h.ctx, limits.D(h.lim.CloseMS))
		_ = conn.Write(ctx, false, protocol.AbortFrame(reason, ""))
		_ = conn.CloseNow()
		if control != nil {
			_ = control.Join(ctx)
		}
		cancel()
		return
	}
	if control == nil {
		h.terminate(ch, protocol.CodeChannelInterrupted, h.now())
		h.mu.Unlock()
		_ = conn.CloseNow()
		return
	}
	ctx, cancel := context.WithCancel(h.ctx)
	l := &leg{h: h, ch: ch, role: res.role, conn: conn, out: make(chan outMsg, legQueue),
		done: make(chan struct{}), ctx: ctx, cancel: cancel, control: control}
	l.diagnostics.started = time.Now()
	if err := control.EnableProbe(l.diagnostics.started, limits.D(h.lim.PingMS)); err != nil {
		h.terminate(ch, protocol.CodeChannelInterrupted, h.now())
		h.mu.Unlock()
		cancel()
		_ = conn.CloseNow()
		l.joinControl()
		return
	}
	l.diagnostics.workers.Store(3)
	ch.legs[res.role] = l
	h.conns.Add(1)
	h.loops.Add(2)
	go l.writeLoop()
	go l.pingLoop()
	h.tryCommit(ch, h.now())
	h.mu.Unlock()
	l.readLoop()
	l.joinControl()
	l.workerEnded()
	h.conns.Done()
}

func (l *leg) writeLoop() {
	defer l.h.loops.Done()
	defer l.workerEnded()
	for {
		select {
		case <-l.ctx.Done():
			l.drain()
			return
		case m := <-l.out:
			if m.close {
				_ = l.conn.Close("")
				l.drain()
				return
			}
			if m.done != nil && l.ch.st.Load() != stateOpen {
				l.finish(m, false)
				continue
			}
			if m.after != nil {
				select {
				case <-m.after.ch:
				case <-l.ctx.Done():
					l.finish(m, false)
					l.drain()
					return
				}
				if !m.after.written.Load() {
					l.finish(m, false)
					continue
				}
			}
			op := l.beginOperation()
			err := l.conn.Write(l.ctx, false, m.frame)
			if err != nil {
				l.endOperation(&op, l.ctx, err)
				op.MessageType = diagnosticMessage(m.frame)
				publishLegOperation(&l.diagnostics.write, op)
			}
			if err == nil && len(m.frame) > 0 && m.frame[0] == protocol.FrameFin {
				l.finWritten.Store(true)
			}
			l.finish(m, err == nil)
			if err != nil {
				l.closeNow("write_error")
				l.drain()
				return
			}
			l.activity.Add(1)
		}
	}
}

// pingLoop coordinates fixed Attach-time windows; the sole control worker
// writes independently. DATA owners and valid PONGs have distinct meanings.
func (l *leg) pingLoop() {
	defer l.h.loops.Done()
	defer l.workerEnded()
	for {
		op := l.beginOperation()
		before := l.control.Snapshot()
		err := l.control.Advance()
		if err == nil {
			err = l.control.Err()
		}
		if err != nil {
			l.endOperation(&op, l.ctx, err)
			op.MissesBefore, op.MissesAfter = before.Misses, l.control.Snapshot().Misses
			op.PingPhase = "control_write"
			source := "control_error"
			if errors.Is(err, wsio.ErrIdleWindows) || errors.Is(err, wsio.ErrProbeExhausted) {
				op.PingPhase, source = "idle_window", "ping_misses"
			}
			publishLegOperation(&l.diagnostics.ping, op)
			l.closeNow(source)
			return
		}
		next, ok := l.control.NextTick()
		if !ok {
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-l.ctx.Done():
			timer.Stop()
			return
		case <-l.control.Failed():
			timer.Stop()
		case <-timer.C:
		}
	}
}

// The channel already owns the cleanup deadline. Joining the new worker must
// not restart it, and only happens after the reader's transport cleanup.
func (l *leg) joinControl() {
	// Anchor before sampling the injected Hub clock: lock acquisition and
	// context construction consume this budget rather than shifting it later.
	anchor := time.Now()
	l.h.mu.Lock()
	remaining := max(0, l.ch.forceAt.Sub(l.h.now()))
	l.h.mu.Unlock()
	ctx, cancel := context.WithDeadline(context.Background(), anchor.Add(remaining))
	defer cancel()
	if err := l.control.Join(ctx); err != nil {
		l.h.log.Error("data control cleanup incomplete", "channel", l.ch.id, "role", l.role.String(), "error_class", wsio.ErrorClass(err))
	}
}

func (l *leg) readLoop() {
	for {
		op := l.beginOperation()
		text, data, err := l.conn.Read(l.ctx)
		if err != nil {
			l.endOperation(&op, l.ctx, err)
			op.ReadPhase = wsio.ReadPhase(err)
			publishLegOperation(&l.diagnostics.read, op)
			code := protocol.CodeChannelInterrupted
			if errors.Is(err, ErrTooBig) {
				code = protocol.CodePayloadTooLarge
			}
			l.h.legGone(l, code)
			return
		}
		l.activity.Add(1)
		if text {
			l.h.violation(l, protocol.CodeProtocolError)
			continue
		}
		f, err := protocol.DecodeFrame(data, l.h.lim.DataMax)
		switch {
		case err != nil:
			l.h.violation(l, protocol.CodeOf(err))
		case f.Type == protocol.FrameData:
			l.h.onData(l, f.Data)
		case f.Type == protocol.FrameFin:
			l.h.onFin(l)
		case f.Type == protocol.FrameAbort:
			l.h.onAbort(l, f)
		default: // READY is sent only by OIS
			l.h.violation(l, protocol.CodeProtocolError)
		}
	}
}

func (h *Hub) violation(l *leg, code protocol.Code) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminate(l.ch, code, h.now())
}

func (h *Hub) onData(l *leg, p []byte) {
	h.mu.Lock()
	ch := l.ch
	now := h.now()
	switch {
	case ch.state == statePreparing || (ch.state == stateOpen && ch.fin[l.role]):
		// DATA before READY or after FIN in the same direction.
		h.terminate(ch, protocol.CodeProtocolError, now)
		h.mu.Unlock()
		return
	case ch.state != stateOpen:
		h.mu.Unlock()
		return
	}
	peer := ch.legs[1-l.role]
	forwarding, err := l.control.BeginData(wsio.DataForwarding)
	if err != nil {
		h.terminate(ch, protocol.CodeChannelInterrupted, now)
		h.mu.Unlock()
		return
	}
	pending, err := peer.control.BeginData(wsio.DataPending)
	if err != nil {
		forwarding.End()
		h.terminate(ch, protocol.CodeChannelInterrupted, now)
		h.mu.Unlock()
		return
	}
	ch.lastData = now
	l.control.Data()
	m := outMsg{frame: protocol.DataFrame(p), done: make(chan struct{}), n: len(p), owner: pending}
	h.addInflight(int64(m.n))
	l.forwarding.Store(true)
	peer.enqueue(m)
	h.mu.Unlock()
	select {
	case <-m.done:
	case <-peer.done:
	}
	l.forwarding.Store(false)
	forwarding.End()
}

func (h *Hub) onFin(l *leg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := l.ch
	now := h.now()
	switch {
	case ch.state == statePreparing || (ch.state == stateOpen && ch.fin[l.role]):
		h.terminate(ch, protocol.CodeProtocolError, now)
		return
	case ch.state != stateOpen:
		return
	}
	ch.fin[l.role] = true
	ch.legs[1-l.role].enqueue(outMsg{frame: protocol.FinFrame()})
	if ch.fin[Consumer] && ch.fin[Provider] {
		ch.reason = protocol.CodeCompleted
		ch.forceAt = now.Add(limits.D(h.lim.CloseMS))
		h.finalize(ch, now)
		for _, x := range ch.legs {
			x.closeAfterQueue()
		}
	}
}

func (h *Hub) onAbort(l *leg, f protocol.Frame) {
	reason := f.Reason
	if reason == "" {
		reason = protocol.CodeProtocolError
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	l.peerAborted = true
	h.terminate(l.ch, reason, h.now())
}

// legGone handles a leg whose reader ended. A leg that disconnects while
// its channel is Preparing or Open interrupts the channel (B1 §5).
func (h *Hub) legGone(l *leg, code protocol.Code) {
	h.mu.Lock()
	l.dead = true
	close(l.done)
	l.cancel()
	l.drain()
	ch := l.ch
	now := h.now()
	if ch.state == stateClosing && ch.liveLegs() == 0 {
		h.finalize(ch, now)
	} else {
		h.terminate(ch, code, now)
	}
	h.mu.Unlock()
	l.closeNow("reader_cleanup")
}
