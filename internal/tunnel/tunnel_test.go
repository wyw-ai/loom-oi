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
	closedByOIS(leg)
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
		t.Fatal("leg not closed after OIS closed it")
	}
}

// closedByOIS ends the leg as OIS does once it forwarded both FINs.
func closedByOIS(leg *fakeLeg) { leg.in <- msg{err: io.EOF} }

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
	closedByOIS(leg)
	if r := wait(t, res); r.Code != protocol.CodeCompleted {
		t.Fatalf("%+v", r)
	}
}

// finWriteLeg separates delivery from publication of the FIN write result.
// A delivered write keeps its chosen result even if CloseNow races with the
// caller returning. A still-blocked write must instead be unblocked by close
// or cancellation, just like the WebSocket's underlying transport.
type finWriteLeg struct {
	*fakeLeg
	blockedUntilClose bool
	started           chan struct{}
	returnWrite       chan error
	readEOF           chan struct{}
	closeCalled       chan struct{}
	readOnce          sync.Once
	closeOnce         sync.Once
}

func newFINWriteLeg(blocked bool) *finWriteLeg {
	return &finWriteLeg{fakeLeg: newLeg(), blockedUntilClose: blocked,
		started: make(chan struct{}), returnWrite: make(chan error, 1),
		readEOF: make(chan struct{}), closeCalled: make(chan struct{})}
}

func (l *finWriteLeg) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	if len(p) == 0 || p[0] != protocol.FrameFin {
		return l.fakeLeg.Write(ctx, typ, p)
	}
	if !l.blockedUntilClose {
		if err := l.fakeLeg.Write(ctx, typ, p); err != nil {
			return err
		}
	}
	close(l.started)
	if l.blockedUntilClose {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.closed:
			return net.ErrClosed
		}
	}
	return <-l.returnWrite
}

func (l *finWriteLeg) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	typ, p, err := l.fakeLeg.Read(ctx)
	if err != nil {
		l.readOnce.Do(func() { close(l.readEOF) })
	}
	return typ, p, err
}

func (l *finWriteLeg) CloseNow() error {
	err := l.fakeLeg.CloseNow()
	l.closeOnce.Do(func() { close(l.closeCalled) })
	return err
}

func waitTunnelSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("missing %s", name)
	}
}

