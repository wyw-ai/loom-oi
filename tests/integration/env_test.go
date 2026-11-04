package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

// testLimits are the B4 §3 vector values.
func testLimits() map[string]any {
	return map[string]any{
		"prepare_ms": 2000, "close_ms": 1000, "retain_ms": 5000, "heartbeat_ms": 1000, "offline_misses": 3,
		"license_ttl_ms": 3000, "license_margin_ms": 500, "ping_ms": 1000, "idle_ms": 2000, "store_probe_ms": 500,
	}
}

const (
	tClose = time.Second
	settle = tClose + time.Second
)

func backends(t *testing.T) []string {
	list := os.Getenv("OI_IT_BACKENDS")
	if list == "" {
		list = "sqlite"
	}
	var out []string
	for _, b := range strings.Split(list, ",") {
		b = strings.TrimSpace(b)
		if b == "postgres" && os.Getenv("OI_TEST_PG_DSN") == "" {
			t.Fatal("OI_IT_BACKENDS includes postgres but OI_TEST_PG_DSN is not set")
		}
		out = append(out, b)
	}
	return out
}

// each runs fn once per selected backend.
func each(t *testing.T, fn func(t *testing.T, be string)) {
	for _, be := range backends(t) {
		t.Run(be, func(t *testing.T) { fn(t, be) })
	}
}

type envOpts struct {
	limits map[string]any
	noOICs bool
	// altAdmission overrides lab-a/alt's admission mode (default require_ready).
	altAdmission string
}

type oicProc struct {
	*proc
	access string
	path   string
	cfg    map[string]any
	diag   string
	leaf   *pki.Leaf
	// ctrl indexes the control connection in the access proxy.
	ctrl int
}

