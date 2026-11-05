package wsio

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type controlSocketPair struct {
	ctx          context.Context
	client, peer *websocket.Conn
	gate         *diagnosticGate
	control      *Control
	ping, pong   chan string
	tasks        []<-chan struct{}
}

func (p *controlSocketPair) run(fn func() error) <-chan error {
	done, result := make(chan struct{}), make(chan error, 1)
	p.tasks = append(p.tasks, done)
	go func() { defer close(done); result <- fn() }()
	return result
}

func controlSocketFixture(t *testing.T, clock func() time.Time) *controlSocketPair {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	p := &controlSocketPair{ctx: ctx, control: NewControl(clock), ping: make(chan string, 4), pong: make(chan string, 4)}
	accepted, gates := make(chan *websocket.Conn, 1), make(chan *diagnosticGate, 1)
	quit, handlerDone := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OnPongReceived: func(_ context.Context, b []byte) {
			select {
			case p.pong <- string(b):
			default:
			}
		}})
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
		g := newDiagnosticGate(c, true)
		gates <- g
		return g, nil
	}}
	t.Cleanup(func() {
		p.control.Stop()
		if p.gate != nil {
			_ = p.gate.Close()
			p.gate.free()
		}
		if p.client != nil {
			_ = p.client.CloseNow()
		}
		if p.peer != nil {
			_ = p.peer.CloseNow()
		}
		if err := p.control.Join(ctx); err != nil {
			t.Error("control worker cleanup", err)
		}
		close(quit)
		srv.Close()
		transport.CloseIdleConnections()
		for _, done := range p.tasks {
			diagnosticCleanupWait(t, ctx, done, "real control fixture worker")
		}
		if p.peer != nil {
			diagnosticCleanupWait(t, ctx, handlerDone, "real control fixture handler")
		}
		cancel()
	})
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport}, OnPingReceived: func(ctx context.Context, b []byte) bool {
			answer := p.control.OnPing(ctx, b)
			select {
			case p.ping <- string(b):
			default:
			}
			return answer
		}, OnPongReceived: p.control.OnPong,
	})
	if err != nil {
		t.Fatal(err)
	}
	p.client = c
	select {
	case p.peer = <-accepted:
	case <-ctx.Done():
		t.Fatal("control peer accept", ctx.Err())
	}
	select {
	case p.gate = <-gates:
	case <-ctx.Done():
		t.Fatal("control socket gate", ctx.Err())
	}
	return p
}

type controlWriteEntry struct{ kind, payload string }
type tracedControlWriter struct {
	ControlWriter
	entered chan controlWriteEntry
}

func (w *tracedControlWriter) WritePing(ctx context.Context, b []byte) error {
	select {
	case w.entered <- controlWriteEntry{"ping", string(b)}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return w.ControlWriter.WritePing(ctx, b)
}
func (w *tracedControlWriter) WritePong(ctx context.Context, b []byte) error {
	select {
	case w.entered <- controlWriteEntry{"pong", string(b)}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return w.ControlWriter.WritePong(ctx, b)
}

func receiveControlString(t *testing.T, p *controlSocketPair, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("control payload %q, want %q", got, want)
		}
	case <-p.ctx.Done():
		t.Fatal("control payload watchdog", p.ctx.Err())
	}
}

func readControlData(ctx context.Context, c *websocket.Conn, want string) error {
	typ, b, err := c.Read(ctx)
	if err != nil {
		return err
	}
	if typ != websocket.MessageBinary || string(b) != want {
		return errors.New("real DATA was truncated or replaced")
	}
	return nil
}

