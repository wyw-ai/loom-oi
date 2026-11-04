package client

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestRecoveryAdmin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	admin := w.tp.AdminToken
	run := func(want int, args ...string) string {
		t.Helper()
		code, out, errOut := w.oi(admin, "", args...)
		if code != want {
			t.Fatalf("%v: %d %s %s", args, code, out, errOut)
		}
		return out + errOut
	}
	if out := run(0, "admin", "maintenance", "on"); !strings.Contains(out, `"maintenance": true`) {
		t.Fatal(out)
	}
	if _, err := w.c.OpenLatest(ctx, "lab-a/http", "", false); protocol.CodeOf(err) != protocol.CodeMaintenance {
		t.Fatalf("open in maintenance: %v", err)
	}
	run(2, "admin", "maintenance")
	run(2, "admin", "maintenance", "maybe")
	oisAdmin := func(args ...string) (int, string) {
		var out, errb bytes.Buffer
		code := OISAdmin(ctx, args, &out, &errb)
		return code, out.String() + errb.String()
	}
	for _, args := range [][]string{nil, {"pause"}, {"resume", "extra"}, {"resume", "--bogus"}, {"resume", "--ois", "http://x", "--token", "t"}} {
		if code, out := oisAdmin(args...); code != 2 {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	if code, _ := oisAdmin("resume", "--ois", w.tp.URL(), "--ca", w.tp.CAFile, "--token", w.token); code != 1 {
		t.Fatal("a consumer token resumed")
	}
	if code, out := oisAdmin("resume", "--ois", w.tp.URL(), "--ca", w.tp.CAFile, "--token", admin); code != 0 || !strings.Contains(out, `"maintenance": false`) {
		t.Fatalf("%d %s", code, out)
	}
	s, err := w.c.Dial(ctx, "lab-a/http")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	if out := run(0, "admin", "store"); !strings.Contains(out, `"backend":"sqlite"`) {
		t.Fatal(out)
	}
	if out := run(0, "admin", "store", "maintain", "--checkpoint", "passive", "--optimize"); !strings.Contains(out, `"checkpoint"`) {
		t.Fatal(out)
	}
	run(2, "admin", "store", "nope")
	run(2, "admin", "store", "maintain", "x")
	run(1, "admin", "store", "maintain", "--checkpoint", "bogus")

	dir := t.TempDir()
	file := filepath.Join(dir, "audit.json")
	if out := run(0, "admin", "audit", "export", "--through-seq", "2", "--out", file); !strings.Contains(out, `"count": 2`) {
		t.Fatal(out)
	}
	run(1, "admin", "audit", "export", "--out", file)
	run(2, "admin", "audit", "export")
	run(2, "admin", "audit", "export", "x", "--out", filepath.Join(dir, "x"))
	run(2, "admin", "audit", "export", "--through-seq", "-1", "--out", filepath.Join(dir, "y"))
	run(2, "admin", "audit", "prune")
	run(2, "admin", "audit", "prune", "--export", file, "--through-seq", "1")
	run(1, "admin", "audit", "prune", "--export", filepath.Join(dir, "absent.json"))
	junk := filepath.Join(dir, "junk.json")
	if err := os.WriteFile(junk, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(1, "admin", "audit", "prune", "--export", junk)
	var exp protocol.AuditExport
	data, _ := os.ReadFile(file)
	if err := json.Unmarshal(data, &exp); err != nil || exp.ThroughSeq != 2 || len(exp.Items) != 2 {
		t.Fatalf("%+v %v", exp, err)
	}
	exp.Items[0].Actor = "someone else"
	tampered := filepath.Join(dir, "tampered.json")
	b, _ := json.Marshal(exp)
	if err := os.WriteFile(tampered, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if out := run(1, "admin", "audit", "prune", "--export", tampered); !strings.Contains(out, "does not match") {
		t.Fatal(out)
	}
	if out := run(0, "admin", "audit", "prune", "--export", file); !strings.Contains(out, `"deleted":2`) {
		t.Fatal(out)
	}
	// The records are gone, so the same export no longer matches.
	run(1, "admin", "audit", "prune", "--export", file)
	ac, err := New(Options{URL: w.tp.URL(), Token: admin, CAFile: w.tp.CAFile})
	if err != nil {
		t.Fatal(err)
	}
	defer ac.CloseIdleConnections()
	all, err := ac.ExportAudit(ctx, 0)
	if err != nil || all.Count == 0 || all.ThroughSeq != all.Items[len(all.Items)-1].Seq || all.Items[0].Seq <= 2 {
		t.Fatalf("%+v %v", all, err)
	}
	if _, err := w.c.SetMaintenance(ctx, true); protocol.CodeOf(err) != protocol.CodeDenied {
		t.Fatalf("consumer maintenance: %v", err)
	}
	if _, err := w.c.ExportAudit(ctx, 0); protocol.CodeOf(err) != protocol.CodeDenied {
		t.Fatalf("consumer export: %v", err)
	}
}
