package ois

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"gopkg.in/yaml.v3"
)

type fixture struct {
	t      *testing.T
	s      *Server
	http   *httptest.Server
	config string
}
type reply struct {
	status int
	header http.Header
	data   map[string]any
	raw    string
	err    error
}

func testFixture(t *testing.T, amend func(*Config)) *fixture {
	t.Helper()
	t.Setenv("OIS_TEST_ADMIN", "secret-admin")
	t.Setenv("OIS_TEST_LINUX", "secret-linux")
	t.Setenv("OIS_TEST_WINDOWS", "secret-windows")
	t.Setenv("OIS_TEST_CONSUMER", "secret-consumer")
	cfg := Config{Version: 1, Admin: AdminConfig{TokenEnv: "OIS_TEST_ADMIN"}, Server: ServerConfig{Listen: "127.0.0.1:0", RequestTimeoutMS: 2000}, Channels: map[string]Channel{
		"linux-151": {ExpectedHostID: "linux-151", CredentialEnv: "OIS_TEST_LINUX"}, "windows-local": {ExpectedHostID: "windows-local", CredentialEnv: "OIS_TEST_WINDOWS"},
	}, Tools: map[string]Tool{}, VMCPS: map[string]VMCP{}}
	var names []string
	for _, host := range []string{"linux", "windows"} {
		ch, app := "linux-151", "bash"
		if host == "windows" {
			ch, app = "windows-local", "powershell"
		}
		for _, action := range []string{"exec_command", "write_stdin"} {
			name := host + "_" + action
			names = append(names, name)
			cfg.Tools[name] = Tool{Description: host + " command", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"yield_time_ms": map[string]any{"type": "integer"}}}, Source: Source{ch, app, action}}
		}
	}
	cfg.VMCPS["workbench"] = VMCP{Tools: names, Access: Access{CredentialEnv: "OIS_TEST_CONSUMER"}}
	if amend != nil {
		amend(&cfg)
	}
	path := filepath.Join(t.TempDir(), "ois.yaml")
	b, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, s: s, http: httptest.NewServer(s.Handler()), config: path}
	if cfg.Server.PublicURL == "" {
		s.cfg.Server.PublicURL = f.http.URL
	}
	t.Cleanup(func() { _ = s.Close(); f.http.Close() })
	return f
}

