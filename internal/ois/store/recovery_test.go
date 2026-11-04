package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func views(entries []AuditEntry) []protocol.AuditView {
	out := make([]protocol.AuditView, len(entries))
	for i, a := range entries {
		out[i] = protocol.AuditView{Seq: a.Seq, At: a.At, Actor: a.Actor, Action: a.Action, ObjectType: a.ObjectType,
			ObjectID: a.ObjectID, ConfigVersion: a.ConfigVersion, Detail: a.Detail}
	}
	return out
}

func listAll(t *testing.T, s Store) []AuditEntry {
	t.Helper()
	out, err := s.ListAudit(context.Background(), 0, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMaintenanceMutationAndProbeWrite(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		v, err := s.ApplyConfig(ctx, 0, []Mutation{SetMaintenance{On: true}}, []AuditEntry{audit("maintenance_entered")})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Driver == SQLite {
			if _, err := s.wdb.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil || fileSize(cfg.SQLitePath+"-wal") != 0 {
				t.Fatalf("WAL not empty before the probe: %v", err)
			}
		}
		if err := s.ProbeWrite(ctx); err != nil {
			t.Fatal(err)
		}
		// The probe must really write: on a full disk a write-free probe
		// would report recovery (TV-B3-11e).
		if cfg.Driver == SQLite && fileSize(cfg.SQLitePath+"-wal") == 0 {
			t.Fatal("ProbeWrite wrote nothing to the WAL")
		}
		snap, err := s.LoadSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !snap.Meta.Maintenance || snap.Meta.ConfigVersion != v {
			t.Fatalf("meta %+v", snap.Meta)
		}
		if _, err := s.ApplyConfig(ctx, v, []Mutation{SetMaintenance{}}, nil); err != nil {
			t.Fatal(err)
		}
		if snap, _ = s.LoadSnapshot(ctx); snap.Meta.Maintenance {
			t.Fatal("still in maintenance")
		}
	})
}

func TestUsageAndMaintain(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		u, err := s.Usage(ctx)
		if err != nil || u.DBBytes <= 0 {
			t.Fatalf("usage %+v %v", u, err)
		}
		for _, bad := range []protocol.StoreMaintain{{Checkpoint: "full"}, {IncrementalVacuumPages: -1}, {IncrementalVacuumPages: protocol.MaxVacuumPages + 1}} {
			if _, err := s.Maintain(ctx, bad); !errors.Is(err, ErrInvalid) {
				t.Fatalf("%+v: %v", bad, err)
			}
		}
		if cfg.Driver == Postgres {
			if u.PageCount != 0 || u.WALBytes != 0 {
				t.Fatalf("postgres usage %+v", u)
			}
			for _, bad := range []protocol.StoreMaintain{{Checkpoint: "passive"}, {IncrementalVacuumPages: 1}} {
				if _, err := s.Maintain(ctx, bad); !errors.Is(err, ErrInvalid) {
					t.Fatalf("%+v: %v", bad, err)
				}
			}
			out, err := s.Maintain(ctx, protocol.StoreMaintain{Optimize: true})
			if err != nil || out.Checkpoint != nil || out.After.DBBytes <= 0 {
				t.Fatalf("%+v %v", out, err)
			}
			return
		}
		if u.PageSize <= 0 || u.PageCount <= 0 {
			t.Fatalf("sqlite usage %+v", u)
		}
		out, err := s.Maintain(ctx, protocol.StoreMaintain{Checkpoint: "passive"})
		if err != nil || out.Checkpoint == nil || out.Checkpoint.Busy {
			t.Fatalf("%+v %v", out, err)
		}
		out, err = s.Maintain(ctx, protocol.StoreMaintain{Checkpoint: "truncate", IncrementalVacuumPages: 10, Optimize: true})
		if err != nil || out.After.WALBytes != 0 {
			t.Fatalf("%+v %v", out, err)
		}
	})
}

