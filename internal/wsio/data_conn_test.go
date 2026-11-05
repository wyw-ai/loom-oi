package wsio

import (
	"bytes"
	"context"
	"errors"
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

var errManagedSocket = errors.New("controlled ordinary PONG socket error")

type managedErrorSocket struct {
	net.Conn
	armed                    atomic.Bool
	allWrites                atomic.Bool
	entered, release, closed chan struct{}
	once                     sync.Once
}

func (c *managedErrorSocket) Write(b []byte) (int, error) {
	if len(b) != 0 && (c.allWrites.Load() || b[0]&15 == 10) && c.armed.CompareAndSwap(true, false) {
		close(c.entered)
		select {
		case <-c.release:
			return 0, errManagedSocket
		case <-c.closed:
			return 0, net.ErrClosed
		}
	}
	return c.Conn.Write(b)
}
func (c *managedErrorSocket) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type managedPair struct {
	ctx    context.Context
	conn   *ManagedConn
	peer   *websocket.Conn
	socket *managedErrorSocket
	pongs  chan string
	wg     sync.WaitGroup
}

func managedFixture(t *testing.T) *managedPair {
	return managedFixtureTLS(t, false)
}

func managedFixtureTLS(t *testing.T, useTLS bool) *managedPair {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	p := &managedPair{ctx: ctx, pongs: make(chan string, 4)}
	accepted, sockets := make(chan *websocket.Conn, 1), make(chan *managedErrorSocket, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OnPongReceived: func(_ context.Context, b []byte) { p.pongs <- string(b) }})
		if err == nil {
			accepted <- c
		}
	}))
	if useTLS {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	tr := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		s := &managedErrorSocket{Conn: c, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		sockets <- s
		return s, nil
	}}
	if useTLS {
		tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	}
	t.Cleanup(func() {
		if p.socket != nil {
			_ = p.socket.Close()
		}
		if p.conn != nil {
			_ = p.conn.CloseNow()
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
		cleanup, cancelCleanup := context.WithDeadline(context.Background(), deadline)
		defer cancelCleanup()
		diagnosticCleanupWait(t, cleanup, done, "managed socket fixture workers")
	})
	var err error
	p.conn, _, err = DialData(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}}, time.Second)
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

func (p *managedPair) run(fn func() error) <-chan error {
	p.wg.Add(1)
	result := make(chan error, 1)
	go func() { defer p.wg.Done(); result <- fn() }()
	return result
}