func (f *fixture) request(method, path, bearer string, body any, headers map[string]string) reply {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r, err := http.NewRequest(method, f.http.URL+path, bytes.NewReader(b))
	if err != nil {
		return reply{err: err}
	}
	r.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	res, err := f.http.Client().Do(r)
	if err != nil {
		return reply{err: err}
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	var data map[string]any
	_ = json.Unmarshal(raw, &data)
	return reply{status: res.StatusCode, header: res.Header, data: data, raw: string(raw), err: err}
}
func (f *fixture) must(method, path, bearer string, body any, status int) reply {
	f.t.Helper()
	r := f.request(method, path, bearer, body, nil)
	if r.err != nil || r.status != status {
		f.t.Fatalf("%s %s = %d %s (%v), want %d", method, path, r.status, r.raw, r.err, status)
	}
	return r
}
func (r reply) errorCode() string {
	e, _ := r.data["error"].(map[string]any)
	v, _ := e["code"].(string)
	return v
}

func (f *fixture) dial(host, channel, bearer string) *websocket.Conn {
	f.t.Helper()
	h := http.Header{}
	h.Set("Authorization", "Bearer "+bearer)
	c, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.http.URL, "http")+"/api/v1/oic/connect", h)
	if err != nil {
		if res != nil {
			res.Body.Close()
		}
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { c.Close() })
	if err = c.WriteJSON(protocol.Frame{Type: "bind", Protocol: 1, HostID: host, ChannelID: channel}); err != nil {
		f.t.Fatal(err)
	}
	return c
}
func readTestFrame(t *testing.T, c *websocket.Conn) protocol.Frame {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var frame protocol.Frame
		if err := c.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Type == "ping" {
			_ = c.WriteJSON(protocol.Frame{Type: "pong"})
			continue
		}
		return frame
	}
}
func fakeApps(app string) []protocol.AppInfo {
	return []protocol.AppInfo{{App: app, Ready: true, Actions: []protocol.ActionInfo{{Name: "exec_command", Description: "Local description differs", Ready: true, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"cmd": map[string]any{"type": "string"}}}}, {Name: "write_stdin", Ready: true, InputSchema: map[string]any{"type": "object"}}}}}
}
func (f *fixture) connect(channel string) *websocket.Conn {
	f.t.Helper()
	app, bearer := "bash", "secret-linux"
	if channel == "windows-local" {
		app, bearer = "powershell", "secret-windows"
	}
	c := f.dial(channel, channel, bearer)
	if frame := readTestFrame(f.t, c); frame.Type != "bound" {
		f.t.Fatalf("bind: %+v", frame)
	}
	if err := c.WriteJSON(protocol.Frame{Type: "register", Host: &protocol.HostInfo{ID: channel, Name: channel, OS: app, Arch: "amd64"}, Apps: fakeApps(app)}); err != nil {
		f.t.Fatal(err)
	}
	if frame := readTestFrame(f.t, c); frame.Type != "registered" {
		f.t.Fatalf("register: %+v", frame)
	}
	return c
}
func (f *fixture) asyncExec(tool string, args map[string]any) <-chan reply {
	done := make(chan reply, 1)
	go func() {
		done <- f.request("POST", "/api/v1/exec", "secret-consumer", map[string]any{"vmcp": "workbench", "tool": tool, "arguments": args}, nil)
	}()
	return done
}
func awaitReply(t *testing.T, ch <-chan reply) reply {
	t.Helper()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r
	case <-time.After(4 * time.Second):
		t.Fatal("request did not finish")
		return reply{}
	}
}
func awaitCondition(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestManagementPersistenceAndReferenceRules(t *testing.T) {
	f := testFixture(t, nil)
	f.must("GET", "/healthz", "", nil, 200)
	for _, bearer := range []string{"", "secret-consumer", "secret-linux"} {
		f.must("GET", "/api/v1/status", bearer, nil, 401)
	}
	for _, path := range []string{"status", "channels", "channels/linux-151", "channels/linux-151/apps", "tools", "tools/linux_exec_command", "vmcps", "vmcps/workbench", "hosts", "actions"} {
		r := f.must("GET", "/api/v1/"+path, "secret-admin", nil, 200)
		if strings.Contains(r.raw, "secret-") {
			t.Fatal("secret exposed")
		}
	}
	if r := f.must("DELETE", "/api/v1/channels/linux-151", "secret-admin", nil, 409); r.errorCode() != "RESOURCE_IN_USE" {
		t.Fatal(r.raw)
	}
	if r := f.must("DELETE", "/api/v1/tools/linux_exec_command", "secret-admin", nil, 409); r.errorCode() != "RESOURCE_IN_USE" {
		t.Fatal(r.raw)
	}
	target := f.s.Config().Tools["linux_exec_command"]
	target.Source.Channel = "windows-local"
	if r := f.must("PUT", "/api/v1/tools/linux_exec_command", "secret-admin", target, 409); r.errorCode() != "BINDING_IMMUTABLE" {
		t.Fatal(r.raw)
	}
	f.must("PUT", "/api/v1/tools/newtool", "secret-admin", Tool{InputSchema: map[string]any{}, Source: Source{"missing", "bash", "exec_command"}}, 400)
	f.must("PUT", "/api/v1/hosts/linux-151", "secret-admin", map[string]any{}, 404)
	f.must("PUT", "/api/v1/actions/exec_command", "secret-admin", map[string]any{}, 404)
	f.must("PUT", "/api/v1/channels/spare", "secret-admin", Channel{Description: "persist me", ExpectedHostID: "spare-host", CredentialEnv: "OIS_TEST_LINUX"}, 201)
	f.must("PUT", "/api/v1/channels/spare", "secret-admin", Channel{Description: "updated", ExpectedHostID: "spare-host", CredentialEnv: "OIS_TEST_LINUX"}, 200)
	f.must("PUT", "/api/v1/tools/sparetool", "secret-admin", Tool{InputSchema: map[string]any{}, Source: Source{"spare", "bash", "exec_command"}}, 201)
	precise := f.s.Config().Tools["linux_exec_command"]
	precise.InputSchema["properties"].(map[string]any)["opaque_integer"] = map[string]any{"type": "integer", "default": json.Number("9007199254740993")}
	f.must("PUT", "/api/v1/tools/linux_exec_command", "secret-admin", precise, 200)
	f.must("PUT", "/api/v1/vmcps/sparevm", "secret-admin", VMCP{Tools: []string{"sparetool"}, Access: Access{CredentialEnv: "OIS_TEST_CONSUMER"}}, 201)
	f.must("DELETE", "/api/v1/tools/sparetool", "secret-admin", nil, 409)
	f.must("DELETE", "/api/v1/vmcps/sparevm", "secret-admin", nil, 204)
	f.must("DELETE", "/api/v1/tools/sparetool", "secret-admin", nil, 204)
	reloaded, err := New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if reloaded.state.Channels["spare"].Description != "updated" {
		t.Fatal("YAML overwrote persistent API configuration")
	}
	serialized, _ := json.Marshal(reloaded.state.Tools["linux_exec_command"])
	if !strings.Contains(string(serialized), "9007199254740993") {
		t.Fatal("persistent schema value lost integer precision during another API update or restart")
	}
	f.must("DELETE", "/api/v1/channels/spare", "secret-admin", nil, 204)
	f.must("GET", "/api/v1/channels/spare", "secret-admin", nil, 404)
	if !filepath.IsAbs(f.s.Config().Server.DataFile) || f.s.Config().StateDir != ".ois" {
		t.Fatal("startup defaults not resolved")
	}
}

func TestChannelIdentityRegistrationAndReadOnlyViews(t *testing.T) {
	f := testFixture(t, nil)
	for _, tc := range []struct{ host, channel, token string }{{"wrong", "linux-151", "secret-linux"}, {"windows-local", "windows-local", "secret-linux"}} {
		c := f.dial(tc.host, tc.channel, tc.token)
		frame := readTestFrame(t, c)
		if frame.Type != "error" || frame.Error.Code != "FORBIDDEN" {
			t.Fatalf("bad binding accepted: %+v", frame)
		}
		c.Close()
	}
	c := f.connect("linux-151")
	dup := f.dial("linux-151", "linux-151", "secret-linux")
	if frame := readTestFrame(t, dup); frame.Error == nil || frame.Error.Code != "CHANNEL_CONNECTED" {
		t.Fatalf("duplicate: %+v", frame)
	}
	dup.Close()
	ch := f.s.Config().Channels["linux-151"]
	ch.ExpectedHostID = "another-host"
	f.must("PUT", "/api/v1/channels/linux-151", "secret-admin", ch, 409)
	for _, path := range []string{"hosts/linux-151", "hosts/linux-151/apps", "hosts/linux-151/actions", "channels/linux-151/apps/bash/actions", "channels/linux-151/apps/bash/actions/exec_command", "actions?host_id=linux-151&channel=linux-151&app=bash"} {
		f.must("GET", "/api/v1/"+path, "secret-admin", nil, 200)
	}
	r := f.must("GET", "/api/v1/actions?host_id=windows-local", "secret-admin", nil, 200)
	if len(r.data["items"].([]any)) != 0 {
		t.Fatal("host filter ignored")
	}
	if r := f.must("GET", "/api/v1/tools/linux_exec_command", "secret-admin", nil, 200); r.data["available"] != true {
		t.Fatal("independent local schema blocked readiness")
	}
	if err := c.WriteJSON(protocol.Frame{Type: "register", Apps: []protocol.AppInfo{}}); err != nil {
		t.Fatal(err)
	}
	readTestFrame(t, c)
	f.must("GET", "/api/v1/channels/linux-151/apps/bash/actions/exec_command", "secret-admin", nil, 404)
	r = awaitReply(t, f.asyncExec("linux_exec_command", map[string]any{"cmd": "pwd"}))
	if r.status != 503 || r.errorCode() != "APP_UNAVAILABLE" {
		t.Fatal(r.raw)
	}
	if err := c.WriteJSON(protocol.Frame{Type: "register", Apps: fakeApps("bash")}); err != nil {
		t.Fatal(err)
	}
	readTestFrame(t, c)
	c.Close()
	awaitCondition(t, func() bool { f.s.mu.RLock(); defer f.s.mu.RUnlock(); return f.s.live["linux-151"] == nil })
	r = f.must("GET", "/api/v1/channels/linux-151/apps/bash/actions/exec_command", "secret-admin", nil, 200)
	if r.data["available"] != false || r.data["stale"] != true {
		t.Fatal(r.raw)
	}
}

func TestExecUsesFixedSourceAndPreservesBusinessValues(t *testing.T) {
	f := testFixture(t, nil)
	linux := f.connect("linux-151")
	windows := f.connect("windows-local")
	for _, tc := range []struct {
		tool, app string
		c         *websocket.Conn
	}{{"linux_exec_command", "bash", linux}, {"windows_exec_command", "powershell", windows}} {
		done := f.asyncExec(tc.tool, map[string]any{"cmd": "exit 7", "yield_time_ms": "1000", "access_identity": "spoofed", "host_id": "elsewhere"})
		frame := readTestFrame(t, tc.c)
		if frame.Type != "invoke" || frame.App != tc.app || frame.Action != "exec_command" || frame.Input["yield_time_ms"] != "1000" || frame.Context.AccessIdentity != "consumer:workbench:OIS_TEST_CONSUMER" {
			t.Fatalf("wrong invoke: %+v", frame)
		}
		if err := tc.c.WriteJSON(protocol.ResultFrame(frame.CallID, map[string]any{"output": "stderr and stdout", "exit_code": 7})); err != nil {
			t.Fatal(err)
		}
		r := awaitReply(t, done)
		if r.status != 200 || r.data["result"].(map[string]any)["exit_code"] != float64(7) || r.data["call_id"] != frame.CallID {
			t.Fatal(r.raw)
		}
	}
	f.must("POST", "/api/v1/exec", "secret-admin", map[string]any{"vmcp": "workbench", "tool": "linux_exec_command", "arguments": map[string]any{}}, 401)
	f.must("POST", "/api/v1/exec", "secret-consumer", map[string]any{"vmcp": "workbench", "tool": "linux_exec_command", "arguments": map[string]any{}, "host_id": "windows-local"}, 400)
}

func TestAcceptedCallKeepsConfigurationSnapshot(t *testing.T) {
	f := testFixture(t, nil)
	c := f.connect("linux-151")
	done := f.asyncExec("linux_exec_command", map[string]any{"cmd": "slow"})
	frame := readTestFrame(t, c)
	vm := f.s.Config().VMCPS["workbench"]
	vm.Tools = []string{}
	f.must("PUT", "/api/v1/vmcps/workbench", "secret-admin", vm, 200)
	r := awaitReply(t, f.asyncExec("linux_exec_command", map[string]any{"cmd": "later"}))
	if r.status != 404 {
		t.Fatal(r.raw)
	}
	if err := c.WriteJSON(protocol.ResultFrame(frame.CallID, map[string]any{"output": "accepted before edit", "exit_code": 0})); err != nil {
		t.Fatal(err)
	}
	if r = awaitReply(t, done); r.status != 200 {
		t.Fatal(r.raw)
	}
}

func TestOICDisconnectIsUnknownAndNotReplayed(t *testing.T) {
	f := testFixture(t, nil)
	c := f.connect("linux-151")
	done := f.asyncExec("linux_exec_command", map[string]any{"cmd": "possibly started"})
	frame := readTestFrame(t, c)
	if frame.Type != "invoke" {
		t.Fatal(frame)
	}
	c.Close()
	r := awaitReply(t, done)
	if r.status != 504 || r.errorCode() != "CALL_RESULT_UNKNOWN" {
		t.Fatal(r.raw)
	}
	r = awaitReply(t, f.asyncExec("linux_exec_command", map[string]any{"cmd": "offline"}))
	if r.status != 503 || r.errorCode() != "CHANNEL_UNAVAILABLE" {
		t.Fatal(r.raw)
	}
	replacement := f.connect("linux-151")
	f.s.mu.RLock()
	pending := len(f.s.pending)
	f.s.mu.RUnlock()
	if pending != 0 {
		t.Fatal("old request retained for replay")
	}
	_ = replacement.SetReadDeadline(time.Now().Add(60 * time.Millisecond))
	_, _, err := replacement.ReadMessage()
	if err == nil {
		t.Fatal("unexpected replay on reconnect")
	}
}

func TestDeadlineCancelRequiresOwnerConfirmation(t *testing.T) {
	f := testFixture(t, func(c *Config) { c.Server.RequestTimeoutMS = 30 })
	c := f.connect("linux-151")
	done := f.asyncExec("linux_exec_command", map[string]any{"cmd": "slow"})
	started := readTestFrame(t, c)
	cancel := readTestFrame(t, c)
	if cancel.Type != "cancel" || cancel.CallID != started.CallID {
		t.Fatalf("cancel: %+v", cancel)
	}
	select {
	case <-done:
		t.Fatal("reported cancellation before owner confirmed")
	default:
	}
	if err := c.WriteJSON(protocol.Frame{Type: "error", CallID: started.CallID, Error: protocol.Err("CANCELLED", "execution owner confirmed cleanup")}); err != nil {
		t.Fatal(err)
	}
	r := awaitReply(t, done)
	if r.status != 422 || r.errorCode() != "CANCELLED" {
		t.Fatal(r.raw)
	}
}

func TestClientDisconnectDoesNotCancelExecution(t *testing.T) {
	f := testFixture(t, nil)
	c := f.connect("linux-151")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := strings.NewReader(`{"vmcp":"workbench","tool":"linux_exec_command","arguments":{"cmd":"slow"}}`)
	req, _ := http.NewRequestWithContext(ctx, "POST", f.http.URL+"/api/v1/exec", body)
	req.Header.Set("Authorization", "Bearer secret-consumer")
	done := make(chan error, 1)
	go func() {
		r, e := f.http.Client().Do(req)
		if r != nil {
			r.Body.Close()
		}
		done <- e
	}()
	frame := readTestFrame(t, c)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("HTTP client was not cancelled")
	}
	// A result can still be committed after the client loses its connection.
	if err := c.WriteJSON(protocol.ResultFrame(frame.CallID, map[string]any{"output": "finished", "exit_code": 0})); err != nil {
		t.Fatal(err)
	}
	awaitCondition(t, func() bool { f.s.mu.RLock(); defer f.s.mu.RUnlock(); return len(f.s.pending) == 0 })
	_ = c.SetReadDeadline(time.Now().Add(60 * time.Millisecond))
	_, data, err := c.ReadMessage()
	if err == nil {
		t.Fatalf("disconnect sent unexpected message: %s", data)
	}
}

