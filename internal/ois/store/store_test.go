package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const testBusy = 200 * time.Millisecond

type backend struct {
	name string
	cfg  func(t *testing.T) Config
}

// backends returns SQLite always and PostgreSQL when OI_TEST_PG_DSN points
// at a server where the test may create and drop databases.
func backends(t *testing.T) []backend {
	b := []backend{{SQLite, func(t *testing.T) Config {
		return Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	}}}
	if os.Getenv("OI_TEST_PG_DSN") != "" {
		b = append(b, backend{Postgres, newPGDatabase})
	} else {
		t.Log("OI_TEST_PG_DSN not set: PostgreSQL cases skipped")
	}
	return b
}

func adminDB(t *testing.T) *sql.DB {
	db, err := sql.Open("pgx", os.Getenv("OI_TEST_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func newPGDatabase(t *testing.T) Config {
	admin := adminDB(t)
	name := "oi_t_" + strings.ReplaceAll(protocol.NewID(""), "-", "")
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		_ = admin.Close()
	})
	u, err := url.Parse(os.Getenv("OI_TEST_PG_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return Config{Driver: Postgres, PostgresDSN: u.String()}
}

func migrated(t *testing.T, cfg Config) *SQLStore {
	t.Helper()
	s, applied, err := Migrate(context.Background(), cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(applied, []int{1}) {
		t.Fatalf("applied %v", applied)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func forEach(t *testing.T, fn func(t *testing.T, cfg Config, s *SQLStore)) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			cfg := b.cfg(t)
			fn(t, cfg, migrated(t, cfg))
		})
	}
}

var t0 = time.Date(2026, 11, 4, 1, 2, 3, 456789000, time.UTC)

func audit(action string) AuditEntry {
	return AuditEntry{At: t0, Actor: "admin:root", Action: action, ObjectType: "test", ObjectID: "x", Detail: json.RawMessage(`{"k":"v"}`)}
}

func TestMigrateAndEmptySnapshot(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		snap, err := s.LoadSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		m := snap.Meta
		if m.Backend != cfg.Driver || m.ConfigVersion != 0 || m.Epoch != 0 || !strings.HasPrefix(m.StoreID, "st_") || m.Maintenance || m.LastStartedAt != nil {
			t.Fatalf("meta %+v", m)
		}
		if len(snap.Accesses)+len(snap.Tokens)+len(snap.Policies)+len(snap.Exposures) != 0 {
			t.Fatalf("not empty: %+v", snap)
		}
		if s.Backend() != cfg.Driver || s.EngineVersion() == "" {
			t.Fatal("backend info")
		}
		// Evidence runs record the engine each contract run used (B3 §2/§5).
		t.Logf("ENGINE %s %s", s.Backend(), s.EngineVersion())
		if err := s.Health(ctx); err != nil {
			t.Fatal(err)
		}
		_ = s.Close()
		// Re-running migrate is a no-op.
		s2, applied, err := Migrate(ctx, cfg, testBusy)
		if err != nil || len(applied) != 0 {
			t.Fatalf("second migrate: %v %v", applied, err)
		}
		_ = s2.Close()
	})
}