// TestIncrementalVacuumShrinks: deleting audit records leaves free pages
// that only an incremental vacuum returns to the file system (§8.1.1).
func TestIncrementalVacuumShrinks(t *testing.T) {
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	s := migrated(t, cfg)
	ctx := context.Background()
	big := json.RawMessage(fmt.Sprintf(`{"pad":%q}`, strings.Repeat("x", 4000)))
	for i := 0; i < 50; i++ {
		e := audit("bulk")
		e.Detail = big
		if err := s.AppendAudit(ctx, []AuditEntry{e, e, e, e}); err != nil {
			t.Fatal(err)
		}
	}
	rows := listAll(t, s)
	digest, err := protocol.AuditDigest(views(rows))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.PruneAudit(ctx, rows[len(rows)-1].Seq, digest, audit("audit_pruned")); err != nil || n != int64(len(rows)) {
		t.Fatalf("pruned %d %v", n, err)
	}
	if _, err := s.Maintain(ctx, protocol.StoreMaintain{Checkpoint: "truncate"}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Usage(ctx)
	out, err := s.Maintain(ctx, protocol.StoreMaintain{IncrementalVacuumPages: protocol.MaxVacuumPages, Checkpoint: "truncate"})
	if err != nil {
		t.Fatal(err)
	}
	if before.FreelistCount == 0 || out.After.FreelistCount != 0 || out.After.DBBytes >= before.DBBytes {
		t.Fatalf("before %+v after %+v", before, out.After)
	}
}

func TestPruneAudit(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		for i := 0; i < 5; i++ {
			e := audit(fmt.Sprintf("a%d", i))
			e.Detail = json.RawMessage(`{"n": 1.0, "nested": {"b": [1, 2], "a": "é"}}`)
			if err := s.AppendAudit(ctx, []AuditEntry{e}); err != nil {
				t.Fatal(err)
			}
		}
		rows := listAll(t, s)
		first3 := rows[:3]
		digest, err := protocol.AuditDigest(views(first3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.PruneAudit(ctx, first3[2].Seq, strings.Repeat("0", 64), audit("audit_pruned")); !errors.Is(err, ErrDigestMismatch) {
			t.Fatal(err)
		}
		if _, err := s.PruneAudit(ctx, 0, digest, audit("audit_pruned")); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
		if len(listAll(t, s)) != 5 {
			t.Fatal("a refused prune deleted records")
		}
		n, err := s.PruneAudit(ctx, first3[2].Seq, digest, audit("audit_pruned"))
		if err != nil || n != 3 {
			t.Fatalf("%d %v", n, err)
		}
		left := listAll(t, s)
		last := left[len(left)-1]
		if len(left) != 3 || last.Action != "audit_pruned" || !strings.Contains(string(last.Detail), digest) {
			t.Fatalf("left %+v", left)
		}
		if _, err := s.PruneAudit(ctx, first3[2].Seq, digest, audit("audit_pruned")); !errors.Is(err, ErrInvalid) {
			t.Fatalf("pruning nothing: %v", err)
		}
	})
}

func TestFailureKinds(t *testing.T) {
	if FailureKind(nil) != "" || FailureKind(ErrBusy) != "" || FailureKind(ErrUnavailable) != FailureUnavailable {
		t.Fatal("plain errors")
	}
	if k := FailureKind(fmt.Errorf("x: %w", &unavailableError{kind: FailureDiskFull, msg: "m"})); k != FailureDiskFull {
		t.Fatal(k)
	}
	if e := (&unavailableError{kind: FailureIO, msg: "boom"}); e.Error() != "store unavailable: boom" || !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	for code, want := range map[string]string{"53100": FailureDiskFull, "25006": FailureReadOnly, "58030": FailureIO, "08006": FailureUnavailable} {
		if got := postgresFailureKind(&pgconn.PgError{Code: code}); got != want {
			t.Fatalf("%s: %s", code, got)
		}
	}
	if postgresFailureKind(errors.New("x")) != FailureUnavailable || sqliteFailureKind(errors.New("x")) != FailureUnavailable {
		t.Fatal("non-driver errors")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "k.db")
	w, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.SetMaxOpenConns(1)
	if _, err := w.Exec("CREATE TABLE t (b BLOB)"); err != nil {
		t.Fatal(err)
	}
	ro, err := sql.Open("sqlite", readOnlyDSN(path, testBusy))
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.Exec("INSERT INTO t VALUES (x'00')"); sqliteFailureKind(err) != FailureReadOnly {
		t.Fatalf("read-only: %v", err)
	}
	if _, err := w.Exec("PRAGMA max_page_count = 8"); err != nil {
		t.Fatal(err)
	}
	var full error
	for i := 0; i < 100 && full == nil; i++ {
		_, full = w.Exec("INSERT INTO t VALUES (zeroblob(4096))")
	}
	if sqliteFailureKind(full) != FailureDiskFull {
		t.Fatalf("full: %v", full)
	}
	missing, err := sql.Open("sqlite", filepath.Join(dir, "no", "such", "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	if err := missing.Ping(); sqliteFailureKind(err) != FailureIO {
		t.Fatalf("cantopen: %v", err)
	}
	if os.Getenv("OI_TEST_PG_DSN") != "" {
		pg := adminDB(t)
		defer pg.Close()
		tx, err := pg.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		_, err = tx.Exec("SET TRANSACTION READ ONLY")
		if err == nil {
			_, err = tx.Exec("CREATE TEMP TABLE ro_probe (x int)")
		}
		c := &sqlCore{d: postgresDialect()}
		if got := FailureKind(c.classify(err)); got != FailureReadOnly {
			t.Fatalf("pg read-only: %v (%s)", err, got)
		}
	}
}

// writer applies configuration changes until stopped and reports how many
// committed and the first unexpected error.
func writer(s Store) (stop func() (int, error)) {
	var n atomic.Int64
	var first error
	var mu sync.Mutex
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ctx := context.Background()
		v, _ := s.LoadSnapshot(ctx)
		version := v.Meta.ConfigVersion
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			next, err := s.ApplyConfig(ctx, version, []Mutation{PutPolicy{Policy{PolicyID: fmt.Sprintf("w%d", i%7), Subject: "w",
				ExposureSelector: "lab-a/*", Actions: []string{"discover"}, Enabled: true}}}, []AuditEntry{audit("w")})
			if err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
				return
			}
			version = next
			n.Add(1)
		}
	}()
	return func() (int, error) {
		close(done)
		<-finished
		mu.Lock()
		defer mu.Unlock()
		return int(n.Load()), first
	}
}

func TestBackupWhileWritingAndRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	stop := writer(s)
	out := filepath.Join(dir, "backup.db")
	info, err := Backup(ctx, cfg, testBusy, out, t0)
	n, werr := stop()
	if err != nil || werr != nil || n == 0 {
		t.Fatalf("backup %v, writer %d %v", err, n, werr)
	}
	snap, _ := s.LoadSnapshot(ctx)
	if info.Path != out || info.StoreID != snap.Meta.StoreID || info.SchemaVersion != 1 || info.Bytes <= 0 || len(info.SHA256) != 64 || !info.CreatedAt.Equal(t0) {
		t.Fatalf("info %+v", info)
	}
	if _, err := os.Stat(out + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial copy left behind")
	}
	if _, err := Backup(ctx, cfg, testBusy, out, t0); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatal(err)
	}
	// The backup is internally consistent: the newest audit version is the
	// meta version (TV-B3-11a).
	chk, err := sql.Open("sqlite", readOnlyDSN(out, testBusy))
	if err != nil {
		t.Fatal(err)
	}
	var maxAudit, metaVersion int64
	if err := chk.QueryRow("SELECT (SELECT max(config_version) FROM audit), (SELECT config_version FROM meta)").Scan(&maxAudit, &metaVersion); err != nil || maxAudit != metaVersion || metaVersion != info.ConfigVersion {
		t.Fatalf("audit %d meta %d info %d %v", maxAudit, metaVersion, info.ConfigVersion, err)
	}
	_ = chk.Close()

	// After the backup: revoke the token, delete the policy, run again.
	if _, err := s.ApplyConfig(ctx, snap.Meta.ConfigVersion, []Mutation{DeletePolicy{PolicyID: "p1"}}, []AuditEntry{audit("after")}); err != nil {
		t.Fatal(err)
	}
	prevEpoch, err := s.StartRun(ctx, "run_2", t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: out, Now: t0}); !errors.Is(err, ErrAnotherInstance) {
		t.Fatalf("restore while serving: %v", err)
	}
	_ = s.Close()

	res, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: out, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Maintenance || res.Epoch != prevEpoch || res.EpochBefore != info.Epoch || res.PreservedAs == "" || res.StoreID != info.StoreID {
		t.Fatalf("restore %+v (prev epoch %d, backup %+v)", res, prevEpoch, info)
	}
	for _, p := range []string{cfg.SQLitePath + ".restore.tmp", cfg.SQLitePath + "-wal"} {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			t.Fatalf("%s left behind", p)
		}
	}
	r, err := Open(ctx, cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := r.LoadSnapshot(ctx)
	rows := listAll(t, r)
	_ = r.Close()
	last := rows[len(rows)-1]
	if !got.Meta.Maintenance || got.Meta.Epoch != prevEpoch || len(got.Policies) == 0 || last.Action != "restored_from" ||
		!strings.Contains(string(last.Detail), `"preserved_as"`) || !strings.Contains(string(last.Detail), `"readable":true`) {
		t.Fatalf("restored %+v policies %d last %+v", got.Meta, len(got.Policies), last)
	}
	// The replaced database is preserved with the post-backup state.
	old, err := Open(ctx, Config{Driver: SQLite, SQLitePath: res.PreservedAs}, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	prev, _ := old.LoadSnapshot(ctx)
	_ = old.Close()
	for _, p := range prev.Policies {
		if p.PolicyID == "p1" {
			t.Fatal("preserved database lost the post-backup deletion")
		}
	}
}

func TestBackupFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if _, err := Backup(ctx, Config{}, testBusy, "x", t0); err == nil {
		t.Fatal("invalid config")
	}
	if _, err := Backup(ctx, Config{Driver: Postgres, PostgresDSN: "postgres://x"}, testBusy, "x", t0); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "none.db")}
	if _, err := Backup(ctx, cfg, testBusy, filepath.Join(dir, "b.db"), t0); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatal(err)
	}
	cfg.SQLitePath = filepath.Join(dir, "ois.db")
	s := migrated(t, cfg)
	seed(t, s)
	if _, err := Backup(ctx, cfg, testBusy, filepath.Join(dir, "missing-dir", "b.db"), t0); err == nil {
		t.Fatal("unwritable destination")
	}
	if _, err := Backup(ctx, Config{Driver: SQLite, SQLitePath: dir}, testBusy, filepath.Join(dir, "c.db"), t0); err == nil {
		t.Fatal("directory as database")
	}
	// A stale partial copy from an interrupted run is replaced (TV-B3-11i).
	out := filepath.Join(dir, "b.db")
	if err := os.WriteFile(out+".partial", []byte("torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(ctx, cfg, testBusy, out, t0); err != nil {
		t.Fatal(err)
	}
}

