//go:build !js

package websocket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests use real HTTP upgrades and real WebSocket readers. A gate is
// selected at Write entry, so a late-returning upgrade cannot consume an arm.
type explicitGate struct {
	net.Conn
	mu        sync.Mutex
	armed     *explicitArm
	closed    chan struct{}
	closeOnce sync.Once
}

type explicitArm struct {
	op      opcode
	entered chan []byte
	release chan struct{}
	once    sync.Once
	err     error
}

func (a *explicitArm) free() { a.once.Do(func() { close(a.release) }) }

func (g *explicitGate) arm(op opcode, err error) *explicitArm {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.armed != nil {
		panic("an explicit control gate is already armed")
	}
	a := &explicitArm{op: op, entered: make(chan []byte, 1), release: make(chan struct{}), err: err}
	g.armed = a
	return a
}

func (g *explicitGate) Write(p []byte) (int, error) {
	g.mu.Lock()
	a := g.armed
	if a == nil || len(p) == 0 || opcode(p[0]&15) != a.op {
		a = nil
	} else {
		g.armed = nil
	}
	g.mu.Unlock()
	if a != nil {
		a.entered <- bytes.Clone(p)
		select {
		case <-a.release:
		case <-g.closed:
			return 0, net.ErrClosed
		}
		if a.err != nil {
			return 0, a.err
		}
	}
	return g.Conn.Write(p)
}

func (g *explicitGate) Close() error {
	g.closeOnce.Do(func() { close(g.closed) })
	return g.Conn.Close()
}

type explicitEvent struct {
	op      opcode
	payload []byte
}
type explicitRead struct {
	typ     MessageType
	payload []byte
	err     error
}
type explicitPair struct {
	t                          *testing.T
	ctx                        context.Context
	client, server             *Conn
	gate                       *explicitGate
	clientEvents, serverEvents chan explicitEvent
	wg                         sync.WaitGroup
}

func explicitAwait[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatalf("explicit control test total deadline: %v", ctx.Err())
	}
	var zero T
	return zero
}

func newExplicitPair(t *testing.T, deadline time.Time) *explicitPair {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	p := &explicitPair{t: t, ctx: ctx, clientEvents: make(chan explicitEvent, 8), serverEvents: make(chan explicitEvent, 8)}
	callback := func(ch chan<- explicitEvent, op opcode, b []byte) {
		select {
		case ch <- explicitEvent{op, bytes.Clone(b)}:
		default:
			panic("explicit control test event capacity exceeded")
		}
	}
	type accepted struct {
		c   *Conn
		err error
	}
	accept := make(chan accepted, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Accept(w, r, &AcceptOptions{
			OnPingReceived: func(_ context.Context, b []byte) bool { callback(p.serverEvents, opPing, b); return false },
			OnPongReceived: func(_ context.Context, b []byte) { callback(p.serverEvents, opPong, b) },
		})
		accept <- accepted{c, err}
	}))
	gate := make(chan *explicitGate, 1)
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		g := &explicitGate{Conn: c, closed: make(chan struct{})}
		gate <- g
		return g, nil
	}}
	t.Cleanup(func() {
		// Cleanup is bounded by the same absolute deadline, not a fresh window.
		cancel()
		if p.gate != nil {
			_ = p.gate.Close()
		}
		if p.client != nil {
			_ = p.client.CloseNow()
		}
		if p.server != nil {
			_ = p.server.CloseNow()
		}
		tr.CloseIdleConnections()
		srv.Close()
		done := make(chan struct{})
		go func() { p.wg.Wait(); close(done) }()
		cleanupCtx, cleanupCancel := context.WithDeadline(context.Background(), deadline)
		defer cleanupCancel()
		select {
		case <-done:
		case <-cleanupCtx.Done():
			t.Error("explicit control test workers did not join within original deadline")
		}
	})
	var err error
	p.client, _, err = Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &DialOptions{
		HTTPClient:     &http.Client{Transport: tr},
		OnPingReceived: func(_ context.Context, b []byte) bool { callback(p.clientEvents, opPing, b); return false },
		OnPongReceived: func(_ context.Context, b []byte) { callback(p.clientEvents, opPong, b) },
	})
	if err != nil {
		t.Fatal(err)
	}
	p.gate = explicitAwait(t, ctx, gate)
	a := explicitAwait(t, ctx, accept)
	p.server = a.c
	if a.err != nil {
		t.Fatal(a.err)
	}
	return p
}

