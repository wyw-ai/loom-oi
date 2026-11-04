package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

const wait = 5 * time.Second

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// ---- fake OIS ----

type attachReq struct {
	id, ticket string
	c          *websocket.Conn
}

type fakeOIS struct {
	srv      *httptest.Server
	controls chan *websocket.Conn
	attaches chan attachReq
	reject   chan int // when non-empty, the next attach answers this status
	quit     chan struct{}
}

func newFakeOIS(t *testing.T, ca *pki.CA) *fakeOIS {
	f := &fakeOIS{controls: make(chan *websocket.Conn, 4), attaches: make(chan attachReq, 4), reject: make(chan int, 1), quit: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/access/control", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.ControlSubprotocol}})
		if err != nil {
			return
		}
		f.controls <- c
		<-f.quit
	})
	mux.HandleFunc("/v1/channels/{id}/attach", func(w http.ResponseWriter, r *http.Request) {
		select {
		case code := <-f.reject:
			w.WriteHeader(code)
			return
		default:
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.TunnelSubprotocol}})
		if err != nil {
			return
		}
		f.attaches <- attachReq{id: r.PathValue("id"), ticket: r.Header.Get(protocol.HeaderAttachTicket), c: c}
		<-f.quit
	})
	f.srv = httptest.NewUnstartedServer(mux)
	leaf := must(ca.Server("ois"))
	f.srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{must(leaf.TLS())},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: ca.Pool()}
	f.srv.StartTLS()
	t.Cleanup(func() {
		close(f.quit)
		f.srv.Close()
	})
	return f
}

type peer struct {
	t   *testing.T
	c   *websocket.Conn
	seq uint64
}

func (p *peer) send(m protocol.Message) {
	p.t.Helper()
	p.seq++
	if err := p.c.Write(context.Background(), websocket.MessageText, protocol.EncodeControl(m, p.seq)); err != nil {
		p.t.Fatal(err)
	}
}

func (p *peer) recv() protocol.Message {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	typ, data, err := p.c.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		p.t.Fatalf("control read: %v", err)
	}
	return must(protocol.DecodeControl(data))
}

func expectMsg[T protocol.Message](p *peer) T {
	p.t.Helper()
	for {
		m := p.recv()
		if v, ok := m.(T); ok {
			return v
		}
		if _, ok := m.(*protocol.HeartbeatAck); !ok {
			p.t.Fatalf("got %T %+v", m, m)
		}
	}
}

func (f *fakeOIS) control(t *testing.T) *peer {
	t.Helper()
	select {
	case c := <-f.controls:
		return &peer{t: t, c: c}
	case <-time.After(wait):
		t.Fatal("OIC did not connect")
		return nil
	}
}

func (f *fakeOIS) attach(t *testing.T) attachReq {
	t.Helper()
	select {
	case a := <-f.attaches:
		return a
	case <-time.After(wait):
		t.Fatal("OIC did not attach")
		return attachReq{}
	}
}

func legFrame(t *testing.T, c *websocket.Conn) protocol.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("leg read: %v", err)
	}
	return must(protocol.DecodeFrame(data, 1<<20))
}

func legSend(t *testing.T, c *websocket.Conn, b []byte) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		t.Fatal(err)
	}
}

// ---- targets ----

type targetSrv struct {
	ln    net.Listener
	conns chan net.Conn
}

func newTarget(t *testing.T) *targetSrv {
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	ts := &targetSrv{ln: ln, conns: make(chan net.Conn, 8)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			ts.conns <- c
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ts
}

func (ts *targetSrv) next(t *testing.T) net.Conn {
	t.Helper()
	select {
	case c := <-ts.conns:
		t.Cleanup(func() { _ = c.Close() })
		return c
	case <-time.After(wait):
		t.Fatal("target not dialed")
		return nil
	}
}

func expectRST(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 64)
	for {
		_, err := c.Read(buf)
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			t.Fatal("target saw EOF instead of a reset")
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("target saw no reset")
		}
		return
	}
}

