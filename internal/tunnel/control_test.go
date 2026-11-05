package tunnel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/wsio"
)

var errBridgeControlSocket = errors.New("ordinary control Flush failure")

type bridgeControlSocket struct {
	net.Conn
	armed                    atomic.Bool
	entered, release, closed chan struct{}
	closeOnce, releaseOnce   sync.Once
}

func (c *bridgeControlSocket) Write(b []byte) (int, error) {
	if len(b) > 0 && b[0]&15 == 10 && c.armed.CompareAndSwap(true, false) {
		close(c.entered)
		select {
		case <-c.release:
			return 0, errBridgeControlSocket
		case <-c.closed:
			return 0, net.ErrClosed
		}
	}
	return c.Conn.Write(b)
}
func (c *bridgeControlSocket) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}
func (c *bridgeControlSocket) free() { c.releaseOnce.Do(func() { close(c.release) }) }

type bridgeControlPair struct {
	ctx    context.Context
	leg    *wsio.ManagedConn
	peer   *websocket.Conn
	socket *bridgeControlSocket
	wg     sync.WaitGroup
}

func bridgeControlFixture(t *testing.T) *bridgeControlPair {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	p := &bridgeControlPair{ctx: ctx}
	accepted, sockets := make(chan *websocket.Conn, 1), make(chan *bridgeControlSocket, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err == nil {
			accepted <- c
		}
	}))
	tr := &http.Transport{DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, n, a)
		if err != nil {
			return nil, err
		}
		s := &bridgeControlSocket{Conn: c, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		sockets <- s
		return s, nil
	}}
	t.Cleanup(func() {
		if p.socket != nil {
			p.socket.free()
			_ = p.socket.Close()
		}
		if p.leg != nil {
			_ = p.leg.CloseNow()
		}
		if p.peer != nil {
			_ = p.peer.CloseNow()
		}
		cancel()
		tr.CloseIdleConnections()
		srv.Close()
		done := make(chan struct{})
		go func() { p.wg.Wait(); close(done) }()
		deadline, _ := ctx.Deadline()
		cleanup, stop := context.WithDeadline(context.Background(), deadline)
		defer stop()
		select {
		case <-done:
		case <-cleanup.Done():
			t.Error("control bridge workers failed original watchdog")
		}
	})
	var err error
	p.leg, _, err = wsio.DialData(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case p.peer = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case p.socket = <-sockets:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return p
}
func (p *bridgeControlPair) run(b *Bridge) <-chan Result {
	result := make(chan Result, 1)
	p.wg.Add(1)
	go func() { defer p.wg.Done(); result <- b.Run() }()
	return result
}
func controlSignal(t *testing.T, ctx context.Context, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
func controlResult(t *testing.T, p *bridgeControlPair, ch <-chan Result) Result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	return Result{}
}
func controlFrame(t *testing.T, p *bridgeControlPair) protocol.Frame {
	t.Helper()
	_, raw, err := p.peer.Read(p.ctx)
	if err != nil {
		t.Fatal(err)
	}
	f, err := protocol.DecodeFrame(raw, 32768)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

type blockedControlLocal struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
	reset   atomic.Bool
}

func (c *blockedControlLocal) Write(b []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(b)
}
func (c *blockedControlLocal) CloseWrite() error     { return nil }
func (c *blockedControlLocal) SetLinger(n int) error { c.reset.Store(n == 0); return nil }

type abortWitnessLeg struct {
	*wsio.ManagedConn
	entered chan struct{}
	once    sync.Once
}

func (l *abortWitnessLeg) Write(ctx context.Context, typ websocket.MessageType, b []byte) error {
	if f, err := protocol.DecodeFrame(b, 32768); err == nil && f.Type == protocol.FrameAbort {
		l.once.Do(func() { close(l.entered) })
	}
	return l.ManagedConn.Write(ctx, typ, b)
}