func TestApplyConfigCRUDAndAudit(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		acc := Access{AccessID: "lab-a", Enabled: true, SPKIPins: []string{"pin1"}, MaxExposures: 128, Labels: map[string]string{"env": "lab"}, CreatedAt: t0, UpdatedAt: t0}
		revoked := t0.Add(time.Hour)
		tok := Token{TokenID: "0123456789abcdef", Subject: "alice", Scope: protocol.ScopeConsumer, SecretSHA256: []byte{1, 2, 3}, ExpiresAt: t0.Add(24 * time.Hour), CreatedAt: t0}
		pol := Policy{PolicyID: "p1", Subject: "alice", ExposureSelector: "lab-a/*", Actions: []string{"discover", "open"}, Enabled: true}
		v, err := s.ApplyConfig(ctx, 0, []Mutation{PutAccess{acc}, PutToken{tok}, PutPolicy{pol}}, []AuditEntry{audit("create"), audit("create2")})
		if err != nil || v != 1 {
			t.Fatalf("apply: %d %v", v, err)
		}
		if err := s.UpsertRegistry(ctx, "lab-a", []Exposure{{ExposureID: "lab-a/http", AccessID: "lab-a", Revision: "rv_x", Protocol: "http", TransportProfile: "tcp", AdmissionMode: "best_effort", Description: "d", UpdatedAt: t0}}, nil); err != nil {
			t.Fatal(err)
		}
		snap, err := s.LoadSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		pol.UpdatedVersion = 1
		want := Snapshot{Accesses: []Access{acc}, Tokens: []Token{tok}, Policies: []Policy{pol}}
		if !reflect.DeepEqual(snap.Accesses, want.Accesses) || !reflect.DeepEqual(snap.Tokens, want.Tokens) || !reflect.DeepEqual(snap.Policies, want.Policies) {
			t.Fatalf("snapshot mismatch:\n%+v\n%+v", snap, want)
		}
		if snap.Meta.ConfigVersion != 1 || len(snap.Exposures) != 1 || snap.Exposures[0].UpdatedAt != t0 {
			t.Fatalf("meta/exposures %+v", snap)
		}
		// Update, revoke, delete; deleting the Access cascades its registry.
		acc.Enabled, acc.SPKIPins, acc.UpdatedAt = false, []string{"pin1", "pin2"}, t0.Add(time.Minute)
		v, err = s.ApplyConfig(ctx, 1, []Mutation{PutAccess{acc}, RevokeToken{TokenID: tok.TokenID, At: revoked}, DeletePolicy{PolicyID: "p1"}}, []AuditEntry{audit("update")})
		if err != nil || v != 2 {
			t.Fatalf("update: %d %v", v, err)
		}
		snap, _ = s.LoadSnapshot(ctx)
		if snap.Accesses[0].Enabled || len(snap.Accesses[0].SPKIPins) != 2 || snap.Tokens[0].RevokedAt == nil || !snap.Tokens[0].RevokedAt.Equal(revoked) || len(snap.Policies) != 0 {
			t.Fatalf("after update %+v", snap)
		}
		if _, err := s.ApplyConfig(ctx, 2, []Mutation{DeleteAccess{AccessID: "lab-a"}}, nil); err != nil {
			t.Fatal(err)
		}
		snap, _ = s.LoadSnapshot(ctx)
		if len(snap.Accesses) != 0 || len(snap.Exposures) != 0 {
			t.Fatalf("cascade failed %+v", snap)
		}
		// Nil slices/maps are stored as empty values.
		if _, err := s.ApplyConfig(ctx, 3, []Mutation{PutAccess{Access{AccessID: "lab-b", CreatedAt: t0, UpdatedAt: t0}}}, nil); err != nil {
			t.Fatal(err)
		}
		snap, _ = s.LoadSnapshot(ctx)
		if snap.Accesses[0].SPKIPins == nil || snap.Accesses[0].Labels == nil {
			t.Fatalf("nil fields not normalized: %+v", snap.Accesses[0])
		}
		entries, err := s.ListAudit(ctx, 0, 10)
		if err != nil || len(entries) != 3 {
			t.Fatalf("audit %d %v", len(entries), err)
		}
		if entries[0].ConfigVersion != 1 || entries[2].ConfigVersion != 2 || entries[0].Action != "create" || entries[0].At != t0 {
			t.Fatalf("audit content %+v", entries)
		}
		var d map[string]string
		if err := json.Unmarshal(entries[0].Detail, &d); err != nil || d["k"] != "v" {
			t.Fatalf("detail %s", entries[0].Detail)
		}
		page, _ := s.ListAudit(ctx, entries[1].Seq, 10)
		if len(page) != 1 || page[0].Seq != entries[2].Seq {
			t.Fatalf("paging %+v", page)
		}
	})
}

func TestConcurrentApplyConfigConflict(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i := range errs {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = s.ApplyConfig(ctx, 0, []Mutation{PutPolicy{Policy{PolicyID: fmt.Sprintf("p%d", i), Subject: "s", ExposureSelector: "a/b", Actions: []string{"open"}}}}, []AuditEntry{audit("race")})
			}(i)
		}
		wg.Wait()
		var ok, conflicts int
		for _, err := range errs {
			var ce *ConflictError
			switch {
			case err == nil:
				ok++
			case errors.As(err, &ce) && ce.Current == 1:
				conflicts++
			case errors.Is(err, ErrBusy):
				conflicts++ // SQLite may also refuse the loser while the winner commits
			default:
				t.Fatalf("unexpected %v", err)
			}
		}
		if ok != 1 || conflicts != 1 {
			t.Fatalf("ok=%d conflicts=%d", ok, conflicts)
		}
		entries, _ := s.ListAudit(ctx, 0, 10)
		if len(entries) != 1 {
			t.Fatalf("audit rows %d", len(entries))
		}
		_, err := s.ApplyConfig(ctx, 0, nil, nil)
		var ce *ConflictError
		if !errors.As(err, &ce) || ce.Current != 1 || !strings.Contains(err.Error(), "current version is 1") {
			t.Fatalf("stale write: %v", err)
		}
	})
}