func TestOriginAndMalformedBodies(t *testing.T) {
	f := testFixture(t, nil)
	r := f.request("GET", "/healthz", "", nil, map[string]string{"Origin": "https://attacker.invalid"})
	if r.status != 403 {
		t.Fatal(r.raw)
	}
	r = f.request("GET", "/healthz", "", nil, map[string]string{"Origin": f.http.URL})
	if r.status != 200 {
		t.Fatal(r.raw)
	}
	for _, body := range []string{`{"vmcp":"workbench"} {}`, `[]`, `{"arguments":42}`, `null`} {
		req, _ := http.NewRequest("POST", f.http.URL+"/api/v1/exec", strings.NewReader(body))
		res, err := f.http.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatalf("%s: %d", body, res.StatusCode)
		}
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.must("GET", "/healthz", "", nil, 503)
}

func TestHeartbeatMarksDisconnectedChannelUnavailable(t *testing.T) {
	f := testFixture(t, func(c *Config) { c.Server.HeartbeatIntervalMS = 10; c.Server.HeartbeatTimeoutMS = 50 })
	c := f.connect("linux-151")
	// Stop reading/responding. An authenticated but silent peer is not healthy.
	awaitCondition(t, func() bool { f.s.mu.RLock(); defer f.s.mu.RUnlock(); return f.s.live["linux-151"] == nil })
	c.Close()
	r := f.must("GET", "/api/v1/tools/linux_exec_command", "secret-admin", nil, 200)
	if r.data["available"] != false {
		t.Fatal(r.raw)
	}
}