func TestFINWriteOutcomeAfterLegEOF(t *testing.T) {
	broken := errors.New("FIN write failed")
	cases := []struct {
		name             string
		writeErr         error
		blocked          bool
		noRemoteFIN      bool
		expiredAtEOF     bool
		expiredAfterEOF  bool
		expiredAtFinish  bool
		abortFirst       bool
		abortAfterFinish bool
		code             protocol.Code
	}{
		{name: "successful write not yet published", code: protocol.CodeCompleted},
		{name: "failed write", writeErr: broken, code: protocol.CodeChannelInterrupted},
		{name: "blocked write released by close", blocked: true, code: protocol.CodeChannelInterrupted},
		{name: "missing remote FIN", noRemoteFIN: true, code: protocol.CodeChannelInterrupted},
		{name: "license expired at EOF", expiredAtEOF: true, code: protocol.CodeLicenseExpired},
		{name: "license expired before write returns", expiredAfterEOF: true, code: protocol.CodeLicenseExpired},
		{name: "license expired at completion decision", expiredAtFinish: true, code: protocol.CodeLicenseExpired},
		{name: "Abort wins before write returns", abortFirst: true, code: protocol.CodeRevisionChanged},
		{name: "Completed wins before later Abort", abortAfterFinish: true, code: protocol.CodeCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leg := newFINWriteLeg(tc.blocked)
			t.Cleanup(func() {
				select {
				case leg.returnWrite <- net.ErrClosed:
				default:
				}
				_ = leg.CloseNow()
			})
			local, peer := pair(t)
			var expired atomic.Bool
			var checkCompletion atomic.Bool
			var completionChecks atomic.Int32
			b := New(context.Background(), leg, local, 64, grace, func() protocol.Code {
				if expired.Load() {
					return protocol.CodeLicenseExpired
				}
				// Allow the post-write check, then expire exactly at the
				// terminal decision inside once, without a wall-clock race.
				if checkCompletion.Load() && completionChecks.Add(1) > 1 {
					return protocol.CodeLicenseExpired
				}
				return ""
			})
			leg.push(protocol.ReadyFrame())
			if err := b.WaitReady(); err != nil {
				t.Fatal(err)
			}
			res := run(b)
			if _, err := peer.Write([]byte("hello world")); err != nil {
				t.Fatal(err)
			}
			_ = peer.CloseWrite()
			if f := leg.frame(t); f.Type != protocol.FrameData || string(f.Data) != "hello world" {
				t.Fatalf("unexpected DATA: %+v", f)
			}
			waitTunnelSignal(t, leg.started, "FIN write start")
			if !tc.blocked {
				if f := leg.frame(t); f.Type != protocol.FrameFin {
					t.Fatalf("unexpected FIN: %+v", f)
				}
			}
			leg.push(protocol.DataFrame([]byte("reply")))
			if !tc.noRemoteFIN {
				leg.push(protocol.FinFrame())
				if s := readAll(t, peer); s != "reply" {
					t.Fatal(s)
				}
			} else {
				_ = peer.SetReadDeadline(time.Now().Add(5 * time.Second))
				reply := make([]byte, 5)
				if _, err := io.ReadFull(peer, reply); err != nil || string(reply) != "reply" {
					t.Fatalf("reply=%q err=%v", reply, err)
				}
			}
			expired.Store(tc.expiredAtEOF)
			closedByOIS(leg.fakeLeg)
			waitTunnelSignal(t, leg.readEOF, "leg EOF read")
			waitTunnelSignal(t, leg.closeCalled, "transport close after EOF")
			if !tc.blocked && !tc.noRemoteFIN && !tc.expiredAtEOF {
				select {
				case <-b.Done():
					t.Fatalf("bridge ended before FIN write result was published: %+v", b.Result())
				default:
				}
			}
			if tc.expiredAfterEOF {
				expired.Store(true)
			}
			checkCompletion.Store(tc.expiredAtFinish)
			if tc.abortFirst {
				b.Abort(protocol.CodeRevisionChanged)
				waitTunnelSignal(t, b.Done(), "Abort terminal result")
			}
			leg.returnWrite <- tc.writeErr
			r := wait(t, res)
			if r.Code != tc.code || r.Remote || r.Up != 11 || r.Down != 5 || b.Buffered() != 0 {
				t.Fatalf("want %s, got %+v buffered=%d", tc.code, r, b.Buffered())
			}
			if tc.expiredAtFinish && completionChecks.Load() < 2 {
				t.Fatal("completion did not recheck the license after the write check")
			}
			if tc.abortAfterFinish {
				b.Abort(protocol.CodeRevisionChanged)
				if after := b.Result(); after != r {
					t.Fatalf("later Abort replaced terminal result: before=%+v after=%+v", r, after)
				}
			}
			waitTunnelSignal(t, b.Done(), "terminal result")
		})
	}
}

// After completion the bridge keeps draining until OIS closes the leg:
// bytes it wrote may still be in flight, so T_close does not cut the leg.
// Only a lapsed license ends the wait (B2 §5).
func TestCompletedLingersUntilOISCloses(t *testing.T) {
	var expired atomic.Bool
	allow := func() protocol.Code {
		if expired.Load() {
			return protocol.CodeLicenseExpired
		}
		return ""
	}
	complete := func() (*fakeLeg, chan Result) {
		leg := newLeg()
		local, peer := pair(t)
		b := New(context.Background(), leg, local, 64, grace, allow)
		leg.push(protocol.ReadyFrame())
		if err := b.WaitReady(); err != nil {
			t.Fatal(err)
		}
		res := run(b)
		_ = peer.CloseWrite()
		if f := leg.frame(t); f.Type != protocol.FrameFin {
			t.Fatalf("%+v", f)
		}
		leg.push(protocol.FinFrame())
		<-b.Done()
		leg.push(protocol.ReadyFrame()) // drained, not interpreted
		return leg, res
	}
	leg, res := complete()
	select {
	case <-res:
		t.Fatal("bridge ended before OIS closed the leg")
	case <-leg.closed:
		t.Fatal("leg cut before OIS closed it")
	case <-time.After(3 * grace):
	}
	closedByOIS(leg)
	if r := wait(t, res); r.Code != protocol.CodeCompleted {
		t.Fatalf("%+v", r)
	}
	<-leg.closed

	leg, res = complete()
	expired.Store(true)
	select {
	case <-leg.closed:
	case <-time.After(3 * grace):
		t.Fatal("leg kept after the license lapsed")
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

	// So does an ABORT that OIS sent while the process was paused past L
	// (TV-B2-11): the expiry stopped forwarding first.
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
	leg.push(protocol.AbortFrame(protocol.CodeAccessOffline, ""))
	if r := wait(t, res); r.Code != protocol.CodeLicenseExpired || r.Remote || r.Down != 0 {
		t.Fatalf("%+v", r)
	}
	expectReset(t, peer)
	<-leg.closed
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