// TestBackupOfStoppedDatabase: with OIS stopped the WAL and shared-memory
// files are gone; the read-only connection must still read the database.
func TestBackupOfStoppedDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	snap, _ := s.LoadSnapshot(ctx)
	_ = s.Close()
	info, err := Backup(ctx, cfg, testBusy, filepath.Join(dir, "b.db"), t0)
	if err != nil || info.ConfigVersion != snap.Meta.ConfigVersion || info.Epoch != snap.Meta.Epoch {
		t.Fatalf("%+v %v", info, err)
	}
	if r, err := Open(ctx, cfg, testBusy); err != nil {
		t.Fatalf("database unusable after the backup: %v", err)
	} else {
		_ = r.Close()
	}
}

func copyTo(t *testing.T, src, dst string) {
	t.Helper()
	if _, err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
}

func TestInspectRejectsBadBackups(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	good := filepath.Join(dir, "good.db")
	if _, err := Backup(ctx, cfg, testBusy, good, t0); err != nil {
		t.Fatal(err)
	}
	mutate := func(name, q string) string {
		p := filepath.Join(dir, name)
		copyTo(t, good, p)
		db, err := sql.Open("sqlite", p)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		mutate("sum.db", "UPDATE schema_migrations SET checksum = 'x'"):                                      "checksum",
		mutate("new.db", "INSERT INTO schema_migrations VALUES (99, 'future', 'x', '2026-01-01T00:00:00Z')"): "newer",
		mutate("pg.db", "UPDATE meta SET backend = 'postgres'"):                                              "not sqlite",
		mutate("nometa.db", "DELETE FROM meta"):                                                              "meta",
		mutate("nomig.db", "DELETE FROM schema_migrations"):                                                  "pending",
		mutate("notable.db", "DROP TABLE schema_migrations"):                                                 "schema_migrations",
	}
	junk := filepath.Join(dir, "junk.db")
	if err := os.WriteFile(junk, bytes.Repeat([]byte("not a database "), 512), 0o600); err != nil {
		t.Fatal(err)
	}
	cases[junk] = "not a database"
	torn := filepath.Join(dir, "torn.db")
	b, _ := os.ReadFile(good)
	if err := os.WriteFile(torn, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	cases[torn] = ""
	for path, want := range cases {
		_, err := inspectSQLiteFile(ctx, path, testBusy)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v (want %q)", filepath.Base(path), err, want)
		}
	}
}

