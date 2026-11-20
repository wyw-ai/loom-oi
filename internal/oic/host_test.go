package oic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"gopkg.in/yaml.v3"
)

// The test binary doubles as an independent, business-neutral worker. This
// exercises real stdio and process cleanup rather than mocking the runtime.
func TestWorkerProcess(t *testing.T) {
	helper := false
	for _, arg := range os.Args {
		if arg == "oic-worker" {
			helper = true
		}
	}
	if !helper {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), protocol.MaxFrameBytes)
	enc := json.NewEncoder(os.Stdout)
	var config map[string]any
	var mu sync.Mutex
	send := func(f protocol.Frame) { mu.Lock(); defer mu.Unlock(); _ = enc.Encode(f) }
	for scanner.Scan() {
		var f protocol.Frame
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			os.Exit(3)
		}
		switch f.Type {
		case "start":
			config = f.Config
			if path, ok := config["started_file"].(string); ok {
				_ = os.WriteFile(path, []byte(fmt.Sprint(os.Getpid())), 0600)
			}
			var formats []string
			switch config["format_handshake"] {
			case "accept":
				formats = f.ResultFormats
			case "overclaim":
				formats = []string{protocol.FormatJSON, "future"}
			case "downgrade":
				// Old worker: omit result_formats.
			default:
				if f.ResultFormats != nil {
					os.Exit(4) // Old strict worker rejects extension fields.
				}
			}
			send(protocol.Frame{Type: "ready", Protocol: 1, ResultFormats: formats, Handlers: []string{"echo", "echo_exit", "preview", "wait", "crash"}})
		case "invoke":
			switch f.Handler {
			case "preview":
				result := protocol.ResultFrame(f.CallID, map[string]any{"content": []map[string]string{{"type": "text", "text": "preview"}}})
				result.ResultFormat = protocol.FormatContent
				send(result)
			case "echo":
				send(protocol.ResultFrame(f.CallID, map[string]any{"pid": os.Getpid(), "input": f.Input, "config": config, "token": os.Getenv("OIC_TEST_TOKEN"), "extra_secret": os.Getenv("OIC_OTHER_SECRET")}))
			case "wait":
			case "echo_exit":
				send(protocol.ResultFrame(f.CallID, map[string]any{"output": "final result"}))
				os.Exit(0)
			case "crash":
				os.Exit(9)
			}
		case "cancel":
			send(workerError(f.CallID, "CALL_CANCELLED", "fixture cancelled"))
		}
	}
	if path, ok := config["exited_file"].(string); ok {
		_ = os.WriteFile(path, []byte("closed"), 0600)
	}
	os.Exit(0)
}

