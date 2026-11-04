package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

type testServer struct {
	t      *testing.T
	dir    string
	path   string
	cfg    Config
	srv    *Server
	ca     *pki.CA
	admin  string
	client *http.Client
}

// must panics on error; test setup failures need no recovery.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// writeConfig creates certificates and a configuration file in dir.
func writeConfig(t *testing.T, dir string, ca *pki.CA, extra map[string]any) string {
	t.Helper()
	leaf := must(ca.Server("ois"))
	if _, _, err := leaf.Write(dir, "server"); err != nil {
		t.Fatal(err)
	}
	if _, err := ca.Write(dir, "client-ca"); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"api_listen": "127.0.0.1:0", "access_listen": "127.0.0.1:0",
		"tls":    map[string]string{"cert_file": "server.pem", "key_file": "server-key.pem", "client_ca_file": "client-ca.pem"},
		"store":  map[string]string{"driver": "sqlite", "sqlite_path": "ois.db"},
		"limits": map[string]any{"prepare_ms": 2000, "close_ms": 1000, "retain_ms": 5000, "heartbeat_ms": 1000, "store_probe_ms": 500},
	}
	for k, v := range extra {
		if v == nil {
			delete(cfg, k)
		} else {
			cfg[k] = v
		}
	}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "ois.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runMain(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Main(context.Background(), args, &out, &errb)
	return code, out.String(), errb.String()
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{t: t, dir: t.TempDir(), ca: must(pki.NewCA("test-ca"))}
	ts.path = writeConfig(t, ts.dir, ts.ca, nil)
	code, out, errOut := runMain("init", "--config", ts.path, "--subject", "root")
	if code != 0 {
		t.Fatalf("init: %d %s", code, errOut)
	}
	var tok protocol.TokenCreated
	if err := json.Unmarshal([]byte(out), &tok); err != nil || tok.Scope != protocol.ScopeAdmin || tok.Subject != "root" {
		t.Fatalf("init output %q: %v", out, err)
	}
	ts.admin = tok.Token
	ts.cfg = must(LoadConfig(ts.path))
	ts.srv = must(Start(context.Background(), ts.cfg, slog.New(slog.DiscardHandler)))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		ts.srv.Shutdown(ctx)
	})
	ts.client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ts.ca.Pool()}}}
	return ts
}

func (ts *testServer) url(path string) string { return "https://" + ts.srv.APIAddr() + path }

type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r resp) code(t *testing.T) protocol.Code {
	t.Helper()
	var e protocol.ErrorBody
	r.decode(t, &e)
	return e.Code
}

func (ts *testServer) do(method, path, token string, body any, header ...string) resp {
	ts.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req := must(http.NewRequest(method, ts.url(path), rd))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	res := must(ts.client.Do(req))
	defer res.Body.Close()
	return resp{status: res.StatusCode, header: res.Header, body: must(io.ReadAll(res.Body))}
}

func (ts *testServer) version() string {
	ts.t.Helper()
	var cv protocol.ConfigView
	r := ts.do("GET", "/v1/admin/config", ts.admin, nil)
	r.decode(ts.t, &cv)
	if r.header.Get("ETag") != strconv.Quote(strconv.FormatInt(cv.ConfigVersion, 10)) {
		ts.t.Fatalf("ETag %q for %+v", r.header.Get("ETag"), cv)
	}
	return strconv.Quote(strconv.FormatInt(cv.ConfigVersion, 10))
}

// write performs an admin write with the current If-Match.
func (ts *testServer) write(method, path string, body any, want int) resp {
	ts.t.Helper()
	r := ts.do(method, path, ts.admin, body, "If-Match", ts.version())
	if r.status != want {
		ts.t.Fatalf("%s %s: %d %s", method, path, r.status, r.body)
	}
	return r
}

func (ts *testServer) consumerToken(subject string) string {
	ts.t.Helper()
	var c protocol.TokenCreated
	ts.write("POST", "/v1/admin/tokens", protocol.TokenCreate{Subject: subject, Scope: protocol.ScopeConsumer}, 201).decode(ts.t, &c)
	return c.Token
}

// oicClient dials the access listener with a client certificate.
func (ts *testServer) oicClient(leaf *pki.Leaf) *http.Client {
	cert := must(leaf.TLS())
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ts.ca.Pool(), Certificates: []tls.Certificate{cert}}}}
}

func (ts *testServer) dialWS(client *http.Client, url string, header http.Header, protos ...string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: client, HTTPHeader: header, Subprotocols: protos})
}