// ---- environment ----

type env struct {
	t      *testing.T
	ois    *fakeOIS
	tgt    *targetSrv
	dir    string
	path   string
	cfg    map[string]any
	agent  *Agent
	cancel context.CancelFunc
	done   chan struct{}
	ctl    *peer
	rev    string
}

func baseConfig(t *testing.T, dir string, ca *pki.CA, ois *fakeOIS, tgt *targetSrv) map[string]any {
	leaf := must(ca.Client("lab-a"))
	if _, _, err := leaf.Write(dir, "lab-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Write(dir, "ca"); err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"access_id": "lab-a",
		"ois":       map[string]any{"url": "wss://" + ois.srv.Listener.Addr().String() + "/", "ca_file": "ca.pem"},
		"tls":       map[string]any{"cert_file": "lab-a.pem", "key_file": "lab-a-key.pem"},
		"exposures": []any{
			map[string]any{"name": "http", "protocol": "http", "target": map[string]any{"address": tgt.ln.Addr().String()}},
			map[string]any{"name": "closed", "protocol": "http", "target": map[string]any{"address": "127.0.0.1:1"}},
			map[string]any{"name": "named", "protocol": "http", "target": map[string]any{"address": "svc.test:80", "allowed_cidrs": []string{"127.0.0.0/8"}}},
			map[string]any{"name": "off", "protocol": "http", "enabled": false, "target": map[string]any{"address": tgt.ln.Addr().String()}},
		},
		"limits":      map[string]any{"license_margin_ms": 100, "close_ms": 300, "dial_ms": 1000, "max_local_channels": 2},
		"diag_listen": "127.0.0.1:0",
	}
}

func (e *env) writeConfig() {
	e.t.Helper()
	b, _ := json.Marshal(e.cfg)
	if err := os.WriteFile(e.path, b, 0o600); err != nil {
		e.t.Fatal(err)
	}
}

type resolver struct{ addrs []netip.Addr }

func (r resolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addrs, nil
}

func newEnv(t *testing.T, mod func(cfg map[string]any)) *env {
	ca := must(pki.NewCA("ca"))
	e := &env{t: t, ois: newFakeOIS(t, ca), tgt: newTarget(t), dir: t.TempDir()}
	e.path = filepath.Join(e.dir, "oic.json")
	e.cfg = baseConfig(t, e.dir, ca, e.ois, e.tgt)
	if mod != nil {
		mod(e.cfg)
	}
	e.writeConfig()
	a, err := New(Options{ConfigPath: e.path, BackoffMin: 10 * time.Millisecond, BackoffMax: 40 * time.Millisecond,
		ReloadEvery: 20 * time.Millisecond, Resolver: resolver{addrs: []netip.Addr{netip.MustParseAddr("10.9.9.9")}}})
	if err != nil {
		t.Fatal(err)
	}
	e.agent = a
	e.rev = a.cfg.Find("lab-a/http").Revision
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel, e.done = cancel, make(chan struct{})
	go func() {
		_ = a.Run(ctx)
		close(e.done)
	}()
	t.Cleanup(e.stop)
	return e
}

func (e *env) stop() {
	e.cancel()
	select {
	case <-e.done:
	case <-time.After(wait):
		e.t.Error("agent did not stop")
	}
}