func TestRestoreSQLiteEdges(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	backup := filepath.Join(dir, "b.db")
	info, err := Backup(ctx, cfg, testBusy, backup, t0)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// A backup of another store is refused and nothing changes.
	other := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "other.db")}
	o := migrated(t, other)
	foreign := filepath.Join(dir, "foreign.db")
	if _, err := Backup(ctx, other, testBusy, foreign, t0); err != nil {
		t.Fatal(err)
	}
	_ = o.Close()
	if _, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: foreign, Now: t0}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "belongs to store") {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: filepath.Join(dir, "absent.db"), Now: t0}); err == nil {
		t.Fatal("missing backup")
	}
	// A stale temporary file from an interrupted restore is ignored.
	if err := os.WriteFile(cfg.SQLitePath+".restore.tmp", []byte("torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Disaster recovery: the database is unreadable. It is preserved with
	// its WAL and replaced; MinEpoch bounds the unknown epoch.
	if err := os.WriteFile(cfg.SQLitePath, bytes.Repeat([]byte("garbage!"), 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.SQLitePath+"-wal", []byte("old wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: backup, MinEpoch: info.Epoch + 40, Now: t0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Epoch != info.Epoch+40 || res.PreservedAs == "" {
		t.Fatalf("%+v", res)
	}
	if b, err := os.ReadFile(res.PreservedAs + "-wal"); err != nil || string(b) != "old wal" {
		t.Fatalf("preserved wal %q %v", b, err)
	}
	r, err := Open(ctx, cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	rows := listAll(t, r)
	_ = r.Close()
	if d := string(rows[len(rows)-1].Detail); !strings.Contains(d, `"readable":false`) {
		t.Fatal(d)
	}
	// Restoring where no database exists recreates it from the backup.
	fresh := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "new", "ois.db")}
	if err := os.MkdirAll(filepath.Dir(fresh.SQLitePath), 0o700); err != nil {
		t.Fatal(err)
	}
	res, err = Restore(ctx, fresh, testBusy, RestoreRequest{From: backup, Now: t0})
	if err != nil || res.PreservedAs != "" || res.Epoch != info.Epoch {
		t.Fatalf("%+v %v", res, err)
	}
	// A database path that is a directory is refused before anything moves.
	if _, err := Restore(ctx, Config{Driver: SQLite, SQLitePath: dir}, testBusy, RestoreRequest{From: backup, Now: t0}); err == nil {
		t.Fatal("directory as database")
	}
}

// TestRestoreConvertsAutoVacuum: a copy made outside OIS without
// incremental auto_vacuum is converted so OIS can serve it (B3 §5).
func TestRestoreConvertsAutoVacuum(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	backup := filepath.Join(dir, "b.db")
	if _, err := Backup(ctx, cfg, testBusy, backup, t0); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	db, err := sql.Open("sqlite", backup)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA auto_vacuum = NONE", "VACUUM"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()
	if _, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: backup, Now: t0}); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
}

func TestRestoreExternal(t *testing.T) {
	ctx := context.Background()
	if _, err := Restore(ctx, Config{}, testBusy, RestoreRequest{External: "x"}); err == nil {
		t.Fatal("invalid config")
	}
	sq := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "x.db")}
	for _, req := range []RestoreRequest{{}, {From: "a", External: "b"}, {External: "b", MinEpoch: -1}} {
		if _, err := Restore(ctx, sq, testBusy, req); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", req, err)
		}
	}
	if _, err := Restore(ctx, Config{Driver: Postgres, PostgresDSN: "postgres://x"}, testBusy, RestoreRequest{From: "f"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, sq, testBusy, RestoreRequest{External: "snapshot"}); err == nil {
		t.Fatal("restoring a missing database")
	}
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		seed(t, s)
		if _, err := Restore(ctx, cfg, testBusy, RestoreRequest{External: "pg_restore", Now: t0}); !errors.Is(err, ErrAnotherInstance) {
			t.Fatalf("while serving: %v", err)
		}
		_ = s.Close()
		res, err := Restore(ctx, cfg, testBusy, RestoreRequest{External: "pg_restore 2026-11-04", MinEpoch: 50, Now: t0})
		if err != nil || res.Epoch != 50 || !res.Maintenance || res.EpochBefore != 1 {
			t.Fatalf("%+v %v", res, err)
		}
		// MinEpoch never lowers the epoch.
		res, err = Restore(ctx, cfg, testBusy, RestoreRequest{External: "again", MinEpoch: 3, Now: t0})
		if err != nil || res.Epoch != 50 {
			t.Fatalf("%+v %v", res, err)
		}
		r, err := Open(ctx, cfg, testBusy)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		rows := listAll(t, r)
		last := rows[len(rows)-1]
		detail, err := protocol.CanonicalJSON(last.Detail)
		if err != nil || last.Action != "restored_from" || !strings.Contains(string(detail), `"external":"again"`) {
			t.Fatalf("%+v %v", last, err)
		}
	})
}