type ctrl struct {
	t   *testing.T
	c   *websocket.Conn
	seq uint64
}

func (c *ctrl) send(m protocol.Message) {
	c.t.Helper()
	c.seq++
	if err := c.c.Write(context.Background(), websocket.MessageText, protocol.EncodeControl(m, c.seq)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *ctrl) recv() protocol.Message {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data := mustT2(c.t)(c.c.Read(ctx))
	m := must(protocol.DecodeControl(data))
	return m
}

func mustT2(t *testing.T) func(websocket.MessageType, []byte, error) (websocket.MessageType, []byte) {
	return func(typ websocket.MessageType, b []byte, err error) (websocket.MessageType, []byte) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return typ, b
	}
}

func frame(t *testing.T, c *websocket.Conn) protocol.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data := mustT2(t)(c.Read(ctx))
	if typ != websocket.MessageBinary {
		t.Fatalf("text frame %q", data)
	}
	return must(protocol.DecodeFrame(data, 1<<20))
}

func sendFrame(t *testing.T, c *websocket.Conn, b []byte) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		t.Fatal(err)
	}
}

type world struct {
	*testServer
	leaf     *pki.Leaf
	oic      *ctrl
	consumer string
	rev      string
}

func newWorld(t *testing.T) *world {
	ts := newTestServer(t)
	leaf := must(ts.ca.Client("lab-a"))
	pin := identity.SPKIPin(leaf.Cert)
	ts.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: "lab-a", SPKIPins: []string{pin}}, 201)
	ts.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "p1", Subject: "alice", ExposureSelector: "lab-a/*",
		Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}}, 201)
	w := &world{testServer: ts, leaf: leaf, consumer: ts.consumerToken("alice"), rev: "rv_" + strings.Repeat("a", 26)}
	c, _, err := ts.dialWS(ts.oicClient(leaf), "wss://"+ts.srv.AccessAddr()+"/v1/access/control", nil, protocol.ControlSubprotocol)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	w.oic = &ctrl{t: t, c: c}
	w.oic.send(&protocol.Hello{OICVersion: "test"})
	if _, ok := w.oic.recv().(*protocol.Welcome); !ok {
		t.Fatal("no welcome")
	}
	w.oic.send(&protocol.Register{Exposures: []protocol.RegisterExposure{{ExposureID: "lab-a/http", Revision: w.rev,
		Protocol: "http", TransportProfile: "tcp", AdmissionMode: protocol.AdmissionBestEffort, Description: "T1"}}})
	if ack, ok := w.oic.recv().(*protocol.RegisterAck); !ok || len(ack.Accepted) != 1 {
		t.Fatalf("register: %+v", ack)
	}
	return w
}

func (w *world) open() (protocol.OpenResponse, *protocol.PrepareChannel) {
	w.t.Helper()
	r := w.do("POST", "/v1/channels", w.consumer, protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: w.rev})
	if r.status != 201 {
		w.t.Fatalf("open: %d %s", r.status, r.body)
	}
	var o protocol.OpenResponse
	r.decode(w.t, &o)
	for {
		if p, ok := w.oic.recv().(*protocol.PrepareChannel); ok {
			return o, p
		}
	}
}

func (w *world) attachProvider(id, ticket string) *websocket.Conn {
	w.t.Helper()
	h := http.Header{protocol.HeaderAttachTicket: {ticket}}
	c, _, err := w.dialWS(w.oicClient(w.leaf), "wss://"+w.srv.AccessAddr()+"/v1/channels/"+id+"/attach", h, protocol.TunnelSubprotocol)
	if err != nil {
		w.t.Fatal(err)
	}
	return c
}

func (w *world) attachConsumer(o protocol.OpenResponse) *websocket.Conn {
	w.t.Helper()
	h := http.Header{protocol.HeaderAttachTicket: {o.Attach.Ticket}, "Authorization": {"Bearer " + w.consumer}}
	c, _, err := w.dialWS(w.client, o.Attach.URL, h, protocol.TunnelSubprotocol)
	if err != nil {
		w.t.Fatal(err)
	}
	return c
}

