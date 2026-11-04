package client

import (
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
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type streamWriteGate struct {
	net.Conn
	armed                             atomic.Bool
	calls                             atomic.Int64
	entered, release, closed          chan struct{}
	enterOnce, releaseOnce, closeOnce sync.Once
}

func (c *streamWriteGate) Write(p []byte) (int, error) {
	if c.armed.Load() {
		c.calls.Add(1)
		c.enterOnce.Do(func() { close(c.entered) })
		select {
		case <-c.release:
		case <-c.closed:
			return 0, net.ErrClosed
		}
	}
	return c.Conn.Write(p)
}
func (c *streamWriteGate) releaseWrite() { c.releaseOnce.Do(func() { close(c.release) }) }
func (c *streamWriteGate) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func TestStreamBackpressureBounded(t *testing.T) {
	const f = 32 << 10
	const upload = 3*f + 17
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverResult := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.TunnelSubprotocol}})
		if err != nil {
			serverResult <- err
			return
		}
		defer ws.CloseNow()
		ws.SetReadLimit(f + 3)
		if err = ws.Write(ctx, websocket.MessageBinary, protocol.ReadyFrame()); err != nil {
			serverResult <- err
			return
		}
		for frame := 0; frame < 2; frame++ {
			payload := make([]byte, f)
			for i := range payload {
				payload[i] = byte(frame*f + i)
			}
			if err = ws.Write(ctx, websocket.MessageBinary, protocol.DataFrame(payload)); err != nil {
				serverResult <- err
				return
			}
		}
		if err = ws.Write(ctx, websocket.MessageBinary, protocol.FinFrame()); err != nil {
			serverResult <- err
			return
		}
		off, frames := 0, 0
		for {
			typ, p, e := ws.Read(ctx)
			if e != nil {
				serverResult <- e
				return
			}
			fr, e := protocol.DecodeFrame(p, f)
			if e != nil || typ != websocket.MessageBinary {
				serverResult <- errors.New("invalid upload frame")
				return
			}
			if fr.Type == protocol.FrameFin {
				break
			}
			if fr.Type != protocol.FrameData || len(fr.Data) > f {
				serverResult <- errors.New("upload exceeded one F frame")
				return
			}
			for _, b := range fr.Data {
				if b != byte(off) {
					serverResult <- errors.New("upload data changed")
					return
				}
				off++
			}
			frames++
		}
		if off != upload || frames != 4 {
			serverResult <- errors.New("upload was truncated or reframed beyond F")
			return
		}
		serverResult <- nil
		_ = ws.Close(websocket.StatusNormalClosure, "")
	}))
	defer srv.Close()
	var wire *streamWriteGate
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		wire = &streamWriteGate{Conn: c, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		return wire, nil
	}}
	defer tr.CloseIdleConnections()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}, Subprotocols: []string{protocol.TunnelSubprotocol}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { wire.releaseWrite(); _ = ws.CloseNow() }()
	ws.SetReadLimit(f + 1)
	if err := waitReady(ctx, ws, f); err != nil {
		t.Fatal(err)
	}
	s := &Stream{leg: ws, id: "bounded", dataMax: f, closeGrace: 100 * time.Millisecond}
	var first [1]byte
	if n, err := s.Read(first[:]); n != 1 || err != nil || first[0] != 0 {
		t.Fatal(n, err, first)
	}
	if len(s.pending) != f-1 {
		t.Fatalf("download pending=%d want F-1", len(s.pending))
	}
	// Arm only after the handshake. A Write now stops inside the actual
	// transport while the other direction retains exactly one partial frame.
	wire.armed.Store(true)
	payload := make([]byte, upload)
	for i := range payload {
		payload[i] = byte(i)
	}
	written := make(chan error, 1)
	go func() {
		n, err := s.Write(payload)
		if err == nil && n != len(payload) {
			err = io.ErrShortWrite
		}
		written <- err
	}()
	select {
	case <-wire.entered:
	case <-ctx.Done():
		t.Fatal("writer never entered transport")
	}
	if wire.calls.Load() != 1 || len(s.pending) != f-1 {
		t.Fatal("stream queued additional work while transport was blocked")
	}
	if s.writeMu.TryLock() {
		s.writeMu.Unlock()
		t.Fatal("write returned before the blocked transport")
	}
	select {
	case err := <-written:
		t.Fatalf("backpressure not applied: %v", err)
	default:
	}
	wire.releaseWrite()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("write did not resume")
	}
	if err := s.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	remaining, err := io.ReadAll(s)
	if err != nil || len(remaining) != 2*f-1 {
		t.Fatalf("remaining download %d %v", len(remaining), err)
	}
	for i, b := range remaining {
		if b != byte(i+1) {
			t.Fatalf("download differs at %d", i+1)
		}
	}
	if len(s.pending) != 0 {
		t.Fatal("pending frame retained after consumption")
	}
	_ = s.Close() // The peer may already have completed its closing handshake.
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server did not receive full upload")
	}
}