func TestControlRealWebSocketBackpressure(t *testing.T) {
	t.Run("data_lock_reader_and_latest_pong", func(t *testing.T) {
		p := controlSocketFixture(t, nil)
		trace := &tracedControlWriter{ControlWriter: p.client, entered: make(chan controlWriteEntry, 4)}
		if err := p.control.Start(trace); err != nil {
			t.Fatal(err)
		}
		reader := p.run(func() error {
			if err := readControlData(p.ctx, p.client, "reverse-one"); err != nil {
				return err
			}
			return readControlData(p.ctx, p.client, "reverse-two")
		})
		p.gate.armed.Store(true)
		data := p.run(func() error { return p.client.Write(p.ctx, websocket.MessageBinary, []byte("held-data")) })
		diagnosticWait(t, p.ctx, p.gate.entered, "DATA owns real frame lock")
		if p.gate.opcode.Load() != 2 {
			t.Fatal("wrong held frame")
		}
		if err := p.peer.WritePing(p.ctx, []byte("first")); err != nil {
			t.Fatal(err)
		}
		receiveControlString(t, p, p.ping, "first")
		select {
		case entry := <-trace.entered:
			if entry.kind != "pong" || entry.payload != "first" {
				t.Fatal("wrong claimed PONG", entry)
			}
		case <-p.ctx.Done():
			t.Fatal("PONG not claimed")
		}
		for _, payload := range []string{"discarded", "latest"} {
			if err := p.peer.WritePing(p.ctx, []byte(payload)); err != nil {
				t.Fatal(err)
			}
			receiveControlString(t, p, p.ping, payload)
		}
		for _, payload := range []string{"reverse-one", "reverse-two"} {
			if err := p.peer.Write(p.ctx, websocket.MessageBinary, []byte(payload)); err != nil {
				t.Fatal(err)
			}
		}
		if err := diagnosticResult(t, p.ctx, reader, "unique reader progresses while PONG waits for DATA lock"); err != nil {
			t.Fatal(err)
		}
		if s := p.control.Snapshot(); s.InflightPongBytes != 5 || s.PendingPongBytes != 6 || s.Wire != "pong" {
			t.Fatal("bounded PONG ownership lost", s)
		}
		peerReader := p.run(func() error {
			if err := readControlData(p.ctx, p.peer, "held-data"); err != nil {
				return err
			}
			return readControlData(p.ctx, p.peer, "next-data")
		})
		p.gate.free()
		if err := diagnosticResult(t, p.ctx, data, "original DATA write returns"); err != nil {
			t.Fatal(err)
		}
		receiveControlString(t, p, p.pong, "first")
		receiveControlString(t, p, p.pong, "latest")
		if err := p.client.Write(p.ctx, websocket.MessageBinary, []byte("next-data")); err != nil {
			t.Fatal(err)
		}
		if err := diagnosticResult(t, p.ctx, peerReader, "original and subsequent DATA intact"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("actual_control_write_close_join", func(t *testing.T) {
		p := controlSocketFixture(t, nil)
		if err := p.control.Start(p.client); err != nil {
			t.Fatal(err)
		}
		reader := p.run(func() error { _, _, err := p.client.Read(p.ctx); return err })
		p.gate.armed.Store(true)
		if err := p.peer.WritePing(p.ctx, []byte("blocked")); err != nil {
			t.Fatal(err)
		}
		receiveControlString(t, p, p.ping, "blocked")
		diagnosticWait(t, p.ctx, p.gate.entered, "PONG enters actual socket Write")
		if p.gate.opcode.Load() != 10 || p.control.Snapshot().Wire != "pong" {
			t.Fatal("not a real blocked PONG")
		}
		select {
		case <-p.gate.closed:
			t.Fatal("helper closed live transport")
		default:
		}
		p.control.Stop()
		_ = p.client.CloseNow()
		if err := p.control.Join(p.ctx); err != nil {
			t.Fatal(err)
		}
		if err := diagnosticResult(t, p.ctx, reader, "original reader is joined after transport close"); err == nil {
			t.Fatal("closed real reader returned success")
		}
		if p.control.Err() != nil || !p.control.Snapshot().WorkerDone {
			t.Fatal("expected close became a new first cause")
		}
	})
}
