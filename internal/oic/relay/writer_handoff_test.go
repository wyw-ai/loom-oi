package relay

import (
	"context"
	"fmt"
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
)

// handoffConn holds only the return after the real complete frame
// has been accepted by the socket. The peer must read it before replacement.
type handoffConn struct {
	net.Conn
	armed   atomic.Bool
	written chan struct{}
	release chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (c *handoffConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == nil && n == len(p) && c.armed.CompareAndSwap(true, false) {
		close(c.written)
		select {
		case <-c.release:
		case <-c.closed:
		}
	}
	return n, err
}

func (c *handoffConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func handoffWriterPair(t *testing.T, slots int) (*controlWriter, *peer, *handoffConn, func()) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	quit := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(rw, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.ControlSubprotocolV2}})
		if err != nil {
			return
		}
		accepted <- c
		<-quit
		_ = c.CloseNow()
	}))
	wrapped := make(chan *handoffConn, 1)
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		gate := &handoffConn{Conn: c, written: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		wrapped <- gate
		return gate, nil
	}}
	c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{Subprotocols: []string{protocol.ControlSubprotocolV2}, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		close(quit)
		srv.Close()
		t.Fatal(err)
	}
	p := &peer{t: t, c: <-accepted}
	gate := <-wrapped
	w := newControlWriter(context.Background(), c, 4, 16, slots, 4096, 100*time.Millisecond)
	var free sync.Once
	t.Cleanup(func() {
		free.Do(func() { close(gate.release) })
		w.stop()
		close(quit)
		srv.Close()
		transport.CloseIdleConnections()
	})
	return w, p, gate, func() { free.Do(func() { close(gate.release) }) }
}

