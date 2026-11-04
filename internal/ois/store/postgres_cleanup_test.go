package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The data operation commits normally. Only its final lock-release query is
// blackholed, so an error must report cleanup uncertainty without pretending
// the completed restore was rolled back or retrying its audit mutation.
func TestPostgresRestoreReportsCleanupFailure(t *testing.T) {
	var gate *pgQueryGate
	base, wrapped := pgCloseConfig(t, func(conn net.Conn) net.Conn {
		gate = &pgQueryGate{Conn: conn, match: []byte("pg_advisory_unlock"), drop: true,
			entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		return gate
	})
	ctx := context.Background()
	initial := migrated(t, base)
	before, err := initial.LoadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	result, restoreErr := Restore(ctx, wrapped, testBusy, RestoreRequest{External: "cleanup-failure-barrier", MinEpoch: 77, Now: t0})
	elapsed := time.Since(started)
	if gate == nil {
		t.Fatal("restore never opened its dedicated lock connection")
	}
	defer gate.Conn.Close()
	waitPGClose(t, gate.entered)
	waitPGClose(t, gate.closed)
	// Inspect the committed data through a separate real PG connection. Do not
	// retry Restore or rely on another OIS lock acquisition after the failure.
	db, err := sql.Open("pgx", base.PostgresDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var epoch, audits int64
	var maintenance bool
	if err := db.QueryRowContext(ctx, "SELECT epoch, maintenance FROM meta WHERE id=1").Scan(&epoch, &maintenance); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit WHERE action='restored_from'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("completed_result=%s returned_error=%v elapsed=%v unlock_blackhole_hit=true physical_socket_closed=true stored_epoch=%d stored_maintenance=%v restored_audits=%d", encoded, restoreErr, elapsed, epoch, maintenance, audits)
	if result.StoreID != before.Meta.StoreID || result.EpochBefore != before.Meta.Epoch || result.Epoch != 77 || !result.Maintenance || epoch != 77 || !maintenance || audits != 1 {
		t.Fatalf("completed restore result/data changed or operation repeated: %+v epoch=%d maintenance=%v audits=%d", result, epoch, maintenance, audits)
	}
	if elapsed < lockCheck {
		t.Fatalf("unlock blackhole did not exercise the original %v deadline: %v", lockCheck, elapsed)
	}
	assertCompletedCleanup(t, "restore", restoreErr)
}

func assertCompletedCleanup(t *testing.T, operation string, err error) {
	t.Helper()
	var cleanup *CleanupError
	if !errors.As(err, &cleanup) || !cleanup.Completed || cleanup.Operation != operation ||
		!errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) ||
		!strings.Contains(err.Error(), "operation completed but cleanup failed") {
		t.Fatalf("want structured completed %s plus unavailable/deadline cleanup failure, got %v", operation, err)
	}
}

func TestPostgresTransferReportsCleanupFailure(t *testing.T) {
	for _, operation := range []string{"export", "import"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			source := migrated(t, Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "source.db")})
			if _, err := source.ApplyConfig(ctx, 0, nil, []AuditEntry{audit("cleanup-transfer")}); err != nil {
				t.Fatal(err)
			}
			snap, err := source.LoadSnapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var input bytes.Buffer
			expected, err := source.export(ctx, &input, t0)
			if err != nil {
				t.Fatal(err)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			var gate *pgQueryGate
			base, wrapped := pgCloseConfig(t, func(conn net.Conn) net.Conn {
				gate = &pgQueryGate{Conn: conn, match: []byte("pg_advisory_unlock"), drop: true,
					entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
				return gate
			})
			var result ExportSummary
			var operationErr error
			started := time.Now()
			if operation == "export" {
				if _, err := Import(ctx, base, testBusy, bytes.NewReader(input.Bytes()), t0); err != nil {
					t.Fatal(err)
				}
				// Import appends its own audit. Freeze the actual PG export
				// immediately before fault injection, rather than mistaking
				// the source's pre-import row set for this operation's input.
				beforeExport, err := Open(ctx, base, testBusy)
				if err != nil {
					t.Fatal(err)
				}
				expected, err = beforeExport.export(ctx, io.Discard, t0)
				if err != nil {
					t.Fatal(err)
				}
				if err := beforeExport.Close(); err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				started = time.Now()
				result, operationErr = Export(ctx, wrapped, testBusy, &output, t0)
				// A cleanup failure must retain the complete serialized result.
				check := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "check.db")}
				actual, err := Import(ctx, check, testBusy, bytes.NewReader(output.Bytes()), t0)
				if err != nil || actual.StoreID != snap.Meta.StoreID || !reflect.DeepEqual(actual.Summary, expected) {
					t.Fatalf("completed export output not preserved: %+v %v", actual, err)
				}
			} else {
				actual, err := Import(ctx, wrapped, testBusy, bytes.NewReader(input.Bytes()), t0)
				result, operationErr = actual.Summary, err
				if actual.StoreID != snap.Meta.StoreID || actual.ConfigVersion != 1 || actual.Backend != Postgres {
					t.Fatalf("completed import result lost: %+v", actual)
				}
			}
			if gate == nil {
				t.Fatal("dedicated lock connection missing")
			}
			defer gate.Conn.Close()
			waitPGClose(t, gate.entered)
			waitPGClose(t, gate.closed)
			if time.Since(started) < lockCheck {
				t.Fatal("original unlock deadline not exercised")
			}
			if !reflect.DeepEqual(result, expected) {
				t.Fatalf("completed summary lost: got %+v want %+v", result, expected)
			}
			assertCompletedCleanup(t, operation, operationErr)
			db, err := sql.Open("pgx", base.PostgresDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var id string
			var version, audits int64
			if err := db.QueryRowContext(ctx, "SELECT store_id, config_version FROM meta WHERE id=1").Scan(&id, &version); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit WHERE action='cleanup-transfer'").Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if id != snap.Meta.StoreID || version != 1 || audits != 1 {
				t.Fatalf("data changed or replayed: %s %d %d", id, version, audits)
			}
			t.Logf("operation=%s completed=true exact_summary=true audit_count=%d error=%v", operation, audits, operationErr)
		})
	}
}