// connect completes hello/welcome/register on the next control connection.
func (e *env) connect(gen, licenseMS int64, ack bool) *peer {
	e.t.Helper()
	p := e.ois.control(e.t)
	expectMsg[*protocol.Hello](p)
	p.send(&protocol.Welcome{Epoch: 1, Generation: gen, RunID: "run_x", HeartbeatIntervalMS: 1000, LicenseTTLMS: licenseMS,
		Limits: protocol.WelcomeLimits{DataMax: 32 << 10, CloseGraceMS: 300, DescMax: 2048, MaxExposures: 128}})
	reg := expectMsg[*protocol.Register](p)
	if len(reg.Exposures) != 3 || reg.Exposures[1].ExposureID != "lab-a/http" || reg.Exposures[1].Revision != e.rev {
		e.t.Fatalf("register %+v", reg.Exposures)
	}
	if ack {
		p.send(&protocol.RegisterAck{Accepted: []string{"lab-a/http"}, Rejected: []protocol.Rejection{{ExposureID: "lab-a/x", Code: protocol.CodeDenied}}})
		waitFor(e.t, "registered", func() bool { return e.agent.Stats().Registered })
	}
	e.ctl = p
	return p
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func chanID() string { return protocol.NewID("ch_") }

func (e *env) prepareMsg(exposure, rev string, gen int64) *protocol.PrepareChannel {
	return &protocol.PrepareChannel{ChannelID: chanID(), ExposureID: exposure, Revision: rev, Subject: "alice",
		Epoch: 1, Generation: gen, PrepareTTLMS: 3000, ProviderTicket: "tk_test"}
}

// open runs a prepare through READY and returns the target and leg.
func (e *env) open(gen int64) (*protocol.PrepareChannel, net.Conn, *websocket.Conn) {
	e.t.Helper()
	p := e.prepareMsg("lab-a/http", e.rev, gen)
	e.ctl.send(p)
	tc := e.tgt.next(e.t)
	a := e.ois.attach(e.t)
	if a.id != p.ChannelID || a.ticket != "tk_test" {
		e.t.Fatalf("%+v", a)
	}
	e.waitBound(p.ChannelID)
	legSend(e.t, a.c, protocol.ReadyFrame())
	return p, tc, a.c
}

// waitBound waits until the agent bound a bridge to the channel. An abort
// before that cancels the attach, so no leg exists to carry ABORT.
func (e *env) waitBound(id string) {
	e.t.Helper()
	waitFor(e.t, "bridge bound", func() bool {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		ch := e.agent.channels[id]
		return ch != nil && ch.bridge != nil
	})
}

// ---- tests ----

func TestProvideCompletes(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 5000, true)
	_, tc, leg := e.open(1)
	legSend(t, leg, protocol.DataFrame([]byte("GET /")))
	buf := make([]byte, 5)
	_ = tc.SetReadDeadline(time.Now().Add(wait))
	if _, err := io.ReadFull(tc, buf); err != nil || string(buf) != "GET /" {
		t.Fatal(string(buf), err)
	}
	legSend(t, leg, protocol.FinFrame())
	if _, err := tc.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("target expected FIN: %v", err)
	}
	_, _ = tc.Write([]byte("200 OK"))
	_ = tc.(*net.TCPConn).CloseWrite()
	if f := legFrame(t, leg); string(f.Data) != "200 OK" {
		t.Fatalf("%+v", f)
	}
	if f := legFrame(t, leg); f.Type != protocol.FrameFin {
		t.Fatalf("%+v", f)
	}
	_ = leg.Close(websocket.StatusNormalClosure, "")
	waitFor(t, "completed", func() bool { return e.agent.Stats().Closed[string(protocol.CodeCompleted)] == 1 })
	s := e.agent.Stats()
	if s.Channels != 0 || s.RelayBytes != 11 || !s.Connected || s.Generation != 1 || s.Epoch != 1 || s.LicenseRemainingMS <= 0 || s.AccessID != "lab-a" {
		t.Fatalf("%+v", s)
	}
	// The diagnostics endpoint serves the same counters on loopback.
	res := must(http.Get("http://" + e.agent.DiagAddr() + "/stats"))
	defer res.Body.Close()
	var ds protocol.OICStats
	if err := json.NewDecoder(res.Body).Decode(&ds); err != nil || ds.RelayBytes != 11 {
		t.Fatalf("%+v %v", ds, err)
	}
}

