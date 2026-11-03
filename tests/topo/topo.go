// Package topo builds in-process OI topologies for tests: an OIS with a
// SQLite store, OIC agents and deterministic TCP target fixtures (B4 §4).
package topo

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
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/oic/relay"
	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/ois/server"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

// Topology is a running OIS with helpers to configure it.
type Topology struct {
	T          testing.TB
	Dir        string
	CA         *pki.CA
	CAFile     string
	OIS        *server.Server
	AdminToken string
	http       *http.Client
	Limits     map[string]any
}

// Options tune the OIS.
type Options struct {
	// Limits override B4 §3 values for both OIS and OICs.
	Limits map[string]any
}

// must panics on setup errors; tests need no recovery from them.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// New starts an OIS on loopback with a fresh SQLite store.
func New(t testing.TB, o Options) *Topology {
	t.Helper()
	tp := &Topology{T: t, Dir: t.TempDir(), Limits: o.Limits}
	tp.CA = must(pki.NewCA("topo-ca"))
	tp.CAFile = must(tp.CA.Write(tp.Dir, "ca"))
	leaf := must(tp.CA.Server("ois"))
	if _, _, err := leaf.Write(tp.Dir, "ois"); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"api_listen": "127.0.0.1:0", "access_listen": "127.0.0.1:0",
		"tls":   map[string]string{"cert_file": "ois.pem", "key_file": "ois-key.pem", "client_ca_file": "ca.pem"},
		"store": map[string]string{"driver": "sqlite", "sqlite_path": "ois.db"},
	}
	if o.Limits != nil {
		cfg["limits"] = o.Limits
	}
	path := filepath.Join(tp.Dir, "ois.json")
	writeJSON(t, path, cfg)
	var out, errb bytes.Buffer
	if code := server.Main(context.Background(), []string{"init", "--config", path, "--subject", "root"}, &out, &errb); code != 0 {
		t.Fatalf("ois init: %s", errb.String())
	}
	var tok protocol.TokenCreated
	if err := json.Unmarshal(out.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	tp.AdminToken = tok.Token
	scfg := must(server.LoadConfig(path))
	tp.OIS = must(server.Start(context.Background(), scfg, slog.New(slog.DiscardHandler)))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tp.OIS.Shutdown(ctx)
	})
	tp.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: tp.CA.Pool()}}}
	return tp
}

func writeJSON(t testing.TB, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// URL is the api listener base URL.
func (tp *Topology) URL() string { return "https://" + tp.OIS.APIAddr() }

// API performs a request on the api listener and returns status and body.
func (tp *Topology) API(method, path, token, ifMatch string, body any) (int, []byte) {
	tp.T.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := must(http.NewRequest(method, tp.URL()+path, rd))
	req.Header.Set("Authorization", "Bearer "+token)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	res := must(tp.http.Do(req))
	defer res.Body.Close()
	return res.StatusCode, must(io.ReadAll(res.Body))
}

// Admin performs an admin write with the current If-Match and fails the
// test unless the status is 2xx.
func (tp *Topology) Admin(method, path string, body any) []byte {
	tp.T.Helper()
	_, cfgBody := tp.API(http.MethodGet, "/v1/admin/config", tp.AdminToken, "", nil)
	var cv protocol.ConfigView
	if err := json.Unmarshal(cfgBody, &cv); err != nil {
		tp.T.Fatal(err)
	}
	status, out := tp.API(method, path, tp.AdminToken, strconv.Quote(strconv.FormatInt(cv.ConfigVersion, 10)), body)
	if status >= 300 {
		tp.T.Fatalf("%s %s: %d %s", method, path, status, out)
	}
	return out
}

// Token creates a consumer token for subject.
func (tp *Topology) Token(subject string) string {
	tp.T.Helper()
	var c protocol.TokenCreated
	if err := json.Unmarshal(tp.Admin(http.MethodPost, "/v1/admin/tokens", protocol.TokenCreate{Subject: subject, Scope: protocol.ScopeConsumer}), &c); err != nil {
		tp.T.Fatal(err)
	}
	return c.Token
}

// Grant lets subject discover and open the exposures matched by selector.
func (tp *Topology) Grant(id, subject, selector string) {
	tp.T.Helper()
	tp.Admin(http.MethodPost, "/v1/admin/policies", protocol.PolicySpec{PolicyID: id, Subject: subject, ExposureSelector: selector,
		Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
}

// OIC is a running agent.
type OIC struct {
	Agent  *relay.Agent
	Path   string
	Config map[string]any
	cancel context.CancelFunc
	done   chan struct{}
}

// Exposure names one OIC exposure and its target address.
type Exposure struct {
	Name, Address, Protocol string
}

// StartOIC creates the Access, writes an OIC configuration and runs the
// agent until it has registered.
func (tp *Topology) StartOIC(accessID string, exps ...Exposure) *OIC {
	tp.T.Helper()
	leaf := must(tp.CA.Client(accessID))
	if _, _, err := leaf.Write(tp.Dir, accessID); err != nil {
		tp.T.Fatal(err)
	}
	tp.Admin(http.MethodPost, "/v1/admin/accesses", protocol.AccessSpec{AccessID: accessID, SPKIPins: []string{identity.SPKIPin(leaf.Cert)}})
	var list []any
	for _, e := range exps {
		p := e.Protocol
		if p == "" {
			p = "http"
		}
		list = append(list, map[string]any{"name": e.Name, "protocol": p, "description": e.Name,
			"target": map[string]any{"address": e.Address}})
	}
	cfg := map[string]any{
		"access_id": accessID,
		"ois":       map[string]any{"url": "wss://" + tp.OIS.AccessAddr(), "ca_file": "ca.pem"},
		"tls":       map[string]any{"cert_file": accessID + ".pem", "key_file": accessID + "-key.pem"},
		"exposures": list,
	}
	if tp.Limits != nil {
		cfg["limits"] = tp.Limits
	}
	o := &OIC{Path: filepath.Join(tp.Dir, accessID+".json"), Config: cfg, done: make(chan struct{})}
	writeJSON(tp.T, o.Path, cfg)
	o.Agent = must(relay.New(relay.Options{ConfigPath: o.Path, BackoffMin: 20 * time.Millisecond,
		BackoffMax: 200 * time.Millisecond, ReloadEvery: 50 * time.Millisecond}))
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	go func() {
		_ = o.Agent.Run(ctx)
		close(o.done)
	}()
	tp.T.Cleanup(o.Stop)
	deadline := time.Now().Add(10 * time.Second)
	for !o.Agent.Stats().Registered {
		if time.Now().After(deadline) {
			tp.T.Fatalf("OIC %s did not register", accessID)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return o
}

// Stop ends the agent.
func (o *OIC) Stop() {
	o.cancel()
	select {
	case <-o.done:
	case <-time.After(10 * time.Second):
		panic(fmt.Sprintf("OIC %s did not stop", o.Path))
	}
}
