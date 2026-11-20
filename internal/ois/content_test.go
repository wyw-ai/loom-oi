package ois

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestContentFormatFollowsDispatchedAction(t *testing.T) {
	f := testFixture(t, nil)
	channel := "linux-151"
	ws := f.dial(channel, channel, "secret-linux", protocol.FormatJSON, protocol.FormatContent)
	if bound := readTestFrame(t, ws); !protocol.SupportsFormat(bound.ResultFormats, protocol.FormatContent) {
		t.Fatal("image result capability was not negotiated", bound)
	}
	apps := fakeApps("bash") // the host uses the binding, regardless of App name
	register := func(format string) {
		t.Helper()
		apps[0].Actions[0].ResultFormat = format
		if err := ws.WriteJSON(protocol.Frame{Type: "register", Apps: apps}); err != nil {
			t.Fatal(err)
		}
		if got := readTestFrame(t, ws); got.Type != "registered" {
			t.Fatalf("registration failed: %+v", got)
		}
	}
	register(protocol.FormatContent)
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNgAAAAAgABSK+kcQAAAABJRU5ErkJggg=="
	payload := map[string]any{
		"content":           []any{map[string]any{"type": "image", "mimeType": "image/png", "data": png}},
		"structuredContent": map[string]any{"encoded_bytes": 67},
	}
	answer := func(id, format string) {
		t.Helper()
		frame := protocol.ResultFrame(id, payload)
		frame.ResultFormat = format
		if err := ws.WriteJSON(frame); err != nil {
			t.Fatal(err)
		}
	}
	replyChan := f.asyncExec("linux_exec_command", map[string]any{})
	call := readTestFrame(t, ws)
	register("") // an in-flight invocation keeps its original declaration
	answer(call.CallID, protocol.FormatContent)
	got := awaitReply(t, replyChan)
	if got.status != 200 || got.data["result_format"] != protocol.FormatContent || strings.Count(got.raw, png) != 1 {
		t.Fatal(got.raw)
	}
	replyChan = f.asyncExec("linux_exec_command", map[string]any{})
	call = readTestFrame(t, ws)
	answer(call.CallID, protocol.FormatContent)
	got = awaitReply(t, replyChan)
	if got.status == 200 || got.errorCode() != "RESULT_FORMAT_UNSUPPORTED" {
		t.Fatal(got.raw)
	}

	session := f.initialize()
	mcp := func() <-chan reply {
		ch := make(chan reply, 1)
		go func() {
			ch <- f.mcpRequest(session, rpc(3, "tools/call", map[string]any{"name": "linux_exec_command", "arguments": map[string]any{}}))
		}()
		return ch
	}
	plain := mcp()
	answer(readTestFrame(t, ws).CallID, "")
	body := awaitReply(t, plain).data["result"].(map[string]any)
	if body["content"].([]any)[0].(map[string]any)["type"] != "text" {
		t.Fatal("ordinary JSON was promoted to an image")
	}
	register(protocol.FormatContent)
	native := mcp()
	answer(readTestFrame(t, ws).CallID, protocol.FormatContent)
	got = awaitReply(t, native)
	body = got.data["result"].(map[string]any)
	if body["content"].([]any)[0].(map[string]any)["data"] != png || strings.Count(got.raw, png) != 1 || body["isError"] != false {
		t.Fatal(got.raw)
	}
}

func TestLegacyConnectionDisablesOnlyImageAction(t *testing.T) {
	f := testFixture(t, nil)
	ws := f.dial("linux-151", "linux-151", "secret-linux")
	if bound := readTestFrame(t, ws); bound.ResultFormats != nil {
		t.Fatal("old bind should receive a legacy bound")
	}
	apps := fakeApps("bash")
	apps[0].Actions[0].ResultFormat = protocol.FormatContent
	if err := ws.WriteJSON(protocol.Frame{Type: "register", Apps: apps}); err != nil {
		t.Fatal(err)
	}
	if got := readTestFrame(t, ws); got.Type != "registered" {
		t.Fatal(got)
	}
	image := awaitReply(t, f.asyncExec("linux_exec_command", map[string]any{}))
	if image.status != 503 || image.errorCode() != "APP_UNAVAILABLE" {
		t.Fatal(image.raw)
	}
	plain := f.asyncExec("linux_write_stdin", map[string]any{})
	if err := ws.WriteJSON(protocol.ResultFrame(readTestFrame(t, ws).CallID, 1)); err != nil {
		t.Fatal(err)
	}
	if got := awaitReply(t, plain); got.status != 200 {
		t.Fatal(got.raw)
	}
}

func TestRejectAmbiguousArgumentsBeforeDispatch(t *testing.T) {
	for _, raw := range []string{`{"arguments":{"x":1,"\u0078":2}}`, `{"arguments":{"x":"\uD800"}}`} {
		var payload map[string]any
		req := httptest.NewRequest("POST", "/", bytes.NewBufferString(raw))
		if err := readJSON(httptest.NewRecorder(), req, &payload); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestMCPExpandedResultOverBudgetLeavesChannelUsable(t *testing.T) {
	f := testFixture(t, nil)
	ws := f.connect("linux-151")
	session := f.initialize()
	done := make(chan reply, 1)
	go func() {
		done <- f.mcpRequest(session, rpc(4, "tools/call", map[string]any{"name": "linux_exec_command"}))
	}()
	request := readTestFrame(t, ws)
	// Legacy JSON appears as both serialized text and structured content.
	// Its channel frame fits; the final consumer envelope does not.
	if err := ws.WriteJSON(protocol.ResultFrame(request.CallID, map[string]string{"output": strings.Repeat("x", 600000)})); err != nil {
		t.Fatal(err)
	}
	r := awaitReply(t, done)
	body := r.data["result"].(map[string]any)
	if body["isError"] != true || body["structuredContent"].(map[string]any)["error"].(map[string]any)["code"] != "RESULT_TOO_LARGE" || len(r.raw) > 4096 {
		t.Fatal("expected bounded tool error")
	}
	follow := f.asyncExec("linux_exec_command", map[string]any{})
	if err := ws.WriteJSON(protocol.ResultFrame(readTestFrame(t, ws).CallID, true)); err != nil {
		t.Fatal(err)
	}
	if got := awaitReply(t, follow); got.status != 200 {
		t.Fatal(got.raw)
	}
}