func TestAuditFailureRollsBackConfig(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		s.hookMu.Lock()
		s.beforeAudit = func() error { return errors.New("injected before audit") }
		s.hookMu.Unlock()
		_, err := s.ApplyConfig(ctx, 0, []Mutation{PutAccess{Access{AccessID: "lab-a", CreatedAt: t0, UpdatedAt: t0}}}, []AuditEntry{audit("x")})
		if err == nil {
			t.Fatal("injected failure ignored")
		}
		s.hookMu.Lock()
		s.beforeAudit = nil
		s.hookMu.Unlock()
		snap, _ := s.LoadSnapshot(ctx)
		entries, _ := s.ListAudit(ctx, 0, 10)
		if len(snap.Accesses) != 0 || snap.Meta.ConfigVersion != 0 || len(entries) != 0 {
			t.Fatalf("partial write survived: %+v %v", snap, entries)
		}
	})
}

func TestInvalidMutations(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		tok := Token{TokenID: "0123456789abcdef", Subject: "a", Scope: "consumer", SecretSHA256: []byte{1}, ExpiresAt: t0, CreatedAt: t0}
		if _, err := s.ApplyConfig(ctx, 0, []Mutation{PutToken{tok}}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ApplyConfig(ctx, 1, []Mutation{PutToken{tok}}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("duplicate token: %v", err)
		}
		bad := tok
		bad.TokenID, bad.Scope = "fedcba9876543210", "root"
		if _, err := s.ApplyConfig(ctx, 1, []Mutation{PutToken{bad}}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("check constraint: %v", err)
		}
		if _, err := s.ApplyConfig(ctx, 1, []Mutation{nil}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("nil mutation: %v", err)
		}
		e := Exposure{ExposureID: "ghost/http", AccessID: "ghost", Revision: "r", Protocol: "p", TransportProfile: "tcp", AdmissionMode: "best_effort", UpdatedAt: t0}
		if err := s.UpsertRegistry(ctx, "ghost", []Exposure{e}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("registry FK: %v", err)
		}
		if err := s.UpsertRegistry(ctx, "other", []Exposure{e}, nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("registry namespace: %v", err)
		}
	})
}

