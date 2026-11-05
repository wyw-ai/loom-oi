package wsio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type diagnosticNetError struct {
	timedOut bool
	text     string
}

func (e diagnosticNetError) Error() string   { return e.text }
func (e diagnosticNetError) Timeout() bool   { return e.timedOut }
func (e diagnosticNetError) Temporary() bool { return false }

func TestDiagnosticErrorClasses(t *testing.T) {
	const secret = "Authorization=Bearer-private; ticket=private; payload=private; host=private"
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"none", nil, "none"},
		{"wrapped_deadline", fmt.Errorf("%s: %w", secret, context.DeadlineExceeded), "deadline"},
		{"wrapped_canceled", fmt.Errorf("%s: %w", secret, context.Canceled), "canceled"},
		{"wrapped_eof", fmt.Errorf("%s: %w", secret, io.EOF), "eof"},
		{"wrapped_closed", fmt.Errorf("%s: %w", secret, net.ErrClosed), "closed"},
		{"wrapped_timeout", fmt.Errorf("%s: %w", secret, diagnosticNetError{true, secret}), "timeout"},
		{"non_timeout_network_error", diagnosticNetError{false, secret}, "other"},
		{"wrapped_websocket_close", fmt.Errorf("%s: %w", secret, websocket.CloseError{Code: websocket.StatusPolicyViolation, Reason: secret}), "websocket_close"},
		{"arbitrary_text", errors.New(secret + " context deadline exceeded EOF use of closed network connection"), "other"},
		{"deadline_priority", errors.Join(net.ErrClosed, io.EOF, context.Canceled, context.DeadlineExceeded), "deadline"},
		{"canceled_priority", errors.Join(net.ErrClosed, io.EOF, context.Canceled), "canceled"},
		{"eof_priority", errors.Join(net.ErrClosed, io.EOF), "eof"},
		{"closed_priority", errors.Join(diagnosticNetError{true, secret}, net.ErrClosed), "closed"},
		{"timeout_priority", errors.Join(websocket.CloseError{Code: websocket.StatusPolicyViolation, Reason: secret}, diagnosticNetError{true, secret}), "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ErrorClass(tc.err)
			if got != tc.want {
				t.Fatalf("class=%q want=%q", got, tc.want)
			}
			data, err := json.Marshal(map[string]string{"error_class": got})
			if err != nil || bytes.Contains(data, []byte("private")) {
				t.Fatal("diagnostic label exposed arbitrary error content", err)
			}
		})
	}
}

func TestDiagnosticContextStates(t *testing.T) {
	t.Run("active_then_canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		before := ContextState(ctx)
		cancel()
		if before != "active" || ContextState(ctx) != "canceled" {
			t.Fatal("state was not sampled before cleanup cancellation", before, ContextState(ctx))
		}
	})
	t.Run("deadline_before_cleanup", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		before := ContextState(ctx)
		cancel()
		if before != "deadline" || ContextState(ctx) != "deadline" {
			t.Fatal("cleanup changed the already observed deadline", before, ContextState(ctx))
		}
	})
	t.Run("background", func(t *testing.T) {
		if got := ContextState(context.Background()); got != "active" {
			t.Fatal(got)
		}
	})
}