func TestPrepareRefused(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 5000, false)
	// TV-B1-17: before register_ack nothing is dialed.
	early := e.prepareMsg("lab-a/http", e.rev, 1)
	p.send(early)
	if pf := expectMsg[*protocol.PrepareFailed](p); pf.ChannelID != early.ChannelID || pf.Code != protocol.CodeProtocolError {
		t.Fatalf("%+v", pf)
	}
	p.send(&protocol.RegisterAck{Accepted: []string{}, Rejected: []protocol.Rejection{}})
	waitFor(t, "registered", func() bool { return e.agent.Stats().Registered })
	cases := []struct {
		msg  *protocol.PrepareChannel
		code protocol.Code
	}{
		{e.prepareMsg("lab-a/http", e.rev, 7), protocol.CodeProtocolError},
		{func() *protocol.PrepareChannel { m := e.prepareMsg("lab-a/http", e.rev, 1); m.Epoch = 9; return m }(), protocol.CodeProtocolError},
		{e.prepareMsg("lab-a/none", e.rev, 1), protocol.CodeLocalDenied},
		{e.prepareMsg("lab-a/http", "rv_"+strings.Repeat("z", 26), 1), protocol.CodeLocalDenied},
		{e.prepareMsg("lab-a/off", e.agent.cfg.Find("lab-a/off").Revision, 1), protocol.CodeLocalDenied},
	}
	for _, c := range cases {
		p.send(c.msg)
		if pf := expectMsg[*protocol.PrepareFailed](p); pf.Code != c.code {
			t.Errorf("%+v: %+v", c.msg, pf)
		}
	}
	select {
	case <-e.tgt.conns:
		t.Fatal("a refused prepare dialed the target")
	default:
	}
	// MAX_LOCAL_CHANNELS=2: the third concurrent prepare is refused.
	first, _, _ := e.open(1)
	e.open(1)
	p.send(e.prepareMsg("lab-a/http", e.rev, 1))
	if pf := expectMsg[*protocol.PrepareFailed](p); pf.Code != protocol.CodeResourceExhausted {
		t.Fatalf("%+v", pf)
	}
	p.send(first) // a duplicate channel ID
	if pf := expectMsg[*protocol.PrepareFailed](p); pf.Code != protocol.CodeProtocolError {
		t.Fatalf("%+v", pf)
	}
}

func TestDialFailures(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 5000, true)
	for exp, code := range map[string]protocol.Code{
		"lab-a/closed": protocol.CodeTargetConnectFailed,
		"lab-a/named":  protocol.CodeTargetAddressNotAllowed,
	} {
		p.send(e.prepareMsg(exp, e.agent.cfg.Find(exp).Revision, 1))
		if pf := expectMsg[*protocol.PrepareFailed](p); pf.Code != code {
			t.Errorf("%s: %+v", exp, pf)
		}
	}
	// The OIS refuses the provider leg: the dialed target is reset.
	e.ois.reject <- http.StatusGone
	p.send(e.prepareMsg("lab-a/http", e.rev, 1))
	expectRST(t, e.tgt.next(t))
	waitFor(t, "dropped", func() bool { return e.agent.Stats().Channels == 0 })
}

