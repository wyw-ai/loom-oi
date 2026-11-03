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

// AccessSpec creates or patches an Access. Nil fields are left unchanged on
// PATCH and take defaults on create.
type AccessSpec struct {
	AccessID     string            `json:"access_id,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	SPKIPins     []string          `json:"spki_pins,omitempty"`
	MaxExposures *int              `json:"max_exposures,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
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
