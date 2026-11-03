package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const grace = 200 * time.Millisecond

type msg struct {
	typ  websocket.MessageType
	data []byte
	err  error
}

type fakeLeg struct {
	in       chan msg
	out      chan []byte
	closed   chan struct{}
	once     sync.Once
	writeErr atomic.Pointer[error]
	graceful atomic.Bool
}

func newLeg() *fakeLeg {
	return &fakeLeg{in: make(chan msg, 16), out: make(chan []byte, 64), closed: make(chan struct{})}
}

func (l *fakeLeg) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case m := <-l.in:
		return m.typ, m.data, m.err
	case <-l.closed:
		return 0, nil, net.ErrClosed
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (l *fakeLeg) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	if e := l.writeErr.Load(); e != nil {
		return *e
	}
	select {
	case <-l.closed:
		return net.ErrClosed
	case l.out <- append([]byte(nil), p...):
		return nil
	}
}

func (l *fakeLeg) Close(websocket.StatusCode, string) error {
	l.graceful.Store(true)
	return l.CloseNow()
}

func (l *fakeLeg) CloseNow() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *fakeLeg) push(b []byte) { l.in <- msg{typ: websocket.MessageBinary, data: b} }

func (l *fakeLeg) frame(t *testing.T) protocol.Frame {
	t.Helper()
	select {
	case b := <-l.out:
		f, err := protocol.DecodeFrame(b, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("no frame written")
		return protocol.Frame{}
	}
}

func (l *fakeLeg) expectAbort(t *testing.T, code protocol.Code) {
	t.Helper()
	if f := l.frame(t); f.Type != protocol.FrameAbort || f.Reason != code {
		t.Fatalf("want ABORT(%s), got %d %s", code, f.Type, f.Reason)
	}
}

// pair returns the bridged end (as *net.TCPConn) and the peer end.
func pair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s := <-accepted
	t.Cleanup(func() { _ = c.Close(); _ = s.Close() })
	return c.(*net.TCPConn), s.(*net.TCPConn)
}

// expectReset reads until the peer sees a reset rather than a clean EOF.
func expectReset(t *testing.T, peer net.Conn) {
	t.Helper()
	_ = peer.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 1024)
	for {
		_, err := peer.Read(buf)
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			// A FIN arrived first (half-close); the reset must still follow.
			expectWriteReset(t, peer)
			return
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("peer saw neither EOF nor reset")
		}
		return
	}
}

// expectWriteReset writes until the reset surfaces as a write error.
func expectWriteReset(t *testing.T, peer net.Conn) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := peer.Write([]byte("x")); err != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("peer never saw a reset after EOF")
}

func readAll(t *testing.T, peer net.Conn) string {
	t.Helper()
	_ = peer.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, err := io.ReadAll(peer)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func run(b *Bridge) chan Result {
	ch := make(chan Result, 1)
	go func() { ch <- b.Run() }()
	return ch
}

func wait(t *testing.T, ch chan Result) Result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("bridge did not end")
		return Result{}
	}
}

func TestCompletedExchange(t *testing.T) {
	leg := newLeg()
	local, peer := pair(t)
	b := New(context.Background(), leg, local, 8, grace, nil)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	res := run(b)
	if _, err := peer.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	_ = peer.CloseWrite()
	var got strings.Builder
	for {
		f := leg.frame(t)
		if f.Type == protocol.FrameFin {
			break
		}
		if len(f.Data) > 8 {
			t.Fatalf("DATA of %d bytes exceeds F", len(f.Data))
		}
		got.Write(f.Data)
	}
	if got.String() != "hello world" {
		t.Fatal(got.String())
	}
	leg.push(protocol.DataFrame([]byte("reply")))
	leg.push(protocol.FinFrame())
	if s := readAll(t, peer); s != "reply" {
		t.Fatal(s)
	}
	r := wait(t, res)
	if r.Code != protocol.CodeCompleted || r.Remote || r.Up != 11 || r.Down != 5 || b.Buffered() != 0 {
		t.Fatalf("%+v", r)
	}
	select {
	case <-b.Done():
	default:
		t.Fatal("done not closed")
	}
	select {
	case <-leg.closed:
	case <-time.After(time.Second):
		t.Fatal("leg not closed after the grace period")
	}
}

