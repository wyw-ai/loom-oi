package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/tunnel"
	"github.com/wyw-ai/loom-oi/internal/wsio"
)

// AbortError reports a channel that ended without both FINs. The business
// outcome is unknown and must not be inferred from it (B1 §7).
type AbortError struct {
	Code   protocol.Code
	Detail string
}

func (e *AbortError) Error() string {
	if e.Detail == "" {
		return "channel aborted: " + string(e.Code)
	}
	return "channel aborted: " + string(e.Code) + ": " + e.Detail
}

// Stream is an open channel used as a byte stream. Read returns io.EOF
// after the peer's FIN and *AbortError after ABORT; CloseWrite sends FIN.
type Stream struct {
	leg        *websocket.Conn
	id         string
	dataMax    int
	closeGrace time.Duration

	readMu  sync.Mutex
	pending []byte
	readErr error
	reader  *wsio.Reader

	writeMu sync.Mutex
	finSent bool

	finRecv atomic.Bool
	ended   atomic.Bool
}

// ChannelID identifies the stream's channel.
func (s *Stream) ChannelID() string { return s.id }

// waitReady reads the first frame: READY opens the stream (B1 §5).
func waitReady(ctx context.Context, leg *websocket.Conn, dataMax int) error {
	typ, msg, err := leg.Read(ctx)
	if err != nil {
		return &AbortError{Code: protocol.CodeChannelInterrupted, Detail: err.Error()}
	}
	f, ferr := protocol.DecodeFrame(msg, max(dataMax, 1<<20))
	switch {
	case typ == websocket.MessageBinary && ferr == nil && f.Type == protocol.FrameReady:
		return nil
	case typ == websocket.MessageBinary && ferr == nil && f.Type == protocol.FrameAbort && f.Reason != "":
		return &AbortError{Code: f.Reason, Detail: f.Detail}
	}
	_ = leg.Write(ctx, websocket.MessageBinary, protocol.AbortFrame(protocol.CodeProtocolError, "expected READY"))
	return &AbortError{Code: protocol.CodeProtocolError, Detail: "the first frame was not READY"}
}

// Attach attaches the consumer leg and waits for READY.
func (c *Client) Attach(ctx context.Context, o protocol.OpenResponse) (*Stream, error) {
	leg, err := c.AttachLeg(ctx, o)
	if err != nil {
		return nil, err
	}
	if err := waitReady(ctx, leg, c.dataMax); err != nil {
		_ = leg.CloseNow()
		return nil, err
	}
	return &Stream{leg: leg, id: o.ChannelID, dataMax: c.dataMax, closeGrace: c.closeGrace}, nil
}

// Dial opens exposureID at its current revision and attaches.
func (c *Client) Dial(ctx context.Context, exposureID string) (*Stream, error) {
	o, err := c.OpenLatest(ctx, exposureID, "", false)
	if err != nil {
		return nil, err
	}
	return c.Attach(ctx, o)
}