func fixtureCommand(t *testing.T) []any {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []any{exe, "-test.run=^TestWorkerProcess$", "--", "oic-worker"}
}
func fixtureDefinition(t *testing.T) AppDefinition {
	t.Helper()
	var d AppDefinition
	d.Version = 1
	d.App.Code = "fixture"
	d.Runtime = map[string]any{"command": fixtureCommand(t)}
	d.Actions = map[string]ActionDefinition{}
	for _, name := range []string{"echo", "echo_exit", "wait", "crash"} {
		d.Actions[name] = ActionDefinition{Description: name, InputSchema: map[string]any{"type": "object", "properties": map[string]any{"count": map[string]any{"type": "integer", "default": 1}}}}
	}
	return d
}
func writeYAML(t *testing.T, path string, v any) {
	t.Helper()
	b, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func newFixture(t *testing.T, appConfig map[string]any) (*Host, string, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "oic.yaml")
	config := map[string]any{"version": 1, "host": map[string]any{"id": "fixture-host"}, "channel": map[string]any{"id": "fixture-channel"}, "ois": map[string]any{"url": "https://127.0.0.1:1", "credential_env": "OIC_TEST_TOKEN"}, "state_dir": ".oic", "app_config": map[string]any{"fixture": appConfig}}
	writeYAML(t, configPath, config)
	h, err := New(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	appPath := filepath.Join(dir, "app.yaml")
	writeYAML(t, appPath, fixtureDefinition(t))
	return h, appPath, configPath
}
func fixtureFrame(id, action string) protocol.Frame {
	return protocol.Frame{Type: "invoke", CallID: id, App: "fixture", Action: action, Input: map[string]any{"count": "7"}, Context: protocol.Context{AccessIdentity: "owner", VMCPID: "workbench"}}
}
func resultMap(t *testing.T, f protocol.Frame) map[string]any {
	t.Helper()
	if f.Type != "result" {
		t.Fatalf("unexpected response: %#v", f)
	}
	var result map[string]any
	if err := json.Unmarshal(f.Result, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestHostLoadPersistenceIdempotenceAndWorkerIsolation(t *testing.T) {
	t.Setenv("OIC_TEST_TOKEN", "must-not-enter-worker")
	t.Setenv("OIC_OTHER_SECRET", "also-private")
	h, path, configPath := newFixture(t, map[string]any{"marker": "local-config"})
	s, err := h.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Ready || s.Registered || s.RuntimeCount != 1 {
		t.Fatalf("unexpected status %+v", s)
	}
	first := resultMap(t, h.Invoke(context.Background(), fixtureFrame("one", "echo")))
	if first["token"] != "" || first["extra_secret"] != "" {
		t.Fatal("OIC credentials leaked into worker environment")
	}
	if first["input"].(map[string]any)["count"] != float64(7) {
		t.Fatal("worker did not receive bound value")
	}
	if _, err = h.Load(path); err != nil {
		t.Fatal(err)
	}
	second := resultMap(t, h.Invoke(context.Background(), fixtureFrame("two", "echo")))
	if first["pid"] != second["pid"] {
		t.Fatal("same definition started another worker")
	}
	d := fixtureDefinition(t)
	a := d.Actions["echo"]
	a.Description = "changed"
	d.Actions["echo"] = a
	writeYAML(t, path, d)
	if _, err = h.Load(path); err == nil {
		t.Fatal("changed live definition accepted")
	}
	if err = h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(configPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	restarted.restore()
	third := resultMap(t, restarted.Invoke(context.Background(), fixtureFrame("three", "echo")))
	if first["pid"] == third["pid"] {
		t.Fatal("restart retained old worker")
	}
	if err = restarted.Unload("fixture"); err != nil {
		t.Fatal(err)
	}
	loaded, err := os.ReadFile(filepath.Join(restarted.StateDir(), "loaded.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(loaded), "fixture") {
		t.Fatal("unload retained loaded record")
	}
}

func TestRuntimeOverrideCreatesSeparateInstanceAndRollback(t *testing.T) {
	h, path, _ := newFixture(t, nil)
	d := fixtureDefinition(t)
	a := d.Actions["echo"]
	a.Handler = "echo"
	d.Actions["same"] = a
	a.Runtime = map[string]any{"command": fixtureCommand(t)}
	d.Actions["separate"] = a
	writeYAML(t, path, d)
	s, err := h.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.RuntimeCount != 2 {
		t.Fatalf("got %d runtime instances", s.RuntimeCount)
	}
	one := resultMap(t, h.Invoke(context.Background(), fixtureFrame("one", "echo")))
	same := resultMap(t, h.Invoke(context.Background(), fixtureFrame("same", "same")))
	other := resultMap(t, h.Invoke(context.Background(), fixtureFrame("other", "separate")))
	if one["pid"] != same["pid"] || one["pid"] == other["pid"] {
		t.Fatal("runtime instance ownership incorrect")
	}
	if err = h.Unload("fixture"); err != nil {
		t.Fatal(err)
	}
	started := filepath.Join(filepath.Dir(path), "started")
	exited := filepath.Join(filepath.Dir(path), "exited")
	h.config.AppConfig["fixture"] = map[string]any{"started_file": started, "exited_file": exited}
	d.Runtime = nil
	d.Actions = map[string]ActionDefinition{"a": {Description: "first", Handler: "echo", InputSchema: map[string]any{"type": "object"}, Runtime: map[string]any{"command": fixtureCommand(t)}}, "z": {Description: "fails", Handler: "echo", InputSchema: map[string]any{"type": "object"}, Runtime: map[string]any{"command": []any{filepath.Join(filepath.Dir(path), "does-not-exist")}}}}
	writeYAML(t, path, d)
	if _, err = h.Load(path); err == nil {
		t.Fatal("partial runtime failure accepted")
	}
	if len(h.Apps()) != 0 {
		t.Fatal("partial App was published")
	}
	if _, err = os.Stat(started); err != nil {
		t.Fatal("rollback test never initialized first runtime")
	}
	eventually(t, func() bool { _, err := os.Stat(exited); return err == nil })
}

func TestCancellationDuplicateConflictAndWorkerCrash(t *testing.T) {
	h, path, _ := newFixture(t, nil)
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan protocol.Frame, 1)
	go func() { done <- h.Invoke(ctx, fixtureFrame("waiting", "wait")) }()
	eventually(t, func() bool { h.mu.RLock(); defer h.mu.RUnlock(); return h.calls["waiting"] != nil })
	cancel()
	select {
	case got := <-done:
		if got.Error == nil || got.Error.Code != "CALL_CANCELLED" {
			t.Fatalf("bad cancelled response: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel was not delivered")
	}
	f := fixtureFrame("cached", "echo")
	one := h.Invoke(context.Background(), f)
	two := h.Invoke(context.Background(), f)
	if string(one.Result) != string(two.Result) {
		t.Fatal("duplicate did not reuse result")
	}
	f.Input = map[string]any{"count": 99}
	conflict := h.Invoke(context.Background(), f)
	if conflict.Error == nil || conflict.Error.Code != "CALL_ID_CONFLICT" {
		t.Fatalf("bad conflict: %+v", conflict)
	}
	h.Invoke(context.Background(), fixtureFrame("crash", "crash"))
	eventually(t, func() bool { return !h.Apps()[0].Ready })
	if got := h.Invoke(context.Background(), fixtureFrame("after-crash", "echo")); got.Error == nil || got.Error.Code != "APP_UNAVAILABLE" {
		t.Fatal("crashed App remained callable")
	}
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	if h.Apps()[0].Ready {
		t.Fatal("same load automatically restarted crashed worker")
	}
}

func TestFinalResponseIsDrainedBeforeWorkerExit(t *testing.T) {
	h, path, _ := newFixture(t, nil)
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	result := resultMap(t, h.Invoke(context.Background(), fixtureFrame("final", "echo_exit")))
	if result["output"] != "final result" {
		t.Fatal(result)
	}
	eventually(t, func() bool { return !h.Apps()[0].Ready })
}

func TestActionOnlyRuntimeAndInvalidAppYAML(t *testing.T) {
	h, path, _ := newFixture(t, nil)
	d := fixtureDefinition(t)
	a := d.Actions["echo"]
	a.Runtime = d.Runtime
	d.Runtime = nil
	d.Actions = map[string]ActionDefinition{"echo": a}
	writeYAML(t, path, d)
	if _, err := h.Load(path); err != nil {
		t.Fatal(err)
	}
	resultMap(t, h.Invoke(context.Background(), fixtureFrame("action-only", "echo")))
	if err := h.Unload("fixture"); err != nil {
		t.Fatal(err)
	}
	a.InputSchema = nil
	d.Actions["echo"] = a
	writeYAML(t, path, d)
	if _, err := h.Load(path); err == nil {
		t.Fatal("missing local Schema accepted")
	}
	writeYAML(t, path, fixtureDefinition(t))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\n---\nignored: document\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Load(path); err == nil {
		t.Fatal("extra YAML document silently ignored")
	}
}
