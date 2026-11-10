//go:build linux

package terminal

import (
	"strings"
	"testing"
)

func TestSignalTerminationPreservesReason(t *testing.T) {
	e := testEngine(t, nil)
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "kill -TERM $$", "yield_time_ms": 3000})))
	r, _ = finish(t, e, r)
	if *r.ExitCode != 143 || r.Signal != 15 || r.TerminationReason != "signal" {
		t.Fatalf("signal was lost through the supervisor: %+v", r)
	}
}

func TestTerminalInterruptDoesNotOverrideTargetSignalDisposition(t *testing.T) {
	e := testEngine(t, nil)
	r := result(t, e.Invoke(invocation("exec_command", map[string]any{"cmd": "trap '' INT; printf ready; sleep 1; printf survived", "tty": true, "yield_time_ms": 100})))
	if r.SessionID == "" {
		t.Fatalf("command completed before interrupt: %+v", r)
	}
	r = result(t, e.Invoke(invocation("write_stdin", map[string]any{"session_id": r.SessionID, "chars": "\x03", "yield_time_ms": 2000})))
	r, out := finish(t, e, r)
	if *r.ExitCode != 0 || !strings.Contains(out, "survived") {
		t.Fatalf("supervisor incorrectly treated terminal Ctrl+C as a kill: %+v %q", r, out)
	}
}
