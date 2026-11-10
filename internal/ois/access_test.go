package ois

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"gopkg.in/yaml.v3"
)

func anonymousConfig(c *Config) {
	c.Admin.TokenEnv = ""
	for id, ch := range c.Channels {
		ch.CredentialEnv = ""
		c.Channels[id] = ch
	}
	for id, vm := range c.VMCPS {
		vm.Access.CredentialEnv = ""
		c.VMCPS[id] = vm
	}
}

func TestAnonymousAccessIgnoresBearerIdentity(t *testing.T) {
	f := testFixture(t, anonymousConfig)
	for _, bearer := range []string{"", "arbitrary-header"} {
		f.must("GET", "/api/v1/status", bearer, nil, 200)
	}
	c := f.dial("linux-151", "linux-151", "")
	if frame := readTestFrame(t, c); frame.Type != "bound" {
		t.Fatal(frame)
	}
	if err := c.WriteJSON(protocol.Frame{Type: "register", Apps: fakeApps("bash")}); err != nil {
		t.Fatal(err)
	}
	if frame := readTestFrame(t, c); frame.Type != "registered" {
		t.Fatal(frame)
	}
	for _, bearer := range []string{"", "first-untrusted-value", "second-untrusted-value"} {
		done := make(chan reply, 1)
		go func() {
			done <- f.request("POST", "/api/v1/exec", bearer, map[string]any{"vmcp": "workbench", "tool": "linux_exec_command", "arguments": map[string]any{"cmd": "echo anonymous"}}, nil)
		}()
		frame := readTestFrame(t, c)
		if frame.Context.AccessIdentity != "anonymous:workbench" || frame.Context.VMCPID != "workbench" {
			t.Fatalf("caller invented anonymous identity: %+v", frame.Context)
		}
		if err := c.WriteJSON(protocol.ResultFrame(frame.CallID, map[string]any{"output": "ok", "exit_code": 0})); err != nil {
			t.Fatal(err)
		}
		if r := awaitReply(t, done); r.status != 200 {
			t.Fatal(r.raw)
		}
	}
	// Protocol conversations created with an arbitrary header can subsequently
	// be used without it: anonymous scope derives only from the vMCP.
	session := f.initialize()
	headers := map[string]string{"Accept": "application/json, text/event-stream", "MCP-Session-Id": session, "MCP-Protocol-Version": protocol.MCPVersion}
	for _, bearer := range []string{"", "different-bearer"} {
		r := f.request("POST", "/mcp/workbench", bearer, rpc(5, "tools/list", nil), headers)
		if r.status != 200 || r.data["error"] != nil {
			t.Fatal(r.raw)
		}
	}
	first, _ := authorizeAccess("workbench", Access{}, "fake-one", false)
	second, _ := authorizeAccess("other", Access{}, "fake-one", false)
	if first == second {
		t.Fatal("anonymous identities crossed vMCP scope")
	}
	bad := f.dial("wrong-host", "windows-local", "")
	if frame := readTestFrame(t, bad); frame.Error == nil || frame.Error.Code != "FORBIDDEN" {
		t.Fatal("anonymous mode removed expected Host binding")
	}
}

func TestConfiguredMissingTokensNeverBecomeAnonymous(t *testing.T) {
	t.Setenv("OIS_TEST_UNSET", "")
	f := testFixture(t, func(c *Config) {
		vm := c.VMCPS["workbench"]
		vm.Access.CredentialEnv = "OIS_TEST_UNSET"
		c.VMCPS["workbench"] = vm
		ch := c.Channels["linux-151"]
		ch.CredentialEnv = "OIS_TEST_UNSET"
		c.Channels["linux-151"] = ch
		// An anonymous channel allows upgrading; the first frame must still
		// enforce the selected protected channel's own credential setting.
		ch = c.Channels["windows-local"]
		ch.CredentialEnv = ""
		c.Channels["windows-local"] = ch
	})
	for _, bearer := range []string{"", "arbitrary"} {
		f.must("POST", "/api/v1/exec", bearer, map[string]any{"vmcp": "workbench", "tool": "linux_exec_command", "arguments": map[string]any{}}, 401)
		f.must("GET", "/mcp/workbench", bearer, nil, 401)
		c := f.dial("linux-151", "linux-151", bearer)
		if frame := readTestFrame(t, c); frame.Error == nil || frame.Error.Code != "FORBIDDEN" {
			t.Fatal("missing channel token became anonymous")
		}
		c.Close()
	}
	t.Setenv("OIS_TEST_ADMIN", "")
	f.must("GET", "/api/v1/status", "", nil, 401)
	f.must("GET", "/api/v1/status", "secret-admin", nil, 401)
	if s, err := New(f.config); err == nil {
		s.Close()
		t.Fatal("configured missing admin credential accepted during startup")
	}
}

