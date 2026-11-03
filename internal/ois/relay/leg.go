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
	h      *Hub
	ch     *channel
	role   Role
	conn   Conn
	out    chan outMsg
	done   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	dead        bool
	abortSent   bool
	peerAborted bool
	closeQueued bool

	activity   atomic.Int64
	forwarding atomic.Bool
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
		_ = l.conn.CloseNow()
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
	h.mu.Lock()
	ch := res.ch
	if ch.state != statePreparing {
		reason := ch.reason
		h.mu.Unlock()
		ctx, cancel := context.WithTimeout(h.ctx, limits.D(h.lim.CloseMS))
		_ = conn.Write(ctx, false, protocol.AbortFrame(reason, ""))
		cancel()
		_ = conn.CloseNow()
		return
	}
	ctx, cancel := context.WithCancel(h.ctx)
	l := &leg{h: h, ch: ch, role: res.role, conn: conn, out: make(chan outMsg, legQueue),
		done: make(chan struct{}), ctx: ctx, cancel: cancel}
	ch.legs[res.role] = l
	h.conns.Add(1)
	h.loops.Add(2)
	go l.writeLoop()
	go l.pingLoop()
	h.tryCommit(ch, h.now())
	h.mu.Unlock()
	l.readLoop()
	h.conns.Done()
}

func (l *leg) writeLoop() {
	defer l.h.loops.Done()
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
			err := l.conn.Write(l.ctx, false, m.frame)
			l.finish(m, err == nil)
			if err != nil {
				_ = l.conn.CloseNow()
				l.drain()
				return
			}
			l.activity.Add(1)
		}
	}
}

// pingLoop drops a leg that showed no sign of life for two PING periods.
// Frames read or written count as life, and so does a reader blocked by
// back-pressure, since pongs cannot be read while it waits.
func (l *leg) pingLoop() {
	defer l.h.loops.Done()
	interval := limits.D(l.h.lim.PingMS)
	misses := 0
	last := l.activity.Load()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-time.After(interval):
		}
		ctx, cancel := context.WithTimeout(l.ctx, interval)
		err := l.conn.Ping(ctx)
		cancel()
		cur := l.activity.Load()
		if err == nil || cur != last || l.forwarding.Load() {
			misses = 0
		} else {
			misses++
		}
		last = cur
		if misses >= 2 {
			_ = l.conn.CloseNow()
			return
		}
	}
}

func (l *leg) readLoop() {
	for {
		text, data, err := l.conn.Read(l.ctx)
		if err != nil {
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
	ch.lastData = now
	m := outMsg{frame: protocol.DataFrame(p), done: make(chan struct{}), n: len(p)}
	h.addInflight(int64(m.n))
	peer.enqueue(m)
	h.mu.Unlock()
	l.forwarding.Store(true)
	select {
	case <-m.done:
	case <-peer.done:
	}
	l.forwarding.Store(false)
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
	_ = l.conn.CloseNow()
}