func TestEndToEnd(t *testing.T) {
	w := newWorld(t)
	var list protocol.ExposureList
	r := w.do("GET", "/v1/exposures?limit=10", w.consumer, nil)
	r.decode(t, &list)
	if r.status != 200 || len(list.Items) != 1 || !list.Items[0].AccessOnline {
		t.Fatalf("%d %s", r.status, r.body)
	}
	o, prep := w.open()
	if o.Attach == nil || o.Attach.URL != "wss://"+w.srv.APIAddr()+"/v1/channels/"+o.ChannelID+"/attach" || prep.ChannelID != o.ChannelID {
		t.Fatalf("%+v", o)
	}
	prov := w.attachProvider(o.ChannelID, prep.ProviderTicket)
	cons := w.attachConsumer(o)
	if frame(t, prov).Type != protocol.FrameReady || frame(t, cons).Type != protocol.FrameReady {
		t.Fatal("READY")
	}
	sendFrame(t, cons, protocol.DataFrame([]byte("ping")))
	if f := frame(t, prov); string(f.Data) != "ping" {
		t.Fatal(string(f.Data))
	}
	sendFrame(t, cons, protocol.FinFrame())
	if frame(t, prov).Type != protocol.FrameFin {
		t.Fatal("FIN")
	}
	sendFrame(t, prov, protocol.DataFrame([]byte("pong")))
	if f := frame(t, cons); string(f.Data) != "pong" {
		t.Fatal(string(f.Data))
	}
	sendFrame(t, prov, protocol.FinFrame())
	if frame(t, cons).Type != protocol.FrameFin {
		t.Fatal("FIN")
	}
	var v protocol.ChannelView
	deadline := time.Now().Add(3 * time.Second)
	for v.State != protocol.StateClosed && time.Now().Before(deadline) {
		w.do("GET", "/v1/channels/"+o.ChannelID, w.consumer, nil).decode(t, &v)
	}
	if v.CloseReason != protocol.CodeCompleted {
		t.Fatalf("%+v", v)
	}
	var st protocol.OISStats
	w.do("GET", "/v1/admin/stats", w.admin, nil).decode(t, &st)
	if st.RelayBytes != 8 || st.AccessesOnline != 1 || !st.StoreHealthy {
		t.Fatalf("%+v", st)
	}
	// Oversized DATA is answered with ABORT(PayloadTooLarge) on both legs.
	o, prep = w.open()
	prov = w.attachProvider(o.ChannelID, prep.ProviderTicket)
	cons = w.attachConsumer(o)
	frame(t, prov)
	frame(t, cons)
	sendFrame(t, cons, protocol.DataFrame(make([]byte, 32<<10+1)))
	if f := frame(t, prov); f.Type != protocol.FrameAbort || f.Reason != protocol.CodePayloadTooLarge {
		t.Fatalf("%+v", f)
	}
	// A message above the read limit closes that leg; the other leg learns
	// why. OIS may drop the leg while the message is still being written.
	o, prep = w.open()
	prov = w.attachProvider(o.ChannelID, prep.ProviderTicket)
	cons = w.attachConsumer(o)
	frame(t, prov)
	frame(t, cons)
	_ = cons.Write(context.Background(), websocket.MessageBinary, protocol.DataFrame(make([]byte, 40<<10)))
	if f := frame(t, prov); f.Type != protocol.FrameAbort || f.Reason != protocol.CodePayloadTooLarge {
		t.Fatalf("%+v", f)
	}
	// DELETE: 202 while it changes the state, then 200 with the final state.
	o, _ = w.open()
	if r := w.do("DELETE", "/v1/channels/"+o.ChannelID, w.consumer, nil); r.status != 202 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if r := w.do("DELETE", "/v1/channels/"+o.ChannelID, w.consumer, nil); r.status != 200 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if r := w.do("GET", "/v1/channels/ch_none", w.consumer, nil); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
	if r := w.do("DELETE", "/v1/channels/ch_none", w.consumer, nil); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
	// Idempotent replay answers 200.
	req := protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: w.rev, ClientRequestID: "r1"}
	if r := w.do("POST", "/v1/channels", w.consumer, req); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if r := w.do("POST", "/v1/channels", w.consumer, req); r.status != 200 {
		t.Fatalf("%d %s", r.status, r.body)
	}
}