// The remote FIN may arrive before the local EOF.
func TestCompletedRemoteFinFirst(t *testing.T) {
	leg := newLeg()
	local, peer := pair(t)
	b := New(context.Background(), leg, local, 64, grace, nil)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	res := run(b)
	leg.push(protocol.FinFrame())
	if s := readAll(t, peer); s != "" {
		t.Fatal(s)
	}
	_ = peer.CloseWrite()
	if f := leg.frame(t); f.Type != protocol.FrameFin {
		t.Fatalf("%+v", f)
	}
	if r := wait(t, res); r.Code != protocol.CodeCompleted {
		t.Fatalf("%+v", r)
	}
}

func TestWaitReady(t *testing.T) {
	cases := []struct {
		name   string
		first  msg
		code   protocol.Code
		remote bool
		abort  bool
	}{
		{"abort", msg{typ: websocket.MessageBinary, data: protocol.AbortFrame(protocol.CodeRevoked, "")}, protocol.CodeRevoked, true, false},
		{"abort unknown", msg{typ: websocket.MessageBinary, data: []byte{protocol.FrameAbort, 9, 9}}, protocol.CodeProtocolError, true, false},
		{"data first", msg{typ: websocket.MessageBinary, data: protocol.DataFrame([]byte("x"))}, protocol.CodeProtocolError, false, true},
		{"text", msg{typ: websocket.MessageText, data: []byte("{}")}, protocol.CodeProtocolError, false, true},
		{"garbage", msg{typ: websocket.MessageBinary, data: []byte{0x77}}, protocol.CodeProtocolError, false, true},
		{"closed", msg{err: errors.New("eof")}, protocol.CodeChannelInterrupted, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			leg := newLeg()
			local, peer := pair(t)
			b := New(context.Background(), leg, local, 64, grace, nil)
			leg.in <- c.first
			err := b.WaitReady()
			if protocol.CodeOf(err) != c.code {
				t.Fatalf("%v", err)
			}
			if r := b.Result(); r.Code != c.code || r.Remote != c.remote {
				t.Fatalf("%+v", r)
			}
			if c.abort {
				leg.expectAbort(t, c.code)
			}
			expectReset(t, peer)
		})
	}
}

func TestRunFailures(t *testing.T) {
	type tc struct {
		name   string
		act    func(t *testing.T, leg *fakeLeg, peer *net.TCPConn)
		code   protocol.Code
		remote bool
		abort  bool
	}
	cases := []tc{
		{"remote abort", func(t *testing.T, leg *fakeLeg, _ *net.TCPConn) {
			leg.push(protocol.AbortFrame(protocol.CodeChannelInterrupted, ""))
		}, protocol.CodeChannelInterrupted, true, false},
		{"second ready", func(t *testing.T, leg *fakeLeg, _ *net.TCPConn) { leg.push(protocol.ReadyFrame()) }, protocol.CodeProtocolError, false, true},
		{"data after fin", func(t *testing.T, leg *fakeLeg, _ *net.TCPConn) {
			leg.push(protocol.FinFrame())
			leg.push(protocol.DataFrame([]byte("late")))
		}, protocol.CodeProtocolError, false, true},
		{"text", func(t *testing.T, leg *fakeLeg, _ *net.TCPConn) {
			leg.in <- msg{typ: websocket.MessageText, data: []byte("x")}
		}, protocol.CodeProtocolError, false, true},
		{"oversize", func(t *testing.T, leg *fakeLeg, _ *net.TCPConn) {
			leg.push(protocol.DataFrame(make([]byte, 65)))
		}, protocol.CodePayloadTooLarge, false, true},
		{"leg lost", func(t *testing.T, leg *fakeLeg, _ *net.TCPConn) { _ = leg.CloseNow() }, protocol.CodeChannelInterrupted, false, false},
		{"local reset", func(t *testing.T, _ *fakeLeg, peer *net.TCPConn) {
			_ = peer.SetLinger(0)
			_ = peer.Close()
		}, protocol.CodeChannelInterrupted, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			leg := newLeg()
			local, peer := pair(t)
			b := New(context.Background(), leg, local, 64, grace, nil)
			leg.push(protocol.ReadyFrame())
			if err := b.WaitReady(); err != nil {
				t.Fatal(err)
			}
			res := run(b)
			c.act(t, leg, peer)
			r := wait(t, res)
			if r.Code != c.code || r.Remote != c.remote {
				t.Fatalf("%+v", r)
			}
			if c.abort {
				leg.expectAbort(t, c.code)
			}
			if c.name != "local reset" {
				expectReset(t, peer)
			}
		})
	}
}

