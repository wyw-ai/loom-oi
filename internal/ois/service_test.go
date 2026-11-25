package ois

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestServiceRoutesRESTAndMCPToItsFixedMembers(t *testing.T) {
	f := testFixture(t, nil)
	linux := f.connect("linux-151")
	windows := f.connect("windows-local")
	svc := Service{
		Actions: []ServiceAction{
			{Name: "run_a", Source: Source{"linux-151", "bash", "exec_command"}},
			{Name: "run_b", Source: Source{"windows-local", "powershell", "exec_command"}},
		},
		Access: Access{CredentialEnv: "OIS_TEST_CONSUMER"}, Publish: Publish{MCP: true},
	}
	created := f.must("PUT", "/api/v1/services/workbench", "secret-admin", svc, 200)
	if created.data["endpoint"] != f.http.URL+"/mcp/workbench" {
		t.Fatal(created.raw)
	}
	f.must("GET", "/api/v1/services", "secret-consumer", nil, 401)
	f.must("GET", "/api/v1/services", "secret-admin", nil, 200)
	session := f.initialize()
	listed := f.mcpRequest(session, rpc(2, "tools/list", nil))
	tools := listed.data["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != "run_a" || tools[1].(map[string]any)["name"] != "run_b" {
		t.Fatal(listed.raw)
	}
	if !strings.Contains(listed.raw, "Local description differs") || !strings.Contains(listed.raw, "\"cmd\"") {
		t.Fatal("source schema and description missing from discovery", listed.raw)
	}
	req := map[string]any{"service": "workbench", "action": "run_a", "arguments": map[string]any{"cmd": "example"}}
	f.must("POST", "/api/v1/exec", "secret-admin", req, 401)
	done := make(chan reply, 1)
	go func() { done <- f.request("POST", "/api/v1/exec", "secret-consumer", req, nil) }()
	a := readTestFrame(t, linux)
	if a.App != "bash" || a.Action != "exec_command" || a.Input["cmd"] != "example" || a.Context.VMCPID != "workbench" || a.Context.AccessIdentity != "consumer:workbench:OIS_TEST_CONSUMER" {
		t.Fatal(a)
	}
	if err := linux.WriteJSON(protocol.ResultFrame(a.CallID, map[string]any{"origin": "A"})); err != nil {
		t.Fatal(err)
	}
	if r := awaitReply(t, done); r.status != 200 || r.data["result"].(map[string]any)["origin"] != "A" {
		t.Fatal(r.raw)
	}
	go func() { done <- f.mcpRequest(session, rpc(3, "tools/call", map[string]any{"name": "run_b"})) }()
	b := readTestFrame(t, windows)
	if b.App != "powershell" || b.Action != "exec_command" || b.Context != a.Context {
		t.Fatal(b)
	}
	if err := windows.WriteJSON(protocol.ResultFrame(b.CallID, map[string]any{"origin": "B"})); err != nil {
		t.Fatal(err)
	}
	if r := awaitReply(t, done); r.status != 200 || r.data["result"].(map[string]any)["structuredContent"].(map[string]any)["origin"] != "B" {
		t.Fatal(r.raw)
	}
	req["action"] = "linux_exec_command"
	f.must("POST", "/api/v1/exec", "secret-consumer", req, 404)
	req["tool"] = "run_a"
	f.must("POST", "/api/v1/exec", "secret-consumer", req, 400)

	resumed, err := New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	members := resumed.state.Services["workbench"].Actions
	if len(members) != 2 || members[1].Source != svc.Actions[1].Source || members[1].InputSchema["properties"] == nil {
		t.Fatalf("native Service did not retain source and published contract on restart: %+v", members)
	}
	bad := svc
	bad.Actions = []ServiceAction{svc.Actions[0], svc.Actions[0]}
	f.must("PUT", "/api/v1/services/workbench", "secret-admin", bad, 400)
	svc.Publish.MCP = false
	f.must("PUT", "/api/v1/services/workbench", "secret-admin", svc, 200)
	if r := f.mcpRequest(session, rpc(4, "tools/list", nil)); r.status != 404 {
		t.Fatal(r.raw)
	}
	f.must("DELETE", "/api/v1/services/workbench", "secret-admin", nil, 204)
	f.must("GET", "/api/v1/services/workbench", "secret-admin", nil, 404)
}

func TestServiceMigratesPersistedBetaState(t *testing.T) {
	f := testFixture(t, nil)
	cfg := f.s.Config()
	oldTool := cfg.Tools["linux_exec_command"]
	oldTool.Description = "Consumer contract on disk"
	oldTool.InputSchema["default"] = json.Number("9007199254740993")
	oldVM := VMCP{Access: Access{CredentialEnv: "PERSISTED_CONSUMER"}, Tools: []string{"saved_alias"}}
	data, err := json.Marshal(map[string]any{
		"version": 1, "channels": cfg.Channels,
		"tools": map[string]Tool{"saved_alias": oldTool}, "vmcps": map[string]VMCP{"disk_only": oldVM},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Server.DataFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	resumed, err := New(f.config)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	svc := resumed.state.Services["disk_only"]
	if len(resumed.state.Services) != 1 || !svc.Publish.MCP || svc.Access != oldVM.Access || len(svc.Actions) != 1 || svc.Actions[0].Name != "saved_alias" || svc.Actions[0].Source != oldTool.Source || svc.Actions[0].Description != oldTool.Description || !reflect.DeepEqual(svc.Actions[0].InputSchema, oldTool.InputSchema) {
		t.Fatalf("persistent beta service changed during import: %+v", resumed.state.Services)
	}
	updated, err := os.ReadFile(cfg.Server.DataFile)
	if err != nil {
		t.Fatal(err)
	}
	var disk persisted
	if err := decodeJSONNumbers(updated, &disk); err != nil || disk.Version != 2 || !reflect.DeepEqual(disk.Services, resumed.state.Services) {
		t.Fatalf("migration not persisted: %s (%v)", updated, err)
	}
}

func TestServiceBootstrapAndScopedNames(t *testing.T) {
	f := testFixture(t, func(cfg *Config) {
		cfg.Services = map[string]Service{
			"rest_only": {Actions: []ServiceAction{{Source: Source{"linux-151", "bash", "exec_command"}}}},
		}
	})
	f.must("GET", "/mcp/rest_only", "", nil, 404)
	ch := Channel{ExpectedHostID: "extra", CredentialEnv: "OIS_TEST_LINUX"}
	f.must("PUT", "/api/v1/channels/extra", "secret-admin", ch, 201)
	other := Service{Actions: []ServiceAction{{Name: "exec_command", Source: Source{"extra", "other-app", "run"}}}}
	f.must("PUT", "/api/v1/services/other", "secret-admin", other, 201)
	f.must("DELETE", "/api/v1/channels/extra", "secret-admin", nil, 409)
	f.must("PUT", "/api/v1/vmcps/other", "secret-admin", VMCP{}, 409)
	f.connect("linux-151")
	r := f.must("GET", "/api/v1/services/rest_only", "secret-admin", nil, 200)
	member := r.data["actions"].([]any)[0].(map[string]any)
	if r.data["endpoint"] != nil || member["name"] != "exec_command" || member["description"] != "Local description differs" || member["input_schema"] == nil {
		t.Fatal(r.raw)
	}
	r = f.must("POST", "/api/v1/exec", "", map[string]any{"service": "other", "action": "exec_command", "arguments": map[string]any{}}, 503)
	if r.errorCode() != "CHANNEL_UNAVAILABLE" {
		t.Fatal(r.raw)
	}
}