func TestControlWriterPeerObservedBeforeWriteReturn(t *testing.T) {
	w, p, gate, free := handoffWriterPair(t, 8)
	b := protocol.ObservationBinding{AccessID: "lab-a", Epoch: 1, Generation: 1, ExposureID: "lab-a/http", Revision: "rv_" + strings.Repeat("a", 26), ObservationRevision: "ob_1", ContractDigest: strings.Repeat("b", 64), SourceID: "local", ContractVersion: 1}
	entered, built := make(chan struct{}), make(chan struct{})
	gate.armed.Store(true)
	deadline := time.Now().Add(500 * time.Millisecond) // actual negotiated budget; gate releases before expiry
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("%032x", i)
		if _, err := w.offerUntil("query:"+id, context.Background(), deadline, func(time.Time) (protocol.Message, error) {
			if id == fmt.Sprintf("%032x", 1) {
				close(entered)
				<-built
			}
			return &protocol.ObservationSnapshot{QueryID: id, Binding: b, UpdateSeq: 1, Attempt: protocol.ObservationAttempt{State: "idle"}, Readiness: protocol.ReadinessUnknown, Reason: "no_sample"}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	<-entered
	close(built)
	<-gate.written
	first := p.recv().(*protocol.ObservationSnapshot)
	if first.QueryID != fmt.Sprintf("%032x", 1) {
		t.Fatal(first.QueryID)
	}
	w.mu.Lock()
	owners, queued := len(w.queries), len(w.telemetry)
	w.mu.Unlock()
	if owners != 8 || queued != 7 || !time.Now().Before(deadline) {
		t.Fatal("bad fixture or expired before replacement", owners, queued)
	}
	// Honest peer has consumed one result: seven old requests remain. Its
	// distinct replacement restores eight outstanding requests, never nine.
	nextID := fmt.Sprintf("%032x", 9)
	p.send(&protocol.ObservationQuery{QueryID: nextID, Binding: b, DeadlineMS: 500})
	rctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := w.c.Read(rctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := protocol.DecodeControlForVersion(data, protocol.ControlSubprotocolV2)
	if err != nil {
		t.Fatal(err)
	}
	q := m.(*protocol.ObservationQuery)
	waiting := &handoffWaitContext{Context: context.Background(), entered: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, err := w.offerUntil("query:"+q.QueryID, waiting, time.Now().Add(500*time.Millisecond), func(time.Time) (protocol.Message, error) { return nil, nil })
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("replacement did not await write handoff: %v", err)
	case <-waiting.entered:
	case <-time.After(wait):
		t.Fatal("replacement did not reach handoff")
	}
	w.mu.Lock()
	owners = len(w.queries)
	w.mu.Unlock()
	if owners != 8 {
		t.Fatal("pending bound changed during handoff", owners)
	}
	critical := make(chan error, 1)
	go func() {
		critical <- w.send(context.Background(), &protocol.Hello{OICVersion: "critical-during-handoff"})
	}()
	waitFor(t, "critical capacity during handoff", func() bool { return len(w.critical) == 1 })
	free()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("legal replacement rejected", err)
		}
	case <-time.After(wait):
		t.Fatal("replacement blocked after handoff")
	}
	if m, ok := p.recv().(*protocol.Hello); !ok || m.OICVersion != "critical-during-handoff" {
		t.Fatal("critical lane was blocked", m)
	}
	if err := <-critical; err != nil {
		t.Fatal(err)
	}
}

func TestControlWriterHandoffUsesExistingDeadlines(t *testing.T) {
	for _, end := range []string{"request_expiry", "request_cancel", "write_expiry"} {
		t.Run(end, func(t *testing.T) {
			w, p, gate, free := handoffWriterPair(t, 1)
			gate.armed.Store(true)
			if _, err := w.offerUntil("query:old", context.Background(), time.Now().Add(500*time.Millisecond), func(time.Time) (protocol.Message, error) {
				return &protocol.HeartbeatAck{AckSeq: 1}, nil
			}); err != nil {
				t.Fatal(err)
			}
			<-gate.written
			_ = p.recv()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deadline := time.Now().Add(500 * time.Millisecond)
			if end == "request_expiry" {
				deadline = time.Now().Add(15 * time.Millisecond)
			}
			waiting := &handoffWaitContext{Context: ctx, entered: make(chan struct{})}
			result := make(chan error, 1)
			go func() {
				_, err := w.offerUntil("query:next", waiting, deadline, func(time.Time) (protocol.Message, error) { t.Error("expired request was built"); return nil, nil })
				result <- err
			}()
			select {
			case err := <-result:
				t.Fatalf("did not wait for the active write: %v", err)
			case <-waiting.entered:
			case <-time.After(wait):
				t.Fatal("did not enter bounded handoff")
			}
			if end == "request_cancel" {
				cancel()
			}
			select {
			case err := <-result:
				if end == "write_expiry" {
					if err == nil || protocol.CodeOf(err) == protocol.CodeProtocolError {
						t.Fatal("write expiry became success/protocol overflow", err)
					}
					if !time.Now().Before(deadline) {
						t.Fatal("wait renewed the write budget to the incoming query deadline")
					}
				} else if err != nil || w.ctx.Err() != nil {
					t.Fatal("only the new request expired but session failed", err, w.ctx.Err())
				}
			case <-time.After(wait):
				t.Fatal("handoff exceeded both fixed deadlines")
			}
			free()
			if end == "write_expiry" {
				select {
				case <-w.done:
				case <-time.After(wait):
					t.Fatal("timed out write did not terminate writer")
				}
			} else {
				waitFor(t, "expired incoming request never acquired a slot", func() bool { return queryQueueEmpty(w) })
				critical := make(chan error, 1)
				go func() { critical <- w.send(context.Background(), &protocol.Hello{OICVersion: "healthy"}) }()
				if h, ok := p.recv().(*protocol.Hello); !ok || h.OICVersion != "healthy" {
					t.Fatal("session did not remain usable", h)
				}
				if err := <-critical; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestControlWriterExpiredSelectedBuilderReleasesSlot(t *testing.T) {
	w, _ := writerPair(t, 2, 4, 1, 4096)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(25*time.Millisecond))
	defer cancel()
	if _, err := w.offerUntil("query:old", ctx, time.Now().Add(time.Hour), func(time.Time) (protocol.Message, error) { close(entered); <-release; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	<-entered
	w.mu.Lock()
	old := w.queries["query:old"]
	w.mu.Unlock()
	if old == nil {
		t.Fatal("bad fixture: builder did not own its selected slot")
	}
	<-ctx.Done() // real deadline, no socket I/O began
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatal(ctx.Err())
	}
	_, err := w.offerUntil("query:next", context.Background(), time.Now().Add(time.Hour), func(time.Time) (protocol.Message, error) { return nil, nil })
	t.Logf("selected old request cancelled before write, replacement result=%v", err)
	if err != nil {
		t.Fatalf("expired/cancelled selected owner rejects fresh request: %v", err)
	}
}

// Done signals when offerUntil is actually selecting the bounded handoff.
type handoffWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *handoffWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}