func TestPingPhaseWrapperPrivacy(t *testing.T) {
	const private = "secret-ticket-and-payload"
	wrap := func(label string, err error) error { return fmt.Errorf("%s%w", label, err) }
	ping := func(err error) error { return wrap("failed to ping: ", err) }
	control := func(err error) error { return wrap("failed to write control frame opPing: ", err) }
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "unknown"},
		{"opaque_keywords", errors.New("failed to ping: failed to write control frame opPing: failed to acquire lock: " + private), "unknown"},
		{"opaque_inner_keywords", ping(errors.New("failed to wait for pong: " + private)), "unknown"},
		{"wrong_outer", wrap("other failure: ", ping(context.DeadlineExceeded)), "unknown"},
		{"changed_outer_text", wrap("failed to ping: changed: ", context.DeadlineExceeded), "unknown"},
		{"pong", ping(wrap("failed to wait for pong: ", context.DeadlineExceeded)), "pong_wait"},
		{"connection_closed", ping(net.ErrClosed), "connection_closed"},
		{"lock", ping(control(wrap("failed to acquire lock: ", context.DeadlineExceeded))), "write_lock"},
		{"frame", ping(control(wrap("failed to write frame: ", errors.New(private)))), "control_write"},
		{"control_closed", ping(control(net.ErrClosed)), "connection_closed"},
		{"opaque_control_keywords", ping(control(errors.New("failed to acquire lock: " + private))), "unknown"},
		{"other_opcode", ping(wrap("failed to write control frame opPong: ", wrap("failed to acquire lock: ", context.DeadlineExceeded))), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := PingPhase(tc.err)
			if got != tc.want {
				t.Fatalf("phase=%q want=%q", got, tc.want)
			}
			data, err := json.Marshal(map[string]string{"ping_phase": got, "error_class": ErrorClass(tc.err)})
			if err != nil || bytes.Contains(data, []byte(private)) {
				t.Fatal("diagnostic output leaked the error leaf", err)
			}
		})
	}
}

// diagnosticGate claims an invocation at entry, before the real socket write.
// A previously entered Upgrade Write can never consume a later arm. The gate
// stores an opcode, not bytes or arbitrary error strings.
type diagnosticGate struct {
	net.Conn
	armed       atomic.Bool
	opcode      atomic.Int32
	hold        bool
	entered     chan struct{}
	written     chan struct{}
	release     chan struct{}
	closed      chan struct{}
	releaseOnce sync.Once
	closeOnce   sync.Once
}

func (c *diagnosticGate) Write(p []byte) (int, error) {
	selected := c.armed.CompareAndSwap(true, false)
	if selected {
		if len(p) > 0 {
			c.opcode.Store(int32(p[0] & 0x0f))
		}
		close(c.entered)
		if c.hold {
			select {
			case <-c.release:
			case <-c.closed:
				return 0, net.ErrClosed
			}
		}
	}
	n, err := c.Conn.Write(p)
	if selected && err == nil && n == len(p) {
		close(c.written)
	}
	return n, err
}

func (c *diagnosticGate) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func (c *diagnosticGate) free() { c.releaseOnce.Do(func() { close(c.release) }) }

func newDiagnosticGate(c net.Conn, hold bool) *diagnosticGate {
	return &diagnosticGate{Conn: c, hold: hold, entered: make(chan struct{}), written: make(chan struct{}),
		release: make(chan struct{}), closed: make(chan struct{})}
}

type diagnosticWriteEntry struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *diagnosticWriteEntry) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Write(p)
}

func TestDiagnosticGateArmingDoesNotSelectInflightWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	local, remote := net.Pipe()
	deadline, _ := ctx.Deadline()
	if err := local.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := remote.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	entry := &diagnosticWriteEntry{Conn: local, entered: make(chan struct{})}
	gate := newDiagnosticGate(entry, true)
	p := &diagnosticPair{ctx: ctx}
	t.Cleanup(func() {
		gate.free()
		_ = gate.Close()
		_ = remote.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, done := range p.tasks {
			diagnosticCleanupWait(t, cleanup, done, "gate arming worker join")
		}
	})
	first := p.run(func() error {
		_, err := gate.Write([]byte("old"))
		return err
	})
	diagnosticWait(t, ctx, entry.entered, "unarmed invocation reached real transport")
	gate.armed.Store(true)
	// The first invocation was already inside the underlying Write when this
	// arm became visible. Returning after it must not retroactively claim it.
	got := make([]byte, 3)
	if _, err := io.ReadFull(remote, got); err != nil || string(got) != "old" {
		t.Fatal("original transport write", err)
	}
	if err := diagnosticResult(t, ctx, first, "unarmed original return"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
		t.Fatal("later arm selected an already in-flight invocation")
	default:
	}
	if !gate.armed.Load() {
		t.Fatal("older invocation consumed the new arm")
	}
	second := p.run(func() error {
		_, err := gate.Write([]byte{0x82, 0})
		return err
	})
	diagnosticWait(t, ctx, gate.entered, "next invocation selected at entry")
	if gate.opcode.Load() != 2 || gate.armed.Load() {
		t.Fatal("new invocation did not own its gate")
	}
	gate.free()
	got = make([]byte, 2)
	if _, err := io.ReadFull(remote, got); err != nil || !bytes.Equal(got, []byte{0x82, 0}) {
		t.Fatal("next transport write", err)
	}
	if err := diagnosticResult(t, ctx, second, "selected new return"); err != nil {
		t.Fatal(err)
	}
	diagnosticWait(t, ctx, gate.written, "selected complete transport write")
}