// B2 §5: once the license check fails nothing more is forwarded.
func TestAllowStopsForwarding(t *testing.T) {
	var expired atomic.Bool
	allow := func() protocol.Code {
		if expired.Load() {
			return protocol.CodeLicenseExpired
		}
		return ""
	}
	leg := newLeg()
	local, peer := pair(t)
	b := New(context.Background(), leg, local, 64, grace, allow)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	res := run(b)
	expired.Store(true)
	leg.push(protocol.DataFrame([]byte("after expiry")))
	if r := wait(t, res); r.Code != protocol.CodeLicenseExpired || r.Down != 0 {
		t.Fatalf("%+v", r)
	}
	leg.expectAbort(t, protocol.CodeLicenseExpired)
	expectReset(t, peer)

	leg = newLeg()
	local, peer = pair(t)
	expired.Store(false)
	b = New(context.Background(), leg, local, 64, grace, allow)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	res = run(b)
	expired.Store(true)
	_, _ = peer.Write([]byte("up"))
	if r := wait(t, res); r.Code != protocol.CodeLicenseExpired || r.Up != 0 {
		t.Fatalf("%+v", r)
	}
	leg.expectAbort(t, protocol.CodeLicenseExpired)

	// A leg lost after expiry reports the expiry.
	leg = newLeg()
	local, _ = pair(t)
	expired.Store(false)
	b = New(context.Background(), leg, local, 64, grace, allow)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	res = run(b)
	expired.Store(true)
	_ = leg.CloseNow()
	if r := wait(t, res); r.Code != protocol.CodeLicenseExpired {
		t.Fatalf("%+v", r)
	}
}

func TestWriteFailures(t *testing.T) {
	// The leg cannot take DATA.
	leg := newLeg()
	local, peer := pair(t)
	b := New(context.Background(), leg, local, 64, grace, nil)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	broken := errors.New("broken")
	leg.writeErr.Store(&broken)
	res := run(b)
	_, _ = peer.Write([]byte("x"))
	if r := wait(t, res); r.Code != protocol.CodeChannelInterrupted {
		t.Fatalf("%+v", r)
	}
	// The leg cannot take FIN.
	leg = newLeg()
	local, peer = pair(t)
	b = New(context.Background(), leg, local, 64, grace, nil)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	leg.writeErr.Store(&broken)
	res = run(b)
	_ = peer.CloseWrite()
	if r := wait(t, res); r.Code != protocol.CodeChannelInterrupted {
		t.Fatalf("%+v", r)
	}
	// The local connection cannot take DATA.
	leg = newLeg()
	local, _ = pair(t)
	b = New(context.Background(), leg, local, 64, grace, nil)
	leg.push(protocol.ReadyFrame())
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	_ = local.CloseWrite()
	res = run(b)
	leg.push(protocol.DataFrame([]byte("x")))
	if r := wait(t, res); r.Code != protocol.CodeChannelInterrupted {
		t.Fatalf("%+v", r)
	}
}

func TestExternalAbort(t *testing.T) {
	leg := newLeg()
	local, peer := pair(t)
	b := New(context.Background(), leg, local, 64, grace, nil)
	go b.Abort(protocol.CodeRevisionChanged)
	if err := b.WaitReady(); protocol.CodeOf(err) != protocol.CodeRevisionChanged {
		t.Fatalf("%v", err)
	}
	leg.expectAbort(t, protocol.CodeRevisionChanged)
	expectReset(t, peer)
}
