package store

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// waitReaderQueued observes the pool's actual queue, not a scheduling delay.
func waitReaderQueued(t *testing.T, ctx context.Context, db *sql.DB, before int64, done <-chan error) {
	t.Helper()
	for db.Stats().WaitCount == before {
		select {
		case err := <-done:
			t.Fatalf("reader completed while the only reader was held: %v; pool %+v", err, db.Stats())
		case <-ctx.Done():
			t.Fatalf("reader never reached pool wait: %v; pool %+v", ctx.Err(), db.Stats())
		default:
			runtime.Gosched()
		}
	}
}

// A queued reader must honor cancellation without taking the writer with it.
// The held read transaction also proves that WAL still allows a write while
// another connection is reading, and that the next snapshot sees that write.
func TestSQLiteReaderWaitCancellationAndWALWrite(t *testing.T) {
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	s := migrated(t, cfg)
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	hold, err := s.rdb.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	tx, err := hold.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var version int64
	if err := tx.QueryRowContext(ctx, "SELECT config_version FROM meta WHERE id=1").Scan(&version); err != nil {
		t.Fatal(err)
	}
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	waits := s.rdb.Stats().WaitCount
	go func() { _, err := s.LoadSnapshot(readCtx); done <- err }()
	waitReaderQueued(t, ctx, s.rdb, waits, done)
	if got, err := s.ApplyConfig(ctx, version, []Mutation{SetMaintenance{On: true}}, []AuditEntry{audit("while_reader_held")}); err != nil || got != version+1 {
		t.Fatalf("independent WAL writer: version=%d error=%v", got, err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued reader cancellation: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("queued reader ignored cancellation")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := hold.Close(); err != nil {
		t.Fatal(err)
	}
	snap, err := s.LoadSnapshot(ctx)
	if err != nil || snap.Meta.ConfigVersion != version+1 || !snap.Meta.Maintenance {
		t.Fatalf("snapshot after reader release: %+v, %v", snap.Meta, err)
	}
	if stats := s.rdb.Stats(); stats.InUse != 0 || stats.Idle != 1 {
		t.Fatalf("reader not returned after cancellation/reuse: %+v", stats)
	}
}

// sqliteFDLinks reads only descriptors for this test's unique database. The
// /proc directory opened by this observer and other tests' files do not count.
func sqliteFDLinks(t *testing.T, path string) (map[string]string, map[string]int) {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	links, counts := map[string]string{}, map[string]int{}
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if errors.Is(err, os.ErrNotExist) { // the observer's directory is already closed
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if link == path || strings.HasPrefix(link, path+"-") || strings.HasPrefix(link, path+".") {
			links[entry.Name()] = link
			counts[link]++
		}
	}
	return links, counts
}

// The negative control restores the old pool policy only in its own store.
// Both cases pin a real reader, query the same WAL database from a second
// caller, then release everything. The production default must restore every
// database-related descriptor count; the old pool must expose its retained
// main-file descriptor even though database/sql reports only one idle reader.
func TestSQLiteReaderDescriptorsReturnToBaseline(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc descriptor evidence")
	}
	for _, oldPool := range []bool{false, true} {
		name := "production_default"
		if oldPool {
			name = "old_pool_negative_control"
		}
		t.Run(name, func(t *testing.T) {
			cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
			s := migrated(t, cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if oldPool {
				s.rdb.SetMaxOpenConns(4)
				s.rdb.SetMaxIdleConns(1)
			}
			// Use the normal snapshot before the one and only baseline so both
			// policies have opened their ordinary read and write WAL handles.
			if _, err := s.LoadSnapshot(ctx); err != nil {
				t.Fatal(err)
			}
			beforeLinks, before := sqliteFDLinks(t, cfg.SQLitePath)
			if before[cfg.SQLitePath] < 2 {
				t.Fatalf("read/write database descriptors missing: %v", beforeLinks)
			}
			hold, err := s.rdb.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Close()
			var version int64
			if err := hold.QueryRowContext(ctx, "SELECT config_version FROM meta WHERE id=1").Scan(&version); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			waits := s.rdb.Stats().WaitCount
			go func() { _, err := s.LoadSnapshot(ctx); done <- err }()
			if oldPool {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("old pool did not use the independent reader")
				}
				if stats := s.rdb.Stats(); stats.OpenConnections != 2 || stats.InUse != 1 || stats.Idle != 1 {
					t.Fatalf("negative control did not create a second reader: %+v", stats)
				}
			} else {
				waitReaderQueued(t, ctx, s.rdb, waits, done)
			}
			if err := hold.Close(); err != nil {
				t.Fatal(err)
			}
			if !oldPool {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("queued snapshot did not finish after reader release")
				}
			}
			afterLinks, after := sqliteFDLinks(t, cfg.SQLitePath)
			stats := s.rdb.Stats()
			t.Logf("reader descriptors old_pool=%v before=%v after=%v pool=%+v", oldPool, beforeLinks, afterLinks, stats)
			if stats.InUse != 0 || stats.Idle != 1 || stats.OpenConnections != 1 {
				t.Fatalf("reader pool did not become idle: %+v", stats)
			}
			if oldPool {
				want := maps.Clone(before)
				want[cfg.SQLitePath]++
				if !maps.Equal(after, want) {
					t.Fatalf("old pool did not reproduce one retained main-file FD: before=%v after=%v", before, after)
				}
			} else if !maps.Equal(after, before) {
				t.Fatalf("database FD counts did not return to the original baseline: before=%v after=%v", before, after)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if links, counts := sqliteFDLinks(t, cfg.SQLitePath); len(counts) != 0 {
				t.Fatalf("database descriptors survive store close: %v", links)
			}
		})
	}
}