func (p *explicitPair) async(fn func() error) <-chan error {
	p.wg.Add(1)
	ch := make(chan error, 1)
	go func() { defer p.wg.Done(); ch <- fn() }()
	return ch
}

func (p *explicitPair) reader(c *Conn) <-chan explicitRead {
	p.wg.Add(1)
	ch := make(chan explicitRead, 8)
	go func() {
		defer p.wg.Done()
		for {
			mt, b, err := c.Read(p.ctx)
			select {
			case ch <- explicitRead{mt, b, err}:
			case <-p.ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

func explicitData(t *testing.T, p *explicitPair, from *Conn, read <-chan explicitRead, body string) {
	t.Helper()
	if err := from.Write(p.ctx, MessageBinary, []byte(body)); err != nil {
		t.Fatal(err)
	}
	v := explicitAwait(t, p.ctx, read)
	if v.err != nil || v.typ != MessageBinary || string(v.payload) != body {
		t.Fatalf("DATA: %+v", v)
	}
}

func TestExplicitControlFrames(t *testing.T) {
	for _, side := range []string{"client", "server"} {
		for _, op := range []opcode{opPing, opPong} {
			for _, n := range []int{0, 8, 125} {
				t.Run(fmt.Sprintf("%s_%s_%d", side, op, n), func(t *testing.T) {
					p := newExplicitPair(t, time.Now().Add(10*time.Second))
					cr, sr := p.reader(p.client), p.reader(p.server)
					c, events := p.client, p.serverEvents
					if side == "server" {
						c, events = p.server, p.clientEvents
					}
					payload := bytes.Repeat([]byte{0x80}, n)
					original := bytes.Clone(payload)
					var err error
					if op == opPing {
						err = c.WritePing(p.ctx, payload)
					} else {
						err = c.WritePong(p.ctx, payload)
					}
					if err != nil {
						t.Fatal(err)
					}
					got := explicitAwait(t, p.ctx, events)
					if got.op != op || !bytes.Equal(got.payload, original) || !bytes.Equal(payload, original) {
						t.Fatalf("control frame or caller bytes changed: %+v", got)
					}
					c.activePingsMu.Lock()
					active := len(c.activePings)
					c.activePingsMu.Unlock()
					if active != 0 {
						t.Fatalf("frame-only API registered %d waiting pings", active)
					}
					// No PONG is generated for PING: returning above proves no reply wait.
					explicitData(t, p, p.client, sr, "client-to-server")
					explicitData(t, p, p.server, cr, "server-to-client")
				})
			}
		}
	}
}

func TestExplicitControlValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   opcode
		size int
	}{
		{"ping_126", opPing, 126}, {"pong_126", opPong, 126},
		{"continuation", opContinuation, 1}, {"text", opText, 1}, {"binary", opBinary, 1}, {"close", opClose, 1}, {"reserved", opcode(15), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newExplicitPair(t, time.Now().Add(10*time.Second))
			if err := p.client.writeExplicitControl(p.ctx, tc.op, make([]byte, tc.size)); err == nil {
				t.Fatal("invalid control frame accepted")
			}
			cr, sr := p.reader(p.client), p.reader(p.server)
			explicitData(t, p, p.client, sr, "still-open")
			explicitData(t, p, p.server, cr, "reply")
			if len(p.serverEvents) != 0 {
				t.Fatal("invalid control payload reached peer")
			}
		})
	}
}

func TestExplicitControlCancellation(t *testing.T) {
	for _, op := range []opcode{opPing, opPong} {
		for _, phase := range []string{"write_lock", "control_write"} {
			for _, reason := range []string{"cancel", "close"} {
				t.Run(fmt.Sprintf("%s_%s_%s", op, phase, reason), func(t *testing.T) {
					p := newExplicitPair(t, time.Now().Add(10*time.Second))
					cr, sr := p.reader(p.client), p.reader(p.server)
					var dataDone <-chan error
					var a *explicitArm
					if phase == "write_lock" {
						a = p.gate.arm(opBinary, nil)
						dataDone = p.async(func() error { return p.client.Write(p.ctx, MessageBinary, []byte("held-data")) })
						explicitAwait(t, p.ctx, a.entered)
					} else {
						a = p.gate.arm(op, nil)
					}
					defer a.free()
					ctx, cancel := context.WithCancel(p.ctx)
					defer cancel()
					writeDone := p.async(func() error { return p.client.writeExplicitControl(ctx, op, []byte("nonce")) })
					if phase == "control_write" {
						explicitAwait(t, p.ctx, a.entered)
					}
					if reason == "cancel" {
						cancel()
					} else {
						_ = p.client.CloseNow()
					}
					err := explicitAwait(t, p.ctx, writeDone)
					want := context.Canceled
					if reason == "close" {
						want = net.ErrClosed
					}
					if !errors.Is(err, want) {
						t.Fatalf("got %v, want %v", err, want)
					}
					if phase == "write_lock" && reason == "cancel" {
						if p.client.isClosed() {
							t.Fatal("cancel while waiting for frame lock closed transport")
						}
						a.free()
						if err := explicitAwait(t, p.ctx, dataDone); err != nil {
							t.Fatal(err)
						}
						got := explicitAwait(t, p.ctx, sr)
						if got.err != nil || string(got.payload) != "held-data" {
							t.Fatalf("held DATA: %+v", got)
						}
						explicitData(t, p, p.client, sr, "after-cancel")
						explicitData(t, p, p.server, cr, "reverse-after-cancel")
					} else {
						if !p.client.isClosed() {
							t.Fatal("active control cancellation/Close did not close transport")
						}
						if dataDone != nil && explicitAwait(t, p.ctx, dataDone) == nil {
							t.Fatal("closed held DATA unexpectedly succeeded")
						}
					}
				})
			}
		}
	}
}

func TestExplicitControlWriteError(t *testing.T) {
	for _, op := range []opcode{opPing, opPong} {
		t.Run(op.String(), func(t *testing.T) {
			p := newExplicitPair(t, time.Now().Add(10*time.Second))
			injected := errors.New("controlled socket write failure")
			a := p.gate.arm(op, injected)
			defer a.free()
			done := p.async(func() error { return p.client.writeExplicitControl(p.ctx, op, []byte("nonce")) })
			raw := explicitAwait(t, p.ctx, a.entered)
			if len(raw) < 2 || raw[0] != byte(0x80|op) || raw[1]&0x80 == 0 {
				t.Fatalf("not a final masked control frame: %x", raw)
			}
			a.free()
			err := explicitAwait(t, p.ctx, done)
			if !errors.Is(err, injected) || !strings.Contains(err.Error(), "failed to write frame") {
				t.Fatalf("write error chain lost: %v", err)
			}
		})
	}
}

func TestExplicitControlKeepsCallerDeadline(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	old, next := newExplicitPair(t, deadline), newExplicitPair(t, deadline)
	old.reader(old.server)
	next.reader(next.server)
	oldGate, nextGate := old.gate.arm(opBinary, nil), next.gate.arm(opBinary, nil)
	defer oldGate.free()
	defer nextGate.free()
	oldData := old.async(func() error { return old.client.Write(old.ctx, MessageBinary, []byte("old-data")) })
	nextData := next.async(func() error { return next.client.Write(next.ctx, MessageBinary, []byte("new-data")) })
	explicitAwait(t, old.ctx, oldGate.entered)
	explicitAwait(t, next.ctx, nextGate.entered)
	nextDone := next.async(func() error { return next.client.WritePong(next.ctx, []byte("new")) })
	started := time.Now()
	oldDone := old.async(func() error { return old.client.writeControl(old.ctx, opPong, []byte("old")) })
	if err := explicitAwait(t, old.ctx, oldDone); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("old default no longer has its deadline: %v", err)
	}
	if time.Since(started) < 5*time.Second {
		t.Fatal("old default expired before its original five seconds")
	}
	select {
	case err := <-nextDone:
		t.Fatalf("new API installed an independent timeout: %v", err)
	default:
	}
	if old.client.isClosed() || next.client.isClosed() {
		t.Fatal("waiting for the frame lock closed a connection")
	}
	oldGate.free()
	nextGate.free()
	if err := explicitAwait(t, old.ctx, oldData); err != nil {
		t.Fatal(err)
	}
	if err := explicitAwait(t, next.ctx, nextData); err != nil {
		t.Fatal(err)
	}
	if err := explicitAwait(t, next.ctx, nextDone); err != nil {
		t.Fatal(err)
	}
	got := explicitAwait(t, next.ctx, next.serverEvents)
	if got.op != opPong || string(got.payload) != "new" {
		t.Fatalf("new frame: %+v", got)
	}
}