func TestRegistryAndEpoch(t *testing.T) {
	forEach(t, func(t *testing.T, cfg Config, s *SQLStore) {
		ctx := context.Background()
		if _, err := s.ApplyConfig(ctx, 0, []Mutation{PutAccess{Access{AccessID: "lab-a", CreatedAt: t0, UpdatedAt: t0}}}, nil); err != nil {
			t.Fatal(err)
		}
		mk := func(name, rev string) Exposure {
			return Exposure{ExposureID: "lab-a/" + name, AccessID: "lab-a", Revision: rev, Protocol: "http", TransportProfile: "tcp", AdmissionMode: "best_effort", UpdatedAt: t0}
		}
		if err := s.UpsertRegistry(ctx, "lab-a", []Exposure{mk("a", "r1"), mk("b", "r1")}, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertRegistry(ctx, "lab-a", []Exposure{mk("b", "r2")}, []AuditEntry{audit("revision_changed")}); err != nil {
			t.Fatal(err)
		}
		snap, _ := s.LoadSnapshot(ctx)
		if len(snap.Exposures) != 1 || snap.Exposures[0].Revision != "r2" {
			t.Fatalf("registry %+v", snap.Exposures)
		}
		e1, err := s.StartRun(ctx, "run_1", t0)
		e2, err2 := s.StartRun(ctx, "run_2", t0.Add(time.Second))
		if err != nil || err2 != nil || e1 != 1 || e2 != 2 {
			t.Fatalf("epochs %d %d %v %v", e1, e2, err, err2)
		}
		raised, err := s.RaiseEpoch(ctx, 7, audit("epoch_rollback_detected"))
		if err != nil || !raised {
			t.Fatalf("raise %v %v", raised, err)
		}
		raised, err = s.RaiseEpoch(ctx, 8, AuditEntry{})
		if err != nil || !raised {
			t.Fatalf("silent raise %v %v", raised, err)
		}
		raised, err = s.RaiseEpoch(ctx, 3, audit("noop"))
		if err != nil || raised {
			t.Fatalf("raise lower %v %v", raised, err)
		}
		if err := s.AppendAudit(ctx, []AuditEntry{audit("runtime")}); err != nil {
			t.Fatal(err)
		}
		snap, _ = s.LoadSnapshot(ctx)
		if snap.Meta.Epoch != 8 || snap.Meta.LastRunID != "run_2" || snap.Meta.LastStartedAt == nil {
			t.Fatalf("meta %+v", snap.Meta)
		}
		entries, _ := s.ListAudit(ctx, 0, 10)
		var actions []string
		for _, e := range entries {
			actions = append(actions, e.Action)
		}
		if strings.Join(actions, ",") != "revision_changed,epoch_rollback_detected,runtime" {
			t.Fatalf("audit actions %v", actions)
		}
	})
}

func rawDB(t *testing.T, cfg Config) *sql.DB {
	t.Helper()
	driver, dsn := "sqlite", cfg.SQLitePath
	if cfg.Driver == Postgres {
		driver, dsn = "pgx", cfg.PostgresDSN
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSchemaVersionChecks(t *testing.T) {
	cases := []struct {
		name string
		sql  func(backend string) string
		want error
	}{
		{"checksum", func(string) string { return "UPDATE schema_migrations SET checksum = 'x'" }, ErrMigrationChecksum},
		{"too new", func(b string) string {
			return "INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (99, 'future', 'x', " + nowLiteral(b) + ")"
		}, ErrSchemaTooNew},
		{"pending", func(string) string { return "DELETE FROM schema_migrations WHERE version = 1" }, ErrMigrationPending},
	}
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			fresh := b.cfg(t)
			if _, err := Open(ctx, fresh, testBusy); !errors.Is(err, ErrMigrationPending) {
				t.Fatalf("fresh database: %v", err)
			}
			for _, c := range cases {
				cfg := b.cfg(t)
				s := migrated(t, cfg)
				_ = s.Close()
				db := rawDB(t, cfg)
				if _, err := db.Exec(c.sql(cfg.Driver)); err != nil {
					t.Fatal(err)
				}
				_ = db.Close()
				if _, err := Open(ctx, cfg, testBusy); !errors.Is(err, c.want) {
					t.Fatalf("%s: Open got %v want %v", c.name, err, c.want)
				}
				if c.want != ErrMigrationPending {
					if _, _, err := Migrate(ctx, cfg, testBusy); !errors.Is(err, c.want) {
						t.Fatalf("%s: Migrate got %v want %v", c.name, err, c.want)
					}
				}
			}
		})
	}
}

// Initialize creates an empty database but never upgrades one that has
// applied migrations: upgrades stay behind `ois migrate` (B3 §7).
func TestInitialize(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := b.cfg(t)
			s, applied, err := Initialize(ctx, cfg, testBusy)
			if err != nil || !reflect.DeepEqual(applied, []int{1}) {
				t.Fatalf("fresh: %v %v", applied, err)
			}
			_ = s.Close()
			s, applied, err = Initialize(ctx, cfg, testBusy)
			if err != nil || len(applied) != 0 {
				t.Fatalf("initialized: %v %v", applied, err)
			}
			_ = s.Close()
			fsys := fstest.MapFS{}
			for _, be := range []string{SQLite, Postgres} {
				dir := "migrations/" + be
				entries, err := fs.ReadDir(migrationFS, dir)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range entries {
					data, _ := fs.ReadFile(migrationFS, dir+"/"+e.Name())
					fsys[dir+"/"+e.Name()] = &fstest.MapFile{Data: data}
				}
				fsys[dir+"/0002_extra.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE extra (x INTEGER);")}
			}
			withMigrations(t, fsys)
			if _, _, err := Initialize(ctx, cfg, testBusy); !errors.Is(err, ErrMigrationPending) {
				t.Fatalf("pending upgrade: %v", err)
			}
		})
	}
}
func nowLiteral(backend string) string {
	if backend == Postgres {
		return "now()"
	}
	return "'2026-11-04T00:00:00Z'"
}

