package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func diagnosticFixture(t *testing.T, log *idleLog, mods ...func(*limits.Limits)) *fixture {
	t.Helper()
	e := newEnv(t, mods...)
	e.h.log = slog.New(slog.NewJSONHandler(log, nil))
	e.addAccess("lab-a", pin(1))
	e.addPolicy("p1", "alice", "lab-a/*", protocol.ActionDiscover, protocol.ActionOpen)
	alice, _ := e.token("alice", protocol.ScopeConsumer, 0)
	o := e.connect("lab-a", pin(1), expo(exp1, rv(1)))
	return &fixture{env: e, o: o, alice: alice, rev: rv(1)}
}

func legRecords(t *testing.T, log *idleLog, channel string, role Role) []map[string]any {
	t.Helper()
	log.mu.Lock()
	defer log.mu.Unlock()
	var out []map[string]any
	for _, line := range bytes.Split(log.b.Bytes(), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if bytes.Contains(line, []byte("private transport token")) {
			t.Fatal("raw error reached log")
		}
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if row["msg"] == "leg ended" && row["channel"] == channel && row["role"] == role.String() {
			out = append(out, row)
		}
	}
	return out
}

func TestLegDiagnosticsFollowActualErrors(t *testing.T) {
	for _, operation := range []string{"read", "write", "ping"} {
		t.Run(operation, func(t *testing.T) {
			log := &idleLog{}
			f := diagnosticFixture(t, log, func(l *limits.Limits) {
				if operation == "ping" {
					l.PingMS = 5
				}
			})
			fl := f.openFlow()
			f.h.mu.Lock()
			leg := f.h.channels[f.id(fl)].legs[Provider]
			f.h.mu.Unlock()
			secret := errors.New("private transport token must stay private")
			source := "reader_cleanup"
			switch operation {
			case "read":
				fl.prov.conn.in <- wsMsg{err: secret}
			case "write":
				source = "write_error"
				fl.prov.conn.writeErr.Store(&secret)
				fl.cons.data("payload")
			case "ping":
				source = "ping_misses"
				fl.prov.conn.pingErr.Store(&secret)
				waitFor(t, "first Ping failure", func() bool { return leg.diagnostics.ping.Load() != nil })
				later := error(context.DeadlineExceeded)
				fl.prov.conn.pingErr.Store(&later)
			}
			fl.cons.expectAbort(protocol.CodeChannelInterrupted)
			fl.prov.waitDone()
			waitFor(t, "all original leg workers ended", func() bool { return len(legRecords(t, log, f.id(fl), Provider)) == 1 })
			got := leg.diagnosticSnapshot()
			var failed *legOperationDiagnostic
			switch operation {
			case "read":
				failed = got.Read
			case "write":
				failed = got.Write
			case "ping":
				failed = got.Ping
			}
			if failed == nil || failed.ErrorClass != "other" || failed.ContextState != "active" || failed.StartedNS <= 0 || failed.CompletedNS < failed.StartedNS || failed.CompletedNS > got.SampledNS || got.FirstClose == nil || got.FirstClose.Source != source || got.FirstClose.AtNS > got.SampledNS || got.ChannelID != f.id(fl) || got.Role != Provider.String() {
				t.Fatalf("operation %s snapshot=%+v failure=%+v", operation, got, failed)
			}
			if operation == "write" && failed.MessageType != "DATA" {
				t.Fatal(failed.MessageType)
			}
			if operation == "ping" && failed.PingPhase != "unknown" {
				t.Fatal(failed.PingPhase)
			}
			wantReadPhase := ""
			if operation == "read" {
				wantReadPhase = "unknown"
			}
			if failed.ReadPhase != wantReadPhase {
				t.Fatalf("read phase on %s: %q", operation, failed.ReadPhase)
			}
			first := *got.FirstClose
			leg.closeNow("other_owned")
			if *leg.diagnosticSnapshot().FirstClose != first {
				t.Fatal("later cleanup replaced first CloseNow")
			}
			encoded, err := json.Marshal(got)
			if err != nil || strings.Contains(string(encoded), "private transport token") {
				t.Fatal(string(encoded), err)
			}
			if leg.diagnostics.workers.Load() != 0 || len(legRecords(t, log, f.id(fl), Provider)) != 1 {
				t.Fatal("unjoined or duplicate diagnostic")
			}
		})
	}
}

func TestLegReadPhase(t *testing.T) {
	wrap := func(prefix string, err error) error { return fmt.Errorf("%s%w", prefix, err) }
	for _, tc := range []struct {
		name, phase string
		err         error
	}{
		{"pong_lock", "pong_write_lock", wrap("failed to get reader: ", wrap("failed to handle control frame opPing: ", wrap("failed to write control frame opPong: ", wrap("failed to acquire lock: ", context.DeadlineExceeded))))},
		{"control_payload", "control_payload", wrap("failed to read: ", wrap("failed to handle control frame opPing: ", context.DeadlineExceeded))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &idleLog{}
			f := diagnosticFixture(t, log)
			fl := f.openFlow()
			f.h.mu.Lock()
			leg := f.h.channels[f.id(fl)].legs[Provider]
			f.h.mu.Unlock()
			fl.prov.conn.in <- wsMsg{err: tc.err}
			fl.cons.expectAbort(protocol.CodeChannelInterrupted)
			fl.prov.waitDone()
			waitFor(t, "final read diagnostic", func() bool { return len(legRecords(t, log, f.id(fl), Provider)) == 1 })
			got := leg.diagnosticSnapshot()
			if got.Read == nil || got.Read.ReadPhase != tc.phase || got.Read.ErrorClass != "deadline" || got.Read.ContextState != "active" || got.FirstClose == nil || got.FirstClose.Source != "reader_cleanup" || leg.diagnostics.workers.Load() != 0 {
				t.Fatalf("wrong final read diagnostic: %+v", got)
			}
			first := *got.Read
			publishLegOperation(&leg.diagnostics.read, legOperationDiagnostic{ReadPhase: "unknown"})
			if *leg.diagnosticSnapshot().Read != first {
				t.Fatal("later failure replaced the first read observation")
			}
			row := legRecords(t, log, f.id(fl), Provider)[0]["io_failures"].(map[string]any)
			if row["read"].(map[string]any)["read_phase"] != tc.phase {
				t.Fatal("final worker log lost read phase")
			}
		})
	}
}

func TestLegDiagnosticFinalSnapshotIncludesReaderCleanup(t *testing.T) {
	log := &idleLog{}
	f := diagnosticFixture(t, log)
	fl := f.openFlow()
	f.h.mu.Lock()
	leg := f.h.channels[f.id(fl)].legs[Provider]
	f.h.mu.Unlock()
	fl.prov.conn.in <- wsMsg{err: errors.New("transport loss")}
	fl.cons.expectAbort(protocol.CodeChannelInterrupted)
	fl.prov.waitDone()
	waitFor(t, "final leg record", func() bool { return len(legRecords(t, log, f.id(fl), Provider)) == 1 })
	rows := legRecords(t, log, f.id(fl), Provider)
	d := rows[0]["io_failures"].(map[string]any)
	if d["first_close"].(map[string]any)["source"] != "reader_cleanup" || d["read"] == nil || leg.diagnostics.workers.Load() != 0 {
		t.Fatalf("final record missed post-finalize reader cleanup: %+v", d)
	}
	// This final snapshot is independent of the Hub lock, including at shutdown.
	f.h.mu.Lock()
	_ = leg.diagnosticSnapshot()
	f.h.mu.Unlock()
}