type diagnosticPair struct {
	client *websocket.Conn
	peer   *websocket.Conn
	gate   *diagnosticGate
	ctx    context.Context
	tasks  []<-chan struct{}
}

func (p *diagnosticPair) run(fn func() error) <-chan error {
	result, done := make(chan error, 1), make(chan struct{})
	p.tasks = append(p.tasks, done) // All additions are made by the test goroutine.
	go func() {
		defer close(done)
		result <- fn()
	}()
	return result
}

func diagnosticWait(t *testing.T, ctx context.Context, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal(label, "watchdog expired")
	}
}

func diagnosticCleanupWait(t *testing.T, ctx context.Context, done <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-done:
	case <-ctx.Done():
		// Preserve all remaining join attempts if one teardown itself fails.
		t.Error(label, "watchdog expired")
	}
}

func diagnosticResult(t *testing.T, ctx context.Context, done <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal(label, "watchdog expired")
		return ctx.Err()
	}
}

func newDiagnosticPair(t *testing.T, hold bool) *diagnosticPair {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	accepted, wrapped := make(chan *websocket.Conn, 1), make(chan *diagnosticGate, 1)
	quit, handlerDone := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- c
		<-quit
		_ = c.CloseNow()
	}))
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		gate := newDiagnosticGate(c, hold)
		wrapped <- gate
		return gate, nil
	}}
	p := &diagnosticPair{ctx: ctx}
	t.Cleanup(func() {
		// Release before close: even an assertion failure cannot strand the
		// instrument's socket Write or the original library's write lock.
		if p.gate != nil {
			p.gate.free()
			_ = p.gate.Close()
		}
		if p.client != nil {
			_ = p.client.CloseNow()
		}
		if p.peer != nil {
			_ = p.peer.CloseNow()
		}
		close(quit)
		srv.Close()
		transport.CloseIdleConnections()
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, done := range p.tasks {
			diagnosticCleanupWait(t, cleanup, done, "diagnostic worker join")
		}
		// A failed HTTP dial may never have entered the handler.
		if p.peer != nil {
			diagnosticCleanupWait(t, cleanup, handlerDone, "diagnostic server handler join")
		}
	})
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal("WebSocket instrument handshake", err)
	}
	p.client = c
	select {
	case p.peer = <-accepted:
	case <-ctx.Done():
		t.Fatal("WebSocket accept watchdog expired")
	}
	select {
	case p.gate = <-wrapped:
	case <-ctx.Done():
		t.Fatal("WebSocket socket identity missing")
	}
	// Ping uses the actual concurrent client reader. The peer deliberately
	// does not read until each test releases its own input condition.
	p.run(func() error {
		_, _, err := c.Read(context.Background())
		return err
	})
	return p
}

func diagnosticPingFailure(t *testing.T, p *diagnosticPair, ctx context.Context, done <-chan error, phase string) {
	t.Helper()
	diagnosticWait(t, p.ctx, ctx.Done(), "original 1s Ping deadline")
	err := diagnosticResult(t, p.ctx, done, "Ping return")
	if err == nil || ErrorClass(err) != "deadline" || ContextState(ctx) != "deadline" || PingPhase(err) != phase {
		t.Fatalf("unexpected bounded Ping diagnostic class=%q state=%q phase=%q", ErrorClass(err), ContextState(ctx), PingPhase(err))
	}
}

