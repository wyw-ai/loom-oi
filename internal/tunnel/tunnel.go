// Package tunnel relays bytes between a local TCP connection and an
// oi.tunnel.v1 leg for the endpoints other than OIS: the OIC with its
// target and the oi client with its local connection (B1 §5). Nothing is
// read from or written to the local connection before READY; FIN maps to
// a half-close and ABORT to a reset (SO_LINGER=0), so a truncated stream
// never looks like a normal end.
package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// abortWriteTimeout bounds the best-effort ABORT write.
const abortWriteTimeout = 2 * time.Second

// Leg is the WebSocket of a tunnel leg; *websocket.Conn implements it.
type Leg interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	Close(code websocket.StatusCode, reason string) error
	CloseNow() error
}

// Conn is the local side: a TCP connection that can half-close and reset.
type Conn interface {
	net.Conn
	CloseWrite() error
	SetLinger(sec int) error
}

// Result says how a bridged channel ended.
type Result struct {
	// Code is Completed or the ABORT reason.
	Code protocol.Code
	// Remote is set when the reason arrived from the leg.
	Remote bool
	// Up counts bytes sent to the leg; Down counts bytes written locally.
	Up, Down int64
}

// Bridge relays one channel.
type Bridge struct {
	leg        Leg
	conn       Conn
	dataMax    int
	closeGrace time.Duration
	allow      func() protocol.Code

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}
	result Result

	finSent, finRecv atomic.Bool
	up, down         atomic.Int64
	buffered         atomic.Int64
}