func TestCloseChannel(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 5000, true)
	// close_channel while waiting for READY.
	m := e.prepareMsg("lab-a/http", e.rev, 1)
	p.send(m)
	tc := e.tgt.next(t)
	a := e.ois.attach(t)
	e.waitBound(m.ChannelID)
	p.send(&protocol.CloseChannel{ChannelID: m.ChannelID, Code: protocol.CodePrepareTimeout})
	if f := legFrame(t, a.c); f.Type != protocol.FrameAbort || f.Reason != protocol.CodePrepareTimeout {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
	// close_channel for an open channel, an unknown one, and a bogus code.
	_, tc, leg := e.open(1)
	p.send(&protocol.CloseChannel{ChannelID: chanID(), Code: protocol.CodeRevoked})
	p.send(&protocol.CloseChannel{ChannelID: "ch_" + strings.Repeat("q", 26), Code: "Bogus"})
	waitFor(t, "two channels", func() bool { return e.agent.Stats().Channels == 1 })
	e.agent.mu.Lock()
	var id string
	for k := range e.agent.channels {
		id = k
	}
	e.agent.mu.Unlock()
	p.send(&protocol.CloseChannel{ChannelID: id, Code: protocol.CodeRevoked})
	if f := legFrame(t, leg); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeRevoked {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
	waitFor(t, "revoked counted", func() bool { return e.agent.Stats().Closed[string(protocol.CodeRevoked)] == 1 })
}

func TestAbortDuringDial(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 5000, true)
	ch := &channel{id: "ch_x", cancel: func() {}}
	e.agent.mu.Lock()
	e.agent.channels[ch.id] = ch
	e.agent.mu.Unlock()
	e.agent.abort(ch.id, protocol.CodeRevoked)
	e.agent.abort(ch.id, protocol.CodeShutdown) // a second abort keeps the first reason
	if ch.aborted != protocol.CodeRevoked || e.agent.drop(ch) {
		t.Fatalf("aborted %q", ch.aborted)
	}
}

// B2 §5 / TV-B2-10/11: without heartbeats the license expires and every
// channel of the generation is aborted; nothing is relayed afterwards.
func TestLicenseExpiry(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 700, true)
	p.send(&protocol.Heartbeat{LicenseTTLMS: 700})
	if ack := expectMsg[*protocol.HeartbeatAck](p); ack.AckSeq != p.seq {
		t.Fatalf("%+v", ack)
	}
	_, tc, leg := e.open(1)
	if f := legFrame(t, leg); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeLicenseExpired {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
	p.send(e.prepareMsg("lab-a/http", e.rev, 1))
	if pf := expectMsg[*protocol.PrepareFailed](p); pf.Code != protocol.CodeLicenseExpired {
		t.Fatalf("%+v", pf)
	}
	if s := e.agent.Stats(); s.LicenseRemainingMS != 0 {
		t.Fatalf("%+v", s)
	}
}

// TV-B2-11: after a pause past L the heartbeats OIS sent meanwhile arrive
// late. Reading one cannot revive the channels: the lapse is handled first
// and only later channels run under the renewed license (B2 §5).
func TestLateHeartbeatAfterLapse(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 5000, true)
	_, tc, leg := e.open(1)
	// The deadline passed while the process was stopped; the timer has not
	// run yet.
	e.agent.license.Store(&license{gen: 1, deadline: time.Now().Add(-time.Second)})
	p.send(&protocol.Heartbeat{LicenseTTLMS: 5000})
	if f := legFrame(t, leg); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeLicenseExpired {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
	expectMsg[*protocol.HeartbeatAck](p)
	waitFor(t, "expiry recorded", func() bool { return e.agent.Stats().Closed[string(protocol.CodeLicenseExpired)] == 1 })
	if code := e.agent.allowFor(1)(); code != "" {
		t.Fatalf("license not renewed: %s", code)
	}
}

func TestReconnectAbortsOldGeneration(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 5000, true)
	_, tc, leg := e.open(1)
	_ = p.c.CloseNow()
	p2 := e.ois.control(t)
	if h := expectMsg[*protocol.Hello](p2); h.LastEpochSeen != 1 || !strings.HasPrefix(h.OICVersion, "oic/") {
		t.Fatalf("%+v", h)
	}
	if e.agent.Stats().Channels != 1 {
		t.Fatal("channel must survive the control loss until the license expires")
	}
	p2.send(&protocol.Welcome{Epoch: 1, Generation: 2, HeartbeatIntervalMS: 1000, LicenseTTLMS: 5000, Limits: protocol.WelcomeLimits{DataMax: 32 << 10}})
	expectMsg[*protocol.Register](p2)
	if f := legFrame(t, leg); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeChannelInterrupted {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
}

