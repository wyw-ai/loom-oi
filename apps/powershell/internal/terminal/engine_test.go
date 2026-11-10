//go:build windows

package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wyw-ai/loom-oi/apps/powershell/internal/protocol"
)

func testEngine(t *testing.T, adjust func(*Config)) *Engine {
	t.Helper()
	c := DefaultConfig()
	c.DefaultCwd = t.TempDir()
	if _, e := exec.LookPath("pwsh"); e != nil {
		t.Skip(e)
	}
	if adjust != nil {
		adjust(&c)
	}
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}
func invocation(handler string, input map[string]any) protocol.Frame {
	return protocol.Frame{Type: "invoke", CallID: protocol.ID("test_"), Handler: handler, Input: input, Context: protocol.Context{AccessIdentity: "test-identity", VMCPID: "test-vmcp"}}
}
func result(t *testing.T, frame protocol.Frame) Result {
	t.Helper()
	if frame.Error != nil {
		t.Fatalf("%s: %s (%v)", frame.Error.Code, frame.Error.Message, frame.Error.Details)
	}
	var r Result
	if e := json.Unmarshal(frame.Result, &r); e != nil {
		t.Fatal(e)
	}
	if (r.SessionID == "") == (r.ExitCode == nil) {
		t.Fatalf("expected exactly session_id or exit_code: %s", frame.Result)
	}
	return r
}
func finish(t *testing.T, e *Engine, r Result) (Result, string) {
	t.Helper()
	all := r.Output
	deadline := time.Now().Add(12 * time.Second)
	for r.SessionID != "" && time.Now().Before(deadline) {
		r = result(t, e.Invoke(invocation("write_stdin", map[string]any{"session_id": r.SessionID, "yield_time_ms": 1000})))
		all += r.Output
	}
	if r.SessionID != "" {
		t.Fatalf("execution did not finish; output=%q", all)
	}
	return r, all
}
func wantError(t *testing.T, f protocol.Frame, code string) {
	t.Helper()
	if f.Error == nil || f.Error.Code != code {
		t.Fatalf("want %s, got %+v", code, f)
	}
}

func TestPipeCompletionExitAndFreshEnvironment(t *testing.T) {
	e := testEngine(t, nil)
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "Write-Output 'hello 世界'; [Console]::Error.WriteLine('stderr'); exit 7", "yield_time_ms": 3000})))
	r, out := finish(t, e, r)
	if *r.ExitCode != 7 || !strings.Contains(out, "hello 世界") || !strings.Contains(out, "stderr") {
		t.Fatalf("result=%+v output=%q", r, out)
	}
	r = result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "Write-Output $env:USERPROFILE", "yield_time_ms": 3000})))
	r, _ = finish(t, e, r)
	if *r.ExitCode != 0 {
		t.Fatalf("exit=%d", *r.ExitCode)
	}
}

func TestLongExecutionPipeEOFAndIdentity(t *testing.T) {
	e := testEngine(t, nil)
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "Start-Sleep -Milliseconds 1000; Write-Output 'tail'", "yield_time_ms": 0})))
	id := r.SessionID
	if id == "" {
		t.Fatal("long execution lacked session")
	}
	wrong := invocation("write_stdin", map[string]any{"session_id": id, "chars": ""})
	wrong.Context.AccessIdentity = "someone-else"
	wantError(t, e.Invoke(wrong), "SESSION_UNAVAILABLE")
	wantError(t, e.Invoke(invocation("write_stdin", map[string]any{"session_id": id, "chars": "input"})), "STDIN_NOT_WRITABLE")
	r, out := finish(t, e, r)
	if *r.ExitCode != 0 || !strings.Contains(out, "tail") {
		t.Fatalf("%+v %q", r, out)
	}
	wantError(t, e.Invoke(invocation("write_stdin", map[string]any{"session_id": id})), "SESSION_UNAVAILABLE")
	eof := "$x=[Console]::In.ReadToEnd(); Write-Output ('eof:'+ $x.Length)"
	r = result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": eof, "yield_time_ms": 3000})))
	_, out = finish(t, e, r)
	if !strings.Contains(out, "eof") {
		t.Fatal(out)
	}
}

