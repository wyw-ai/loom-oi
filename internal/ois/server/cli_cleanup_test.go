package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Only the final unlock request is blackholed. All data operations use the
// real PostgreSQL server, including commit and the independent state readback.
type cliUnlockBlackhole struct {
	net.Conn
	once sync.Once
	hit  chan struct{}
}

func (c *cliUnlockBlackhole) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("pg_advisory_unlock")) {
		c.once.Do(func() { close(c.hit) })
		return len(p), nil
	}
	return c.Conn.Write(p)
}

func cliCleanupDatabase(t *testing.T) (store.Config, store.Config, *sql.DB, <-chan struct{}) {
	t.Helper()
	if os.Getenv("OI_TEST_PG_DSN") == "" {
		t.Skip("OI_TEST_PG_DSN not set")
	}
	pc, err := pgx.ParseConfig(os.Getenv("OI_TEST_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if pc.TLSConfig != nil {
		t.Fatal("unlock fixture requires local PostgreSQL with sslmode=disable")
	}
	admin := stdlib.OpenDB(*pc)
	name := "oi_cli_" + protocol.NewID("")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec("CREATE DATABASE " + quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE " + quoted + " WITH (FORCE)")
		_ = admin.Close()
	})
	pc.Database = name
	plain := stdlib.RegisterConnConfig(pc)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(plain) })
	db := stdlib.OpenDB(*pc)
	t.Cleanup(func() { _ = db.Close() })
	wrapped := pc.Copy()
	dial := wrapped.DialFunc
	hit := make(chan struct{})
	var once sync.Once
	wrapped.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		var out net.Conn = conn
		once.Do(func() { out = &cliUnlockBlackhole{Conn: conn, hit: hit} })
		return out, nil
	}
	registered := stdlib.RegisterConnConfig(wrapped)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(registered) })
	return store.Config{Driver: store.Postgres, PostgresDSN: plain},
		store.Config{Driver: store.Postgres, PostgresDSN: registered}, db, hit
}