// richSeed adds rows whose canonical form differs between backends unless
// normalized: nested JSON, unicode, floats, nil labels and a revoked token.
func richSeed(t *testing.T, s *SQLStore) {
	t.Helper()
	seed(t, s)
	ctx := context.Background()
	snap, _ := s.LoadSnapshot(ctx)
	e := audit("rich")
	e.Detail = json.RawMessage(`{"z": [1.50, 2e3, -0], "a": {"ü": "<&>"}, "n": null}`)
	if _, err := s.ApplyConfig(ctx, snap.Meta.ConfigVersion, []Mutation{
		PutAccess{Access{AccessID: "lab-b", SPKIPins: []string{strings.Repeat("a", 64)}, MaxExposures: 3,
			Labels: map[string]string{"site": "b", "zone": "1"}, CreatedAt: t0, UpdatedAt: t0}},
		PutPolicy{Policy{PolicyID: "p2", Subject: "b", ExposureSelector: "lab-b/http", Actions: []string{"discover", "open"}}},
		PutToken{Token{TokenID: "live-token-00001", Subject: "b", Scope: "consumer", SecretSHA256: []byte{9, 8}, ExpiresAt: t0, CreatedAt: t0}},
		SetMaintenance{On: true},
	}, []AuditEntry{e}); err != nil {
		t.Fatal(err)
	}
}

func exportOf(t *testing.T, cfg Config) ([]byte, ExportSummary) {
	t.Helper()
	var buf bytes.Buffer
	sum, err := Export(context.Background(), cfg, testBusy, &buf, t0)
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), sum
}

func freshConfig(t *testing.T, driver string) Config {
	if driver == Postgres {
		return newPGDatabase(t)
	}
	return Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "imported.db")}
}

func TestExportImportAcrossBackends(t *testing.T) {
	ctx := context.Background()
	for _, src := range backends(t) {
		for _, dst := range backends(t) {
			t.Run(src.name+"-to-"+dst.name, func(t *testing.T) {
				srcCfg := src.cfg(t)
				s := migrated(t, srcCfg)
				richSeed(t, s)
				before, _ := s.LoadSnapshot(ctx)
				srcAudit := listAll(t, s)
				if _, err := Export(ctx, srcCfg, testBusy, &bytes.Buffer{}, t0); !errors.Is(err, ErrAnotherInstance) {
					t.Fatalf("export while serving: %v", err)
				}
				_ = s.Close()
				file, sum := exportOf(t, srcCfg)
				if sum.Tables["accesses"].Count != 2 || sum.Tables["audit"].Count != int64(len(srcAudit)) {
					t.Fatalf("%+v", sum)
				}
				dstCfg := freshConfig(t, dst.name)
				res, err := Import(ctx, dstCfg, testBusy, bytes.NewReader(file), t0)
				if err != nil {
					t.Fatal(err)
				}
				if res.StoreID != before.Meta.StoreID || res.Epoch != before.Meta.Epoch || res.Backend != dst.name || res.SourceBackend != src.name {
					t.Fatalf("%+v", res)
				}
				d, err := Open(ctx, dstCfg, testBusy)
				if err != nil {
					t.Fatal(err)
				}
				after, _ := d.LoadSnapshot(ctx)
				dstAudit := listAll(t, d)
				// The imported database keeps counting audit seq numbers.
				if err := d.AppendAudit(ctx, []AuditEntry{audit("next")}); err != nil {
					t.Fatal(err)
				}
				next := listAll(t, d)
				_ = d.Close()
				after.Meta.Backend = before.Meta.Backend
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("snapshot\n%+v\n%+v", before, after)
				}
				srcDigest, _ := protocol.AuditDigest(views(srcAudit))
				dstDigest, _ := protocol.AuditDigest(views(dstAudit[:len(srcAudit)]))
				if srcDigest != dstDigest || dstAudit[len(dstAudit)-1].Action != "imported_from" || next[len(next)-1].Seq != dstAudit[len(dstAudit)-1].Seq+1 {
					t.Fatalf("audit %s %s %+v", srcDigest, dstDigest, next[len(next)-1])
				}
				if _, err := Import(ctx, dstCfg, testBusy, bytes.NewReader(file), t0); !errors.Is(err, ErrNotEmpty) {
					t.Fatalf("non-empty target: %v", err)
				}
			})
		}
	}
}

