package store

import (
	"context"
	"time"
)

// SQLStore is the shared implementation for both backends.
type SQLStore struct {
	*sqlCore
	healthFn func(context.Context) error
	closeFn  func() error
}

// Health is the light probe used by OIS every STORE_PROBE (B3 §6).
func (s *SQLStore) Health(ctx context.Context) error { return s.healthFn(ctx) }

// Close releases connections and the single-instance lock.
func (s *SQLStore) Close() error { return s.closeFn() }

// Open opens the configured backend for serving: it takes the
// single-instance lock, refuses newer, pending or tampered schemas and reads
// back the required settings (B3 §5–§7). It never creates a database.
func Open(ctx context.Context, cfg Config, busy time.Duration) (*SQLStore, error) {
	s, _, err := open(ctx, cfg, busy, modeServe)
	return s, err
}

// Migrate applies pending migrations explicitly (`ois migrate`) and returns
// the store opened on the migrated schema plus the applied versions.
func Migrate(ctx context.Context, cfg Config, busy time.Duration) (*SQLStore, []int, error) {
	return open(ctx, cfg, busy, modeMigrate)
}

// Initialize creates the schema of an empty database (ois init). A
// database with applied migrations is only checked as Open does, so an
// upgrade always goes through ois migrate and its backup reminder.
func Initialize(ctx context.Context, cfg Config, busy time.Duration) (*SQLStore, []int, error) {
	return open(ctx, cfg, busy, modeInit)
}

type openMode int

const (
	modeServe openMode = iota
	modeMigrate
	modeInit
	// modeRestore checks the schema like serving but accepts pending
	// migrations: a restored database is marked first and migrated with
	// ois migrate afterwards (B3 §7–§8).
	modeRestore
)

func open(ctx context.Context, cfg Config, busy time.Duration, mode openMode) (*SQLStore, []int, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if cfg.Driver == SQLite {
		return openSQLite(ctx, cfg, busy, mode)
	}
	return openPostgres(ctx, cfg, mode)
}