func cliCleanupConfig(t *testing.T, cfg store.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ois.json")
	b, err := json.Marshal(Config{APIListen: "127.0.0.1:0", AccessListen: "127.0.0.1:0",
		TLS: TLSConfig{CertFile: "unused-cert", KeyFile: "unused-key", ClientCAFile: "unused-ca"}, Store: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cliCleanupMigrate(t *testing.T, cfg store.Config) {
	t.Helper()
	s, _, err := store.Migrate(context.Background(), cfg, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCLICompletedOperationReportsCleanupFailure(t *testing.T) {
	for _, operation := range []string{"migrate", "migrate_current", "init", "maintain", "export", "import", "restore"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			base, wrapped, db, hit := cliCleanupDatabase(t)
			ctx := context.Background()
			if operation != "migrate" && operation != "init" && operation != "import" {
				cliCleanupMigrate(t, base)
			}
			path := cliCleanupConfig(t, wrapped)
			command := strings.TrimSuffix(operation, "_current")
			args := []string{command, "--config", path}
			var file string
			var want store.ExportSummary
			switch command {
			case "migrate":
				args = append(args, "--yes")
			case "maintain":
				args = append(args, "--optimize")
			case "restore":
				args = append(args, "--yes", "--external", "cli-cleanup", "--min-epoch", "77")
			case "export":
				file = filepath.Join(t.TempDir(), "export.jsonl")
				args = append(args, "--out", file)
			case "import":
				source := store.Config{Driver: store.SQLite, SQLitePath: filepath.Join(t.TempDir(), "source.db")}
				cliCleanupMigrate(t, source)
				var input bytes.Buffer
				var err error
				want, err = store.Export(ctx, source, 200*time.Millisecond, &input, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				file = filepath.Join(t.TempDir(), "import.jsonl")
				if err := os.WriteFile(file, input.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--in", file, "--yes")
			}
			started := time.Now()
			code, out, errOut := runMain(args...)
			select {
			case <-hit:
			default:
				t.Fatal("CLI did not reach the final unlock request")
			}
			// Never include the init token or configuration DSN in test logs.
			t.Logf("command=%s exit=%d stdout_bytes=%d elapsed=%v cleanup_diagnostic=%v", operation, code, len(out), time.Since(started), strings.Contains(errOut, "cleanup failed"))
			if code != 1 || !strings.Contains(errOut, "operation completed but cleanup failed") {
				t.Errorf("completed operation must report cleanup failure, got exit=%d diagnostic=%q", code, errOut)
			}
			var storeID string
			var epoch int64
			var maintenance bool
			if err := db.QueryRow("SELECT store_id, epoch, maintenance FROM meta WHERE id=1").Scan(&storeID, &epoch, &maintenance); err != nil {
				t.Fatal(err)
			}
			switch command {
			case "migrate":
				message := "applied migrations [1]"
				if operation == "migrate_current" {
					message = "schema is current"
				}
				var count int
				if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 || strings.TrimSpace(out) != message {
					t.Fatalf("migration result missing/repeated: count=%d output=%q err=%v", count, out, err)
				}
			case "init":
				var token protocol.TokenCreated
				decoder := json.NewDecoder(strings.NewReader(out))
				if err := decoder.Decode(&token); err != nil || token.Token == "" || token.TokenID == "" || token.Scope != protocol.ScopeAdmin {
					t.Fatal("completed init did not preserve its token JSON")
				}
				if decoder.Decode(new(any)) != io.EOF || strings.Contains(errOut, token.Token) {
					t.Fatal("token was repeated or leaked to stderr")
				}
				var count, audits int
				if err := db.QueryRow("SELECT count(*) FROM consumer_tokens WHERE token_id=$1", token.TokenID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow("SELECT count(*) FROM audit WHERE action='admin_token_bootstrapped'").Scan(&audits); err != nil || count != 1 || audits != 1 {
					t.Fatalf("token/audit commit changed: tokens=%d audits=%d err=%v", count, audits, err)
				}
			case "maintain":
				var result protocol.StoreMaintained
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.Before.DBBytes <= 0 || result.After.DBBytes <= 0 {
					t.Fatalf("completed maintenance result lost: %v %+v", err, result)
				}
			case "restore":
				var result store.RestoreResult
				var audits int
				if err := db.QueryRow("SELECT count(*) FROM audit WHERE action='restored_from'").Scan(&audits); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.StoreID != storeID || result.Epoch != 77 || epoch != 77 || !result.Maintenance || !maintenance || audits != 1 || !strings.Contains(errOut, "ois admin resume") {
					t.Fatalf("completed restore result lost/replayed: %v %+v epoch=%d maintenance=%v audits=%d", err, result, epoch, maintenance, audits)
				}
			case "import":
				var result store.ImportResult
				if err := json.Unmarshal([]byte(out), &result); err != nil || result.StoreID != storeID || result.Backend != store.Postgres || !reflect.DeepEqual(result.Summary, want) {
					t.Fatalf("completed import result lost: %v %+v", err, result)
				}
			case "export":
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatalf("complete export file was removed: %v", err)
				}
				var result store.ExportSummary
				if err := json.Unmarshal([]byte(out), &result); err != nil {
					t.Fatalf("completed export summary lost: %v", err)
				}
				check := store.Config{Driver: store.SQLite, SQLitePath: filepath.Join(t.TempDir(), "verify.db")}
				actual, err := store.Import(ctx, check, 200*time.Millisecond, bytes.NewReader(data), time.Now())
				if err != nil || actual.StoreID != storeID || !reflect.DeepEqual(actual.Summary, result) {
					t.Fatalf("export file/summary not complete: %v %+v", err, actual)
				}
			}
		})
	}
}

type cliOutputFailure struct {
	err   error
	calls int
}

func (w *cliOutputFailure) Write([]byte) (int, error) {
	w.calls++
	return 0, w.err
}

func TestCLIOperationAndCleanupFailures(t *testing.T) {
	for _, scenario := range []string{"init_existing", "init_output", "maintain_invalid"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			base, wrapped, db, hit := cliCleanupDatabase(t)
			ctx := context.Background()
			if scenario == "init_existing" {
				cfg, err := LoadConfig(cliCleanupConfig(t, base))
				if err != nil {
					t.Fatal(err)
				}
				if err := initStore(ctx, cfg, protocol.TokenCreate{Subject: "admin", Scope: protocol.ScopeAdmin}, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
			path := cliCleanupConfig(t, wrapped)
			if scenario == "maintain_invalid" {
				cliCleanupMigrate(t, base)
				code, out, diagnostic := runMain("maintain", "--config", path, "--checkpoint", "bogus")
				if code != 1 || out != "" || !strings.Contains(diagnostic, "checkpoint") || !strings.Contains(diagnostic, "cleanup failed") || strings.Contains(diagnostic, "operation completed") {
					t.Fatalf("must preserve primary failure and cleanup without a success result: %d %q %q", code, out, diagnostic)
				}
			} else {
				cfg, err := LoadConfig(path)
				if err != nil {
					t.Fatal(err)
				}
				outputErr := errors.New("token output unavailable")
				failedOutput := &cliOutputFailure{err: outputErr}
				err = initStore(ctx, cfg, protocol.TokenCreate{Subject: "admin", Scope: protocol.ScopeAdmin}, failedOutput)
				want, calls := outputErr, 1
				if scenario == "init_existing" {
					want, calls = errInitialized, 0
				}
				var cleanup *store.CleanupError
				if !errors.Is(err, want) || !errors.Is(err, store.ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &cleanup) || cleanup.Completed || cleanup.Operation != "init" || failedOutput.calls != calls {
					t.Fatalf("operation/cleanup causes or single output attempt lost: err=%v writes=%d", err, failedOutput.calls)
				}
				var tokens, audits int
				if err := db.QueryRow("SELECT count(*) FROM consumer_tokens").Scan(&tokens); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow("SELECT count(*) FROM audit WHERE action='admin_token_bootstrapped'").Scan(&audits); err != nil || tokens != 1 || audits != 1 {
					t.Fatalf("init was replayed or its committed token changed: tokens=%d audits=%d err=%v", tokens, audits, err)
				}
			}
			select {
			case <-hit:
			default:
				t.Fatal("cleanup failure not exercised")
			}
		})
	}
}

type cliExportFile struct {
	syncErr, closeErr error
	operations        []string
}

func (f *cliExportFile) Sync() error {
	f.operations = append(f.operations, "sync")
	return f.syncErr
}

func (f *cliExportFile) Close() error {
	f.operations = append(f.operations, "close")
	return f.closeErr
}

func TestCLIExportFileCompletion(t *testing.T) {
	dataErr := errors.New("export data write failed")
	syncErr := errors.New("file sync failed")
	closeErr := errors.New("file close failed")
	cleanupCause := errors.New("unlock not acknowledged")
	cleanup := &store.CleanupError{Operation: "export", Completed: true, Err: cleanupCause}
	for _, tc := range []struct {
		name       string
		operation  error
		syncErr    error
		closeErr   error
		keep       bool
		operations []string
	}{
		{"success", nil, nil, nil, true, []string{"sync", "close"}},
		{"cleanup_only", cleanup, nil, nil, true, []string{"sync", "close"}},
		{"sync_failure", nil, syncErr, nil, false, []string{"sync", "close"}},
		{"close_failure", nil, nil, closeErr, false, []string{"sync", "close"}},
		{"cleanup_and_sync", cleanup, syncErr, nil, false, []string{"sync", "close"}},
		{"cleanup_and_close", cleanup, nil, closeErr, false, []string{"sync", "close"}},
		{"cleanup_and_both_file_errors", cleanup, syncErr, closeErr, false, []string{"sync", "close"}},
		{"data_failure", dataErr, nil, nil, false, []string{"close"}},
		{"data_and_cleanup", errors.Join(dataErr, &store.CleanupError{Operation: "export", Completed: false, Err: cleanupCause}), nil, nil, false, []string{"close"}},
		{"data_and_close", dataErr, nil, closeErr, false, []string{"close"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &cliExportFile{syncErr: tc.syncErr, closeErr: tc.closeErr}
			keep, err := finishExportFile(f, tc.operation)
			if keep != tc.keep || !reflect.DeepEqual(f.operations, tc.operations) {
				t.Fatalf("bad durable-file decision/order: keep=%v operations=%v err=%v", keep, f.operations, err)
			}
			for _, cause := range []error{tc.operation, tc.syncErr, tc.closeErr} {
				if cause != nil && !errors.Is(err, cause) {
					t.Fatalf("lost cause %v: %v", cause, err)
				}
			}
			if tc.operation == nil && tc.syncErr == nil && tc.closeErr == nil && err != nil {
				t.Fatal(err)
			}
		})
	}
}