func TestAPIErrors(t *testing.T) {
	w := newWorld(t)
	cases := []struct {
		method, path, token string
		body                any
		status              int
		code                protocol.Code
	}{
		{"GET", "/v1/exposures", "", nil, 401, protocol.CodeUnauthenticated},
		{"GET", "/v1/exposures", "oi_0123456789abcdef_" + strings.Repeat("A", 43), nil, 401, protocol.CodeUnauthenticated},
		{"GET", "/v1/exposures", w.admin, nil, 403, protocol.CodeDenied},
		{"POST", "/v1/channels", w.admin, protocol.OpenRequest{}, 403, protocol.CodeDenied},
		{"GET", "/v1/admin/config", w.consumer, nil, 403, protocol.CodeDenied},
		{"PUT", "/v1/exposures", w.consumer, nil, 405, protocol.CodeInvalidRequest},
		{"GET", "/v1/nope", w.consumer, nil, 404, protocol.CodeNotFound},
		{"GET", "/v1/exposures?limit=x", w.consumer, nil, 400, protocol.CodeInvalidRequest},
		{"GET", "/v1/exposures?limit=1000", w.consumer, nil, 400, protocol.CodeInvalidRequest},
		{"POST", "/v1/channels", w.consumer, []byte(`{"exposure_id":"lab-a/http","bogus":1}`), 400, protocol.CodeInvalidRequest},
		{"POST", "/v1/channels", w.consumer, bytes.Repeat([]byte(" "), 65<<10), 413, protocol.CodePayloadTooLarge},
		{"POST", "/v1/channels", w.consumer, protocol.OpenRequest{ExposureID: "lab-a/none", ExpectedRevision: w.rev}, 404, protocol.CodeNotFound},
		{"POST", "/v1/admin/accesses", w.admin, protocol.AccessSpec{AccessID: "x"}, 428, protocol.CodePreconditionRequired},
		{"GET", "/v1/admin/audit?after=x", w.admin, nil, 400, protocol.CodeInvalidRequest},
		{"GET", "/v1/admin/audit?limit=x", w.admin, nil, 400, protocol.CodeInvalidRequest},
		{"GET", "/v1/admin/audit?limit=1000", w.admin, nil, 400, protocol.CodeInvalidRequest},
		{"GET", "/v1/admin/accesses/none", w.admin, nil, 404, protocol.CodeNotFound},
		{"GET", "/v1/admin/policies/none", w.admin, nil, 404, protocol.CodeNotFound},
	}
	for _, c := range cases {
		r := w.do(c.method, c.path, c.token, c.body)
		if r.status != c.status || r.code(t) != c.code {
			t.Errorf("%s %s: %d %s", c.method, c.path, r.status, r.body)
		}
	}
	for _, h := range []string{"3", `"x"`, `""`, `"-1"`} {
		if r := w.do("POST", "/v1/admin/accesses", w.admin, protocol.AccessSpec{AccessID: "x"}, "If-Match", h); r.status != 400 {
			t.Errorf("If-Match %s: %d", h, r.status)
		}
	}
	r := w.do("POST", "/v1/admin/accesses", w.admin, protocol.AccessSpec{AccessID: "lab-x"}, "If-Match", `"0"`)
	var e protocol.ErrorBody
	r.decode(t, &e)
	if r.status != 409 || e.Code != protocol.CodeVersionConflict || e.ConfigVersion < 3 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	// Request bodies are decoded after If-Match for every write.
	for _, path := range []string{"/v1/admin/accesses", "/v1/admin/tokens", "/v1/admin/policies"} {
		if r := w.do("POST", path, w.admin, []byte(`{"x":1}`), "If-Match", w.version()); r.status != 400 {
			t.Errorf("POST %s: %d", path, r.status)
		}
	}
	for _, path := range []string{"/v1/admin/accesses/lab-a", "/v1/admin/policies/p1"} {
		if r := w.do("PATCH", path, w.admin, []byte(`{"x":1}`), "If-Match", w.version()); r.status != 400 {
			t.Errorf("PATCH %s: %d", path, r.status)
		}
		if r := w.do("PATCH", path, w.admin, nil); r.status != 428 {
			t.Errorf("PATCH %s without If-Match: %d", path, r.status)
		}
	}
	for _, path := range []string{"/v1/admin/accesses/lab-a", "/v1/admin/policies/p1", "/v1/admin/tokens/0123456789abcdef"} {
		if r := w.do("DELETE", path, w.admin, nil); r.status != 428 {
			t.Errorf("DELETE %s: %d", path, r.status)
		}
	}
	rec := &bytes.Buffer{}
	hw := &headerRecorder{h: http.Header{}}
	(&Server{log: slog.New(slog.NewTextHandler(rec, nil))}).writeError(hw, fmt.Errorf("disk on fire"))
	if hw.status != 500 || strings.Contains(hw.body.String(), "fire") || !strings.Contains(rec.String(), "fire") {
		t.Fatalf("%d %s", hw.status, hw.body.String())
	}
}

