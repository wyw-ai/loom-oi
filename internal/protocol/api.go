package protocol

import (
	"encoding/json"
	"time"
)

// ErrorBody is the JSON body of every non-2xx API response.
type ErrorBody struct {
	Code    Code   `json:"code"`
	Message string `json:"message,omitempty"`
	// ConfigVersion is set on VersionConflict (B3 §3).
	ConfigVersion int64 `json:"config_version,omitempty"`
}

// ExposureView is one discover item (B1 §3).
type ExposureView struct {
	ExposureID       string `json:"exposure_id"`
	AccessID         string `json:"access_id"`
	Revision         string `json:"revision"`
	Protocol         string `json:"protocol"`
	TransportProfile string `json:"transport_profile"`
	Description      string `json:"description"`
	AdmissionMode    string `json:"admission_mode"`
	Readiness        string `json:"readiness"`
	AccessOnline     bool   `json:"access_online"`
}

// ExposureList is the GET /v1/exposures response.
type ExposureList struct {
	Items      []ExposureView `json:"items"`
	NextCursor string         `json:"next_cursor"`
}

// OpenRequest is the POST /v1/channels body.
type OpenRequest struct {
	ExposureID       string `json:"exposure_id"`
	ExpectedRevision string `json:"expected_revision"`
	ClientRequestID  string `json:"client_request_id,omitempty"`
	Admission        string `json:"admission,omitempty"`
}

// Attach tells the consumer how to attach its leg.
type Attach struct {
	URL         string `json:"url"`
	Ticket      string `json:"ticket"`
	ExpiresInMS int64  `json:"expires_in_ms"`
}

// OpenResponse is the POST /v1/channels response; Attach is null once the
// consumer ticket has been consumed (idempotent replay).
type OpenResponse struct {
	ChannelID          string  `json:"channel_id"`
	State              string  `json:"state"`
	ExposureID         string  `json:"exposure_id"`
	Revision           string  `json:"revision"`
	AdmissionMode      string  `json:"admission_mode"`
	Readiness          string  `json:"readiness"`
	Attach             *Attach `json:"attach"`
	PrepareExpiresInMS int64   `json:"prepare_expires_in_ms"`
}

// ChannelView is the GET/DELETE /v1/channels/{id} response.
type ChannelView struct {
	ChannelID   string `json:"channel_id"`
	State       string `json:"state"`
	CloseReason Code   `json:"close_reason,omitempty"`
	ExposureID  string `json:"exposure_id"`
	Revision    string `json:"revision"`
}

// ConfigView is GET /v1/admin/config.
type ConfigView struct {
	ConfigVersion int64  `json:"config_version"`
	Epoch         int64  `json:"epoch"`
	StoreID       string `json:"store_id"`
	Backend       string `json:"backend"`
	RunID         string `json:"run_id"`
	Maintenance   bool   `json:"maintenance"`
	StoreHealthy  bool   `json:"store_healthy"`
}