func TestRealTerminalInput(t *testing.T) {
	e := testEngine(t, nil)
	command := "$name = Read-Host 'name'; Write-Output ('hello ' + $name)"
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": command, "tty": true, "yield_time_ms": 1000})))
	if r.SessionID == "" {
		t.Fatalf("terminal exited before input: %+v", r)
	}
	before := r.Output
	chars := "loom\r"
	r = result(t, e.Invoke(invocation("write_stdin", map[string]any{"session_id": r.SessionID, "chars": chars, "yield_time_ms": 1000})))
	r, out := finish(t, e, r)
	out = before + out
	if *r.ExitCode != 0 || !strings.Contains(out, "hello loom") {
		t.Fatalf("terminal %+v output %q", r, out)
	}
}

func TestOutputBudgetBufferAndNoReplay(t *testing.T) {
	e := testEngine(t, func(c *Config) { c.Limits.SessionBufferBytes = 128 })
	command := "[Console]::Write(('a' * 5000)); Start-Sleep -Seconds 1; Write-Output 'END'"
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": command, "yield_time_ms": 700, "max_output_tokens": 8})))
	if !r.OutputTruncated || r.OmittedBytes < 4800 {
		t.Fatalf("expected bounded loss: %+v", r)
	}
	r.Output = ""
	r, out := finish(t, e, r)
	if !strings.Contains(out, "END") || strings.Contains(out, "00000") || strings.Contains(out, "aaaaa") {
		t.Fatalf("history replay or missing tail: %q", out)
	}
}

func TestDedupConflictAndConcurrentWriteCancel(t *testing.T) {
	e := testEngine(t, nil)
	f := invocation("exec_command", map[string]any{"cmd": "Start-Sleep -Seconds 3; Write-Output 'done'", "yield_time_ms": 0})
	first := e.Invoke(f)
	r := result(t, first)
	again := e.Invoke(f)
	if !bytes.Equal(first.Result, again.Result) {
		t.Fatal("duplicate did not return same response")
	}
	e.Cancel(f.CallID) // A committed result makes a late cancel ineffectual.
	f.Input = map[string]any{"cmd": "different"}
	wantError(t, e.Invoke(f), "CALL_ID_CONFLICT")
	read := invocation("write_stdin", map[string]any{"session_id": r.SessionID, "yield_time_ms": 30000})
	done := make(chan protocol.Frame, 1)
	go func() { done <- e.Invoke(read) }()
	deadline := time.Now().Add(time.Second)
	for {
		e.mu.Lock()
		busy := e.sessions[r.SessionID].busy
		e.mu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("read not started")
		}
		time.Sleep(time.Millisecond)
	}
	wantError(t, e.Invoke(invocation("write_stdin", map[string]any{"session_id": r.SessionID, "chars": ""})), "SESSION_BUSY")
	e.Cancel(read.CallID)
	wantError(t, <-done, "CALL_CANCELLED")
	final, out := finish(t, e, r)
	if *final.ExitCode != 0 || !strings.Contains(out, "done") {
		t.Fatalf("cancelled read killed execution: %+v %q", final, out)
	}
}

func TestLimitsValidationAndRuntimeReclaim(t *testing.T) {
	e := testEngine(t, func(c *Config) { c.Limits.MaxConcurrentSessions = 1; c.Limits.MaxRuntimeMS = 1000 })
	wantError(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "x", "yield_time_ms": map[string]any{}})), "INVALID_ARGUMENT")
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "Start-Sleep -Seconds 30", "yield_time_ms": "0"})))
	wantError(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "x", "yield_time_ms": 0})), "CAPACITY_EXCEEDED")
	r, _ = finish(t, e, r)
	if r.TerminationReason != "runtime_limit" {
		t.Fatalf("expected runtime limit: %+v", r)
	}
}

func TestIdleLimitDoesNotCountOutput(t *testing.T) {
	e := testEngine(t, func(c *Config) { c.Limits.IdleTimeoutMS = 300 })
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "while ($true) { [Console]::Write('x'); Start-Sleep -Milliseconds 50 }", "yield_time_ms": 0})))
	time.Sleep(700 * time.Millisecond)
	r, _ = finish(t, e, r)
	if r.TerminationReason != "idle_limit" {
		t.Fatalf("output extended idle time: %+v", r)
	}
}

