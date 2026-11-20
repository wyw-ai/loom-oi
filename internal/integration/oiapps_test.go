//go:build oiapps

// These tests load built App packages in a real OIC, connected to a local OIS
// over WebSocket. Each test has separate private state and a loopback HTTP port.
package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/oic"
	"github.com/wyw-ai/loom-oi/internal/ois"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"gopkg.in/yaml.v3"
)

type object = map[string]any

type consumer struct {
	t            *testing.T
	url, session string
	client       *http.Client
}

func waitUntil(t *testing.T, description string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func startApp(t *testing.T, app string, settings object) *consumer {
	t.Helper()
	pkg := os.Getenv("OI_" + strings.ToUpper(app) + "_MANIFEST")
	if pkg == "" {
		t.Fatalf("set OI_%s_MANIFEST to the built package app.yaml", strings.ToUpper(app))
	}
	dir := t.TempDir()
	t.Setenv("OI_DEMO_ADMIN_TOKEN", "test-admin")
	t.Setenv("OI_CHANNEL_TOKEN", "test-channel")
	t.Setenv("OI_WORKBENCH_TOKEN", "test-consumer")
	sample, err := os.ReadFile("../../docs/spec/reference/oiapps/examples/ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	oisPath := filepath.Join(dir, "ois.yaml")
	if err := os.WriteFile(oisPath, sample, 0600); err != nil {
		t.Fatal(err)
	}
	center, err := ois.New(oisPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(center.Handler())
	t.Cleanup(func() { _ = center.Close(); server.Close() })

	cfg, err := yaml.Marshal(object{"version": 1, "host": object{"id": "local-workstation"}, "channel": object{"id": "local-workstation"},
		"ois": object{"url": server.URL, "credential_env": "OI_CHANNEL_TOKEN"}, "state_dir": filepath.Join(dir, "host"), "app_config": object{app: settings}})
	if err != nil {
		t.Fatal(err)
	}
	oicPath := filepath.Join(dir, "oic.yaml")
	if err := os.WriteFile(oicPath, cfg, 0600); err != nil {
		t.Fatal(err)
	}
	host, err := oic.New(oicPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("OIC did not shut down")
		}
	})
	waitUntil(t, "OIC connection", func() bool { return host.Status().Connected })
	if _, err := host.Load(pkg); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "App registration", func() bool { apps := host.Apps(); return len(apps) == 1 && apps[0].Ready && apps[0].Registered })

	c := &consumer{t: t, client: server.Client(), url: server.URL}
	_, header := c.post("/mcp/workbench", object{"id": 1, "jsonrpc": "2.0", "method": "initialize", "params": object{"protocolVersion": protocol.MCPVersion}})
	c.session = header.Get("Mcp-Session-Id")
	if c.session == "" {
		t.Fatal("missing MCP session")
	}
	c.post("/mcp/workbench", object{"jsonrpc": "2.0", "method": "notifications/initialized"})
	listed, _ := c.post("/mcp/workbench", object{"id": 2, "jsonrpc": "2.0", "method": "tools/list"})
	assertPublishedActions(t, pkg, listed["result"].(object)["tools"].([]any))
	return c
}

func assertPublishedActions(t *testing.T, pkg string, tools []any) {
	t.Helper()
	if got := len(tools); got != 25 {
		t.Fatalf("tools/list has %d tools", got)
	}
	var packaged oic.AppDefinition
	manifest, err := os.ReadFile(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(manifest, &packaged); err != nil {
		t.Fatal(err)
	}
	published := make(map[string]any)
	for _, item := range tools {
		tool := item.(object)
		name := tool["name"].(string)
		if _, exists := published[name]; exists {
			t.Fatalf("duplicate tools/list entry %s", name)
		}
		published[name] = tool["inputSchema"]
	}
	for name, action := range packaged.Actions {
		tool := packaged.App.Code + "_" + name
		want, err := json.Marshal(action.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(published[tool])
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("tools/list schema differs from packaged Action %s", tool)
		}
	}
}

func (c *consumer) post(path string, body object) (object, http.Header) {
	c.t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		c.t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), "POST", c.url+path, bytes.NewReader(raw))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-consumer")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
		req.Header.Set("MCP-Protocol-Version", protocol.MCPVersion)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		c.t.Fatalf("%s: HTTP %d: %.1000s", path, resp.StatusCode, data)
	}
	out := object{}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &out); err != nil {
			c.t.Fatal(err)
		}
	}
	if out["error"] != nil {
		c.t.Fatalf("RPC error: %v", out["error"])
	}
	return out, resp.Header
}