type headerRecorder struct {
	h      http.Header
	status int
	body   bytes.Buffer
}

func (r *headerRecorder) Header() http.Header         { return r.h }
func (r *headerRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *headerRecorder) WriteHeader(s int)           { r.status = s }

func TestAdminCRUD(t *testing.T) {
	w := newWorld(t)
	var accesses items[protocol.AccessView]
	w.do("GET", "/v1/admin/accesses", w.admin, nil).decode(t, &accesses)
	if len(accesses.Items) != 1 || !accesses.Items[0].Online {
		t.Fatalf("%+v", accesses)
	}
	var av protocol.AccessView
	w.do("GET", "/v1/admin/accesses/lab-a", w.admin, nil).decode(t, &av)
	if av.AccessID != "lab-a" || av.Generation != 1 {
		t.Fatalf("%+v", av)
	}
	w.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: "lab-b"}, 201)
	w.write("PATCH", "/v1/admin/accesses/lab-b", protocol.AccessSpec{Labels: map[string]string{"site": "x"}}, 200)
	w.write("DELETE", "/v1/admin/accesses/lab-b", nil, 200)
	var pol protocol.PolicyView
	w.do("GET", "/v1/admin/policies/p1", w.admin, nil).decode(t, &pol)
	if pol.Subject != "alice" {
		t.Fatalf("%+v", pol)
	}
	w.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "p2", Subject: "bob", ExposureSelector: "lab-a/http", Actions: []string{"discover"}}, 201)
	w.write("PATCH", "/v1/admin/policies/p2", protocol.PolicySpec{Actions: []string{"open"}}, 200)
	var pols items[protocol.PolicyView]
	w.do("GET", "/v1/admin/policies", w.admin, nil).decode(t, &pols)
	if len(pols.Items) != 2 {
		t.Fatalf("%+v", pols)
	}
	w.write("DELETE", "/v1/admin/policies/p2", nil, 200)
	var toks items[protocol.TokenView]
	w.do("GET", "/v1/admin/tokens", w.admin, nil).decode(t, &toks)
	if len(toks.Items) != 2 || bytes.Contains(w.do("GET", "/v1/admin/tokens", w.admin, nil).body, []byte("secret")) {
		t.Fatalf("%+v", toks)
	}
	id, _, _ := identity.ParseToken(w.consumer)
	w.write("DELETE", "/v1/admin/tokens/"+id, nil, 200)
	if r := w.do("GET", "/v1/exposures", w.consumer, nil); r.status != 401 {
		t.Fatalf("revoked token: %d", r.status)
	}
	w.write("DELETE", "/v1/admin/tokens/ffffffffffffffff", nil, 404)
	var audit protocol.AuditList
	w.do("GET", "/v1/admin/audit?after=0&limit=100", w.admin, nil).decode(t, &audit)
	if len(audit.Items) < 8 || audit.Items[0].Action != "admin_token_bootstrapped" {
		t.Fatalf("%+v", audit.Items)
	}
	var cv protocol.ConfigView
	w.do("GET", "/v1/admin/config", w.admin, nil).decode(t, &cv)
	if cv.Backend != "sqlite" || cv.Epoch != 1 || !strings.HasPrefix(cv.RunID, "run_") || !cv.StoreHealthy {
		t.Fatalf("%+v", cv)
	}
}

