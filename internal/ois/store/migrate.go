package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

//go:embed migrations
var migrationFS embed.FS

// migrationsFS is the source used at runtime; tests replace it to inject
// broken migrations.
var migrationsFS fs.FS = migrationFS

type migration struct {
	version  int
	name     string
	sql      string
	checksum string
}

// loadMigrations returns the embedded migrations of a backend in version
// order. File names are "<version>_<name>.sql".
func loadMigrations(fsys fs.FS, backend string) ([]migration, error) {
	dir := path.Join("migrations", backend)
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		num, name, ok := strings.Cut(strings.TrimSuffix(e.Name(), ".sql"), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 || !strings.HasSuffix(e.Name(), ".sql") {
			return nil, fmt.Errorf("bad migration file name %q", e.Name())
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		out = append(out, migration{version: v, name: name, sql: string(body), checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// schemaOps are the backend-specific pieces of the migration runner.
type schemaOps struct {
	d           *dialect
	backend     string
	tableExists string // query with one placeholder, returns a count
	bootstrap   string // creates schema_migrations
}

type schemaReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (o *schemaOps) hasTable(ctx context.Context, db schemaReader, name string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, o.d.expand(o.tableExists), name).Scan(&n)
	return n > 0, err
}

func (o *schemaOps) applied(ctx context.Context, db schemaReader) (map[int]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var v int
		var sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
	}
	return out, rows.Err()
}

// check compares the database with the embedded migrations (B3 §7).
func (o *schemaOps) check(ctx context.Context, db schemaReader, migs []migration) error {
	ok, err := o.hasTable(ctx, db, "schema_migrations")
	if err != nil {
		return err
	}
	if !ok {
		return ErrMigrationPending
	}
	applied, err := o.applied(ctx, db)
	if err != nil {
		return err
	}
	known := map[int]migration{}
	for _, m := range migs {
		known[m.version] = m
	}
	for v, sum := range applied {
		m, ok := known[v]
		if !ok {
			return fmt.Errorf("%w (version %d)", ErrSchemaTooNew, v)
		}
		if m.checksum != sum {
			return fmt.Errorf("%w (version %d)", ErrMigrationChecksum, v)
		}
	}
	for _, m := range migs {
		if _, ok := applied[m.version]; !ok {
			return fmt.Errorf("%w (version %d)", ErrMigrationPending, m.version)
		}
	}
	return nil
}

// migrate applies each pending migration in its own transaction and seeds
// the meta row together with the first migration.
func (o *schemaOps) migrate(ctx context.Context, db *sql.DB, migs []migration) ([]int, error) {
	if _, err := db.ExecContext(ctx, o.bootstrap); err != nil {
		return nil, err
	}
	if err := o.check(ctx, db, migs); err == nil {
		return nil, nil
	} else if !errors.Is(err, ErrMigrationPending) {
		return nil, err
	}
	applied, err := o.applied(ctx, db)
	if err != nil {
		return nil, err
	}
	var done []int
	for _, m := range migs {
		if _, ok := applied[m.version]; ok {
			continue
		}
		if err := o.applyOne(ctx, db, m); err != nil {
			return done, fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		done = append(done, m.version)
	}
	return done, nil
}

// prepare runs the mode's schema step: check, migrate, or initialize an
// empty database.
func (o *schemaOps) prepare(ctx context.Context, db *sql.DB, migs []migration, mode openMode) ([]int, error) {
	switch mode {
	case modeImport:
		return nil, nil
	case modeMigrate:
		return o.migrate(ctx, db, migs)
	case modeInit:
		if _, err := db.ExecContext(ctx, o.bootstrap); err != nil {
			return nil, err
		}
		applied, err := o.applied(ctx, db)
		if err != nil {
			return nil, err
		}
		if len(applied) == 0 {
			return o.migrate(ctx, db, migs)
		}
	case modeRestore:
		if err := o.check(ctx, db, migs); err != nil && !errors.Is(err, ErrMigrationPending) {
			return nil, err
		}
		return nil, nil
	}
	return nil, o.check(ctx, db, migs)
}

func (o *schemaOps) applyOne(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := o.applyInTx(ctx, tx, m, true); err != nil {
		return err
	}
	return tx.Commit()
}

func (o *schemaOps) applyInTx(ctx context.Context, tx *sql.Tx, m migration, seedMeta bool) error {
	for _, stmt := range splitStatements(m.sql) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if m.version == 1 && seedMeta {
		if _, err := tx.ExecContext(ctx, o.d.expand(`INSERT INTO meta (id, store_id, backend, config_version, epoch, last_run_id, maintenance)
VALUES (1, ?, ?, 0, 0, '', ?)`), protocol.NewID("st_"), o.backend, false); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, o.d.expand("INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)"),
		m.version, m.name, m.checksum, o.d.timeParam(time.Now().UTC().Truncate(time.Microsecond))); err != nil {
		return err
	}
	return nil
}

// splitStatements splits a migration on ";" terminators. Migrations never
// contain semicolons inside literals.
func splitStatements(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
