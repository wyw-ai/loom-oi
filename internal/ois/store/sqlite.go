package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// SQLiteEngineVersion is the SQLite engine embedded in the pinned
// modernc.org/sqlite v1.61.1 driver (B4 §2). OIS refuses to start on any
// other engine so the WAL-reset fix and readback assumptions hold (B3 §5).
const SQLiteEngineVersion = "3.54.1"

// requiredSQLiteVersion is a variable only so tests can exercise the
// mismatch path; production never changes it.
var requiredSQLiteVersion = SQLiteEngineVersion

const (
	sqliteBusy       = 5
	sqliteLocked     = 6
	sqliteReadOnly   = 8
	sqliteIOErr      = 10
	sqliteFull       = 13
	sqliteCantOpen   = 14
	sqliteConstraint = 19
)

func sqliteFailureKind(err error) string {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case sqliteFull:
			return FailureDiskFull
		case sqliteReadOnly:
			return FailureReadOnly
		case sqliteIOErr, sqliteCantOpen:
			return FailureIO
		}
	}
	return FailureUnavailable
}

func sqliteDialect() *dialect {
	return &dialect{
		name:       SQLite,
		jsonIn:     "?",
		jsonOutFmt: "%s",
		isBusy: func(err error) bool {
			var se *sqlite.Error
			return errors.As(err, &se) && (se.Code()&0xff == sqliteBusy || se.Code()&0xff == sqliteLocked)
		},
		isInvalid: func(err error) bool {
			var se *sqlite.Error
			return errors.As(err, &se) && se.Code()&0xff == sqliteConstraint
		},
		timeParam:   func(t time.Time) any { return t.Format(time.RFC3339Nano) },
		failureKind: sqliteFailureKind,
	}
}

func sqliteOps() *schemaOps {
	d := sqliteDialect()
	return &schemaOps{
		d:           d,
		backend:     SQLite,
		tableExists: "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		bootstrap:   sqliteBootstrap,
	}
}

const sqliteBootstrap = `CREATE TABLE IF NOT EXISTS schema_migrations (
version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL)`

// sqliteFile reports whether path holds a database, i.e. a non-empty
// regular file. Anything else that exists is refused on every OS: a
// directory, for one, reports a non-zero size on Linux.
func sqliteFile(path string) (bool, error) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("sqlite_path %s is not a regular file", path)
	}
	return info.Size() > 0, nil
}