// Read returns DATA bytes in order.
func (s *Stream) Read(p []byte) (int, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	for len(s.pending) == 0 {
		if s.readErr != nil {
			return 0, s.readErr
		}
		s.readErr = s.next()
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

// next reads one frame; it returns the sticky error once the stream has
// no more data.
func (s *Stream) next() error {
	if s.reader == nil {
		// Match AttachLeg's existing receive limit. DataMax limits uploads;
		// receiving larger configured peer frames remains supported.
		s.reader = wsio.NewReader(s.dataMax+2, max(s.dataMax, 1<<20)+3+protocol.MaxAbortDetail)
	}
	typ, msg, err := s.reader.Read(context.Background(), s.leg)
	if err != nil {
		if s.finRecv.Load() {
			return io.EOF
		}
		return &AbortError{Code: protocol.CodeChannelInterrupted, Detail: err.Error()}
	}
	f, ferr := protocol.DecodeFrame(msg, max(s.dataMax, 1<<20))
	switch {
	case typ != websocket.MessageBinary || ferr != nil || f.Type == protocol.FrameReady || s.finRecv.Load():
		s.Abort(protocol.CodeProtocolError)
		return &AbortError{Code: protocol.CodeProtocolError, Detail: "unexpected frame from OIS"}
	case f.Type == protocol.FrameAbort:
		s.ended.Store(true)
		code := f.Reason
		if code == "" {
			code = protocol.CodeProtocolError
		}
		return &AbortError{Code: code, Detail: f.Detail}
	case f.Type == protocol.FrameFin:
		s.finRecv.Store(true)
		return io.EOF
	}
	s.pending = f.Data
	return nil
}

var errWriteClosed = errors.New("write after CloseWrite or abort")

// Write sends p as DATA frames of at most F bytes.
func (s *Stream) Write(p []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.finSent || s.ended.Load() {
		return 0, errWriteClosed
	}
	n := 0
	for len(p) > 0 {
		k := min(len(p), s.dataMax)
		if err := s.leg.Write(context.Background(), websocket.MessageBinary, protocol.DataFrame(p[:k])); err != nil {
			return n, err
		}
		n += k
		p = p[k:]
	}
	return n, nil
}

// CloseWrite sends FIN: no more DATA in this direction.
func (s *Stream) CloseWrite() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.finSent || s.ended.Load() {
		return errWriteClosed
	}
	s.finSent = true
	return s.leg.Write(context.Background(), websocket.MessageBinary, protocol.FinFrame())
}

// Abort ends the channel with code (ABORT, B1 §5).
func (s *Stream) Abort(code protocol.Code) {
	if s.ended.Swap(true) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.leg.Write(ctx, websocket.MessageBinary, protocol.AbortFrame(code, ""))
}

// Close releases the stream. A stream not finished in both directions is
// aborted with ClosedByClient first, and Close waits up to T_close for OIS
// to close the leg so the ABORT is not lost to a reset. A completed stream
// waits until OIS closes the leg, which it does once it has forwarded both
// FINs: bytes written may still be in flight, and cutting the leg could
// truncate them.
func (s *Stream) Close() error {
	s.writeMu.Lock()
	complete := s.finSent && s.finRecv.Load()
	s.writeMu.Unlock()
	aborted := s.ended.Load()
	if !complete {
		s.Abort(protocol.CodeClosedByClient)
	}
	if !aborted && s.readMu.TryLock() {
		ctx, cancel := context.Background(), context.CancelFunc(func() {})
		if !complete {
			ctx, cancel = context.WithTimeout(ctx, s.closeGrace)
		}
		for {
			if _, _, err := s.leg.Read(ctx); err != nil {
				break
			}
		}
		cancel()
		s.readMu.Unlock()
	}
	return s.leg.CloseNow()
}

// Forward relays one local TCP connection over a new channel with the
// tunnel bridge: nothing is read locally before READY, FIN half-closes
// and ABORT resets the local connection (B1 §5).
func (c *Client) Forward(ctx context.Context, local *net.TCPConn, exposureID string) (tunnel.Result, error) {
	o, err := c.OpenLatest(ctx, exposureID, "", false)
	if err != nil {
		_ = local.Close()
		return tunnel.Result{}, err
	}
	leg, err := c.AttachLeg(ctx, o)
	if err != nil {
		_ = local.SetLinger(0)
		_ = local.Close()
		return tunnel.Result{}, err
	}
	return forwardBridge(ctx, leg, local, c.dataMax, c.closeGrace)
}

// forwardBridge keeps the reader alive while a caller cancellation sends
// ABORT. Passing ctx directly to Bridge would instead race the ABORT with a
// read failure and could replace ClosedByClient with ChannelInterrupted.
func forwardBridge(ctx context.Context, leg tunnel.Leg, local tunnel.Conn, dataMax int, closeGrace time.Duration) (tunnel.Result, error) {
	b := tunnel.New(context.Background(), leg, local, dataMax, closeGrace, nil)
	finished, cancelled := make(chan struct{}), make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(cancelled)
		// The deadline starts when cancellation is observed, before an ABORT
		// can block behind a DATA write. It also releases a completed bridge
		// whose peer has not closed the leg after both FINs.
		forced := make(chan struct{})
		timer := time.AfterFunc(closeGrace, func() {
			_ = leg.CloseNow()
			close(forced)
		})
		b.Abort(protocol.CodeClosedByClient)
		select {
		case <-finished:
			if !timer.Stop() {
				<-forced
			}
		case <-forced:
		}
	})
	defer func() {
		if !stop() {
			// Run can also return after a local write failure. The ABORT
			// still needs its closing grace; drain until the peer closes or
			// the already-running cancellation deadline closes the leg.
			for {
				if _, _, err := leg.Read(context.Background()); err != nil {
					break
				}
			}
			close(finished)
			<-cancelled
		} else {
			close(finished)
		}
	}()
	if ctx.Err() != nil {
		// An already-cancelled caller must not start local I/O before
		// AfterFunc gets scheduled. Only drain the leg after ABORT, bounded
		// by the cancellation deadline, so the peer can receive the reason.
		<-b.Done()
		return b.Result(), protocol.Errorf(protocol.CodeClosedByClient, "forwarding cancelled before READY")
	}
	if err := b.WaitReady(); err != nil {
		return b.Result(), err
	}
	return b.Run(), nil
}

// Listen accepts loopback connections on addr and forwards each over its
// own channel until ctx ends (D4-f: explicit loopback TCP forwarding).
func (c *Client) Listen(ctx context.Context, addr, exposureID string, ready func(net.Addr), report func(tunnel.Result, error)) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("--listen accepts only loopback addresses, got %q", host)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	ready(ln.Addr())
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			report(c.Forward(ctx, conn.(*net.TCPConn), exposureID))
		}()
	}
}