func TestControlProtocolErrors(t *testing.T) {
	e := newEnv(t, nil)
	// The first message must be welcome.
	p := e.ois.control(t)
	expectMsg[*protocol.Hello](p)
	p.send(&protocol.Heartbeat{LicenseTTLMS: 1})
	if ce := expectMsg[*protocol.ControlError](p); ce.Code != protocol.CodeProtocolError {
		t.Fatalf("%+v", ce)
	}
	// Unexpected messages and binary frames end the session with an error.
	p = e.connect(1, 5000, true)
	p.send(&protocol.Hello{})
	if ce := expectMsg[*protocol.ControlError](p); ce.Code != protocol.CodeProtocolError {
		t.Fatalf("%+v", ce)
	}
	p = e.connect(2, 5000, true)
	_ = p.c.Write(context.Background(), websocket.MessageBinary, []byte("{}"))
	if ce := expectMsg[*protocol.ControlError](p); ce.Code != protocol.CodeProtocolError {
		t.Fatalf("%+v", ce)
	}
	// An error from OIS ends the session; the agent reconnects.
	p = e.connect(3, 5000, true)
	p.send(&protocol.ControlError{Code: protocol.CodeSuperseded})
	p = e.connect(4, 5000, true)
	_ = p.c.CloseNow()
	// A connection that never answers hello is retried.
	p = e.ois.control(t)
	expectMsg[*protocol.Hello](p)
	_ = p.c.CloseNow()
	e.connect(5, 5000, true)
}

func TestReload(t *testing.T) {
	e := newEnv(t, nil)
	p := e.connect(1, 5000, true)
	_, tc, leg := e.open(1)
	// Description changes keep channels; the snapshot is re-registered.
	exps := e.cfg["exposures"].([]any)
	exps[0].(map[string]any)["description"] = "renamed"
	e.writeConfig()
	reg := expectMsg[*protocol.Register](p)
	if reg.Exposures[1].Description != "renamed" || e.agent.Stats().Channels != 1 {
		t.Fatalf("%+v", reg.Exposures)
	}
	// Re-targeting aborts bound channels with RevisionChanged (B2 §4).
	other := newTarget(t)
	exps[0].(map[string]any)["target"] = map[string]any{"address": other.ln.Addr().String()}
	e.writeConfig()
	reg = expectMsg[*protocol.Register](p)
	if reg.Exposures[1].Revision == e.rev {
		t.Fatal("revision unchanged")
	}
	if f := legFrame(t, leg); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeRevisionChanged {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
	// Invalid files and runtime changes keep the running configuration.
	if err := os.WriteFile(e.path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "config error", func() bool { return e.agent.Stats().ConfigError != "" })
	e.cfg["access_id"] = "lab-z"
	e.writeConfig()
	waitFor(t, "restart required", func() bool { return strings.Contains(e.agent.Stats().ConfigError, "restart") })
	if err := os.Remove(e.path); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "missing file", func() bool { return !strings.Contains(e.agent.Stats().ConfigError, "restart") })
	if e.agent.Stats().AccessID != "lab-a" {
		t.Fatal("access changed by reload")
	}
}

func TestShutdownAbortsChannels(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 5000, true)
	_, tc, leg := e.open(1)
	e.stop()
	if f := legFrame(t, leg); f.Type != protocol.FrameAbort || f.Reason != protocol.CodeShutdown {
		t.Fatalf("%+v", f)
	}
	expectRST(t, tc)
}

