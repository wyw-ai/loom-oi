package protocol

import (
	"crypto/rand"
	"encoding/base32"
	"regexp"
	"strings"
)

// Enumerations used on the wire.
const (
	TransportTCP = "tcp"

	AdmissionBestEffort   = "best_effort"
	AdmissionRequireReady = "require_ready"

	ReadinessUnknown  = "unknown"
	ReadinessReady    = "ready"
	ReadinessNotReady = "not_ready"

	StatePreparing = "preparing"
	StateOpen      = "open"
	StateClosing   = "closing"
	StateClosed    = "closed"

	ScopeConsumer = "consumer"
	ScopeAdmin    = "admin"

	ActionDiscover = "discover"
	ActionOpen     = "open"

	// HeaderAttachTicket carries a one-time attach ticket (B2 §3).
	HeaderAttachTicket = "OI-Attach-Ticket"
	// ControlSubprotocol and TunnelSubprotocol are the v1 WebSocket
	// subprotocols (B1 §2).
	ControlSubprotocol = "oi.control.v1"
	TunnelSubprotocol  = "oi.tunnel.v1"
)

var (
	accessIDRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	nameRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	revisionRe  = regexp.MustCompile(`^rv_[a-z2-7]{26}$`)
	channelIDRe = regexp.MustCompile(`^ch_[a-z2-7]{26}$`)
	subjectRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@:-]{0,127}$`)
	protocolRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9.+_-]{0,63}$`)
	tokenIDRe   = regexp.MustCompile(`^[0-9a-f]{16}$`)
	requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

// ValidAccessID reports whether s is an Access identifier (B3 §4).
func ValidAccessID(s string) bool { return accessIDRe.MatchString(s) }

// ValidName reports whether s may be the name part of an exposure ID.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// ValidPolicyID reports whether s is a policy identifier.
func ValidPolicyID(s string) bool { return accessIDRe.MatchString(s) }

// SplitExposureID splits "<access_id>/<name>" (B2 §4).
func SplitExposureID(id string) (accessID, name string, ok bool) {
	accessID, name, found := strings.Cut(id, "/")
	if !found || !ValidAccessID(accessID) || !ValidName(name) {
		return "", "", false
	}
	return accessID, name, true
}

// ValidRevision reports whether s has the v1 revision form "rv_"+26 base32.
func ValidRevision(s string) bool { return revisionRe.MatchString(s) }

// ValidChannelID reports whether s is a channel identifier.
func ValidChannelID(s string) bool { return channelIDRe.MatchString(s) }

// ValidSubject reports whether s is a consumer or admin subject.
func ValidSubject(s string) bool { return subjectRe.MatchString(s) }

// ValidProtocol reports whether s is an application protocol label.
func ValidProtocol(s string) bool { return protocolRe.MatchString(s) }

// ValidTokenID reports whether s is a token identifier.
func ValidTokenID(s string) bool { return tokenIDRe.MatchString(s) }

// ValidClientRequestID reports whether s is an acceptable idempotency key.
func ValidClientRequestID(s string) bool { return requestIDRe.MatchString(s) }

// ValidAdmission reports whether s is an admission mode.
func ValidAdmission(s string) bool {
	return s == AdmissionBestEffort || s == AdmissionRequireReady
}

// StricterAdmission returns the stricter of two admission modes.
func StricterAdmission(a, b string) string {
	if a == AdmissionRequireReady || b == AdmissionRequireReady {
		return AdmissionRequireReady
	}
	return AdmissionBestEffort
}

// Base32 is lowercase RFC 4648 base32 without padding.
var Base32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID returns prefix followed by 128 random bits in Base32 (26 chars).
func NewID(prefix string) string {
	return prefix + Base32.EncodeToString(RandomBytes(16))
}

// RandomBytes returns n bytes from the operating system CSPRNG.
// crypto/rand.Read never returns an error on supported platforms.
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}
