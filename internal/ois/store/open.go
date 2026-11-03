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
	s, _, err := open(ctx, cfg, busy, false)
	return s, err
}

// Migrate applies pending migrations explicitly (`ois migrate`) and returns
// the store opened on the migrated schema plus the applied versions.
func Migrate(ctx context.Context, cfg Config, busy time.Duration) (*SQLStore, []int, error) {
	return open(ctx, cfg, busy, true)
}

func open(ctx context.Context, cfg Config, busy time.Duration, migrateOnly bool) (*SQLStore, []int, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if cfg.Driver == SQLite {
		return openSQLite(ctx, cfg, busy, migrateOnly)
	}
	return openPostgres(ctx, cfg, migrateOnly)
}