func TestOperationCleanupErrorPreservesCauses(t *testing.T) {
	operationErr := &ConflictError{Current: 7}
	closeErr := errors.Join(ErrUnavailable, context.DeadlineExceeded)
	for _, scenario := range []string{"success", "operation_only", "cleanup_only", "both"} {
		t.Run(scenario, func(t *testing.T) {
			var opErr, cleanupErr error
			if scenario == "operation_only" || scenario == "both" {
				opErr = operationErr
			}
			if scenario == "cleanup_only" || scenario == "both" {
				cleanupErr = closeErr
			}
			got := operationCleanupError("test", opErr, cleanupErr)
			if cleanupErr == nil {
				if got != opErr {
					t.Fatalf("original error identity lost: %v", got)
				}
				return
			}
			var cleanup *CleanupError
			if !errors.As(got, &cleanup) || cleanup.Operation != "test" || cleanup.Completed != (opErr == nil) ||
				!errors.Is(got, ErrUnavailable) || !errors.Is(got, context.DeadlineExceeded) {
				t.Fatalf("cleanup facts/cause lost: %v", got)
			}
			if opErr != nil {
				var conflict *ConflictError
				if !errors.Is(got, operationErr) || !errors.As(got, &conflict) || conflict.Current != 7 ||
					strings.Contains(got.Error(), "operation completed") {
					t.Fatalf("operation cause changed or false success: %v", got)
				}
			}
		})
	}
}

func TestPostgresFailedOpenPreservesCleanupFailure(t *testing.T) {
	var gate *pgQueryGate
	_, cfg := pgCloseConfig(t, func(conn net.Conn) net.Conn {
		gate = &pgQueryGate{Conn: conn, match: []byte("pg_advisory_unlock"), drop: true,
			entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
		return gate
	})
	s, err := Open(context.Background(), cfg, testBusy)
	if s != nil {
		_ = s.Close()
		t.Fatal("missing schema unexpectedly opened")
	}
	if gate == nil {
		t.Fatal("dedicated lock connection missing")
	}
	defer gate.Conn.Close()
	waitPGClose(t, gate.entered)
	waitPGClose(t, gate.closed)
	var cleanup *CleanupError
	if !errors.Is(err, ErrMigrationPending) || !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &cleanup) || cleanup.Completed || cleanup.Operation != "open" {
		t.Fatalf("failed initialization hid its cleanup uncertainty or initial cause: %v", err)
	}
}