// prepareFreshSQLite chooses auto_vacuum before anything writes page 1:
// switching to WAL already fixes the header, after which SQLite silently
// ignores auto_vacuum changes (B3 §5).
func prepareFreshSQLite(ctx context.Context, path string) error {
	if exists, err := sqliteFile(path); err != nil || exists {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	// VACUUM materializes the header without committing any schema. This
	// also lets an import create every table in its own atomic transaction.
	for _, q := range []string{"PRAGMA auto_vacuum = INCREMENTAL", "VACUUM"} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// sqliteDSN builds the modernc DSN. Every connection gets the same
// per-connection pragmas; the write pool also takes BEGIN IMMEDIATE.
func sqliteDSN(path string, busy time.Duration, write bool) string {
	q := fmt.Sprintf("_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)", busy.Milliseconds())
	if write {
		q += "&_pragma=journal_mode(WAL)&_txlock=immediate"
	}
	return path + "?" + q
}

func openSQLitePools(path string, busy time.Duration) (w, r *sql.DB, err error) {
	if strings.ContainsAny(path, "?#") {
		return nil, nil, fmt.Errorf("sqlite_path must not contain '?' or '#'")
	}
	w, err = sql.Open("sqlite", sqliteDSN(path, busy, true))
	if err != nil {
		return nil, nil, err
	}
	w.SetMaxOpenConns(1)
	r, err = sql.Open("sqlite", sqliteDSN(path, busy, false))
	if err != nil {
		_ = w.Close()
		return nil, nil, err
	}
	r.SetMaxOpenConns(4)
	// One idle reader is kept: connections opened for a burst close when
	// it ends, so descriptors return to their baseline (B4 §4).
	r.SetMaxIdleConns(1)
	return w, r, nil
}

// readbackSQLite verifies the B3 §5 settings on one connection of db.
func readbackSQLite(ctx context.Context, db *sql.DB, busy time.Duration, write bool) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var journal, version string
	var syncMode, fk, busyMS, autoVacuum int64
	for q, dst := range map[string]any{
		"PRAGMA journal_mode": &journal, "PRAGMA synchronous": &syncMode, "PRAGMA foreign_keys": &fk,
		"PRAGMA busy_timeout": &busyMS, "PRAGMA auto_vacuum": &autoVacuum, "SELECT sqlite_version()": &version,
	} {
		if err := conn.QueryRowContext(ctx, q).Scan(dst); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrSettings, q, err)
		}
	}
	switch {
	case version != requiredSQLiteVersion:
		return fmt.Errorf("%w: sqlite_version()=%s, want %s", ErrSettings, version, requiredSQLiteVersion)
	case strings.ToLower(journal) != "wal":
		return fmt.Errorf("%w: journal_mode=%s, want wal", ErrSettings, journal)
	case write && syncMode != 2:
		return fmt.Errorf("%w: synchronous=%d, want 2 (FULL)", ErrSettings, syncMode)
	case fk != 1:
		return fmt.Errorf("%w: foreign_keys=%d, want 1", ErrSettings, fk)
	case busyMS != busy.Milliseconds():
		return fmt.Errorf("%w: busy_timeout=%d, want %d", ErrSettings, busyMS, busy.Milliseconds())
	case autoVacuum != 2:
		return fmt.Errorf("%w: auto_vacuum=%d, want 2 (INCREMENTAL)", ErrSettings, autoVacuum)
	}
	return nil
}

func openSQLite(ctx context.Context, cfg Config, busy time.Duration, mode openMode) (*SQLStore, []int, error) {
	lock, err := lockFile(cfg.SQLitePath + ".lock")
	if err != nil {
		return nil, nil, err
	}
	return openSQLiteLocked(ctx, cfg, busy, mode, lock)
}

// openSQLiteLocked opens the database while the caller holds lock; the
// returned store releases it on Close, and on failure it is released here.
func openSQLiteLocked(ctx context.Context, cfg Config, busy time.Duration, mode openMode, lock io.Closer) (*SQLStore, []int, error) {
	if mode != modeMigrate && mode != modeInit && mode != modeImport {
		// Serving never creates a database: opening a missing file would
		// create it in WAL mode and fix auto_vacuum before ois init.
		exists, err := sqliteFile(cfg.SQLitePath)
		if err == nil && !exists {
			err = fmt.Errorf("%w: database %s does not exist; run `ois init`", ErrMigrationPending, cfg.SQLitePath)
		}
		if err != nil {
			_ = lock.Close()
			return nil, nil, err
		}
	} else if err := prepareFreshSQLite(ctx, cfg.SQLitePath); err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	w, r, err := openSQLitePools(cfg.SQLitePath, busy)
	if err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	fail := func(err error) (*SQLStore, []int, error) {
		_ = r.Close()
		_ = w.Close()
		_ = lock.Close()
		return nil, nil, err
	}
	ops := sqliteOps()
	migs, err := loadMigrations(migrationsFS, SQLite)
	if err != nil {
		return fail(err)
	}
	applied, err := ops.prepare(ctx, w, migs, mode)
	if err != nil {
		return fail(err)
	}
	if err := readbackSQLite(ctx, w, busy, true); err != nil {
		return fail(err)
	}
	if err := readbackSQLite(ctx, r, busy, false); err != nil {
		return fail(err)
	}
	core := &sqlCore{d: ops.d, wdb: w, rdb: r, engine: SQLiteEngineVersion, path: cfg.SQLitePath, writable: func(context.Context) error { return nil }}
	s := &SQLStore{sqlCore: core}
	s.healthFn = core.probe
	s.closeFn = func() error {
		err := errors.Join(r.Close(), w.Close())
		return errors.Join(err, lock.Close())
	}
	return s, applied, nil
}