// AccessSpec creates or patches an Access. Nil fields (JSON null or absent)
// are left unchanged on PATCH and take defaults on create; an empty list or
// object clears pins or labels.
type AccessSpec struct {
	AccessID     string            `json:"access_id,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	SPKIPins     []string          `json:"spki_pins"`
	MaxExposures *int              `json:"max_exposures,omitempty"`
	Labels       map[string]string `json:"labels"`
}

// AccessView is an Access record with its runtime state.
type AccessView struct {
	AccessID     string            `json:"access_id"`
	Enabled      bool              `json:"enabled"`
	SPKIPins     []string          `json:"spki_pins"`
	MaxExposures int               `json:"max_exposures"`
	Labels       map[string]string `json:"labels"`
	Online       bool              `json:"online"`
	Generation   int64             `json:"generation"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// TokenCreate creates a consumer or admin token.
type TokenCreate struct {
	Subject    string `json:"subject"`
	Scope      string `json:"scope"`
	TTLSeconds int64  `json:"ttl_seconds,omitempty"`
}

// TokenCreated returns the only copy of the token secret (B2 §2).
type TokenCreated struct {
	TokenID       string    `json:"token_id"`
	Token         string    `json:"token"`
	Subject       string    `json:"subject"`
	Scope         string    `json:"scope"`
	ExpiresAt     time.Time `json:"expires_at"`
	ConfigVersion int64     `json:"config_version"`
}

// TokenView is a token record without its secret.
type TokenView struct {
	TokenID   string     `json:"token_id"`
	Subject   string     `json:"subject"`
	Scope     string     `json:"scope"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// PolicySpec creates or patches a policy.
type PolicySpec struct {
	PolicyID         string   `json:"policy_id,omitempty"`
	Subject          string   `json:"subject,omitempty"`
	ExposureSelector string   `json:"exposure_selector,omitempty"`
	Actions          []string `json:"actions,omitempty"`
	Enabled          *bool    `json:"enabled,omitempty"`
}

// PolicyView is a policy record.
type PolicyView struct {
	PolicyID         string   `json:"policy_id"`
	Subject          string   `json:"subject"`
	ExposureSelector string   `json:"exposure_selector"`
	Actions          []string `json:"actions"`
	Enabled          bool     `json:"enabled"`
	UpdatedVersion   int64    `json:"updated_version"`
}

// WriteResult reports the config version after an admin write.
type WriteResult struct {
	ConfigVersion int64 `json:"config_version"`
}

// AuditView is one audit record.
type AuditView struct {
	Seq           int64           `json:"seq"`
	At            time.Time       `json:"at"`
	Actor         string          `json:"actor"`
	Action        string          `json:"action"`
	ObjectType    string          `json:"object_type"`
	ObjectID      string          `json:"object_id"`
	ConfigVersion int64           `json:"config_version"`
	Detail        json.RawMessage `json:"detail"`
}

// AuditList is GET /v1/admin/audit.
type AuditList struct {
	Items []AuditView `json:"items"`
}

// MaintenanceSpec is PUT /v1/admin/maintenance; If-Match is required
// (B3 §8). Maintenance is a pointer so an absent field is rejected.
type MaintenanceSpec struct {
	Maintenance *bool `json:"maintenance"`
}

// StoreUsage reports storage size counters (§8.1.1). SQLite fills the
// page and WAL fields; PostgreSQL reports only the database size.
type StoreUsage struct {
	DBBytes       int64 `json:"db_bytes"`
	WALBytes      int64 `json:"wal_bytes"`
	PageSize      int64 `json:"page_size"`
	PageCount     int64 `json:"page_count"`
	FreelistCount int64 `json:"freelist_count"`
}

// StoreView is GET /v1/admin/store.
type StoreView struct {
	Backend     string `json:"backend"`
	Engine      string `json:"engine"`
	Healthy     bool   `json:"healthy"`
	Maintenance bool   `json:"maintenance"`
	// WriteFailure is the category of the last write failure while the
	// store stays unavailable: disk_full, read_only, io_error or
	// unavailable; empty once a write probe succeeds.
	WriteFailure string     `json:"write_failure,omitempty"`
	Usage        StoreUsage `json:"usage"`
}

// StoreMaintain is POST /v1/admin/store/maintain. Checkpoint is "",
// "passive" or "truncate"; IncrementalVacuumPages is at most
// MaxVacuumPages. PostgreSQL supports only Optimize (ANALYZE).
type StoreMaintain struct {
	Checkpoint             string `json:"checkpoint,omitempty"`
	IncrementalVacuumPages int64  `json:"incremental_vacuum_pages,omitempty"`
	Optimize               bool   `json:"optimize,omitempty"`
}

// MaxVacuumPages bounds one incremental_vacuum step so maintenance never
// holds the single writer for long (§8.1.1: low-peak, in batches).
const MaxVacuumPages = 10_000

// CheckpointResult is the result row of PRAGMA wal_checkpoint.
type CheckpointResult struct {
	Busy               bool  `json:"busy"`
	LogFrames          int64 `json:"log_frames"`
	CheckpointedFrames int64 `json:"checkpointed_frames"`
}

// StoreMaintained is the POST /v1/admin/store/maintain response.
type StoreMaintained struct {
	Before     StoreUsage        `json:"before"`
	After      StoreUsage        `json:"after"`
	Checkpoint *CheckpointResult `json:"checkpoint,omitempty"`
}

// AuditPrune is POST /v1/admin/audit/prune: records with seq ≤ ThroughSeq
// are deleted only when their digest equals SHA256 (§8.1.1: never delete
// what was not exported).
type AuditPrune struct {
	ThroughSeq int64  `json:"through_seq"`
	SHA256     string `json:"sha256"`
}

// AuditPruned is the prune response.
type AuditPruned struct {
	ThroughSeq int64  `json:"through_seq"`
	Deleted    int64  `json:"deleted"`
	SHA256     string `json:"sha256"`
}

// AuditExport is the file written by `oi admin audit export`.
type AuditExport struct {
	ThroughSeq int64       `json:"through_seq"`
	Count      int         `json:"count"`
	SHA256     string      `json:"sha256"`
	Items      []AuditView `json:"items"`
}

// ChannelCounts groups channels by state.
type ChannelCounts struct {
	Preparing int `json:"preparing"`
	Open      int `json:"open"`
	Closing   int `json:"closing"`
	Closed    int `json:"closed"`
}

// ProcessStats are process-level resource counters (B4 §4, O3).
type ProcessStats struct {
	Goroutines int   `json:"goroutines"`
	OpenFDs    int   `json:"open_fds"`
	RSSBytes   int64 `json:"rss_bytes"`
	HeapBytes  int64 `json:"heap_bytes"`
	CPUMillis  int64 `json:"cpu_ms"`
}

// OISStats is GET /v1/admin/stats.
type OISStats struct {
	Channels         ChannelCounts `json:"channels"`
	Tickets          int           `json:"tickets"`
	AccessesOnline   int           `json:"accesses_online"`
	RelayBytes       int64         `json:"relay_bytes"`
	MaxBufferedBytes int64         `json:"max_buffered_bytes"`
	StoreHealthy     bool          `json:"store_healthy"`
	Epoch            int64         `json:"epoch"`
	Process          ProcessStats  `json:"process"`
}

// OICStats is the optional OIC diagnostics response.
type OICStats struct {
	AccessID           string `json:"access_id"`
	Connected          bool   `json:"connected"`
	Registered         bool   `json:"registered"`
	Epoch              int64  `json:"epoch"`
	Generation         int64  `json:"generation"`
	LicenseRemainingMS int64  `json:"license_remaining_ms"`
	Channels           int    `json:"channels"`
	RelayBytes         int64  `json:"relay_bytes"`
	MaxBufferedBytes   int64  `json:"max_buffered_bytes"`
	ConfigError        string `json:"config_error,omitempty"`
	// Closed counts ended channels by close reason (O4).
	Closed  map[string]int64 `json:"closed"`
	Process ProcessStats     `json:"process"`
}
