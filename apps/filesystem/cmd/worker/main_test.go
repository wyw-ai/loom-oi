package main

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestWorkerUsesProtocolNegotiationAndRecoversFromInvalidCommands(t *testing.T) {
	in, stdin := io.Pipe()
	stdout, out := io.Pipe()
	done := make(chan error, 1)
	go func() { err := serve(in, out, io.Discard); out.CloseWithError(err); done <- err }()
	defer in.Close()
	defer stdout.Close()
	writer := json.NewEncoder(stdin)
	reader := bufio.NewScanner(stdout)
	send := func(v any) {
		t.Helper()
		if err := writer.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	read := func() map[string]any {
		t.Helper()
		if !reader.Scan() {
			t.Fatalf("worker ended: %v", reader.Err())
		}
		var v map[string]any
		if err := json.Unmarshal(reader.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	send(map[string]any{"type": "start", "protocol": 1, "result_formats": []string{"json", "content.v1"}, "config": map[string]any{"root": t.TempDir(), "read_only": true}})
	ready := read()
	if ready["type"] != "ready" || len(ready["handlers"].([]any)) != 7 {
		t.Fatal(ready)
	}
	if _, err := io.WriteString(stdin, "{\"type\":\"invoke\",\"input\":{\"p\":0,\"\\u0070\":1}}\n"); err != nil {
		t.Fatal(err)
	}
	if read()["type"] != "error" {
		t.Fatal("duplicate names must be rejected")
	}
	send(map[string]any{"type": "invoke", "call_id": "abc", "handler": "list_directory", "input": map[string]any{}, "context": map[string]string{"access_identity": "one", "vmcp_id": "scope"}})
	result := read()
	if result["type"] != "result" || result["call_id"] != "abc" || result["result_format"] != nil {
		t.Fatal(result)
	}
	stdin.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker failed to exit")
	}
}
