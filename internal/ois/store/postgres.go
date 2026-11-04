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
	"github.com/jackc/pgx/v5/stdlib" // also registers the "pgx" database/sql driver
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
		timeParam:      func(t time.Time) any { return t },
		failureKind:    postgresFailureKind,
		identityInsert: " OVERRIDING SYSTEM VALUE",
	}
}

func postgresFailureKind(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "53100":
			return FailureDiskFull
		case "25006":
			return FailureReadOnly
		case "58030":
			return FailureIO
		}
	}
	return FailureUnavailable
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
	db       *sql.DB
	mu       sync.Mutex
	conn     *sql.Conn
	closed   bool
	closeErr error
	lostErr  error // a lost session has no acknowledged release until reacquired
}

func (l *pgLock) acquire(ctx context.Context) error {
	if l.closed {
		return ErrUnavailable
	}
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
	l.lostErr = nil
	return nil
}

// lockCheck bounds the liveness query on the lock connection. A caller's
// context is never used there: pgx closes a connection whose query context
// is cancelled, and closing this one would release the lock (B3 §5).
const lockCheck = 5 * time.Second

// held verifies the lock connection is alive; when it is not, the store is
// Unavailable until the lock is acquired again (B3 §5).
func (l *pgLock) held(ctx context.Context, reacquire bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrUnavailable
	}
	if l.conn != nil {
		cctx, cancel := context.WithTimeout(context.Background(), lockCheck)
		var one int
		err := l.conn.QueryRowContext(cctx, "SELECT 1").Scan(&one)
		cancel()
		if err == nil {
			return nil
		}
		_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = l.conn.Close()
		l.conn = nil
		l.lostErr = fmt.Errorf("%w: single-instance lock lost", ErrUnavailable)
	}
	if !reacquire {
		return fmt.Errorf("%w: single-instance lock lost", ErrUnavailable)
	}
	return l.acquire(ctx)
}

func (l *pgLock) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeErr
	}
	l.closed = true
	if l.conn == nil {
		l.closeErr = l.lostErr
		return l.closeErr
	}
	conn := l.conn
	l.conn = nil
	// Retain the physical connection before a timed-out query can cause
	// database/sql to discard its wrapper. pgx can otherwise defer its
	// network cleanup after cancellation for longer than this close budget.
	var wire *pgconn.PgConn
	err := conn.Raw(func(raw any) error {
		wire = raw.(*stdlib.Conn).Conn().PgConn()
		return nil
	})
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), lockCheck)
		var unlocked bool
		err = conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", advisoryKey).Scan(&unlocked)
		cancel()
		if err == nil && !unlocked {
			err = fmt.Errorf("single-instance lock was not held")
		}
	}
	if err != nil {
		err = fmt.Errorf("%w: release single-instance lock: %w", ErrUnavailable, err)
		if wire != nil {
			err = errors.Join(err, wire.Conn().Close())
		}
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	l.closeErr = errors.Join(err, conn.Close())
	return l.closeErr
}

func openPostgres(ctx context.Context, cfg Config, mode openMode) (*SQLStore, []int, error) {
	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(8)
	// Besides the lock connection, one idle connection is kept: extra ones
	// opened for a burst close when it ends, so descriptors return to their
	// baseline (B4 §4).
	db.SetMaxIdleConns(1)
	lock := &pgLock{db: db}
	fail := func(err error) (*SQLStore, []int, error) {
		cleanupErr := errors.Join(db.Close(), lock.close())
		return nil, nil, operationCleanupError("open", err, cleanupErr)
	}
	if err := lock.acquire(ctx); err != nil {
		return fail(err)
	}
	ops := postgresOps()
	migs, err := loadMigrations(migrationsFS, Postgres)
	if err != nil {
		return fail(err)
	}
	applied, err := ops.prepare(ctx, db, migs, mode)
	if err != nil {
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
	lifecycle := newStoreLifecycle()
	core := &sqlCore{d: ops.d, wdb: db, rdb: db, engine: version, lifecycle: lifecycle}
	core.writable = func(ctx context.Context) error { return lock.held(ctx, false) }
	s := &SQLStore{sqlCore: core}
	s.healthFn = func(ctx context.Context) error {
		leave, err := lifecycle.enter()
		if err != nil {
			return err
		}
		defer leave()
		if err := lock.held(ctx, true); err != nil {
			return err
		}
		return core.probe(ctx)
	}
	var closeOnce sync.Once
	var closeErr error
	s.closeFn = func() error {
		closeOnce.Do(func() {
			lifecycle.stop()
			// DB.Close prevents new pool work. The already reserved lock
			// Conn remains usable for its final acknowledged unlock.
			closeErr = errors.Join(db.Close(), lock.close())
		})
		return closeErr
	}
	return s, applied, nil
}