func TestAttachErrors(t *testing.T) {
	w := newWorld(t)
	o, prep := w.open()
	path := "/v1/channels/" + o.ChannelID + "/attach"
	plain := func(c *http.Client, base, query string, h ...string) resp {
		req := must(http.NewRequest("GET", base+path+query, nil))
		for i := 0; i+1 < len(h); i += 2 {
			req.Header.Set(h[i], h[i+1])
		}
		res := must(c.Do(req))
		defer res.Body.Close()
		return resp{status: res.StatusCode, body: must(io.ReadAll(res.Body))}
	}
	api := "https://" + w.srv.APIAddr()
	if r := plain(w.client, api, "?ticket="+o.Attach.Ticket, "Authorization", "Bearer "+w.consumer); r.status != 400 {
		t.Fatalf("query: %d", r.status)
	}
	if r := plain(w.client, api, ""); r.status != 401 {
		t.Fatalf("no auth: %d", r.status)
	}
	if r := plain(w.client, api, "", "Authorization", "Bearer "+w.consumer); r.status != 400 || r.code(t) != protocol.CodeInvalidRequest {
		t.Fatalf("no upgrade: %d %s", r.status, r.body)
	}
	// TV-B1-02: no common subprotocol, no state change.
	h := http.Header{protocol.HeaderAttachTicket: {o.Attach.Ticket}, "Authorization": {"Bearer " + w.consumer}}
	_, res, err := w.dialWS(w.client, o.Attach.URL, h, "oi.tunnel.v2")
	if err == nil || res == nil || res.StatusCode != 400 {
		t.Fatalf("v2: %v %+v", err, res)
	}
	_, res, err = w.dialWS(w.client, o.Attach.URL, http.Header{"Authorization": {"Bearer " + w.consumer}}, protocol.TunnelSubprotocol)
	if err == nil || res.StatusCode != 401 {
		t.Fatalf("missing ticket: %v", err)
	}
	var v protocol.ChannelView
	w.do("GET", "/v1/channels/"+o.ChannelID, w.consumer, nil).decode(t, &v)
	if v.State != protocol.StatePreparing {
		t.Fatalf("%+v", v)
	}
	cons := w.attachConsumer(o)
	defer cons.CloseNow()
	_, res, err = w.dialWS(w.client, o.Attach.URL, h, protocol.TunnelSubprotocol)
	if err == nil || res.StatusCode != 409 {
		t.Fatalf("second attach: %v %+v", err, res)
	}
	acc := "https://" + w.srv.AccessAddr()
	oc := w.oicClient(w.leaf)
	if r := plain(oc, acc, "?x=1"); r.status != 400 {
		t.Fatalf("provider query: %d", r.status)
	}
	if r := plain(oc, acc, ""); r.status != 400 {
		t.Fatalf("provider no upgrade: %d", r.status)
	}
	if r := plain(oc, acc, "", "x", "y"); r.status != 400 {
		t.Fatalf("provider: %d", r.status)
	}
	req := must(http.NewRequest("GET", acc+"/v1/nope", nil))
	if res := must(oc.Do(req)); res.StatusCode != 404 {
		t.Fatalf("access 404: %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	// The Access is disabled after the TLS handshake of a kept-alive
	// connection: the request-level check still refuses it.
	w.write("PATCH", "/v1/admin/accesses/lab-a", protocol.AccessSpec{Enabled: ptrBool(false)}, 200)
	if r := plain(oc, acc, ""); r.status != 401 {
		t.Fatalf("disabled access: %d %s", r.status, r.body)
	}
	creq := must(http.NewRequest("GET", acc+"/v1/access/control", nil))
	if res := must(oc.Do(creq)); res.StatusCode != 401 {
		t.Fatalf("control of disabled access: %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	_ = prep
}

func ptrBool(b bool) *bool { return &b }

func TestControlHandshake(t *testing.T) {
	w := newWorld(t)
	acc := "wss://" + w.srv.AccessAddr() + "/v1/access/control"
	_, res, err := w.dialWS(w.oicClient(w.leaf), acc, nil, "oi.control.v2")
	if err == nil || res.StatusCode != 400 {
		t.Fatalf("subprotocol: %v", err)
	}
	other := must(w.ca.Client("stranger"))
	if _, _, err := w.dialWS(w.oicClient(other), acc, nil, protocol.ControlSubprotocol); err == nil {
		t.Fatal("unpinned certificate accepted")
	}
	foreignCA := must(pki.NewCA("foreign"))
	foreign := must(foreignCA.Client("lab-a"))
	if _, _, err := w.dialWS(w.oicClient(foreign), acc, nil, protocol.ControlSubprotocol); err == nil {
		t.Fatal("certificate from another CA accepted")
	}
	plain := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: w.ca.Pool()}}}
	if _, _, err := w.dialWS(plain, acc, nil, protocol.ControlSubprotocol); err == nil {
		t.Fatal("connection without client certificate accepted")
	}
	// A second connection supersedes the first.
	c2, _, err := w.dialWS(w.oicClient(w.leaf), acc, nil, protocol.ControlSubprotocol)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.CloseNow()
	n := &ctrl{t: t, c: c2}
	n.send(&protocol.Hello{})
	if wl, ok := n.recv().(*protocol.Welcome); !ok || wl.Generation != 2 {
		t.Fatalf("%+v", wl)
	}
	for {
		if ce, ok := w.oic.recv().(*protocol.ControlError); ok {
			if ce.Code != protocol.CodeSuperseded {
				t.Fatalf("%+v", ce)
			}
			break
		}
	}
}
