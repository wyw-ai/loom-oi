package client

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/internal/tunnel"
	"github.com/wyw-ai/loom-oi/tests/fixture"
	"github.com/wyw-ai/loom-oi/tests/topo"
)

type world struct {
	tp    *topo.Topology
	t1    *fixture.Target
	reset *fixture.Target
	token string
	c     *Client
}

func newWorld(t *testing.T) *world {
	t.Helper()
	tp := topo.New(t, topo.Options{Limits: map[string]any{"close_ms": 300}})
	w := &world{tp: tp, t1: fixture.New(t, "T1", fixture.Identify), reset: fixture.New(t, "T3", fixture.ResetMidway)}
	tp.StartOIC("lab-a", topo.Exposure{Name: "http", Address: w.t1.Addr()}, topo.Exposure{Name: "reset", Address: w.reset.Addr()})
	tp.Grant("p1", "alice", "lab-a/*")
	w.token = tp.Token("alice")
	c, err := New(Options{URL: tp.URL(), Token: w.token, CAFile: tp.CAFile, CloseGrace: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	w.c = c
	return w
}

func (w *world) waitState(t *testing.T, id string, reason protocol.Code) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, err := w.c.Channel(context.Background(), id)
		if err == nil && v.State == protocol.StateClosed && v.CloseReason == reason {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("channel %s: %+v %v", id, v, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStreamRoundTrip(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	all, err := w.c.DiscoverAll(ctx)
	if err != nil || len(all) != 2 || !all[0].AccessOnline {
		t.Fatalf("%+v %v", all, err)
	}
	if page, err := w.c.Discover(ctx, 1, ""); err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("%+v %v", page, err)
	}
	s, err := w.c.Dial(ctx, "lab-a/http")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("GET /")); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseWrite(); !errors.Is(err, errWriteClosed) {
		t.Fatalf("second FIN: %v", err)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, errWriteClosed) {
		t.Fatalf("write after FIN: %v", err)
	}
	got, err := io.ReadAll(s)
	if err != nil || string(got) != "T1:GET /" {
		t.Fatalf("%q %v", got, err)
	}
	if n, err := s.Read(make([]byte, 1)); n != 0 || err != io.EOF {
		t.Fatal(n, err)
	}
	_ = s.Close()
	w.waitState(t, s.ChannelID(), protocol.CodeCompleted)
	w.c.CloseIdleConnections()
}

func TestStreamAborts(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	// TV-B1-08 shape: the target resets mid-answer; the stream reports it.
	s, err := w.c.Dial(ctx, "lab-a/reset")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Write([]byte("q"))
	_ = s.CloseWrite()
	_, err = io.ReadAll(s)
	var ae *AbortError
	if !errors.As(err, &ae) || ae.Code != protocol.CodeChannelInterrupted || !strings.Contains(ae.Error(), "ChannelInterrupted") {
		t.Fatalf("%v", err)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, errWriteClosed) {
		t.Fatalf("write after abort: %v", err)
	}
	_ = s.Close()
	// Closing early aborts with ClosedByClient.
	s, err = w.c.Dial(ctx, "lab-a/http")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	w.waitState(t, s.ChannelID(), protocol.CodeClosedByClient)
	if (&AbortError{Code: protocol.CodeRevoked}).Error() != "channel aborted: Revoked" {
		t.Fatal("error text")
	}
}

func TestOpenAndAttachErrors(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.c.Dial(ctx, "lab-a/none"); protocol.CodeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	o, err := w.c.OpenLatest(ctx, "lab-a/http", "req-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.c.AttachLeg(ctx, protocol.OpenResponse{ChannelID: "x"}); protocol.CodeOf(err) != protocol.CodeTicketConsumed {
		t.Fatal(err)
	}
	evil := o
	evil.Attach = &protocol.Attach{URL: "wss://evil.example:1/v1/channels/x/attach", Ticket: o.Attach.Ticket}
	if _, err := w.c.AttachLeg(ctx, evil); protocol.CodeOf(err) != protocol.CodeInvalidRequest {
		t.Fatal(err)
	}
	if _, err := w.c.Close(ctx, o.ChannelID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.c.Attach(ctx, o); protocol.CodeOf(err) != protocol.CodeChannelGone {
		t.Fatalf("attach after close: %v", err)
	}
	if _, err := w.c.OpenLatest(ctx, "lab-a/http", "", true); protocol.CodeOf(err) != protocol.CodeReadinessRequired {
		t.Fatal(err)
	}
	bad, _ := New(Options{URL: w.tp.URL(), Token: "oi_0000000000000000_" + strings.Repeat("A", 43), CAFile: w.tp.CAFile})
	if _, err := bad.DiscoverAll(ctx); protocol.CodeOf(err) != protocol.CodeUnauthenticated {
		t.Fatal(err)
	}
	if _, err := bad.Dial(ctx, "lab-a/http"); protocol.CodeOf(err) != protocol.CodeUnauthenticated {
		t.Fatal(err)
	}
	// The channel is aborted before READY: Attach reports the reason.
	o, err = w.c.OpenLatest(ctx, "lab-a/http", "", false)
	if err != nil {
		t.Fatal(err)
	}
	leg, err := w.c.AttachLeg(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	defer leg.CloseNow()
	if _, err := w.c.Close(ctx, o.ChannelID); err != nil {
		t.Fatal(err)
	}
	var ae *AbortError
	if err := waitReady(ctx, leg, w.c.dataMax); !errors.As(err, &ae) || (ae.Code != protocol.CodeClosedByClient && ae.Code != protocol.CodeChannelInterrupted) {
		t.Fatalf("%v", err)
	}
}

func TestForwardAndListen(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan tunnel.Result, 4)
	addrc := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- w.c.Listen(ctx, "127.0.0.1:0", "lab-a/http", func(a net.Addr) { addrc <- a },
			func(r tunnel.Result, err error) { results <- r })
	}()
	addr := <-addrc
	conn, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("hello"))
	_ = conn.(*net.TCPConn).CloseWrite()
	got, err := io.ReadAll(conn)
	if err != nil || string(got) != "T1:hello" {
		t.Fatalf("%q %v", got, err)
	}
	_ = conn.Close()
	if r := <-results; r.Code != protocol.CodeCompleted || r.Up != 5 || r.Down != 8 {
		t.Fatalf("%+v", r)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"0.0.0.0:0", "example.com:1", "nope"} {
		if err := w.c.Listen(context.Background(), addr, "lab-a/http", nil, nil); err == nil {
			t.Errorf("%s accepted", addr)
		}
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	if err := w.c.Listen(context.Background(), ln.Addr().String(), "lab-a/http", nil, nil); err == nil {
		t.Error("busy address accepted")
	}
	// Forward reports Open and attach failures and resets the local side.
	a, b := tcpPair(t)
	if _, err := w.c.Forward(context.Background(), a, "lab-a/none"); protocol.CodeOf(err) != protocol.CodeNotFound {
		t.Fatal(err)
	}
	_ = b.Close()
}

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		ch <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return c.(*net.TCPConn), (<-ch).(*net.TCPConn)
}

// fakeAPI answers API calls from a script, for edge cases a real OIS
// cannot produce on demand.
func fakeAPI(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pemOf(srv.Certificate().Raw)
	if err := os.WriteFile(ca, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{URL: srv.URL, Token: "tok", CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func pemOf(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestRevisionRediscovery(t *testing.T) {
	rev := 0
	opens := 0
	c := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/exposures":
			rev++
			_ = json.NewEncoder(w).Encode(protocol.ExposureList{Items: []protocol.ExposureView{{ExposureID: "a/b", Revision: "rv_" + strings.Repeat(string(rune('a'+rev)), 26)}}})
		case "/v1/channels":
			opens++
			if opens < 3 {
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(protocol.ErrorBody{Code: protocol.CodeRevisionMismatch})
				return
			}
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(protocol.OpenResponse{ChannelID: "ch_x"})
		case "/v1/odd":
			w.WriteHeader(500)
			_, _ = w.Write([]byte("not json"))
		case "/v1/garbled":
			_, _ = w.Write([]byte("{"))
		}
	})
	ctx := context.Background()
	if _, err := c.OpenLatest(ctx, "a/b", "", false); protocol.CodeOf(err) != protocol.CodeRevisionMismatch || opens != 2 {
		t.Fatalf("%v after %d opens", err, opens)
	}
	if o, err := c.OpenLatest(ctx, "a/b", "", false); err != nil || o.ChannelID != "ch_x" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := c.Do(ctx, "GET", "/v1/odd", "", nil, nil); protocol.CodeOf(err) != protocol.CodeInternal || !strings.Contains(err.Error(), "500") {
		t.Fatal(err)
	}
	var v map[string]any
	if _, err := c.Do(ctx, "GET", "/v1/garbled", "", nil, &v); err == nil {
		t.Fatal("garbled JSON accepted")
	}
	if _, err := c.Do(ctx, "GET", "/v1/x", "", make(chan int), nil); err == nil {
		t.Fatal("unencodable body accepted")
	}
	if _, err := c.Do(ctx, "\n", "/v1/x", "", nil, nil); err == nil {
		t.Fatal("bad method accepted")
	}
	c.base.Host = "127.0.0.1:1"
	if _, err := c.Do(ctx, "GET", "/v1/x", "", nil, nil); err == nil {
		t.Fatal("unreachable server accepted")
	}
	if _, err := c.AttachLeg(ctx, protocol.OpenResponse{Attach: &protocol.Attach{URL: "wss://127.0.0.1:1/v1/channels/x/attach"}}); err == nil {
		t.Fatal("unreachable attach accepted")
	}
}

func TestNewValidation(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	_ = os.WriteFile(empty, []byte("x"), 0o600)
	for _, o := range []Options{
		{URL: "http://x:1", Token: "t"},
		{URL: "https://x:1/path", Token: "t"},
		{URL: "https://x:1"},
		{URL: "https://x:1", Token: "t", CAFile: filepath.Join(dir, "none.pem")},
		{URL: "https://x:1", Token: "t", CAFile: empty},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("%+v accepted", o)
		}
	}
	c, err := New(Options{URL: "https://x:1/", Token: "t"})
	if err != nil || c.dataMax != 32<<10 || c.base.String() != "https://x:1" {
		t.Fatal(c, err)
	}
}