func TestImportRejectsBadFiles(t *testing.T) {
	ctx := context.Background()
	srcCfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "src.db")}
	s := migrated(t, srcCfg)
	richSeed(t, s)
	_ = s.Close()
	file, _ := exportOf(t, srcCfg)
	lines := strings.Split(strings.TrimSuffix(string(file), "\n"), "\n")
	join := func(ls ...string) []byte { return []byte(strings.Join(ls, "\n") + "\n") }
	n := len(lines)
	rowLine := func(table string) int {
		for i, l := range lines {
			if strings.Contains(l, `"table":"`+table+`"`) {
				return i
			}
		}
		t.Fatalf("no %s row", table)
		return 0
	}
	with := func(i int, line string) []byte {
		c := append([]string(nil), lines...)
		c[i] = line
		return join(c...)
	}
	tok := rowLine("consumer_tokens")
	cases := map[string][]byte{
		"empty":            nil,
		"not an export":    join(`{"format":"other","format_version":1}`),
		"schema version":   with(0, strings.Replace(lines[0], `"schema_version":1`, `"schema_version":2`, 1)),
		"tampered row":     with(tok, strings.Replace(lines[tok], `"subject":"a"`, `"subject":"z"`, 1)),
		"tampered meta":    with(0, strings.Replace(lines[0], `"epoch":1`, `"epoch":9`, 1)),
		"no trailer":       join(lines[:n-1]...),
		"after trailer":    join(append(append([]string(nil), lines...), lines[1])...),
		"out of order":     join(append([]string{lines[0], lines[rowLine("policies")], lines[rowLine("accesses")]}, lines[n-1])...),
		"unknown table":    with(1, `{"table":"secrets","row":{}}`),
		"unknown field":    with(tok, strings.Replace(lines[tok], `"subject"`, `"extra":1,"subject"`, 1)),
		"bad access time":  with(rowLine("accesses"), strings.Replace(lines[rowLine("accesses")], `"created_at":"`, `"created_at":"x`, 1)),
		"bad token time":   with(tok, strings.Replace(lines[tok], `"expires_at":"`, `"expires_at":"x`, 1)),
		"bad exposure":     with(rowLine("exposures"), strings.Replace(lines[rowLine("exposures")], `"updated_at":"`, `"updated_at":"x`, 1)),
		"bad audit detail": with(rowLine("audit"), strings.Replace(lines[rowLine("audit")], `"detail":{`, `"detail":[{`, 1)),
		"bad started":      with(0, strings.Replace(lines[0], `"last_started_at":"`, `"last_started_at":"x`, 1)),
		"constraint":       with(rowLine("exposures"), strings.Replace(lines[rowLine("exposures")], `"access_id":"lab-a"`, `"access_id":"ghost"`, 1)),
	}
	dstCfg := freshConfig(t, SQLite)
	for name, body := range cases {
		if _, err := Import(ctx, dstCfg, testBusy, bytes.NewReader(body), t0); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	// Every refusal left the target empty, so the good file still imports
	// (TV-B3-11d/i).
	if _, err := Import(ctx, dstCfg, testBusy, bytes.NewReader(file), t0); err != nil {
		t.Fatal(err)
	}
}

func TestExportImportEdges(t *testing.T) {
	ctx := context.Background()
	// An empty database exports and imports (zero-row tables still have a
	// digest).
	empty := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "e.db")}
	_ = migrated(t, empty).Close()
	file, sum := exportOf(t, empty)
	if sum.Tables["audit"].Count != 0 || len(sum.Tables["audit"].SHA256) != 64 {
		t.Fatalf("%+v", sum)
	}
	if _, err := Import(ctx, freshConfig(t, SQLite), testBusy, bytes.NewReader(file), t0); err != nil {
		t.Fatal(err)
	}
	// A database with history but no configuration is not empty.
	used := freshConfig(t, SQLite)
	u := migrated(t, used)
	if err := u.AppendAudit(ctx, []AuditEntry{audit("x")}); err != nil {
		t.Fatal(err)
	}
	_ = u.Close()
	if _, err := Import(ctx, used, testBusy, bytes.NewReader(file), t0); !errors.Is(err, ErrNotEmpty) || !strings.Contains(err.Error(), "audit") {
		t.Fatal(err)
	}
	if _, err := Import(ctx, Config{}, testBusy, bytes.NewReader(file), t0); err == nil {
		t.Fatal("invalid config")
	}
	if _, err := Export(ctx, Config{}, testBusy, io.Discard, t0); err == nil {
		t.Fatal("invalid config")
	}
}