func (c *consumer) rest(name string, args object) object {
	c.t.Helper()
	out, _ := c.post("/api/v1/exec", object{"vmcp": "workbench", "tool": name, "arguments": args})
	return out["result"].(object)
}
func (c *consumer) mcp(name string, args object) object {
	c.t.Helper()
	out, _ := c.post("/mcp/workbench", object{"id": 3, "jsonrpc": "2.0", "method": "tools/call", "params": object{"name": name, "arguments": args}})
	result := out["result"].(object)
	if result["isError"] != false {
		c.t.Fatalf("%s: %v", name, result)
	}
	return result
}

func assertImage(t *testing.T, response object, width, height int) {
	t.Helper()
	images := 0
	for _, item := range response["content"].([]any) {
		block := item.(object)
		if block["type"] != "image" {
			continue
		}
		images++
		if block["mimeType"] != "image/png" {
			t.Fatalf("unexpected crop MIME %v", block["mimeType"])
		}
		b, err := base64.StdEncoding.Strict().DecodeString(block["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		meta := response["structuredContent"].(object)
		if img.Bounds().Dx() != width || img.Bounds().Dy() != height || int(meta["encoded_bytes"].(float64)) != len(b) || meta["resized"] != false {
			t.Fatalf("inconsistent image dimensions or metadata: %v", meta)
		}
	}
	if images != 1 {
		t.Fatalf("received %d images", images)
	}
}

func TestFilesystemPackage(t *testing.T) {
	root := t.TempDir()
	c := startApp(t, "filesystem", object{"root": root, "read_only": false})
	path := "notes/task.txt"
	c.rest("filesystem_write_file", object{"path": path, "content": "old value\n", "create_parents": true})
	for name, args := range map[string]object{"list_directory": {"path": "notes"}, "find_files": {"pattern": "**/*.txt"}, "search_content": {"query": "old value"}} {
		r := c.mcp("filesystem_"+name, args)
		if !strings.Contains(fmt.Sprint(r["structuredContent"]), "task.txt") {
			t.Fatalf("%s did not find fixture", name)
		}
	}
	before := c.rest("filesystem_read_file", object{"path": path})
	c.mcp("filesystem_edit_file", object{"path": path, "expected_revision": before["revision"], "edits": []object{{"old_text": "old", "new_text": "new"}}})
	after := c.rest("filesystem_read_file", object{"path": path})
	if after["text"] != "new value\n" || after["revision"] == before["revision"] {
		t.Fatal(after)
	}
	original, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNgAAAAAgABSK+kcQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pixel.png"), original, 0600); err != nil {
		t.Fatal(err)
	}
	args := object{"path": "pixel.png", "crop": object{"x": 0, "y": 0, "width": 1, "height": 1}}
	assertImage(t, c.mcp("filesystem_view_image", args), 1, 1)
	assertImage(t, c.rest("filesystem_view_image", args), 1, 1)
	if runtime.GOOS == "windows" {
		t.Run("PowerShellClient", func(t *testing.T) {
			const script = `
$ErrorActionPreference = 'Stop'
Import-Module ../../scripts/McpClient.psm1
$mcp = Connect-LoomMcp -Url $env:OI_TEST_MCP_URL -Token test-consumer
try {
    $preview = Invoke-LoomTool $mcp filesystem_view_image @{ path = 'pixel.png'; crop = @{ x = 0; y = 0; width = 1; height = 1 } }
    $image = @($preview.content | Where-Object { $_.type -eq 'image' })
    if ($image.Count -ne 1 -or $image[0].mimeType -ne 'image/png') { throw 'Missing native image block' }
    $bytes = [Convert]::FromBase64String($image[0].data)
    if ($bytes.Length -ne $preview.structuredContent.encoded_bytes) { throw 'Image byte count mismatch' }
    $plain = Invoke-LoomTool $mcp filesystem_read_file @{ path = 'notes/task.txt' }
    if ($plain.text.TrimEnd() -ne 'new value' -or -not $plain._call_id) { throw 'Invalid plain JSON result' }
} finally { $mcp.Client.Dispose() }
`
			cmd := exec.CommandContext(t.Context(), "pwsh", "-NoProfile", "-NonInteractive", "-Command", script)
			cmd.Env = append(os.Environ(), "OI_TEST_MCP_URL="+c.url+"/mcp/workbench")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("PowerShell MCP client: %v: %s", err, output)
			}
		})
	}
}