func tlsRequest(t *testing.T, server *httptest.Server, method, path, bearer string, body any, headers map[string]string) reply {
	t.Helper()
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	return reply{status: res.StatusCode, data: value, header: res.Header, raw: string(data)}
}

func TestPerVMCPHTTPSRequirementAndEndpointSelection(t *testing.T) {
	f := testFixture(t, func(c *Config) {
		anonymousConfig(c)
		c.Server.PublicURL = "http://ois.example:25100"
		c.Server.HTTPS = &HTTPSConfig{Listen: ":25101", PublicURL: "https://ois.example:25101", CertFile: "cert.pem", KeyFile: "key.pem"}
	})
	secure := httptest.NewTLSServer(f.s.Handler())
	defer secure.Close()
	vm := VMCP{Tools: []string{"linux_exec_command"}, Access: Access{RequireHTTPS: true}}
	r := f.must("PUT", "/api/v1/vmcps/secure", "", vm, 201)
	if r.data["endpoint"] != "https://ois.example:25101/mcp/secure" {
		t.Fatal(r.raw)
	}
	if r = f.must("GET", "/api/v1/vmcps/secure", "", nil, 200); r.data["endpoint"] != "https://ois.example:25101/mcp/secure" {
		t.Fatal(r.raw)
	}
	if r = f.must("GET", "/api/v1/vmcps/workbench", "", nil, 200); r.data["endpoint"] != "http://ois.example:25100/mcp/workbench" {
		t.Fatal(r.raw)
	}
	r = f.must("GET", "/api/v1/vmcps", "", nil, 200)
	for _, item := range r.data["items"].([]any) {
		v := item.(map[string]any)
		if v["id"] == "secure" && v["endpoint"] != "https://ois.example:25101/mcp/secure" {
			t.Fatal(r.raw)
		}
	}
	body := map[string]any{"vmcp": "secure", "tool": "linux_exec_command", "arguments": map[string]any{"cmd": "pwd"}}
	forwarded := map[string]string{"Forwarded": "proto=https", "X-Forwarded-Proto": "https", "X-Forwarded-Ssl": "on"}
	for _, path := range []string{"/api/v1/exec", "/mcp/secure"} {
		r = f.request("POST", path, "", body, forwarded)
		if r.status != 403 || r.errorCode() != "HTTPS_REQUIRED" {
			t.Fatal(r.raw)
		}
	}
	// TLS transport passes the restriction even with no Token configured.
	init := tlsRequest(t, secure, "POST", "/mcp/secure", "", rpc(1, "initialize", map[string]any{"protocolVersion": protocol.MCPVersion}), map[string]string{"Accept": "application/json,text/event-stream"})
	if init.status != 200 || init.header.Get("MCP-Session-Id") == "" {
		t.Fatal(init.raw)
	}
	if r = tlsRequest(t, secure, "POST", "/api/v1/exec", "", body, nil); r.status != 503 || r.errorCode() != "CHANNEL_UNAVAILABLE" {
		t.Fatal(r.raw)
	}
	// Token and transport requirements compose; configuring TLS does not turn
	// missing or invalid application credentials into anonymous access.
	vm.Access.CredentialEnv = "OIS_TEST_CONSUMER"
	f.must("PUT", "/api/v1/vmcps/secure", "", vm, 200)
	if r = tlsRequest(t, secure, "POST", "/api/v1/exec", "", body, nil); r.status != 401 {
		t.Fatal(r.raw)
	}
	if r = tlsRequest(t, secure, "POST", "/api/v1/exec", "secret-consumer", body, nil); r.status != 503 {
		t.Fatal(r.raw)
	}
	if r = f.request("POST", "/api/v1/exec", "secret-consumer", body, nil); r.status != 403 {
		t.Fatal(r.raw)
	}
	// Both configured origins are recognized; arbitrary origins remain denied.
	for _, origin := range []string{"http://ois.example:25100", "https://ois.example:25101"} {
		if r = f.request("GET", "/healthz", "", nil, map[string]string{"Origin": origin}); r.status != 200 {
			t.Fatal(r.raw)
		}
	}
}

