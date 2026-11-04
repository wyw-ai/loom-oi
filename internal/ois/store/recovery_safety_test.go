package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestoreDetachesOrphanWAL(t *testing.T) {
	for _, emptyMain := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing_main", true: "empty_main"}[emptyMain], func(t *testing.T) {
			ctx := context.Background()
			cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
			s := migrated(t, cfg)
			seed(t, s)
			backup := filepath.Join(filepath.Dir(cfg.SQLitePath), "backup.db")
			if _, err := Backup(ctx, cfg, testBusy, backup, t0); err != nil {
				t.Fatal(err)
			}
			if err := checkpointSQLite(ctx, s.wdb); err != nil {
				t.Fatal(err)
			}
			if _, err := s.wdb.Exec("UPDATE meta SET epoch=77, maintenance=false WHERE id=1"); err != nil {
				t.Fatal(err)
			}
			wal, err := os.ReadFile(cfg.SQLitePath + "-wal")
			if err != nil {
				t.Fatal(err)
			}
			if len(wal) == 0 {
				t.Fatal("empty captured WAL")
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(cfg.SQLitePath); err != nil {
				t.Fatal(err)
			}
			if emptyMain {
				if err := os.WriteFile(cfg.SQLitePath, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(cfg.SQLitePath+"-wal", wal, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cfg.SQLitePath+"-shm", []byte("stale"), 0600); err != nil {
				t.Fatal(err)
			}
			res, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: backup, Now: t0})
			if err != nil {
				t.Fatal(err)
			}
			if res.PreservedAs == "" {
				t.Fatal("orphan WAL preservation was not reported")
			}
			kept, err := os.ReadFile(res.PreservedAs + "-wal")
			if err != nil || !bytes.Equal(kept, wal) {
				t.Fatalf("orphan WAL not preserved: %v", err)
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				if _, err := os.Stat(cfg.SQLitePath + suffix); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stale sidecar %s: %v", suffix, err)
				}
			}
			got, err := Open(ctx, cfg, testBusy)
			if err != nil {
				t.Fatal(err)
			}
			defer got.Close()
			snap, err := got.LoadSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !snap.Meta.Maintenance || snap.Meta.Epoch != res.Epoch {
				t.Fatalf("restore reported %+v; reopened meta %+v", res, snap.Meta)
			}
			audits := listAll(t, got)
			if audits[len(audits)-1].Action != "restored_from" {
				t.Fatalf("restore audit lost: %+v", audits)
			}
		})
	}
}

func TestRestoreWaitsForCompleteCheckpoint(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	backup := filepath.Join(filepath.Dir(cfg.SQLitePath), "backup.db")
	if _, err := Backup(ctx, cfg, testBusy, backup, t0); err != nil {
		t.Fatal(err)
	}
	if err := checkpointSQLite(ctx, s.wdb); err != nil {
		t.Fatal(err)
	}
	ro, err := sql.Open("sqlite", readOnlyDSN(cfg.SQLitePath, testBusy))
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	tx, err := ro.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var oldEpoch int64
	if err := tx.QueryRow("SELECT epoch FROM meta WHERE id=1").Scan(&oldEpoch); err != nil {
		t.Fatal(err)
	}
	newEpoch, err := s.StartRun(ctx, "later", t0)
	if err != nil {
		t.Fatal(err)
	}
	if newEpoch <= oldEpoch {
		t.Fatal("epoch did not advance")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Restore(ctx, cfg, 25*time.Millisecond, RestoreRequest{From: backup, Now: t0})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("restore with pinned WAL: %v", err)
	}
	if fileSize(cfg.SQLitePath+"-wal") == 0 {
		t.Fatal("failed restore detached pinned WAL")
	}
	if kept, err := filepath.Glob(cfg.SQLitePath + ".replaced-*"); err != nil || len(kept) != 0 {
		t.Fatalf("restore reached replacement before checkpoint: %v %v", kept, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	// A process stopped before installation still has the complete old state.
	old, err := Open(ctx, cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := old.LoadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Meta.Epoch != newEpoch || snap.Meta.Maintenance {
		t.Fatalf("old state lost: %+v", snap.Meta)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, cfg, testBusy, RestoreRequest{From: backup, Now: t0}); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsSourceWithWAL(t *testing.T) {
	ctx := context.Background()
	src := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "source.db")}
	s := migrated(t, src)
	seed(t, s)
	if fileSize(src.SQLitePath+"-wal") == 0 {
		t.Fatal("source has no WAL")
	}
	dst := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "target.db")}
	_, err := Restore(ctx, dst, testBusy, RestoreRequest{From: src.SQLitePath, Now: t0})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "ois backup") {
		t.Fatalf("live source: %v", err)
	}
	if exists, err := fileExists(dst.SQLitePath); err != nil || exists {
		t.Fatalf("target changed: %v %v", exists, err)
	}
	// The operator's supported recovery path snapshots all committed WAL data.
	backup := filepath.Join(filepath.Dir(src.SQLitePath), "standalone.db")
	if _, err := Backup(ctx, src, testBusy, backup, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, dst, testBusy, RestoreRequest{From: backup, Now: t0}); err != nil {
		t.Fatal(err)
	}
	got, err := Open(ctx, dst, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Close()
	snap, err := got.LoadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Meta.Maintenance || len(snap.Accesses) != 1 || len(snap.Policies) != 1 {
		t.Fatalf("snapshot missing committed data: %+v", snap)
	}
}

func TestRestoreInstallReturnsWALOnFailure(t *testing.T) {
	for _, failure := range []string{"shm_remove", "main_rename"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "ois.db")
			preserved := path + ".replaced"
			main := []byte("damaged database retained exactly")
			wal := []byte("unmerged WAL retained exactly")
			if err := os.WriteFile(path, main, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+"-wal", wal, 0600); err != nil {
				t.Fatal(err)
			}
			if failure == "shm_remove" {
				if err := os.MkdirAll(filepath.Join(path+"-shm", "child"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Missing private file forces the final rename to fail without timing races.
			if err := installSQLiteRestore(filepath.Join(dir, "missing.tmp"), path, preserved); err == nil {
				t.Fatal("expected install failure")
			}
			for file, want := range map[string][]byte{path: main, path + "-wal": wal} {
				got, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("%s changed: %q %v", file, got, err)
				}
			}
		})
	}
}

func TestRestoreDoesNotOverwritePreservedOrphanWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ois.db")
	preserved := path + ".replaced"
	for file, value := range map[string]string{path + "-wal": "new orphan", preserved + "-wal": "existing evidence"} {
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := movePrevWAL(path, preserved); err == nil {
		t.Fatal("existing preserved WAL overwritten")
	}
	for file, want := range map[string]string{path + "-wal": "new orphan", preserved + "-wal": "existing evidence"} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", file, got, err)
		}
	}
}