func TestBrowserPackage(t *testing.T) {
	var submissions atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/submit" && r.Method == "POST" {
			submissions.Add(1)
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Form</title>
<label>Name<input id="name"></label>
<label for="plan">Plan</label><select id="plan"><option value="a">A</option><option value="b">B</option></select>
<label><input id="opt" type="checkbox">Notify</label>
<button onclick="if(confirm('Confirm?')){let form=[document.getElementById('name').value,plan.value,opt.checked];fetch('/submit',{method:'POST'}).then(()=>document.querySelector('output').textContent='Submitted '+form.join(' '))}">Submit</button>
<output></output><div style="height:200vh"></div>`)
	}))
	defer site.Close()
	state := t.TempDir()
	if runtime.GOOS == "windows" {
		account, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		// Temp directories can inherit grants to other local identities.
		// Install the same private DACL required of a deployed profile.
		out, err := exec.Command("icacls.exe", state, "/inheritance:r", "/grant:r",
			"*"+account.Uid+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F").CombinedOutput()
		if err != nil {
			t.Fatalf("make browser state private: %v: %s", err, out)
		}
	} else if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	c := startApp(t, "browser", object{"state_dir": state})
	created := c.rest("browser_create_session", object{})
	session, tab := created["session_id"], created["tab_id"]
	page := func(args object) object { args["session_id"] = session; args["tab_id"] = tab; return args }
	sessions := c.rest("browser_list_sessions", object{})
	if len(sessions["sessions"].([]any)) != 1 {
		t.Fatal("new session missing")
	}
	other := c.rest("browser_open_tab", object{"session_id": session})
	c.rest("browser_close_tab", object{"session_id": session, "tab_id": other["tab_id"]})
	c.rest("browser_navigate", page(object{"url": site.URL}))
	snap := c.rest("browser_snapshot", page(object{}))
	label := func(text string) object {
		return object{"kind": "label", "document_id": snap["document_id"], "text": text}
	}
	c.rest("browser_fill", page(object{"target": label("Name"), "text": "Agent"}))
	c.rest("browser_press_key", page(object{"key": "Tab", "target": label("Name")}))
	c.rest("browser_select_option", page(object{"target": label("Plan"), "values": []string{"b"}}))
	c.rest("browser_set_checked", page(object{"target": label("Notify"), "checked": true}))
	c.rest("browser_scroll", page(object{"delta_y": 160}))
	c.rest("browser_hover", page(object{"target": label("Name")}))
	snap = c.rest("browser_snapshot", page(object{}))
	var target object
	for _, entry := range snap["elements"].([]any) {
		el := entry.(object)
		if el["role"] == "button" && el["name"] == "Submit" {
			target = object{"kind": "ref", "snapshot_id": snap["snapshot_id"], "ref": el["ref"]}
			break
		}
	}
	if target == nil {
		t.Fatal("Submit button missing from snapshot")
	}
	click := c.rest("browser_click", page(object{"target": target}))
	if click["status"] != "pending" {
		t.Fatalf("expected pending click, got %v", click)
	}
	dialog := click["blocked_by"].([]any)[0].(object)["id"]
	c.rest("browser_handle_dialog", page(object{"dialog_id": dialog, "action": "accept"}))
	waitUntil(t, "original click to settle", func() bool {
		tabs := c.rest("browser_list_tabs", object{"session_id": session, "operation_id": click["operation_id"]})
		return tabs["operation"].(object)["state"] == "completed"
	})
	observed := c.rest("browser_wait_for", page(object{"condition": object{"kind": "text", "text": "Submitted Agent b true"}}))
	if observed["status"] != "matched" || submissions.Load() != 1 {
		t.Fatalf("submitted %d times; wait result %v", submissions.Load(), observed)
	}
	args := page(object{"clip": object{"x": 0, "y": 0, "width": 240, "height": 48}})
	assertImage(t, c.mcp("browser_screenshot", args), 240, 48)
	assertImage(t, c.rest("browser_screenshot", args), 240, 48)
	c.rest("browser_close_session", object{"session_id": session})
}
