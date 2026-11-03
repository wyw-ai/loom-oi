package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

// seed creates one row of every kind so corruption tests have targets.
func seed(t *testing.T, s *SQLStore) {
	t.Helper()
	ctx := context.Background()
	revoked := t0
	_, err := s.ApplyConfig(ctx, 0, []Mutation{
		PutAccess{Access{AccessID: "lab-a", Enabled: true, CreatedAt: t0, UpdatedAt: t0}},
		PutToken{Token{TokenID: "0123456789abcdef", Subject: "a", Scope: "consumer", SecretSHA256: []byte{1}, ExpiresAt: t0, RevokedAt: &revoked, CreatedAt: t0}},
		PutPolicy{Policy{PolicyID: "p1", Subject: "a", ExposureSelector: "lab-a/*", Actions: []string{"open"}, Enabled: true}},
	}, []AuditEntry{audit("seed")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertRegistry(ctx, "lab-a", []Exposure{{ExposureID: "lab-a/x", AccessID: "lab-a", Revision: "r", Protocol: "p", TransportProfile: "tcp", AdmissionMode: "best_effort", UpdatedAt: t0}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, "run_1", t0); err != nil {
		t.Fatal(err)
	}
}

func sqliteStore(t *testing.T) (*SQLStore, *sql.DB) {
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")}
	s := migrated(t, cfg)
	seed(t, s)
	raw, err := sql.Open("sqlite", cfg.SQLitePath+"?_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return s, raw
}

func TestCorruptRowsSurfaceErrors(t *testing.T) {
	cases := []string{
		"UPDATE accesses SET spki_pins = 'not json'",
		"UPDATE accesses SET labels = '[1]'",
		"UPDATE accesses SET created_at = 'bad'",
		"UPDATE accesses SET max_exposures = 'many'",
		"UPDATE consumer_tokens SET expires_at = 'bad'",
		"UPDATE consumer_tokens SET revoked_at = 'bad'",
		"UPDATE policies SET actions = '{}'",
		"UPDATE policies SET updated_version = 'v'",
		"UPDATE exposures SET updated_at = 'bad'",
		"UPDATE meta SET last_started_at = 'bad'",
	}
	for _, q := range cases {
		s, raw := sqliteStore(t)
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadSnapshot(context.Background()); err == nil {
			t.Errorf("%s: snapshot accepted corrupt row", q)
		}
	}
	s, raw := sqliteStore(t)
	if _, err := raw.Exec("UPDATE audit SET at = 'bad'"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListAudit(context.Background(), 0, 10); err == nil {
		t.Error("audit accepted corrupt time")
	}
}

func TestMissingTablesFailOperations(t *testing.T) {
	ctx := context.Background()
	s, raw := sqliteStore(t)
	mustExec := func(q string) {
		t.Helper()
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	mustExec("DROP TABLE audit")
	if _, err := s.ApplyConfig(ctx, 1, nil, []AuditEntry{audit("x")}); err == nil {
		t.Error("ApplyConfig without audit table")
	}
	if err := s.UpsertRegistry(ctx, "lab-a", nil, []AuditEntry{audit("x")}); err == nil {
		t.Error("UpsertRegistry without audit table")
	}
	if err := s.AppendAudit(ctx, []AuditEntry{audit("x")}); err == nil {
		t.Error("AppendAudit without audit table")
	}
	if _, err := s.RaiseEpoch(ctx, 50, audit("x")); err == nil {
		t.Error("RaiseEpoch without audit table")
	}
	if _, err := s.ListAudit(ctx, 0, 1); err == nil {
		t.Error("ListAudit without audit table")
	}
	mustExec("DROP TABLE exposures")
	if err := s.UpsertRegistry(ctx, "lab-a", nil, nil); err == nil {
		t.Error("UpsertRegistry without exposures table")
	}
	if _, err := s.LoadSnapshot(ctx); err == nil {
		t.Error("LoadSnapshot without exposures table")
	}
	mustExec("DROP TABLE meta")
	if _, err := s.ApplyConfig(ctx, 1, nil, nil); err == nil {
		t.Error("ApplyConfig without meta")
	}
	if _, err := s.StartRun(ctx, "r", t0); err == nil {
		t.Error("StartRun without meta")
	}
	if _, err := s.RaiseEpoch(ctx, 1, audit("x")); err == nil {
		t.Error("RaiseEpoch without meta")
	}
	if err := s.AppendAudit(ctx, nil); err == nil {
		t.Error("AppendAudit without meta")
	}
	if err := s.UpsertRegistry(ctx, "lab-a", nil, nil); err == nil {
		t.Error("UpsertRegistry without meta")
	}
	if err := s.Health(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Health without meta: %v", err)
	}
}

func TestPartialSchemaChecks(t *testing.T) {
	ctx := context.Background()
	s, raw := sqliteStore(t)
	_ = s.Close()
	if _, err := raw.Exec("ALTER TABLE schema_migrations RENAME COLUMN checksum TO sum"); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Driver: SQLite, SQLitePath: filepath.Join(filepath.Dir(rawPath(t, raw)), "ois.db")}
	if _, err := Open(ctx, cfg, testBusy); err == nil {
		t.Fatal("broken schema_migrations accepted")
	}
	if _, _, err := Migrate(ctx, cfg, testBusy); err == nil {
		t.Fatal("migrate over broken schema_migrations accepted")
	}
}

func rawPath(t *testing.T, raw *sql.DB) string {
	var seq int
	var name, file string
	if err := raw.QueryRow("PRAGMA database_list").Scan(&seq, &name, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

func withMigrations(t *testing.T, fsys fstest.MapFS) {
	old := migrationsFS
	migrationsFS = fsys
	t.Cleanup(func() { migrationsFS = old })
}

func TestBrokenMigrations(t *testing.T) {
	ctx := context.Background()
	newCfg := func() Config { return Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "ois.db")} }
	withMigrations(t, fstest.MapFS{"migrations/sqlite/0001_init.sql": {Data: []byte("CREATE TABLE nope (;")}})
	if _, _, err := Migrate(ctx, newCfg(), testBusy); err == nil {
		t.Error("bad SQL migrated")
	}
	// A first migration that does not create meta cannot be seeded.
	withMigrations(t, fstest.MapFS{"migrations/sqlite/0001_init.sql": {Data: []byte("CREATE TABLE other (x INTEGER);")}})
	if _, _, err := Migrate(ctx, newCfg(), testBusy); err == nil {
		t.Error("migration without meta accepted")
	}
	withMigrations(t, fstest.MapFS{})
	if _, _, err := Migrate(ctx, newCfg(), testBusy); err == nil {
		t.Error("missing migrations accepted")
	}
	if _, err := Open(ctx, newCfg(), testBusy); err == nil {
		t.Error("open without migrations accepted")
	}
}

func TestOpenSQLiteFailures(t *testing.T) {
	ctx := context.Background()
	if _, err := Open(ctx, Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "missing", "ois.db")}, testBusy); err == nil {
		t.Error("lock in missing directory accepted")
	}
	if err := prepareFreshSQLite(ctx, "bad\x00path"); err == nil {
		t.Error("invalid path accepted")
	}
	if err := prepareFreshSQLite(ctx, t.TempDir()); err == nil {
		t.Error("directory accepted as database")
	}
	if _, _, err := Migrate(ctx, Config{Driver: SQLite, SQLitePath: filepath.Join(t.TempDir(), "q?.db")}, testBusy); err == nil {
		t.Error("path with ? accepted")
	}
	// Mutation markers exist only to close the Mutation interface.
	for _, m := range []Mutation{PutAccess{}, DeleteAccess{}, PutToken{}, RevokeToken{}, PutPolicy{}, DeletePolicy{}} {
		m.mutation()
	}
	_ = time.Now()
}