func TestNewErrors(t *testing.T) {
	ca := must(pki.NewCA("ca"))
	dir := t.TempDir()
	ois := newFakeOIS(t, ca)
	cfg := baseConfig(t, dir, ca, ois, newTarget(t))
	write := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		p := filepath.Join(dir, "oic.json")
		_ = os.WriteFile(p, b, 0o600)
		return p
	}
	if _, err := New(Options{ConfigPath: filepath.Join(dir, "none.json")}); err == nil {
		t.Error("missing config")
	}
	bad := map[string]map[string]any{
		"cert":  {"tls": map[string]any{"cert_file": "none.pem", "key_file": "none.pem"}},
		"ca":    {"ois": map[string]any{"url": "wss://127.0.0.1:1", "ca_file": "none.pem"}},
		"empty": {"ois": map[string]any{"url": "wss://127.0.0.1:1", "ca_file": "lab-a-key.pem"}},
	}
	for name, mod := range bad {
		m := map[string]any{}
		for k, v := range cfg {
			m[k] = v
		}
		for k, v := range mod {
			m[k] = v
		}
		if _, err := New(Options{ConfigPath: write(m)}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	// A diag address already in use stops Run.
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	defer ln.Close()
	cfg["diag_listen"] = ln.Addr().String()
	a := must(New(Options{ConfigPath: write(cfg)}))
	if err := a.Run(context.Background()); err == nil {
		t.Error("busy diag address accepted")
	}
}

func TestMainCommand(t *testing.T) {
	run := func(args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		return Main(context.Background(), args, &out, &errb), out.String(), errb.String()
	}
	if code, _, _ := run(); code != 2 {
		t.Fatal(code)
	}
	if code, out, _ := run("help"); code != 0 || !strings.Contains(out, "check") {
		t.Fatal(code, out)
	}
	if code, out, _ := run("version"); code != 0 || !strings.HasPrefix(out, "oic dev") {
		t.Fatal(out)
	}
	if code, _, _ := run("nope"); code != 2 {
		t.Fatal(code)
	}
	for _, args := range [][]string{{"run"}, {"check", "--config", "x", "y"}, {"run", "--bogus"}} {
		if code, _, _ := run(args...); code != 2 {
			t.Errorf("%v: %d", args, code)
		}
	}
	ca := must(pki.NewCA("ca"))
	dir := t.TempDir()
	cfg := baseConfig(t, dir, ca, newFakeOIS(t, ca), newTarget(t))
	b, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "oic.json")
	_ = os.WriteFile(path, b, 0o600)
	code, out, _ := run("check", "--config", path)
	var list []checked
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 4 || list[0].ExposureID != "lab-a/http" || list[3].Enabled {
		t.Fatal(code, out)
	}
	if code, _, _ := run("check", "--config", filepath.Join(dir, "none.json")); code != 1 {
		t.Fatal(code)
	}
	if code, _, _ := run("run", "--config", filepath.Join(dir, "none.json")); code != 2 {
		t.Fatal(code)
	}
	bad := map[string]any{}
	for k, v := range cfg {
		bad[k] = v
	}
	bad["tls"] = map[string]any{"cert_file": "none.pem", "key_file": "none.pem"}
	b, _ = json.Marshal(bad)
	badPath := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badPath, b, 0o600)
	if code, _, _ := run("run", "--config", badPath); code != 1 {
		t.Fatal(code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	old := notifyContext
	notifyContext = func(context.Context) (context.Context, context.CancelFunc) { return ctx, cancel }
	defer func() { notifyContext = old }()
	done := make(chan int)
	var errb syncBuffer
	go func() { done <- Main(context.Background(), []string{"run", "--config", path}, io.Discard, &errb) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code, errb.String())
		}
	case <-time.After(wait):
		t.Fatal("run did not stop")
	}
	if _, stop := old(context.Background()); stop != nil {
		stop()
	}
	busy := map[string]any{}
	for k, v := range cfg {
		busy[k] = v
	}
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	defer ln.Close()
	busy["diag_listen"] = ln.Addr().String()
	b, _ = json.Marshal(busy)
	_ = os.WriteFile(badPath, b, 0o600)
	if code, _, _ := run("run", "--config", badPath); code != 1 {
		t.Fatal("busy diag")
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