type env struct {
	t        *testing.T
	be       string
	dir      string
	ca       *pki.CA
	caFile   string
	cfgPath  string
	limits   map[string]any
	ois      *proc
	api      string
	acc      *proxy
	pg       *proxy
	pgDSN    string
	pgName   string
	sqlite   string
	adminTok string
	admin    *client.Client
	stats    *http.Client
	t1       *fixture.Target
	t2       *fixture.Target
	t3       *fixture.Target
	closed   string
	oics     map[string]*oicProc
	tokens   map[string]string
	clients  map[string]*client.Client
	baseOIS  protocol.ProcessStats
	baseOIC  map[string]protocol.ProcessStats
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.WriteFile(path, must(json.MarshalIndent(v, "", "  ")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T, be string, o envOpts) *env {
	t.Helper()
	dir := filepath.Join(runRoot, strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, be: be, dir: dir, oics: map[string]*oicProc{}, tokens: map[string]string{}, clients: map[string]*client.Client{},
		baseOIC: map[string]protocol.ProcessStats{}}
	e.limits = testLimits()
	for k, v := range o.limits {
		e.limits[k] = v
	}
	e.ca = must(pki.NewCA("it-ca"))
	e.caFile = must(e.ca.Write(dir, "ca"))
	leaf := must(e.ca.Server("ois"))
	if _, _, err := leaf.Write(dir, "ois"); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"api_listen": "127.0.0.1:0", "access_listen": "127.0.0.1:0",
		"tls":   map[string]string{"cert_file": "ois.pem", "key_file": "ois-key.pem", "client_ca_file": "ca.pem"},
		"store": e.storeConfig(), "limits": e.limits,
	}
	e.cfgPath = filepath.Join(dir, "ois.json")
	writeJSON(t, e.cfgPath, cfg)
	out := e.runOIS("init", "--config", e.cfgPath, "--subject", "root")
	var tok protocol.TokenCreated
	if err := json.Unmarshal([]byte(out), &tok); err != nil {
		t.Fatalf("init output %q: %v", out, err)
	}
	e.adminTok = tok.Token
	e.acc = must(newProxy("127.0.0.1:1"))
	t.Cleanup(e.acc.close)
	e.startOIS()
	e.stats = &http.Client{Transport: &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{RootCAs: e.ca.Pool()}}}
	e.t1 = fixture.New(t, "T1", fixture.Program)
	e.t2 = fixture.New(t, "T2", fixture.Program)
	e.t3 = fixture.New(t, "T3", fixture.Program)
	ln := must(net.Listen("tcp", "127.0.0.1:0"))
	e.closed = ln.Addr().String()
	_ = ln.Close()
	e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pa", Subject: "alice", ExposureSelector: "lab-a/*",
		Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
	e.write("POST", "/v1/admin/policies", protocol.PolicySpec{PolicyID: "pc", Subject: "carol", ExposureSelector: "lab-b/*",
		Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
	if !o.noOICs {
		alt := o.altAdmission
		if alt == "" {
			alt = protocol.AdmissionRequireReady
		}
		e.startOIC("lab-a", []any{
			exposure("http", e.t1.Addr(), protocol.AdmissionBestEffort, nil),
			exposure("alt", e.t3.Addr(), alt, nil),
		})
		_, t2port, _ := net.SplitHostPort(e.t2.Addr())
		e.startOIC("lab-b", []any{
			exposure("http", e.t2.Addr(), protocol.AdmissionBestEffort, nil),
			exposure("down", e.closed, protocol.AdmissionBestEffort, nil),
			exposure("named", "localhost:"+t2port, protocol.AdmissionBestEffort, []string{"192.0.2.0/24"}),
		})
	}
	e.baseline()
	return e
}

func exposure(name, addr, admission string, cidrs []string) map[string]any {
	tg := map[string]any{"address": addr}
	if cidrs != nil {
		tg["allowed_cidrs"] = cidrs
	}
	return map[string]any{"name": name, "protocol": "http", "admission_mode": admission, "description": name, "target": tg}
}

// storeConfig selects the backend; PostgreSQL gets a fresh database behind
// a proxy that can simulate outages.
func (e *env) storeConfig() map[string]string {
	if e.be == "sqlite" {
		e.sqlite = filepath.Join(e.dir, "ois.db")
		return map[string]string{"driver": "sqlite", "sqlite_path": e.sqlite}
	}
	admin := must(sql.Open("pgx", os.Getenv("OI_TEST_PG_DSN")))
	e.pgName = "oi_it_" + strings.ToLower(protocol.NewID(""))
	if _, err := admin.Exec("CREATE DATABASE " + e.pgName); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + e.pgName + " WITH (FORCE)")
		_ = admin.Close()
	})
	u := must(url.Parse(os.Getenv("OI_TEST_PG_DSN")))
	e.pg = must(newProxy(u.Host))
	e.t.Cleanup(e.pg.close)
	u.Host = e.pg.addr()
	u.Path = "/" + e.pgName
	e.pgDSN = u.String()
	return map[string]string{"driver": "postgres", "postgres_dsn": e.pgDSN}
}

// runOIS runs a short ois command and returns its standard output.
func (e *env) runOIS(args ...string) string {
	e.t.Helper()
	out, code, errOut := run(oisBin, nil, args...)
	if code != 0 {
		e.t.Fatalf("ois %v: exit %d: %s", args, code, errOut)
	}
	return out
}

func run(bin string, env []string, args ...string) (string, int, string) {
	return runIn(bin, env, "", args...)
}

// runIn runs a command with stdin and returns stdout, exit code, stderr.
func runIn(bin string, env []string, stdin string, args ...string) (string, int, string) {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	return out.String(), code, errb.String()
}

// startOIS starts `ois serve` and points the access proxy at it.
func (e *env) startOIS() {
	e.t.Helper()
	e.ois = startProc(e.t, e.dir, fmt.Sprintf("ois-%d", time.Now().UnixNano()), oisBin, nil, "serve", "--config", e.cfgPath)
	l, _ := e.ois.waitLog(e.t, 20*time.Second, "ois started", 0)
	e.api = l["api"].(string)
	e.acc.setTarget(l["access"].(string))
	e.admin = must(client.New(client.Options{URL: "https://" + e.api, Token: e.adminTok, CAFile: e.caFile}))
	for k := range e.clients {
		delete(e.clients, k)
	}
}

func (e *env) stopOIS() {
	e.t.Helper()
	e.ois.stop(e.t)
}

// startOIC writes an OIC configuration and runs it until registered.
func (e *env) startOIC(access string, exps []any) *oicProc {
	e.t.Helper()
	o := e.oics[access]
	if o == nil {
		leaf := must(e.ca.Client(access))
		if _, _, err := leaf.Write(e.dir, access); err != nil {
			e.t.Fatal(err)
		}
		e.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: access, SPKIPins: []string{identity.SPKIPin(leaf.Cert)}})
		o = &oicProc{access: access, leaf: leaf, path: filepath.Join(e.dir, access+".json")}
		e.oics[access] = o
	}
	o.cfg = map[string]any{
		"access_id": access,
		"ois":       map[string]any{"url": "wss://" + e.acc.addr(), "ca_file": "ca.pem"},
		"tls":       map[string]any{"cert_file": access + ".pem", "key_file": access + "-key.pem"},
		"exposures": exps, "limits": e.limits, "diag_listen": "127.0.0.1:0",
	}
	writeJSON(e.t, o.path, o.cfg)
	o.ctrl = e.acc.count()
	o.proc = startProc(e.t, e.dir, fmt.Sprintf("oic-%s-%d", access, time.Now().UnixNano()), oicBin, nil, "run", "--config", o.path)
	l, _ := o.proc.waitLog(e.t, 20*time.Second, "diagnostics listening", 0)
	o.diag = l["addr"].(string)
	e.waitRegistered(access)
	return o
}

func (e *env) waitRegistered(access string) {
	e.t.Helper()
	e.eventually(15*time.Second, access+" registered", func() bool { return e.oicStats(access).Registered })
}

// rewriteOIC changes an OIC's exposures; the agent reloads by polling.
func (e *env) rewriteOIC(access string, exps []any) {
	e.t.Helper()
	o := e.oics[access]
	o.cfg["exposures"] = exps
	writeJSON(e.t, o.path, o.cfg)
}

func (e *env) eventually(timeout time.Duration, what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *env) oicStats(access string) protocol.OICStats {
	e.t.Helper()
	var s protocol.OICStats
	res, err := e.stats.Get("http://" + e.oics[access].diag + "/stats")
	if err != nil {
		return s
	}
	defer res.Body.Close()
	_ = json.NewDecoder(res.Body).Decode(&s)
	return s
}

func (e *env) oisStats() protocol.OISStats {
	e.t.Helper()
	req := must(http.NewRequest("GET", "https://"+e.api+"/v1/admin/stats", nil))
	req.Header.Set("Authorization", "Bearer "+e.adminTok)
	var s protocol.OISStats
	res, err := e.stats.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	_ = json.NewDecoder(res.Body).Decode(&s)
	return s
}

func (e *env) version() string {
	e.t.Helper()
	var cv protocol.ConfigView
	if _, err := e.admin.Do(context.Background(), "GET", "/v1/admin/config", "", nil, &cv); err != nil {
		e.t.Fatal(err)
	}
	return strconv.FormatInt(cv.ConfigVersion, 10)
}

// tryWrite performs an admin write with the current If-Match.
func (e *env) tryWrite(method, path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	_, err := e.admin.Do(context.Background(), method, path, e.version(), body, &out)
	return out, err
}

func (e *env) write(method, path string, body any) json.RawMessage {
	e.t.Helper()
	out, err := e.tryWrite(method, path, body)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	return out
}

// token returns a consumer bearer token for subject.
func (e *env) token(subject string) string {
	e.t.Helper()
	if tok, ok := e.tokens[subject]; ok {
		return tok
	}
	var c protocol.TokenCreated
	if err := json.Unmarshal(e.write("POST", "/v1/admin/tokens", protocol.TokenCreate{Subject: subject, Scope: protocol.ScopeConsumer}), &c); err != nil {
		e.t.Fatal(err)
	}
	e.tokens[subject] = c.Token
	return c.Token
}

// consumer returns a Go client for subject.
func (e *env) consumer(subject string) *client.Client {
	e.t.Helper()
	if c, ok := e.clients[subject]; ok {
		return c
	}
	c := must(client.New(client.Options{URL: "https://" + e.api, Token: e.token(subject), CAFile: e.caFile,
		CloseGrace: tClose}))
	e.clients[subject] = c
	return c
}

func (e *env) clientFor(token string) *client.Client {
	c := must(client.New(client.Options{URL: "https://" + e.api, Token: token, CAFile: e.caFile, CloseGrace: tClose}))
	e.clients["token:"+token] = c // closeIdle drops its keep-alive connections
	return c
}

// rawAttach dials an attach URL with explicit headers and subprotocols.
func (e *env) rawAttach(url, token, ticket string, cert *pki.Leaf, protos ...string) (*websocket.Conn, *http.Response, error) {
	tc := &tls.Config{RootCAs: e.ca.Pool()}
	if cert != nil {
		tc.Certificates = []tls.Certificate{must(cert.TLS())}
	}
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	if ticket != "" {
		h.Set(protocol.HeaderAttachTicket, ticket)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tc, DisableKeepAlives: true}},
		HTTPHeader: h, Subprotocols: protos})
}

// open opens exposure for subject at its current revision.
func (e *env) open(subject, exposure string) protocol.OpenResponse {
	e.t.Helper()
	o, err := e.consumer(subject).OpenLatest(context.Background(), exposure, "", false)
	if err != nil {
		e.t.Fatalf("open %s: %v", exposure, err)
	}
	return o
}

func readFrame(t *testing.T, c *websocket.Conn) protocol.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("leg read: %v", err)
	}
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

func expectAbort(t *testing.T, c *websocket.Conn, codes ...protocol.Code) protocol.Code {
	t.Helper()
	for {
		f := readFrame(t, c)
		if f.Type == protocol.FrameAbort {
			for _, code := range codes {
				if f.Reason == code {
					return f.Reason
				}
			}
			t.Fatalf("ABORT(%s), want one of %v", f.Reason, codes)
		}
		if f.Type == protocol.FrameReady {
			t.Fatal("READY where an ABORT was expected")
		}
	}
}

// channelState polls a channel until it is Closed and returns the view.
func (e *env) closedState(subject, id string) protocol.ChannelView {
	e.t.Helper()
	var v protocol.ChannelView
	e.eventually(10*time.Second, "channel "+id+" closed", func() bool {
		var err error
		v, err = e.consumer(subject).Channel(context.Background(), id)
		return err == nil && v.State == protocol.StateClosed
	})
	return v
}

// baseline records process counters once the topology is idle.
func (e *env) baseline() {
	e.t.Helper()
	e.closeIdle()
	time.Sleep(200 * time.Millisecond)
	e.baseOIS = e.oisStats().Process
	for a := range e.oics {
		e.baseOIC[a] = e.oicStats(a).Process
	}
}

func (e *env) closeIdle() {
	for _, c := range e.clients {
		c.CloseIdleConnections()
	}
	e.admin.CloseIdleConnections()
}

// settled is the B4 §4 resource regression (TV-B4-03): within T_close+1s
// no channel or ticket is left and goroutines and descriptors are back
// to their baseline.
func (e *env) settled() {
	e.t.Helper()
	e.closeIdle()
	var last string
	deadline := time.Now().Add(settle)
	for {
		s := e.oisStats()
		ok := s.Channels.Preparing+s.Channels.Open+s.Channels.Closing == 0 && s.Tickets == 0 &&
			s.Process.Goroutines <= e.baseOIS.Goroutines+10 && (e.baseOIS.OpenFDs < 0 || s.Process.OpenFDs <= e.baseOIS.OpenFDs)
		last = fmt.Sprintf("ois %+v", s)
		for a, o := range e.oics {
			if o.proc == nil {
				continue
			}
			os := e.oicStats(a)
			base := e.baseOIC[a]
			ok = ok && os.Channels == 0 && os.Process.Goroutines <= base.Goroutines+10 && (base.OpenFDs < 0 || os.Process.OpenFDs <= base.OpenFDs)
			last += fmt.Sprintf("; %s %+v", a, os)
		}
		if ok {
			e.obs("resources", map[string]any{"ois": s.Process, "settled_ms": settle.Milliseconds() - time.Until(deadline).Milliseconds()})
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("resources not settled within T_close+1s: %s (baseline ois %+v oic %+v)", last, e.baseOIS, e.baseOIC)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// fileSize is the size of path, or -1 when it does not exist.
func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return fi.Size()
}

// obs records an observation (O1–O4, B4 §6) in the test log.
func (e *env) obs(kind string, fields map[string]any) {
	fields["kind"] = kind
	fields["backend"] = e.be
	b, _ := json.Marshal(fields)
	e.t.Logf("OBS %s", b)
}