func TestSingleInstanceLock(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			ctx := context.Background()
			cfg := b.cfg(t)
			s := migrated(t, cfg)
			if _, err := Open(ctx, cfg, testBusy); !errors.Is(err, ErrAnotherInstance) {
				t.Fatalf("second instance: %v", err)
			}
			if _, _, err := Migrate(ctx, cfg, testBusy); !errors.Is(err, ErrAnotherInstance) {
				t.Fatalf("migrate while serving: %v", err)
			}
			_ = s.Close()
			s2, err := Open(ctx, cfg, testBusy)
			if err != nil {
				t.Fatalf("reopen after close: %v", err)
			}
			_ = s2.Close()
		})
	}
}

func TestPostgresLockLossFailsClosed(t *testing.T) {
	if os.Getenv("OI_TEST_PG_DSN") == "" {
		t.Skip("OI_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	cfg := newPGDatabase(t)
	s := migrated(t, cfg)
	kill := func() {
		t.Helper()
		admin, err := sql.Open("pgx", cfg.PostgresDSN)
		if err != nil {
			t.Fatal(err)
		}
		defer admin.Close()
		if _, err := admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_locks
WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`); err != nil {
			t.Fatal(err)
		}
	}
	kill()
	if _, err := s.ApplyConfig(ctx, 0, nil, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("write without lock: %v", err)
	}
	if err := s.Health(ctx); err != nil {
		t.Fatalf("health should reacquire: %v", err)
	}
	if _, err := s.ApplyConfig(ctx, 0, nil, nil); err != nil {
		t.Fatalf("write after reacquire: %v", err)
	}
	// A second instance that grabs the lock in the gap keeps us unavailable.
	kill()
	other, err := Open(ctx, cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Health(ctx); !errors.Is(err, ErrAnotherInstance) {
		t.Fatalf("lock taken by other: %v", err)
	}
	_ = other.Close()
}

func TestPostgresRefusesAsyncCommit(t *testing.T) {
	if os.Getenv("OI_TEST_PG_DSN") == "" {
		t.Skip("OI_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	cfg := newPGDatabase(t)
	s := migrated(t, cfg)
	_ = s.Close()
	admin := adminDB(t)
	defer admin.Close()
	u, _ := url.Parse(cfg.PostgresDSN)
	if _, err := admin.Exec("ALTER DATABASE " + strings.TrimPrefix(u.Path, "/") + " SET synchronous_commit = off"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, cfg, testBusy); !errors.Is(err, ErrSettings) {
		t.Fatalf("synchronous_commit=off accepted: %v", err)
	}
	bad := Config{Driver: Postgres, PostgresDSN: "postgres://127.0.0.1:1/x?connect_timeout=1"}
	if _, err := Open(ctx, bad, testBusy); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable server: %v", err)
	}
}

func TestSQLiteBusyAndUnavailable(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	s := migrated(t, cfg)
	other, err := sql.Open("sqlite", cfg.SQLitePath+"?_pragma=busy_timeout(0)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyConfig(ctx, 0, nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("held write lock: %v", err)
	}
	_ = tx.Rollback()
	if _, err := s.ApplyConfig(ctx, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	_ = s.rdb.Close()
	_ = s.wdb.Close()
	if _, err := s.ApplyConfig(ctx, 1, nil, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed db write: %v", err)
	}
	if err := s.Health(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed db health: %v", err)
	}
	if _, err := s.LoadSnapshot(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed db snapshot: %v", err)
	}
	if _, err := s.ListAudit(ctx, 0, 1); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed db audit: %v", err)
	}
}

func TestSQLiteSettingsReadback(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// A file that already holds a table cannot switch auto_vacuum, so the
	// migrated store must be refused (TV-B3-08).
	path := filepath.Join(dir, "preexisting.db")
	pre, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pre.Exec("CREATE TABLE junk (x INTEGER)"); err != nil {
		t.Fatal(err)
	}
	_ = pre.Close()
	if _, _, err := Migrate(ctx, Config{Driver: SQLite, SQLitePath: path}, testBusy); !errors.Is(err, ErrSettings) {
		t.Fatalf("auto_vacuum readback: %v", err)
	}
	cases := []struct {
		name  string
		dsn   string
		write bool
		want  string
	}{
		{"journal", "j.db?_pragma=busy_timeout(200)&_pragma=foreign_keys(1)", false, "journal_mode"},
		{"sync", "s.db?_pragma=busy_timeout(200)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", true, "synchronous"},
		{"fk", "f.db?_pragma=busy_timeout(200)&_pragma=journal_mode(WAL)", false, "foreign_keys"},
		{"busy", "b.db?_pragma=busy_timeout(99)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", false, "busy_timeout"},
		{"vacuum", "v.db?_pragma=busy_timeout(200)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", false, "auto_vacuum"},
	}
	for _, c := range cases {
		db, err := sql.Open("sqlite", filepath.Join(dir, c.dsn))
		if err != nil {
			t.Fatal(err)
		}
		err = readbackSQLite(ctx, db, testBusy, c.write)
		_ = db.Close()
		if !errors.Is(err, ErrSettings) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	db, _ := sql.Open("sqlite", filepath.Join(dir, "ok.db"))
	requiredSQLiteVersion = "0.0.0"
	err = readbackSQLite(ctx, db, testBusy, false)
	requiredSQLiteVersion = SQLiteEngineVersion
	_ = db.Close()
	if !errors.Is(err, ErrSettings) || !strings.Contains(err.Error(), "sqlite_version") {
		t.Fatalf("engine pin: %v", err)
	}
	closed, _ := sql.Open("sqlite", filepath.Join(dir, "closed.db"))
	_ = closed.Close()
	if err := readbackSQLite(ctx, closed, testBusy, false); err == nil {
		t.Fatal("closed pool accepted")
	}
	if _, _, err := openSQLitePools("bad?.db", testBusy); err == nil {
		t.Fatal("path with ? accepted")
	}
}

func TestConfigValidate(t *testing.T) {
	good := []Config{{Driver: SQLite, SQLitePath: "x.db"}, {Driver: Postgres, PostgresDSN: "postgres://x"}}
	for _, c := range good {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := []Config{{}, {Driver: "mysql"}, {Driver: SQLite}, {Driver: SQLite, SQLitePath: "x", PostgresDSN: "y"}, {Driver: Postgres}, {Driver: Postgres, PostgresDSN: "y", SQLitePath: "x"}}
	for _, c := range bad {
		if err := c.Validate(); err == nil {
			t.Errorf("%+v accepted", c)
		}
		if _, err := Open(context.Background(), c, testBusy); err == nil {
			t.Errorf("Open accepted %+v", c)
		}
	}
}

func TestLoadMigrationsRejectsBadNames(t *testing.T) {
	for _, name := range []string{"x.sql", "0_init.sql", "0001init.sql", "0001_init.txt"} {
		fsys := fstest.MapFS{"migrations/sqlite/" + name: {Data: []byte("SELECT 1;")}}
		if _, err := loadMigrations(fsys, SQLite); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := loadMigrations(fstest.MapFS{}, SQLite); err == nil {
		t.Error("missing directory accepted")
	}
	fsys := fstest.MapFS{"migrations/sqlite/0002_b.sql": {Data: []byte("B;")}, "migrations/sqlite/0001_a.sql": {Data: []byte("A;")}}
	migs, err := loadMigrations(fsys, SQLite)
	if err != nil || migs[0].version != 1 || migs[1].name != "b" {
		t.Fatalf("%+v %v", migs, err)
	}
	if got := splitStatements(" a ;\n; b;"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("split %q", got)
	}
}

func TestDialectExpand(t *testing.T) {
	pg := postgresDialect()
	got := pg.expand("SELECT {JO:labels} FROM t WHERE a = ? AND b = {J}")
	if got != "SELECT labels::text FROM t WHERE a = $1 AND b = CAST($2 AS jsonb)" {
		t.Fatal(got)
	}
	if sqliteDialect().expand("x = ? AND y = {J} {JO:z}") != "x = ? AND y = ? z" {
		t.Fatal("sqlite expand")
	}
}

func TestTimeScanning(t *testing.T) {
	var n nullTime
	for _, v := range []any{t0, t0.Format(time.RFC3339Nano), []byte(t0.Format(time.RFC3339Nano))} {
		if err := n.Scan(v); err != nil || !n.t.Equal(t0) || n.ptr() == nil {
			t.Fatalf("%T: %v", v, err)
		}
	}
	if err := n.Scan(nil); err != nil || n.ptr() != nil {
		t.Fatal("NULL")
	}
	if err := n.Scan(42); err == nil {
		t.Fatal("int accepted")
	}
	if err := n.Scan("not a time"); err == nil {
		t.Fatal("bad text accepted")
	}
	var d dbTime
	if err := d.Scan(nil); err == nil {
		t.Fatal("NULL into non-null time accepted")
	}
	if err := d.Scan(42); err == nil {
		t.Fatal("int into time accepted")
	}
}

// blockingStore lets tests hold the serialized writer busy.
type blockingStore struct {
	Store
	release chan struct{}
	started chan struct{}
}

func (b *blockingStore) ApplyConfig(ctx context.Context, e int64, m []Mutation, a []AuditEntry) (int64, error) {
	b.started <- struct{}{}
	<-b.release
	return e + 1, nil
}
func (b *blockingStore) UpsertRegistry(context.Context, string, []Exposure, []AuditEntry) error {
	return nil
}
func (b *blockingStore) StartRun(context.Context, string, time.Time) (int64, error) { return 9, nil }
func (b *blockingStore) RaiseEpoch(context.Context, int64, AuditEntry) (bool, error) {
	return true, nil
}
func (b *blockingStore) AppendAudit(context.Context, []AuditEntry) error { return nil }
func (b *blockingStore) Close() error                                    { return nil }

func TestSerializedQueueAndClose(t *testing.T) {
	ctx := context.Background()
	b := &blockingStore{release: make(chan struct{}), started: make(chan struct{}, 4)}
	w := NewSerialized(b, 1)
	results := make(chan error, 2)
	go func() { _, err := w.ApplyConfig(ctx, 1, nil, nil); results <- err }()
	<-b.started // first job running
	go func() { _, err := w.ApplyConfig(ctx, 2, nil, nil); results <- err }()
	deadline := time.Now().Add(2 * time.Second)
	for len(w.jobs) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := w.ApplyConfig(ctx, 3, nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue: %v", err)
	}
	if err := w.UpsertRegistry(ctx, "a", nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue registry: %v", err)
	}
	if _, err := w.StartRun(ctx, "r", t0); !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue run: %v", err)
	}
	if _, err := w.RaiseEpoch(ctx, 1, AuditEntry{}); !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue epoch: %v", err)
	}
	if err := w.AppendAudit(ctx, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("full queue audit: %v", err)
	}
	b.release <- struct{}{}
	<-b.started
	b.release <- struct{}{}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := w.UpsertRegistry(ctx, "a", nil, nil); err != nil {
		t.Fatal(err)
	}
	if e, err := w.StartRun(ctx, "r", t0); err != nil || e != 9 {
		t.Fatal(e, err)
	}
	if ok, err := w.RaiseEpoch(ctx, 1, AuditEntry{}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := w.AppendAudit(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// A job still queued when Close stops the loop reports Unavailable.
	go func() { _, err := w.ApplyConfig(ctx, 4, nil, nil); results <- err }()
	<-b.started
	go func() { _, err := w.ApplyConfig(ctx, 5, nil, nil); results <- err }()
	for len(w.jobs) == 0 {
		time.Sleep(time.Millisecond)
	}
	closed := make(chan error)
	go func() { closed <- w.Close() }()
	time.Sleep(20 * time.Millisecond)
	b.release <- struct{}{}
	got := []error{<-results, <-results}
	if (got[0] == nil) == (got[1] == nil) {
		t.Fatalf("one job must finish and one must be dropped: %v", got)
	}
	for _, err := range got {
		if err != nil && !errors.Is(err, ErrUnavailable) {
			t.Fatalf("dropped job: %v", err)
		}
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal("second close")
	}
}

func TestSerializedRealStore(t *testing.T) {
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	s, _, err := Migrate(context.Background(), cfg, testBusy)
	if err != nil {
		t.Fatal(err)
	}
	w := NewSerialized(s, 4)
	defer w.Close()
	if v, err := w.ApplyConfig(context.Background(), 0, nil, []AuditEntry{audit("x")}); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	if _, err := w.LoadSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
}
