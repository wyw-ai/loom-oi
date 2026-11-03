package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

// advisoryKey identifies the single active OIS on a PostgreSQL database.
// The lock only prevents a second instance; it is not leader election.
const advisoryKey int64 = 0x6c6f6f6d2d6f6973 // "loom-ois"

func postgresDialect() *dialect {
	return &dialect{
		name:       Postgres,
		numbered:   true,
		forUpdate:  " FOR UPDATE",
		jsonIn:     "CAST(? AS jsonb)",
		jsonOutFmt: "%s::text",
		readTx:     &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true},
		isBusy:     func(error) bool { return false },
		isInvalid: func(err error) bool {
			var pe *pgconn.PgError
			return errors.As(err, &pe) && (strings.HasPrefix(pe.Code, "23") || strings.HasPrefix(pe.Code, "22"))
		},
		timeParam: func(t time.Time) any { return t },
	}
}

func postgresOps() *schemaOps {
	return &schemaOps{
		d:           postgresDialect(),
		backend:     Postgres,
		tableExists: "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname = current_schema() AND tablename = ?",
		bootstrap: `CREATE TABLE IF NOT EXISTS schema_migrations (
version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL)`,
	}
}

// pgLock holds pg_try_advisory_lock on a dedicated connection.
type pgLock struct {
	db   *sql.DB
	mu   sync.Mutex
	conn *sql.Conn
}

func (l *pgLock) acquire(ctx context.Context) error {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", advisoryKey).Scan(&ok); err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if !ok {
		_ = conn.Close()
		return ErrAnotherInstance
	}
	l.conn = conn
	return nil
}

// held verifies the lock connection is alive; when it is not, the store is
// Unavailable until the lock is acquired again (B3 §5).
func (l *pgLock) held(ctx context.Context, reacquire bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != nil {
		var one int
		if err := l.conn.QueryRowContext(ctx, "SELECT 1").Scan(&one); err == nil {
			return nil
		}
		_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = l.conn.Close()
		l.conn = nil
	}
	if !reacquire {
		return fmt.Errorf("%w: single-instance lock lost", ErrUnavailable)
	}
	return l.acquire(ctx)
}

func (l *pgLock) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn == nil {
		return nil
	}
	err := l.conn.Close()
	l.conn = nil
	return err
}

func openPostgres(ctx context.Context, cfg Config, migrateOnly bool) (*SQLStore, []int, error) {
	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(8)
	lock := &pgLock{db: db}
	fail := func(err error) (*SQLStore, []int, error) {
		_ = lock.close()
		_ = db.Close()
		return nil, nil, err
	}
	if err := lock.acquire(ctx); err != nil {
		return fail(err)
	}
	ops := postgresOps()
	migs, err := loadMigrations(migrationsFS, Postgres)
	if err != nil {
		return fail(err)
	}
	var applied []int
	if migrateOnly {
		if applied, err = ops.migrate(ctx, db, migs); err != nil {
			return fail(err)
		}
	} else if err := ops.check(ctx, db, migs); err != nil {
		return fail(err)
	}
	var syncCommit, version string
	if err := db.QueryRowContext(ctx, "SHOW synchronous_commit").Scan(&syncCommit); err != nil {
		return fail(err)
	}
	if syncCommit == "off" {
		return fail(fmt.Errorf("%w: synchronous_commit=off", ErrSettings))
	}
	if err := db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		return fail(err)
	}
	core := &sqlCore{d: ops.d, wdb: db, rdb: db, engine: version}
	core.writable = func(ctx context.Context) error { return lock.held(ctx, false) }
	s := &SQLStore{sqlCore: core}
	s.healthFn = func(ctx context.Context) error {
		if err := lock.held(ctx, true); err != nil {
			return err
		}
		return core.probe(ctx)
	}
	s.closeFn = func() error { return errors.Join(lock.close(), db.Close()) }
	return s, applied, nil
}
