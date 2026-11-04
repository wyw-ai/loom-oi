package integration

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

// ctrlPeer is a raw oi.control.v1 endpoint: a fake OIC talking to the real
// OIS, or a fake OIS talking to the real OIC.
type ctrlPeer struct {
	t   *testing.T
	c   *websocket.Conn
	seq uint64
}

func (p *ctrlPeer) send(m protocol.Message) {
	p.t.Helper()
	p.seq++
	if err := p.c.Write(bg, websocket.MessageText, protocol.EncodeControl(m, p.seq)); err != nil {
		p.t.Fatal(err)
	}
}

func (p *ctrlPeer) sendRaw(s string) {
	p.t.Helper()
	p.seq++
	if err := p.c.Write(bg, websocket.MessageText, []byte(s)); err != nil {
		p.t.Fatal(err)
	}
}

func (p *ctrlPeer) recv() protocol.Message {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	typ, data, err := p.c.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		p.t.Fatalf("control read: %v", err)
	}
	return must(protocol.DecodeControl(data))
}

func (p *ctrlPeer) close() { _ = p.c.CloseNow() }

// expectCtl skips heartbeats and returns the next message of type T.
func expectCtl[T protocol.Message](t *testing.T, p *ctrlPeer) T {
	t.Helper()
	for {
		m := p.recv()
		if v, ok := m.(T); ok {
			return v
		}
		switch m.(type) {
		case *protocol.Heartbeat, *protocol.HeartbeatAck:
		default:
			t.Fatalf("got %T %+v", m, m)
		}
	}
}

func certClient(ca *pki.CA, leaf *pki.Leaf) *http.Client {
	tc := &tls.Config{RootCAs: ca.Pool()}
	if leaf != nil {
		tc.Certificates = []tls.Certificate{must(leaf.TLS())}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: tc, DisableKeepAlives: true}}
}

// fakeOIC creates an Access and opens a raw control connection for it.
func (e *env) fakeOIC(access string) *ctrlPeer {
	e.t.Helper()
	leaf := must(e.ca.Client(access))
	e.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: access, SPKIPins: []string{identity.SPKIPin(leaf.Cert)}})
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "wss://"+e.acc.addr()+"/v1/access/control", &websocket.DialOptions{
		HTTPClient: certClient(e.ca, leaf), Subprotocols: []string{protocol.ControlSubprotocol}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = c.CloseNow() })
	return &ctrlPeer{t: e.t, c: c}
}

// fakeOIS accepts control connections from a real OIC (TV-B1-17).
type fakeOIS struct {
	srv      *httptest.Server
	controls chan *websocket.Conn
	quit     chan struct{}
}

func newFakeOIS(t *testing.T, e *env) *fakeOIS {
	f := &fakeOIS{controls: make(chan *websocket.Conn, 4), quit: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/access/control", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{protocol.ControlSubprotocol}})
		if err != nil {
			return
		}
		f.controls <- c
		<-f.quit
	})
	f.srv = httptest.NewUnstartedServer(mux)
	leaf := must(e.ca.Server("fake-ois"))
	f.srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{must(leaf.TLS())},
		ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: e.ca.Pool()}
	f.srv.StartTLS()
	t.Cleanup(func() {
		close(f.quit)
		f.srv.Close()
	})
	return f
}

func (f *fakeOIS) control(t *testing.T) *ctrlPeer {
	t.Helper()
	select {
	case c := <-f.controls:
		return &ctrlPeer{t: t, c: c}
	case <-time.After(15 * time.Second):
		t.Fatal("OIC did not connect to the fake OIS")
		return nil
	}
}

// runOIC starts a real OIC whose OIS is the fake.
func (f *fakeOIS) runOIC(t *testing.T, e *env, access string, exps ...any) *proc {
	t.Helper()
	leaf := must(e.ca.Client(access))
	if _, _, err := leaf.Write(e.dir, access); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"access_id": access,
		"ois":       map[string]any{"url": "wss://" + f.srv.Listener.Addr().String(), "ca_file": "ca.pem"},
		"tls":       map[string]any{"cert_file": access + ".pem", "key_file": access + "-key.pem"},
		"exposures": exps, "limits": e.limits, "diag_listen": "127.0.0.1:0",
	}
	path := filepath.Join(e.dir, access+".json")
	writeJSON(t, path, cfg)
	p := startProc(t, e.dir, "oic-"+access, oicBin, nil, "run", "--config", path)
	l, _ := p.waitLog(t, 20*time.Second, "diagnostics listening", 0)
	e.oics[access] = &oicProc{proc: p, access: access, path: path, cfg: cfg, diag: l["addr"].(string), leaf: leaf, ctrl: -1}
	return p
}