func writeTestConfig(t *testing.T, cfg Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ois.yaml")
	b, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTransportConfigDefaultsLegacyAndValidation(t *testing.T) {
	path := writeTestConfig(t, Config{Version: 1})
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":25100" || cfg.Server.PublicURL != "http://localhost:25100" || cfg.Server.HTTPS != nil || cfg.Admin.TokenEnv != "" {
		t.Fatalf("unexpected default: %+v", cfg)
	}
	legacy := Config{Version: 1, Server: ServerConfig{Listen: ":25100", TLSCert: "tls/cert.pem", TLSKey: "tls/key.pem"}}
	cfg, err = loadConfig(writeTestConfig(t, legacy))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.PublicURL != "https://localhost:25100" || cfg.Server.httpsURL() != cfg.Server.PublicURL || !filepath.IsAbs(cfg.Server.TLSCert) || !filepath.IsAbs(cfg.Server.TLSKey) {
		t.Fatalf("legacy TLS changed: %+v", cfg.Server)
	}
	dual := Config{Version: 1, Server: ServerConfig{HTTPS: &HTTPSConfig{CertFile: "tls/cert.pem", KeyFile: "tls/key.pem"}}}
	cfg, err = loadConfig(writeTestConfig(t, dual))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.HTTPS.Listen != ":25101" || cfg.Server.HTTPS.PublicURL != "https://localhost:25101" || !filepath.IsAbs(cfg.Server.HTTPS.CertFile) || !filepath.IsAbs(cfg.Server.HTTPS.KeyFile) {
		t.Fatalf("HTTPS defaults not resolved: %+v", cfg.Server.HTTPS)
	}
	for name, server := range map[string]ServerConfig{
		"mixed":              {TLSCert: "cert", TLSKey: "key", HTTPS: &HTTPSConfig{CertFile: "cert", KeyFile: "key"}},
		"half_legacy":        {TLSCert: "cert"},
		"missing_https_key":  {HTTPS: &HTTPSConfig{CertFile: "cert"}},
		"same_listener":      {Listen: ":25100", HTTPS: &HTTPSConfig{Listen: ":25100", CertFile: "cert", KeyFile: "key"}},
		"wrong_plain_scheme": {PublicURL: "https://example.com"},
		"wrong_tls_scheme":   {HTTPS: &HTTPSConfig{PublicURL: "http://example.com", CertFile: "cert", KeyFile: "key"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(writeTestConfig(t, Config{Version: 1, Server: server})); err == nil {
				t.Fatal("invalid transport configuration accepted")
			}
		})
	}
}

func TestHTTPSAvailabilityCheckedOnAPICreateAndRestart(t *testing.T) {
	f := testFixture(t, anonymousConfig)
	vm := VMCP{Tools: []string{}, Access: Access{RequireHTTPS: true}}
	r := f.must("PUT", "/api/v1/vmcps/secure", "", vm, 400)
	if r.errorCode() != "INVALID_REQUEST" {
		t.Fatal(r.raw)
	}
	f.must("GET", "/api/v1/vmcps/secure", "", nil, 404)
	initial := Config{Version: 1, VMCPS: map[string]VMCP{"secure": vm}}
	if s, err := New(writeTestConfig(t, initial)); err == nil {
		s.Close()
		t.Fatal("initial secure vMCP accepted without HTTPS listener")
	}
	legacy := Config{Version: 1, Server: ServerConfig{TLSCert: "cert.pem", TLSKey: "key.pem"}, VMCPS: map[string]VMCP{"secure": vm}}
	legacyServer, err := New(writeTestConfig(t, legacy))
	if err != nil {
		t.Fatal(err)
	}
	defer legacyServer.Close()
	if endpoint := legacyServer.vmcpEndpoint("secure", vm); endpoint != "https://localhost:25100/mcp/secure" {
		t.Fatal(endpoint)
	}
	// API settings remain authoritative across a restart even if bootstrap YAML
	// still contains the old unauthenticated, HTTP-capable definition.
	f = testFixture(t, func(c *Config) {
		anonymousConfig(c)
		c.Server.HTTPS = &HTTPSConfig{CertFile: "cert.pem", KeyFile: "key.pem"}
	})
	vm = f.s.Config().VMCPS["workbench"]
	vm.Access.RequireHTTPS = true
	vm.Access.CredentialEnv = "OIS_TEST_CONSUMER"
	f.must("PUT", "/api/v1/vmcps/workbench", "", vm, 200)
	resumed, err := New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.state.VMCPS["workbench"].Access != vm.Access {
		t.Fatal("restart replaced API access policy with YAML defaults")
	}
	cfg := f.s.Config()
	cfg.Server.HTTPS = nil
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.config, data, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := New(f.config); err == nil {
		s.Close()
		t.Fatal("restart silently downgraded a persisted HTTPS requirement")
	} else if !strings.Contains(err.Error(), "no HTTPS listener") {
		t.Fatal(err)
	}
}