func TestBridgeManagedControlFailure(t *testing.T) {
	for _, scenario := range []string{"blocked_local_write", "license_first", "earlier_abort"} {
		t.Run(scenario, func(t *testing.T) {
			p := bridgeControlFixture(t)
			local, unread := net.Pipe()
			defer unread.Close()
			defer local.Close()
			conn := &blockedControlLocal{Conn: local, entered: make(chan struct{})}
			var denied atomic.Bool
			leg := &abortWitnessLeg{ManagedConn: p.leg, entered: make(chan struct{})}
			b := New(p.ctx, leg, conn, 32768, time.Second, func() protocol.Code {
				if denied.Load() {
					return protocol.CodeLicenseExpired
				}
				return ""
			})
			if err := p.peer.Write(p.ctx, websocket.MessageBinary, protocol.ReadyFrame()); err != nil {
				t.Fatal(err)
			}
			if err := b.WaitReady(); err != nil {
				t.Fatal(err)
			}
			result := p.run(b)
			p.socket.armed.Store(true)
			if err := p.peer.WritePing(p.ctx, []byte("ordinary-error")); err != nil {
				t.Fatal(err)
			}
			controlSignal(t, p.ctx, p.socket.entered)
			if err := p.peer.Write(p.ctx, websocket.MessageBinary, protocol.DataFrame([]byte("blocked"))); err != nil {
				t.Fatal(err)
			}
			controlSignal(t, p.ctx, conn.entered)
			if b.Buffered() != 7 {
				t.Fatal("original DATA is not blocked in local write")
			}
			want := protocol.CodeChannelInterrupted
			if scenario == "license_first" {
				denied.Store(true)
				want = protocol.CodeLicenseExpired
			}
			if scenario == "earlier_abort" {
				want = protocol.CodeRevoked
				p.wg.Add(1)
				go func() { defer p.wg.Done(); b.Abort(protocol.CodeRevoked) }()
				controlSignal(t, p.ctx, leg.entered)
			}
			if p.ctx.Err() != nil {
				t.Fatal("ordinary Flush error must not come from ctx cancellation")
			}
			p.socket.free()
			r := controlResult(t, p, result)
			if r.Code != want || r.Remote || r.Down != 0 || !conn.reset.Load() {
				t.Fatalf("first cause/local blocked Write: %+v", r)
			}
			if !errors.Is(p.leg.ControlError(), errBridgeControlSocket) {
				t.Fatal("actual control error was lost", p.leg.ControlError())
			}
			if err := p.leg.JoinCleanup(); err != nil {
				t.Fatal(err)
			}
			if !p.leg.ControlSnapshot().WorkerDone {
				t.Fatal("control writer not joined")
			}
			if scenario == "blocked_local_write" && b.Diagnostics().Terminal.Operation != "control_write" {
				t.Fatal("wrong diagnostic first cause", b.Diagnostics().Terminal)
			}
		})
	}
}

