package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

func TestRecoveryAPI(t *testing.T) {
	w := newWorld(t)
	maint := func(on bool) protocol.MaintenanceSpec { return protocol.MaintenanceSpec{Maintenance: &on} }
	if r := w.do("PUT", "/v1/admin/maintenance", w.admin, maint(true)); r.status != 428 {
		t.Fatalf("without If-Match: %d", r.status)
	}
	w.write("PUT", "/v1/admin/maintenance", map[string]any{}, 400)
	w.write("PUT", "/v1/admin/maintenance", maint(true), 200)
	r := w.do("POST", "/v1/channels", w.consumer, protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: w.rev})
	if r.status != 503 || r.code(t) != protocol.CodeMaintenance {
		t.Fatalf("open in maintenance: %d %s", r.status, r.body)
	}
	if r := w.do("GET", "/v1/exposures", w.consumer, nil); r.status != 200 {
		t.Fatalf("discovery in maintenance: %d", r.status)
	}
	var sv protocol.StoreView
	w.do("GET", "/v1/admin/store", w.admin, nil).decode(t, &sv)
	if sv.Backend != store.SQLite || !sv.Healthy || !sv.Maintenance || sv.Usage.PageSize <= 0 {
		t.Fatalf("%+v", sv)
	}
	if r := w.do("GET", "/v1/admin/store", w.consumer, nil); r.status != 403 {
		t.Fatalf("consumer reads store: %d", r.status)
	}
	var m protocol.StoreMaintained
	r = w.do("POST", "/v1/admin/store/maintain", w.admin, protocol.StoreMaintain{Checkpoint: "truncate", Optimize: true})
	r.decode(t, &m)
	if r.status != 200 || m.Checkpoint == nil || m.After.WALBytes != 0 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	for _, body := range []any{protocol.StoreMaintain{Checkpoint: "bogus"}, []byte(`{"unknown":1}`)} {
		if r := w.do("POST", "/v1/admin/store/maintain", w.admin, body); r.status != 400 {
			t.Fatalf("bad maintain: %d", r.status)
		}
	}
	// Prune exactly what was exported.
	var list protocol.AuditList
	w.do("GET", "/v1/admin/audit", w.admin, nil).decode(t, &list)
	sum, err := protocol.AuditDigest(list.Items[:2])
	if err != nil {
		t.Fatal(err)
	}
	if r := w.do("POST", "/v1/admin/audit/prune", w.admin, protocol.AuditPrune{ThroughSeq: list.Items[1].Seq, SHA256: strings.Repeat("0", 64)}); r.status != 400 {
		t.Fatalf("mismatched digest: %d %s", r.status, r.body)
	}
	if r := w.do("POST", "/v1/admin/audit/prune", w.admin, []byte(`{`)); r.status != 400 {
		t.Fatalf("bad body: %d", r.status)
	}
	var pr protocol.AuditPruned
	r = w.do("POST", "/v1/admin/audit/prune", w.admin, protocol.AuditPrune{ThroughSeq: list.Items[1].Seq, SHA256: sum})
	r.decode(t, &pr)
	if r.status != 200 || pr.Deleted != 2 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	w.write("PUT", "/v1/admin/maintenance", maint(false), 200)
	w.open()
}

func TestRecoveryCommands(t *testing.T) {
	dir := t.TempDir()
	ca := must(pki.NewCA("test-ca"))
	path := writeConfig(t, dir, ca, nil)
	if code, _, errOut := runMain("init", "--config", path); code != 0 {
		t.Fatal(errOut)
	}
	run := func(want int, args ...string) string {
		t.Helper()
		code, out, errOut := runMain(args...)
		if code != want {
			t.Fatalf("%v: %d, want %d: %s", args, code, want, errOut)
		}
		return out + errOut
	}
	backup := filepath.Join(dir, "b.db")
	var info store.BackupInfo
	if err := json.Unmarshal([]byte(run(0, "backup", "--config", path, "--out", backup)), &info); err != nil || info.Path != backup || info.SHA256 == "" {
		t.Fatalf("%+v %v", info, err)
	}
	run(2, "backup", "--config", path)
	run(1, "backup", "--config", path, "--out", backup)
	if out := run(2, "restore", "--config", path, "--from", backup); !strings.Contains(out, "--yes") {
		t.Fatal(out)
	}
	run(2, "restore", "--config", path, "--bogus")
	run(1, "restore", "--config", path, "--yes")
	out := run(0, "restore", "--config", path, "--from", backup, "--yes")
	if !strings.Contains(out, `"maintenance": true`) || !strings.Contains(out, "ois admin resume") {
		t.Fatal(out)
	}
	export := filepath.Join(dir, "export.jsonl")
	run(2, "export", "--config", path)
	run(0, "export", "--config", path, "--out", export)
	run(1, "export", "--config", path, "--out", export)
	// A failed export leaves no file behind.
	bad := writeConfig(t, t.TempDir(), ca, map[string]any{"store": map[string]string{"driver": "sqlite", "sqlite_path": "absent.db"}})
	failed := filepath.Join(dir, "failed.jsonl")
	run(1, "export", "--config", bad, "--out", failed)
	if _, err := os.Stat(failed); !os.IsNotExist(err) {
		t.Fatal("partial export kept")
	}
	run(1, "export", "--config", path, "--out", filepath.Join(dir, "no", "dir.jsonl"))
	target := writeConfig(t, t.TempDir(), ca, nil)
	run(2, "import", "--config", target)
	run(2, "import", "--config", target, "--in", export)
	run(1, "import", "--config", target, "--in", filepath.Join(dir, "absent.jsonl"), "--yes")
	if out := run(0, "import", "--config", target, "--in", export, "--yes"); !strings.Contains(out, `"source_backend": "sqlite"`) {
		t.Fatal(out)
	}
	run(1, "import", "--config", target, "--in", export, "--yes")
	run(0, "maintain", "--config", path, "--checkpoint", "truncate", "--vacuum-pages", "5", "--optimize")
	run(1, "maintain", "--config", path, "--checkpoint", "bogus")
	run(2, "maintain", "--config", path, "--vacuum-pages", "x")
	run(1, "maintain", "--config", bad)

	// With OIS serving, the stopped-database commands are refused; backup
	// still works. The restored database starts in maintenance.
	cfg := must(LoadConfig(path))
	srv := must(Start(context.Background(), cfg, slog.New(slog.DiscardHandler)))
	defer func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		srv.Shutdown(ctx)
	}()
	if !srv.hub.ConfigView().Maintenance {
		t.Fatal("restored database is not in maintenance")
	}
	run(1, "export", "--config", path, "--out", filepath.Join(dir, "busy.jsonl"))
	run(1, "maintain", "--config", path)
	run(1, "restore", "--config", path, "--from", backup, "--yes")
	run(0, "backup", "--config", path, "--out", filepath.Join(dir, "online.db"))
	if out := run(0, "help"); !strings.Contains(out, "admin resume") || !strings.Contains(out, "restore") {
		t.Fatal(out)
	}
}