func diagnosticReadData(t *testing.T, p *diagnosticPair, want string) {
	t.Helper()
	typ, data, err := p.peer.Read(p.ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != want {
		t.Fatalf("healthy DATA after Ping failure: type=%v bytes=%d err=%v", typ, len(data), err)
	}
}

func TestPingPhasePinnedLibrary(t *testing.T) {
	t.Run("connection_closed_before_write", func(t *testing.T) {
		p := newDiagnosticPair(t, true)
		if err := p.client.CloseNow(); err != nil {
			t.Fatal("close fixture before Ping", err)
		}
		diagnosticWait(t, p.ctx, p.gate.closed, "closed underlying socket")
		p.gate.armed.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := diagnosticResult(t, p.ctx, p.run(func() error { return p.client.Ping(ctx) }), "Ping on closed connection")
		if ErrorClass(err) != "closed" || PingPhase(err) != "connection_closed" {
			t.Fatalf("pre-lock closure misclassified as a write: class=%q phase=%q", ErrorClass(err), PingPhase(err))
		}
		select {
		case <-p.gate.entered:
			t.Fatal("closed Ping unexpectedly reached a socket Write")
		default:
		}
		if !p.gate.armed.Load() {
			t.Fatal("closed Ping consumed the unused socket gate")
		}
	})
	t.Run("active_control_write_deadline", func(t *testing.T) {
		p := newDiagnosticPair(t, true)
		p.gate.armed.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done := p.run(func() error { return p.client.Ping(ctx) })
		diagnosticWait(t, p.ctx, p.gate.entered, "Ping socket Write entry")
		if p.gate.opcode.Load() != 9 || ctx.Err() != nil {
			t.Fatal("fixture did not enter this Ping's socket Write before its original deadline")
		}
		diagnosticPingFailure(t, p, ctx, done, "control_write")
		diagnosticWait(t, p.ctx, p.gate.closed, "library closed active write socket")
		if err := p.client.Write(p.ctx, websocket.MessageBinary, []byte("must-not-write")); err == nil {
			t.Fatal("active control socket write deadline left a writable connection")
		}
	})
	t.Run("write_lock_deadline", func(t *testing.T) {
		p := newDiagnosticPair(t, true)
		p.gate.armed.Store(true)
		write := p.run(func() error {
			return p.client.Write(context.Background(), websocket.MessageBinary, []byte("held-data"))
		})
		diagnosticWait(t, p.ctx, p.gate.entered, "DATA owns frame write lock")
		if p.gate.opcode.Load() != 2 {
			t.Fatal("fixture held an older handshake/control invocation instead of DATA")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done := p.run(func() error { return p.client.Ping(ctx) })
		diagnosticPingFailure(t, p, ctx, done, "write_lock")
		select {
		case <-p.gate.closed:
			t.Fatal("waiting for write lock closed a healthy socket")
		default:
		}
		p.gate.free()
		if err := diagnosticResult(t, p.ctx, write, "original healthy DATA write"); err != nil {
			t.Fatal("Ping lock timeout damaged the original DATA write", err)
		}
		diagnosticReadData(t, p, "held-data")
		if err := p.client.Write(p.ctx, websocket.MessageBinary, []byte("next-data")); err != nil {
			t.Fatal("DATA write after lock timeout", err)
		}
		diagnosticReadData(t, p, "next-data")
	})
	t.Run("pong_wait_deadline", func(t *testing.T) {
		p := newDiagnosticPair(t, false)
		p.gate.armed.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		done := p.run(func() error { return p.client.Ping(ctx) })
		diagnosticWait(t, p.ctx, p.gate.entered, "Ping socket Write entry")
		diagnosticWait(t, p.ctx, p.gate.written, "complete real Ping socket Write")
		if p.gate.opcode.Load() != 9 || ctx.Err() != nil {
			t.Fatal("fixture did not complete this Ping Write within its original deadline")
		}
		// No peer reader has run, hence no peer-generated PONG can satisfy
		// this Ping. No sleep or fabricated error selects the phase.
		diagnosticPingFailure(t, p, ctx, done, "pong_wait")
		select {
		case <-p.gate.closed:
			t.Fatal("waiting for pong closed a healthy socket")
		default:
		}
		if err := p.client.Write(p.ctx, websocket.MessageBinary, []byte("data-after-pong-timeout")); err != nil {
			t.Fatal("DATA write after pong timeout", err)
		}
		diagnosticReadData(t, p, "data-after-pong-timeout")
	})
}