func TestBridgeManagedCompletedKeepsDrain(t *testing.T) {
	p := bridgeControlFixture(t)
	local, target := pair(t)
	b := New(p.ctx, p.leg, local, 32768, time.Second, nil)
	if err := p.peer.Write(p.ctx, websocket.MessageBinary, protocol.ReadyFrame()); err != nil {
		t.Fatal(err)
	}
	if err := b.WaitReady(); err != nil {
		t.Fatal(err)
	}
	result := p.run(b)
	if err := target.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if f := controlFrame(t, p); f.Type != protocol.FrameFin {
		t.Fatal("missing local FIN")
	}
	if err := p.peer.Write(p.ctx, websocket.MessageBinary, protocol.FinFrame()); err != nil {
		t.Fatal(err)
	}
	controlSignal(t, p.ctx, b.Done())
	if b.Result().Code != protocol.CodeCompleted || p.leg.ControlSnapshot().Stopped {
		t.Fatal("Completed stopped control before original drain")
	}
	select {
	case <-result:
		t.Fatal("Completed returned before original peer close")
	default:
	}
	// A genuine later write failure can release the drain but cannot replace
	// the already chosen Completed result.
	p.socket.armed.Store(true)
	if err := p.peer.WritePing(p.ctx, []byte("after-fin")); err != nil {
		t.Fatal(err)
	}
	controlSignal(t, p.ctx, p.socket.entered)
	p.socket.free()
	if r := controlResult(t, p, result); r.Code != protocol.CodeCompleted {
		t.Fatal("late control error replaced Completed", r)
	}
	if err := p.leg.JoinCleanup(); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeManagedFailureBeforeReady(t *testing.T) {
	for _, mode := range []string{"before_bind", "during_ready"} {
		t.Run(mode, func(t *testing.T) {
			p := bridgeControlFixture(t)
			local, target := pair(t)
			p.socket.armed.Store(true)
			var b *Bridge
			read := make(chan error, 1)
			if mode == "during_ready" {
				b = New(p.ctx, p.leg, local, 32768, time.Second, nil)
			}
			p.wg.Add(1)
			go func() {
				defer p.wg.Done()
				if b == nil {
					_, _, err := p.leg.Read(p.ctx)
					read <- err
				} else {
					read <- b.WaitReady()
				}
			}()
			if err := p.peer.WritePing(p.ctx, []byte("before-ready")); err != nil {
				t.Fatal(err)
			}
			controlSignal(t, p.ctx, p.socket.entered)
			p.socket.free()
			select {
			case err := <-read:
				if err == nil {
					t.Fatal("failed leg accepted READY")
				}
			case <-p.ctx.Done():
				t.Fatal(p.ctx.Err())
			}
			if mode == "before_bind" {
				b = New(p.ctx, p.leg, local, 32768, time.Second, nil)
				if err := b.WaitReady(); err == nil {
					t.Fatal("early control error lost during Bind")
				}
			}
			controlSignal(t, p.ctx, b.Done())
			if r := b.Result(); r.Code != protocol.CodeChannelInterrupted || r.Up != 0 || r.Down != 0 {
				t.Fatalf("before READY: %+v", r)
			}
			if err := p.leg.JoinCleanup(); err != nil {
				t.Fatal(err)
			}
			expectReset(t, target)
		})
	}
}

func TestBridgeManagedRejectedReadyDrainsOriginalGrace(t *testing.T) {
	for _, mode := range []string{"peer_closes", "force_at_original_deadline"} {
		t.Run(mode, func(t *testing.T) {
			p := bridgeControlFixture(t)
			local, _ := pair(t)
			b := New(p.ctx, p.leg, local, 32768, 200*time.Millisecond, nil)
			if err := p.peer.Write(p.ctx, websocket.MessageBinary, protocol.DataFrame([]byte("not-ready"))); err != nil {
				t.Fatal(err)
			}
			if err := b.WaitReady(); protocol.CodeOf(err) != protocol.CodeProtocolError {
				t.Fatal("wrong first frame accepted", err)
			}
			if f := controlFrame(t, p); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeProtocolError {
				t.Fatal("ABORT lost before drain", f)
			}
			if p.leg.ControlSnapshot().Stopped {
				t.Fatal("READY failure closed transport before original grace/drain")
			}
			if mode == "peer_closes" {
				_ = p.peer.CloseNow()
			}
			err := p.leg.DrainAndJoin()
			if mode == "peer_closes" && err != nil {
				t.Fatal(err)
			}
			if mode == "force_at_original_deadline" && err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if b.Result().Code != protocol.CodeProtocolError {
				t.Fatal("cleanup changed business first cause")
			}
			// The forced close can consume all of the original D. A late join
			// is reported as such; this watchdog is only test-worker teardown.
			if !p.leg.ControlSnapshot().Stopped {
				t.Fatal("drain never reached actual transport close")
			}
		})
	}
}