func TestFileHelpersFail(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := copyFile(filepath.Join(dir, "absent"), filepath.Join(dir, "x")); err == nil {
		t.Fatal("missing source")
	}
	if _, err := copyFile(src, src); err == nil {
		t.Fatal("existing destination")
	}
	if err := linkOrCopy(src, src); err == nil {
		t.Fatal("link and copy onto an existing file")
	}
	if err := syncFile(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("sync of a missing file")
	}
	if _, _, err := fileDigest(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("digest of a missing file")
	}
	if fileSize(filepath.Join(dir, "absent")) != 0 {
		t.Fatal("size of a missing file")
	}
	// The previous WAL cannot be moved onto a directory, and a directory
	// in place of the shared-memory file cannot be removed.
	db := filepath.Join(dir, "p.db")
	if err := os.WriteFile(db+"-wal", []byte("w"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "keep-wal", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := movePrevWAL(db, filepath.Join(dir, "keep")); err == nil {
		t.Fatal("WAL moved onto a directory")
	}
	if err := os.Remove(db + "-wal"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(db+"-shm", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := movePrevWAL(db, filepath.Join(dir, "keep2")); err == nil {
		t.Fatal("shm directory removed")
	}
}

func TestRecoveryOpsOnClosedStore(t *testing.T) {
	ctx := context.Background()
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		_ = s.Close()
		if _, err := s.Usage(ctx); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
		if _, err := s.Maintain(ctx, protocol.StoreMaintain{Optimize: true}); err == nil {
			t.Fatal("maintain on a closed store")
		}
		if err := s.ProbeWrite(ctx); err == nil {
			t.Fatal("probe on a closed store")
		}
		if _, err := s.PruneAudit(ctx, 1, "x", audit("p")); err == nil {
			t.Fatal("prune on a closed store")
		}
		if _, err := s.export(ctx, io.Discard, t0); err == nil {
			t.Fatal("export from a closed store")
		}
		if _, err := s.markRestored(ctx, 1, map[string]any{}, t0); err == nil {
			t.Fatal("mark on a closed store")
		}
	})
}

func TestBackupCancelled(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(dir, "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(dir, "b.db")
	if _, err := Backup(ctx, cfg, testBusy, out, t0); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	for _, p := range []string{out, out + ".partial"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left behind", p)
		}
	}
}

// TestRecoveryOpsOnMissingTables: every step of export, import, prune and
// restore marking reports a damaged schema instead of carrying on.
func TestRecoveryOpsOnMissingTables(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"audit", "exposures", "policies", "consumer_tokens", "accesses", "meta", "schema_migrations"} {
		t.Run(table, func(t *testing.T) {
			s, raw := sqliteStore(t)
			if _, err := raw.Exec("PRAGMA foreign_keys = OFF; DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			if _, err := s.export(ctx, io.Discard, t0); err == nil {
				t.Fatal("export of a damaged schema")
			}
			switch table {
			case "audit":
				if _, err := s.PruneAudit(ctx, 1, "x", audit("p")); err == nil {
					t.Fatal("prune without an audit table")
				}
			case "meta":
				if _, err := s.markRestored(ctx, 1, map[string]any{}, t0); err == nil {
					t.Fatal("mark without meta")
				}
			}
			if table == "schema_migrations" {
				return // Import checks the schema through Initialize first.
			}
			// An otherwise empty target with the same damage refuses the import.
			fresh := migrated(t, freshConfig(t, SQLite))
			if _, err := fresh.wdb.Exec("PRAGMA foreign_keys = OFF"); err != nil {
				t.Fatal(err)
			}
			if _, err := fresh.wdb.Exec("DROP TABLE " + table); err != nil {
				t.Fatal(err)
			}
			src := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "src.db")}
			_ = migrated(t, src).Close()
			file, _ := exportOf(t, src)
			if _, err := fresh.importFrom(ctx, bytes.NewReader(file), t0); err == nil {
				t.Fatal("import into a damaged schema")
			}
		})
	}
}
