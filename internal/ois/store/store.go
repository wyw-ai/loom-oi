// Package store implements the B3 shared storage contract for OIS on
// PostgreSQL and SQLite. Both backends share one SQL core and one contract
// test suite so Conflict, durability and audit semantics cannot drift.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Backend names accepted by store.driver.
const (
	SQLite   = "sqlite"
	Postgres = "postgres"
)

// Startup and runtime errors. Write errors map to the API as B3 §3 states:
// Conflict→409, Busy→503 retryable, Unavailable→503, Invalid→400.
var (
	ErrUnavailable       = errors.New("store unavailable")
	ErrBusy              = errors.New("store busy")
	ErrInvalid           = errors.New("invalid store mutation")
	ErrAnotherInstance   = errors.New("AnotherInstanceActive: another OIS holds the store lock")
	ErrSchemaTooNew      = errors.New("SchemaTooNew: database schema is newer than this binary")
	ErrMigrationPending  = errors.New("database has pending migrations; run `ois migrate`")
	ErrMigrationChecksum = errors.New("applied migration checksum does not match this binary")
	ErrSettings          = errors.New("database settings do not satisfy B3 §5")
)

// ConflictError is returned when expected_version is stale.
type ConflictError struct{ Current int64 }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("config version conflict: current version is %d", e.Current)
}

// Config selects and locates the backend (store.driver is always explicit).
type Config struct {
	Driver      string `json:"driver"`
	SQLitePath  string `json:"sqlite_path,omitempty"`
	PostgresDSN string `json:"postgres_dsn,omitempty"`
}

// Validate checks that exactly the selected backend is configured.
func (c Config) Validate() error {
	switch c.Driver {
	case SQLite:
		if c.SQLitePath == "" || c.PostgresDSN != "" {
			return errors.New("store: sqlite needs sqlite_path and no postgres_dsn")
		}
	case Postgres:
		if c.PostgresDSN == "" || c.SQLitePath != "" {
			return errors.New("store: postgres needs postgres_dsn and no sqlite_path")
		}
	default:
		return fmt.Errorf("store: driver must be %q or %q", SQLite, Postgres)
	}
	return nil
}

// Meta is the single metadata row.
type Meta struct {
	StoreID       string
	Backend       string
	ConfigVersion int64
	Epoch         int64
	LastRunID     string
	LastStartedAt *time.Time
	Maintenance   bool
}

// Access is a persisted Access record.
type Access struct {
	AccessID     string
	Enabled      bool
	SPKIPins     []string
	MaxExposures int
	Labels       map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Token stores only the SHA-256 of the secret (B2 §2).
type Token struct {
	TokenID      string
	Subject      string
	Scope        string
	SecretSHA256 []byte
	ExpiresAt    time.Time
	RevokedAt    *time.Time
	CreatedAt    time.Time
}

// Policy grants actions on an exact exposure or "<access_id>/*".
type Policy struct {
	PolicyID         string
	Subject          string
	ExposureSelector string
	Actions          []string
	Enabled          bool
	UpdatedVersion   int64
}

// Exposure is one persisted registry entry.
type Exposure struct {
	ExposureID       string
	AccessID         string
	Revision         string
	Protocol         string
	TransportProfile string
	AdmissionMode    string
	Description      string
	UpdatedAt        time.Time
}

// AuditEntry is append-only.
type AuditEntry struct {
	Seq           int64
	At            time.Time
	Actor         string
	Action        string
	ObjectType    string
	ObjectID      string
	ConfigVersion int64
	Detail        json.RawMessage
}

// Snapshot is everything OIS loads at startup.
type Snapshot struct {
	Meta      Meta
	Accesses  []Access
	Tokens    []Token
	Policies  []Policy
	Exposures []Exposure
}

// Mutation is one configuration change inside ApplyConfig.
type Mutation interface{ mutation() }

// PutAccess inserts or replaces an Access.
type PutAccess struct{ Access Access }

// DeleteAccess removes an Access and, by cascade, its registry entries.
type DeleteAccess struct{ AccessID string }

// PutToken inserts a new token.
type PutToken struct{ Token Token }

// RevokeToken marks a token revoked.
type RevokeToken struct {
	TokenID string
	At      time.Time
}

// PutPolicy inserts or replaces a policy.
type PutPolicy struct{ Policy Policy }

// DeletePolicy removes a policy.
type DeletePolicy struct{ PolicyID string }

func (PutAccess) mutation()    {}
func (DeleteAccess) mutation() {}
func (PutToken) mutation()     {}
func (RevokeToken) mutation()  {}
func (PutPolicy) mutation()    {}
func (DeletePolicy) mutation() {}

// Store is the B3 §3 contract.
type Store interface {
	Backend() string
	EngineVersion() string
	LoadSnapshot(ctx context.Context) (Snapshot, error)
	ApplyConfig(ctx context.Context, expected int64, muts []Mutation, audit []AuditEntry) (int64, error)
	UpsertRegistry(ctx context.Context, accessID string, exposures []Exposure, audit []AuditEntry) error
	StartRun(ctx context.Context, runID string, at time.Time) (int64, error)
	// RaiseEpoch records audit only when it raises and audit.Action is set.
	RaiseEpoch(ctx context.Context, min int64, audit AuditEntry) (bool, error)
	AppendAudit(ctx context.Context, entries []AuditEntry) error
	ListAudit(ctx context.Context, afterSeq int64, limit int) ([]AuditEntry, error)
	Health(ctx context.Context) error
	Close() error
}