func TestUTF8TruncationAcrossReadBoundaries(t *testing.T) {
	b := outputBuffer{limit: 12}
	p := []byte("甲乙丙丁戊")
	for _, v := range p {
		b.append([]byte{v}, false)
	}
	out, truncated, omitted := b.take(6)
	if !utf8.ValidString(out) || !truncated || omitted != 9 {
		t.Fatalf("%q %v %d", out, truncated, omitted)
	}
}

func TestInvalidEncodingDoesNotInflateOmittedRawBytes(t *testing.T) {
	b := outputBuffer{limit: 32}
	b.append([]byte{'a', 'b', 0xff, 'c', 'd', 'e', 'f', 'g', 'h'}, true)
	out, truncated, omitted := b.take(4)
	if !utf8.ValidString(out) || !truncated || omitted != 7 {
		t.Fatalf("raw omission count changed during decoding: %q, %v, %d", out, truncated, omitted)
	}
	b = outputBuffer{limit: 4}
	b.append([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80}, true)
	_, truncated, omitted = b.take(32)
	if !truncated || omitted != 2 {
		t.Fatalf("invalid bytes were all discarded instead of retaining the bounded tail: %d", omitted)
	}
}

func TestStdioCancelOrderingAndEOF(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(inR, outW); outW.Close() }()
	defer inW.Close()
	defer outR.Close()
	config := map[string]any{"default_cwd": t.TempDir()}
	config["shell_kind"] = "powershell"
	config["shell_path"] = "pwsh"
	enc := json.NewEncoder(inW)
	dec := json.NewDecoder(outR)
	if err := enc.Encode(protocol.Frame{Type: "start", Protocol: 1, Config: config}); err != nil {
		t.Fatal(err)
	}
	var ready protocol.Frame
	if err := dec.Decode(&ready); err != nil {
		t.Fatal(err)
	}
	if ready.Type != "ready" {
		t.Fatal(ready)
	}
	f := invocation("exec_command", map[string]any{"cmd": "Start-Sleep -Seconds 30", "yield_time_ms": 30000})
	if err := enc.Encode(f); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(protocol.Frame{Type: "cancel", CallID: f.CallID}); err != nil {
		t.Fatal(err)
	}
	var response protocol.Frame
	if err := dec.Decode(&response); err != nil {
		t.Fatal(err)
	}
	wantError(t, response, "CALL_CANCELLED")
	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EOF shutdown did not finish")
	}
}

func TestTerminalWriteTimeoutJoinsWriter(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	n, err := writeTerminal(ctx, w, bytes.Repeat([]byte("x"), 1<<20))
	if err == nil || n == 1<<20 {
		t.Fatal("unread pipe write should time out")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("write timeout did not join promptly")
	}
}

func TestCancelNewExecutionBeforeResultCommit(t *testing.T) {
	e := testEngine(t, nil)
	f := invocation("exec_command", map[string]any{"cmd": "Start-Sleep -Seconds 30", "yield_time_ms": 30000})
	done := make(chan protocol.Frame, 1)
	go func() { done <- e.Invoke(f) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.mu.Lock()
		created := len(e.sessions) > 0
		e.mu.Unlock()
		if created {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("execution did not start")
		}
		time.Sleep(time.Millisecond)
	}
	e.Cancel(f.CallID)
	wantError(t, <-done, "CALL_CANCELLED")
	for {
		e.mu.Lock()
		active := e.active
		e.mu.Unlock()
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled new execution not reclaimed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEOFKillsRunningExecution(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(inR, outW); outW.Close() }()
	defer inW.Close()
	defer outR.Close()
	config := map[string]any{"default_cwd": t.TempDir()}
	config["shell_kind"] = "powershell"
	config["shell_path"] = "pwsh"
	enc := json.NewEncoder(inW)
	dec := json.NewDecoder(outR)
	if err := enc.Encode(protocol.Frame{Type: "start", Protocol: 1, Config: config}); err != nil {
		t.Fatal(err)
	}
	var frame protocol.Frame
	if err := dec.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	f := invocation("exec_command", map[string]any{"cmd": "Start-Sleep -Seconds 30", "yield_time_ms": 0})
	if err := enc.Encode(f); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&frame); err != nil {
		t.Fatal(err)
	}
	if result(t, frame).SessionID == "" {
		t.Fatal("expected running execution")
	}
	inW.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EOF did not reclaim the running execution")
	}
}
