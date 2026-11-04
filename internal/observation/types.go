// Package observation implements bounded local health evidence. It has no
// authority to open a channel, dial a business target, or extend a license.
// Table methods are intentionally unsynchronized: the OIC state owner orders
// them with configuration and admission under its existing lock.
package observation

import (
	"context"
	"errors"
	"net"
	"time"
)

type Readiness string

const (
	Unknown  Readiness = "unknown"
	Ready    Readiness = "ready"
	NotReady Readiness = "not_ready"
)

type Reason string

const (
	MatchedReady    Reason = "matched_ready_status"
	MatchedNotReady Reason = "matched_not_ready_status"
	Unsupported     Reason = "unsupported"
	NoSample        Reason = "no_sample"
	Expired         Reason = "expired"
	SourceInvalid   Reason = "source_invalid"
	Timeout         Reason = "timeout"
	PermissionError Reason = "permission_error"
	TLSError        Reason = "tls_error"
	AddressDenied   Reason = "address_denied"
	ConnectError    Reason = "connect_error"
	StatusUnmapped  Reason = "status_unmapped"
	Malformed       Reason = "malformed_response"
)

type Mode string

const (
	BestEffort   Mode = "best_effort"
	RequireReady Mode = "require_ready"
)

var (
	ErrBinding    = errors.New("observation binding is not current")
	ErrBusy       = errors.New("observation probe already running")
	ErrOverflow   = errors.New("observation identity exhausted; end session")
	ErrCapacity   = errors.New("observation identity capacity exhausted; end session")
	ErrNoProbe    = errors.New("observation source unsupported")
	ErrAddress    = errors.New("probe address denied")
	ErrPermission = errors.New("probe permission denied")
)

// Binding is the complete source identity. Unsupported entries omit SourceID
// and ContractVersion but retain ObservationRevision and the null digest.
type Binding struct {
	AccessID, ExposureID, Revision, ObservationRevision, ContractDigest string
	Epoch, Generation                                                   int64
	SourceID                                                            string
	ContractVersion                                                     int
}

type Session struct {
	AccessID          string
	Epoch, Generation int64
}

// WorkToken also includes a process-local incarnation. Reusing a digest or
// reconstructing a binding never makes a cancelled worker current again.
type WorkToken struct {
	Binding           Binding
	SessionID, WorkID uint64
}

type Attempt struct {
	Seq      uint64
	Running  bool
	Outcome  Reason
	Duration time.Duration
}

// Sample times retain their Go monotonic component. ObservedAt is diagnostic
// wall time only; neither wall-clock comparisons nor message delivery renew it.
type Sample struct {
	Seq                              uint64
	Readiness                        Readiness
	Reason                           Reason
	ObservedAt, StartedAt, ExpiresAt time.Time
}

type Snapshot struct {
	Binding    Binding
	UpdateSeq  uint64
	Attempt    Attempt
	Readiness  Readiness
	Reason     Reason
	Remaining  time.Duration
	LastSample *Sample
}

// Decision is a local in-memory answer, not a channel decision_id. The OIC/OIS
// adapters retain their own final-check deadline and channel state transition.
type Decision struct {
	Allowed  bool
	Mode     Mode
	Snapshot Snapshot
}

// Effect describes work to perform after the caller releases its state lock.
// A stale completion produces a zero effect and cannot release another token.
type Effect struct {
	Dirty               bool
	CancelWork          *WorkToken
	InvalidatePreparing bool
	EndSession          bool
	// These events refer to committed state transitions, not callback arrivals.
	// Empty SampleCreated means none; stale completions never create metrics.
	SampleCreated Readiness
	SampleExpired bool
}

// Outcome contains only finite, nonsensitive classifications. Raw addresses,
// HTTP bodies and transport error strings do not enter evidence or labels.
type Outcome struct {
	Readiness Readiness
	Reason    Reason
	Duration  time.Duration
}

type DialFunc func(context.Context, string, string) (net.Conn, error)

func RemainingMS(remaining time.Duration) int64 {
	if remaining <= 0 {
		return 0
	}
	return remaining.Milliseconds()
}

func (r Reason) failure() bool {
	switch r {
	case Timeout, PermissionError, TLSError, AddressDenied, ConnectError, StatusUnmapped, Malformed:
		return true
	}
	return false
}