func TestManagedDataControlRoundTrip(t *testing.T) {
	p := managedFixture(t)
	read := p.run(func() error {
		typ, b, err := p.conn.Read(p.ctx)
		if err == nil && (typ != websocket.MessageBinary || string(b) != "data") {
			return errors.New("wrong DATA")
		}
		return err
	})
	if err := p.peer.WritePing(p.ctx, bytes.Repeat([]byte{'x'}, 125)); err != nil {
		t.Fatal(err)
	}
	peerRead := p.run(func() error { _, _, err := p.peer.Read(p.ctx); return err })
	select {
	case got := <-p.pongs:
		if len(got) != 125 {
			t.Fatal("truncated PONG")
		}
	case <-p.ctx.Done():
		t.Fatal(p.ctx.Err())
	}
	if err := p.peer.Write(p.ctx, websocket.MessageBinary, []byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := diagnosticResult(t, p.ctx, read, "DATA while asynchronous PONG works"); err != nil {
		t.Fatal(err)
	}
	if s := p.conn.ControlSnapshot(); s.Nonce != 0 || s.Window != 0 || s.ProbeEnded {
		t.Fatal("endpoint originated probe", s)
	}
	if err := p.conn.CloseNow(); err != nil {
		t.Fatal(err)
	}
	if err := p.conn.JoinCleanup(); err != nil {
		t.Fatal(err)
	}
	if err := diagnosticResult(t, p.ctx, peerRead, "peer sees close"); err == nil {
		t.Fatal("peer not closed")
	}
}

func TestManagedDataControlFailure(t *testing.T) {
	for _, when := range []string{"before_bind", "after_bind"} {
		t.Run(when, func(t *testing.T) {
			p := managedFixture(t)
			notified := make(chan error, 2)
			var count atomic.Int32
			handler := func(err error) { count.Add(1); notified <- err }
			if when == "after_bind" {
				p.conn.BindControlFailure(handler)
			}
			read := p.run(func() error { _, _, err := p.conn.Read(p.ctx); return err })
			p.socket.armed.Store(true)
			if err := p.peer.WritePing(p.ctx, []byte("failure")); err != nil {
				t.Fatal(err)
			}
			diagnosticWait(t, p.ctx, p.socket.entered, "PONG entered actual socket Write")
			if p.ctx.Err() != nil {
				t.Fatal("ordinary write failure requires active outer context")
			}
			close(p.socket.release)
			if err := diagnosticResult(t, p.ctx, read, "ordinary write failure releases reader"); err == nil {
				t.Fatal("reader remained successful")
			}
			if when == "before_bind" {
				p.conn.BindControlFailure(handler)
			}
			select {
			case err := <-notified:
				if !errors.Is(err, errManagedSocket) {
					t.Fatal("lost socket cause", err)
				}
			case <-p.ctx.Done():
				t.Fatal("failure callback missing")
			}
			if err := p.conn.JoinCleanup(); err != nil {
				t.Fatal(err)
			}
			p.conn.controlFailed(errors.New("later error"))
			if !errors.Is(p.conn.ControlError(), errManagedSocket) || count.Load() != 1 || !p.conn.ControlSnapshot().WorkerDone {
				t.Fatal("first failure/worker join lost")
			}
		})
	}
}

func TestManagedDataReaderLifecycle(t *testing.T) {
	for _, mode := range []string{"message_eof", "reader_failure", "body_failure", "write_failure", "graceful_close"} {
		t.Run(mode, func(t *testing.T) {
			p := managedFixture(t)
			switch mode {
			case "message_eof":
				if err := p.peer.Write(p.ctx, websocket.MessageBinary, []byte("body")); err != nil {
					t.Fatal(err)
				}
				_, body, err := p.conn.Reader(p.ctx)
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(body)
				if err != nil || string(b) != "body" {
					t.Fatal(string(b), err)
				}
				if p.conn.ControlSnapshot().Stopped {
					t.Fatal("message EOF closed transport")
				}
			case "reader_failure":
				_ = p.peer.CloseNow()
				_, _, err := p.conn.Reader(p.ctx)
				if err == nil {
					t.Fatal("closed Reader succeeded")
				}
			case "body_failure":
				p.conn.SetReadLimit(1)
				if err := p.peer.Write(p.ctx, websocket.MessageBinary, []byte("too long")); err != nil {
					t.Fatal(err)
				}
				_, body, err := p.conn.Reader(p.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = io.ReadAll(body); err == nil {
					t.Fatal("oversize body succeeded")
				}
			case "write_failure":
				_ = p.conn.Conn.CloseNow()
				if err := p.conn.Write(p.ctx, websocket.MessageBinary, []byte("closed")); err == nil {
					t.Fatal("closed Write succeeded")
				}
			case "graceful_close":
				peer := p.run(func() error { _, _, err := p.peer.Read(p.ctx); return err })
				if err := p.conn.Close(websocket.StatusNormalClosure, ""); err != nil {
					t.Fatal(err)
				}
				_ = diagnosticResult(t, p.ctx, peer, "graceful close reader")
			}
			if mode != "message_eof" && !p.conn.ControlSnapshot().WorkerDone {
				t.Fatal("real transport failure did not join control")
			}
		})
	}
}

type managedLateWriter struct{ entered, release chan struct{} }

func (w managedLateWriter) WritePing(context.Context, []byte) error {
	return errors.New("unexpected ping")
}
func (w managedLateWriter) WritePong(context.Context, []byte) error {
	close(w.entered)
	<-w.release
	return nil
}

func TestManagedDataDeadlineIsNotRenewed(t *testing.T) {
	p := managedFixture(t)
	// Deliberately non-cooperative writer proves an expired D is reported,
	// not silently replaced with a fresh grace. It is released by the test.
	_ = p.conn.CloseNow()
	ctrl := NewControl(nil)
	w := managedLateWriter{make(chan struct{}), make(chan struct{})}
	if err := ctrl.Start(w); err != nil {
		t.Fatal(err)
	}
	m := &ManagedConn{Conn: p.conn.Conn, control: ctrl, grace: time.Hour, failureDone: make(chan struct{})}
	defer func() {
		select {
		case <-w.release:
		default:
			close(w.release)
		}
		ctrl.Stop()
		_ = ctrl.Join(p.ctx)
	}()
	ctrl.OnPing(p.ctx, nil)
	diagnosticWait(t, p.ctx, w.entered, "uncooperative writer active")
	deadline := time.Now().Add(-time.Millisecond)
	m.SetCloseDeadline(deadline)
	m.SetCloseDeadline(time.Now().Add(time.Hour))
	if err := m.CloseNow(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expired cleanup D renewed", err)
	}
	close(w.release)
	if err := ctrl.Join(p.ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.JoinCleanup(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("late join erased earlier miss", err)
	}
	if !m.deadline.Equal(deadline) {
		t.Fatal("original deadline changed")
	}
}

func TestManagedDataDialFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c, _, err := DialData(ctx, "ws://127.0.0.1:1", nil, time.Second); err == nil || c != nil {
		t.Fatal("failed handshake created live owner")
	}
}

func TestManagedDataTLSBlockedClose(t *testing.T) {
	for _, mode := range []string{"managed_close_bounded", "raw_close_notify_needs_socket_release"} {
		t.Run(mode, func(t *testing.T) {
			p := managedFixtureTLS(t, true)
			var read <-chan error
			if mode == "managed_close_bounded" {
				read = p.run(func() error { _, _, err := p.conn.Read(p.ctx); return err })
			}
			// The HTTP and TLS handshakes have completed. Select this next TLS
			// application Write at entry, never a returning earlier handshake.
			p.socket.allWrites.Store(true)
			p.socket.armed.Store(true)
			if mode == "managed_close_bounded" {
				if err := p.peer.WritePing(p.ctx, []byte("tls-pong")); err != nil {
					t.Fatal(err)
				}
				diagnosticWait(t, p.ctx, p.socket.entered, "TLS PONG holds output lock")
				if p.conn.ControlSnapshot().Wire != "pong" {
					t.Fatal("gate is not the PONG write")
				}
			}
			deadline := time.Now().Add(time.Second)
			p.conn.SetCloseDeadline(deadline)
			original, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			if mode == "managed_close_bounded" {
				done := p.run(p.conn.CloseNow)
				if err := diagnosticResult(t, original, done, "managed close within original D"); err != nil {
					t.Fatal(err)
				}
				if time.Now().After(deadline) {
					t.Fatal("close returned after original deadline")
				}
				if err := p.conn.JoinCleanup(); err != nil {
					t.Fatal(err)
				}
			} else {
				// With no active application Write, raw coder Close performs an
				// actual TLS close_notify. Holding that write is the negative
				// case; Go TLS already handles Close during an active app Write.
				entered := make(chan struct{})
				done := p.run(func() error { close(entered); return p.conn.Conn.CloseNow() })
				diagnosticWait(t, p.ctx, entered, "raw close invoked")
				diagnosticWait(t, p.ctx, p.socket.entered, "raw close_notify owns real TLS write")
				if p.conn.ControlSnapshot().Wire != "none" {
					t.Fatal("unexpected application control write")
				}
				select {
				case err := <-done:
					t.Fatalf("raw TLS close did not reproduce the blocked close: %v", err)
				case <-original.Done():
				}
				select {
				case <-p.socket.closed:
					t.Fatal("raw close already released underlying socket")
				default:
				}
				// The managed close releases the exact underlying socket, even
				// when the existing coder close has already claimed its once.
				_ = p.conn.CloseNow()
				_ = diagnosticResult(t, p.ctx, done, "negative raw close joined")
			}
			if read != nil {
				if err := diagnosticResult(t, p.ctx, read, "original TLS reader exits"); err == nil {
					t.Fatal("reader returned success after close")
				}
			}
		})
	}
}
