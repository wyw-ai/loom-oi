package ois

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func (f *fixture) mcpRequest(session string, body any) reply {
	h := map[string]string{"Accept": "application/json, text/event-stream", "MCP-Protocol-Version": protocol.MCPVersion}
	if session != "" {
		h["MCP-Session-Id"] = session
	}
	return f.request("POST", "/mcp/workbench", "secret-consumer", body, h)
}
func rpc(id any, method string, params any) map[string]any {
	r := map[string]any{"jsonrpc": "2.0", "method": method}
	if id != nil {
		r["id"] = id
	}
	if params != nil {
		r["params"] = params
	}
	return r
}
func (f *fixture) initialize() string {
	f.t.Helper()
	r := f.mcpRequest("", rpc(1, "initialize", map[string]any{"protocolVersion": protocol.MCPVersion, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}))
	if r.err != nil || r.status != 200 || r.header.Get("MCP-Session-Id") == "" {
		f.t.Fatalf("initialize: %+v", r)
	}
	if r.data["result"].(map[string]any)["protocolVersion"] != protocol.MCPVersion {
		f.t.Fatal(r.raw)
	}
	id := r.header.Get("MCP-Session-Id")
	r = f.mcpRequest(id, rpc(nil, "notifications/initialized", nil))
	if r.status != 202 {
		f.t.Fatal(r.raw)
	}
	return id
}

func TestMCPDiscoveryAndExecution(t *testing.T) {
	f := testFixture(t, nil)
	c := f.connect("linux-151")
	session := f.initialize()
	r := f.mcpRequest(session, rpc(2, "tools/list", map[string]any{}))
	if r.status != 200 {
		t.Fatal(r.raw)
	}
	tools := r.data["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 {
		t.Fatal(r.raw)
	}
	for _, tool := range tools {
		v := tool.(map[string]any)
		if v["inputSchema"] == nil || v["source"] != nil || v["credential_env"] != nil {
			t.Fatal(r.raw)
		}
	}
	done := make(chan reply, 1)
	go func() {
		done <- f.mcpRequest(session, rpc("exec", "tools/call", map[string]any{"name": "linux_exec_command", "arguments": map[string]any{"cmd": "sleep 600", "yield_time_ms": "10"}}))
	}()
	frame := readTestFrame(t, c)
	if frame.Input["yield_time_ms"] != "10" {
		t.Fatal(frame)
	}
	if err := c.WriteJSON(protocol.ResultFrame(frame.CallID, map[string]any{"output": "started", "session_id": "session-local", "opaque_integer": json.Number("9007199254740993")})); err != nil {
		t.Fatal(err)
	}
	r = awaitReply(t, done)
	if strings.Count(r.raw, "9007199254740993") != 2 {
		t.Fatalf("MCP changed an opaque App integer in text or structured content: %s", r.raw)
	}
	result := r.data["result"].(map[string]any)
	if result["isError"] != false || result["structuredContent"].(map[string]any)["session_id"] != "session-local" || result["_meta"].(map[string]any)["call_id"] != frame.CallID {
		t.Fatal(r.raw)
	}
	var content map[string]any
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if json.Unmarshal([]byte(text), &content) != nil || content["output"] != "started" {
		t.Fatal(text)
	}
	go func() {
		done <- f.mcpRequest(session, rpc(3, "tools/call", map[string]any{"name": "linux_write_stdin", "arguments": map[string]any{"session_id": "session-local", "chars": ""}}))
	}()
	frame = readTestFrame(t, c)
	if frame.Action != "write_stdin" || frame.Input["session_id"] != "session-local" {
		t.Fatal(frame)
	}
	if err := c.WriteJSON(protocol.Frame{Type: "error", CallID: frame.CallID, Error: protocol.Err("SESSION_NOT_FOUND", "unknown session")}); err != nil {
		t.Fatal(err)
	}
	r = awaitReply(t, done)
	result = r.data["result"].(map[string]any)
	if result["isError"] != true || result["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "SESSION_NOT_FOUND" {
		t.Fatal(r.raw)
	}
	r = f.mcpRequest(session, rpc(4, "tools/call", map[string]any{"name": "not-exposed", "arguments": map[string]any{}}))
	if r.data["error"].(map[string]any)["code"] != float64(-32602) {
		t.Fatal(r.raw)
	}
}