// New prepares a bridge. closeGrace bounds the closing handshake of the leg
// (T_close). allow, when set, is called before each forward and a
// non-empty code aborts the channel with it (the OIC license, B2 §5).
func New(ctx context.Context, leg Leg, conn Conn, dataMax int, closeGrace time.Duration, allow func() protocol.Code) *Bridge {
	if allow == nil {
		allow = func() protocol.Code { return "" }
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Bridge{leg: leg, conn: conn, dataMax: dataMax, closeGrace: closeGrace, allow: allow, ctx: ctx, cancel: cancel,
		done: make(chan struct{})}
}

// Done is closed once the channel has ended.
func (b *Bridge) Done() <-chan struct{} { return b.done }

// Buffered is the number of bytes currently held in flight.
func (b *Bridge) Buffered() int64 { return b.buffered.Load() }

// Result reports how the channel ended; valid once Done is closed.
func (b *Bridge) Result() Result {
	r := b.result
	r.Up, r.Down = b.up.Load(), b.down.Load()
	return r
}

// Abort ends the channel: ABORT(code) on the leg and a reset locally.
func (b *Bridge) Abort(code protocol.Code) { b.finish(code, false, true) }

func (b *Bridge) finish(code protocol.Code, remote, sendAbort bool) {
	b.once.Do(func() {
		b.result = Result{Code: code, Remote: remote}
		if code == protocol.CodeCompleted {
			_ = b.conn.Close()
		} else {
			if sendAbort {
				ctx, cancel := context.WithTimeout(context.Background(), abortWriteTimeout)
				_ = b.leg.Write(ctx, websocket.MessageBinary, protocol.AbortFrame(code, ""))
				cancel()
			}
			_ = b.conn.SetLinger(0)
			_ = b.conn.Close()
		}
		// OIS closes the leg after ABORT or completion, so the peer sees
		// everything already sent; an unresponsive peer is dropped after T_close.
		// (A graceful Close here would wait for the blocked reader instead.)
		time.AfterFunc(b.closeGrace, func() { _ = b.leg.CloseNow() })
		close(b.done)
	})
}

var errNotReady = protocol.Errorf(protocol.CodeProtocolError, "the first tunnel frame was not READY")

// WaitReady waits for READY. Any other first frame ends the channel and
// is returned as a coded error.
func (b *Bridge) WaitReady() error {
	typ, msg, err := b.leg.Read(b.ctx)
	if err != nil {
		b.finish(protocol.CodeChannelInterrupted, false, false)
		b.cancel()
		return protocol.Errorf(b.result.Code, "leg closed before READY: %v", err)
	}
	f, ferr := protocol.DecodeFrame(msg, b.dataMax)
	switch {
	case typ == websocket.MessageBinary && ferr == nil && f.Type == protocol.FrameReady:
		return nil
	case typ == websocket.MessageBinary && ferr == nil && f.Type == protocol.FrameAbort:
		b.finish(reason(f), true, false)
	default:
		b.finish(protocol.CodeProtocolError, false, true)
	}
	b.cancel()
	return protocol.Errorf(b.result.Code, "channel ended before READY")
}

func reason(f protocol.Frame) protocol.Code {
	if f.Reason == "" {
		return protocol.CodeProtocolError
	}
	return f.Reason
}

// Run relays after READY until the channel ends.
func (b *Bridge) Run() Result {
	go b.uplink()
	b.downlink()
	<-b.done
	b.cancel()
	return b.Result()
}

// downlink writes leg DATA to conn, half-closes on FIN, resets on ABORT.
func (b *Bridge) downlink() {
	for {
		typ, msg, err := b.leg.Read(b.ctx)
		select {
		case <-b.done:
			// Ended elsewhere: stop reading so the closing handshake can.
			return
		default:
		}
		if err != nil {
			switch code := b.allow(); {
			case code != "":
				// E.g. after a process pause: the expiry, not the lost leg,
				// is why the channel ends (B2 §5).
				b.finish(code, false, false)
			case b.finSent.Load() && b.finRecv.Load():
				b.finish(protocol.CodeCompleted, false, false)
			default:
				b.finish(protocol.CodeChannelInterrupted, false, false)
			}
			return
		}
		if typ != websocket.MessageBinary {
			b.finish(protocol.CodeProtocolError, false, true)
			return
		}
		f, err := protocol.DecodeFrame(msg, b.dataMax)
		if err != nil {
			b.finish(protocol.CodeOf(err), false, true)
			return
		}
		switch {
		case f.Type == protocol.FrameAbort:
			b.finish(reason(f), true, false)
			return
		case f.Type == protocol.FrameReady || b.finRecv.Load():
			// A second READY, or DATA/FIN after FIN.
			b.finish(protocol.CodeProtocolError, false, true)
			return
		case f.Type == protocol.FrameFin:
			b.finRecv.Store(true)
			_ = b.conn.CloseWrite()
			if b.finSent.Load() {
				b.finish(protocol.CodeCompleted, false, false)
				return
			}
		default:
			if code := b.allow(); code != "" {
				b.finish(code, false, true)
				return
			}
			n := int64(len(f.Data))
			b.buffered.Add(n)
			_, err := b.conn.Write(f.Data)
			b.buffered.Add(-n)
			if err != nil {
				b.finish(protocol.CodeChannelInterrupted, false, true)
				return
			}
			b.down.Add(n)
		}
	}
}

// uplink sends conn bytes as DATA (at most F each) and FIN at EOF; a read
// error such as a reset ends the channel with ChannelInterrupted.
func (b *Bridge) uplink() {
	buf := make([]byte, b.dataMax)
	for {
		n, err := b.conn.Read(buf)
		if n > 0 {
			if code := b.allow(); code != "" {
				b.finish(code, false, true)
				return
			}
			b.buffered.Add(int64(n))
			werr := b.leg.Write(b.ctx, websocket.MessageBinary, protocol.DataFrame(buf[:n]))
			b.buffered.Add(-int64(n))
			if werr != nil {
				b.finish(protocol.CodeChannelInterrupted, false, false)
				return
			}
			b.up.Add(int64(n))
		}
		switch {
		case errors.Is(err, io.EOF):
			if werr := b.leg.Write(b.ctx, websocket.MessageBinary, protocol.FinFrame()); werr != nil {
				b.finish(protocol.CodeChannelInterrupted, false, false)
				return
			}
			b.finSent.Store(true)
			if b.finRecv.Load() {
				b.finish(protocol.CodeCompleted, false, false)
			}
			return
		case err != nil:
			b.finish(protocol.CodeChannelInterrupted, false, true)
			return
		}
	}
}