func TestMCPCancellationIsScopedAndDoesNotBecomeATool(t *testing.T) {
	f := testFixture(t, nil)
	c := f.connect("linux-151")
	owner, other := f.initialize(), f.initialize()
	done := make(chan reply, 1)
	go func() {
		done <- f.mcpRequest(owner, rpc(9, "tools/call", map[string]any{"name": "linux_exec_command", "arguments": map[string]any{"cmd": "slow"}}))
	}()
	started := readTestFrame(t, c)
	if r := f.mcpRequest(other, rpc(nil, "notifications/cancelled", map[string]any{"requestId": 9})); r.status != 202 {
		t.Fatal(r.raw)
	}
	// A cancellation in a different MCP conversation must not touch this call.
	if err := c.WriteJSON(protocol.ResultFrame(started.CallID, map[string]any{"output": "still owned", "exit_code": 0})); err != nil {
		t.Fatal(err)
	}
	r := awaitReply(t, done)
	if r.status != 200 || r.data["result"] == nil {
		t.Fatalf("other session cancelled the request: %+v", r)
	}
	go func() {
		done <- f.mcpRequest(owner, rpc(10, "tools/call", map[string]any{"name": "linux_exec_command", "arguments": map[string]any{"cmd": "slow"}}))
	}()
	started = readTestFrame(t, c)
	if started.Type != "invoke" {
		t.Fatalf("another session delivered an unexpected cancel: %+v", started)
	}
	if r := f.mcpRequest(owner, rpc(nil, "notifications/cancelled", map[string]any{"requestId": 10})); r.status != 202 {
		t.Fatal(r.raw)
	}
	cancelled := readTestFrame(t, c)
	if cancelled.Type != "cancel" || cancelled.CallID != started.CallID {
		t.Fatal(cancelled)
	}
	if err := c.WriteJSON(protocol.Frame{Type: "error", CallID: started.CallID, Error: protocol.Err("CANCELLED", "owner confirmed cleanup")}); err != nil {
		t.Fatal(err)
	}
	r = awaitReply(t, done)
	if r.status != 204 || r.raw != "" {
		t.Fatalf("cancelled MCP request received result: %+v", r)
	}
	// Closing an MCP conversation invalidates its protocol session, not all
	// application sessions created by this consumer.
	r = f.request("DELETE", "/mcp/workbench", "secret-consumer", nil, map[string]string{"MCP-Session-Id": owner, "MCP-Protocol-Version": protocol.MCPVersion})
	if r.status != 204 {
		t.Fatal(r.raw)
	}
	r = f.mcpRequest(owner, rpc(10, "tools/list", nil))
	if r.status != 404 {
		t.Fatal(r.raw)
	}
}

func TestMCPTransportAndSessionBoundaries(t *testing.T) {
	f := testFixture(t, nil)
	f.must("GET", "/mcp/workbench", "", nil, 401)
	f.must("GET", "/mcp/workbench", "secret-admin", nil, 401)
	f.must("GET", "/mcp/workbench", "secret-consumer", nil, 405)
	r := f.request("POST", "/mcp/workbench", "secret-consumer", rpc(1, "initialize", map[string]any{"protocolVersion": protocol.MCPVersion}), nil)
	if r.status != 406 {
		t.Fatal(r.raw)
	}
	r = f.mcpRequest("", rpc(1, "tools/list", nil))
	if r.status != 400 {
		t.Fatal(r.raw)
	}
	session := f.initialize()
	r = f.mcpRequest(session, rpc(4, "ping", nil))
	if r.data["result"] == nil {
		t.Fatal(r.raw)
	}
	r = f.mcpRequest(session, rpc(4, "unimplemented", nil))
	if r.data["error"].(map[string]any)["code"] != float64(-32601) {
		t.Fatal(r.raw)
	}
	r = f.request("POST", "/mcp/workbench", "secret-consumer", rpc(5, "tools/list", nil), map[string]string{"Accept": "application/json,text/event-stream", "MCP-Session-Id": session, "MCP-Protocol-Version": "2026-07-28"})
	if r.status != 400 {
		t.Fatal(r.raw)
	}
	r = f.request("POST", "/mcp/workbench", "secret-consumer", rpc(5, "tools/list", nil), map[string]string{"Accept": "application/json,text/event-stream", "MCP-Session-Id": session})
	if r.status != 400 {
		t.Fatal(r.raw)
	}
	vm := VMCP{Tools: []string{}, Access: Access{CredentialEnv: "OIS_TEST_CONSUMER"}}
	f.must("PUT", "/api/v1/vmcps/other", "secret-admin", vm, 201)
	r = f.request("POST", "/mcp/other", "secret-consumer", rpc(6, "tools/list", nil), map[string]string{"Accept": "application/json,text/event-stream", "MCP-Session-Id": session, "MCP-Protocol-Version": protocol.MCPVersion})
	if r.status != 403 {
		t.Fatal(r.raw)
	}
	if strings.Contains(r.raw, "secret-") {
		t.Fatal("secret in MCP response")
	}
	if f.s.Status() == nil {
		t.Fatal("missing local status")
	}
	// Reject JSON-RPC batches; Streamable HTTP carries a single message per POST.
	r = f.mcpRequest(session, []any{rpc(1, "ping", nil)})
	if r.data["error"] == nil {
		t.Fatal(r.raw)
	}
}
