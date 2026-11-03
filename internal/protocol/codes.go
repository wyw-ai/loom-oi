// Package protocol defines the v1 wire formats shared by OIS, OIC and oi
// (B1): consumer/admin HTTP bodies, oi.control.v1 messages, oi.tunnel.v1
// frames and the stable error/ABORT code table.
package protocol

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable v1 error code. Codes may be appended; an existing code
// never changes meaning (B1 §7, D1-g).
type Code string

// Error codes from B1 §7, plus the admin/store codes required by B3 §3.
const (
	CodeInvalidRequest          Code = "InvalidRequest"
	CodeVersionUnsupported      Code = "VersionUnsupported"
	CodePayloadTooLarge         Code = "PayloadTooLarge"
	CodeUnauthenticated         Code = "Unauthenticated"
	CodeTicketInvalid           Code = "TicketInvalid"
	CodeDenied                  Code = "Denied"
	CodeNotFound                Code = "NotFound"
	CodeRevisionMismatch        Code = "RevisionMismatch"
	CodeIdempotencyConflict     Code = "IdempotencyConflict"
	CodeTicketConsumed          Code = "TicketConsumed"
	CodeChannelGone             Code = "ChannelGone"
	CodeReadinessRequired       Code = "ReadinessRequired"
	CodeResourceExhausted       Code = "ResourceExhausted"
	CodeTargetNotReady          Code = "TargetNotReady"
	CodeAccessOffline           Code = "AccessOffline"
	CodeStoreUnavailable        Code = "StoreUnavailable"
	CodeRevisionChanged         Code = "RevisionChanged"
	CodeRevoked                 Code = "Revoked"
	CodeTargetConnectFailed     Code = "TargetConnectFailed"
	CodeLocalDenied             Code = "LocalDenied"
	CodeTargetAddressNotAllowed Code = "TargetAddressNotAllowed"
	CodeProtocolError           Code = "ProtocolError"
	CodeChannelInterrupted      Code = "ChannelInterrupted"
	CodeLicenseExpired          Code = "LicenseExpired"
	CodeIdleTimeout             Code = "IdleTimeout"
	CodeClosedByClient          Code = "ClosedByClient"
	CodePrepareTimeout          Code = "PrepareTimeout"
	CodeShutdown                Code = "Shutdown"

	// CodeCompleted is the close reason of a channel whose two directions
	// both ended with FIN (B1 §6 Closed(Completed)).
	CodeCompleted Code = "Completed"
	// CodeVersionConflict and CodePreconditionRequired implement If-Match on
	// admin writes; CodeStoreBusy is the retryable Busy outcome (B3 §3).
	CodeVersionConflict      Code = "VersionConflict"
	CodePreconditionRequired Code = "PreconditionRequired"
	CodeStoreBusy            Code = "StoreBusy"
	// CodeSuperseded is sent on the old control connection when a newer
	// connection for the same Access arrives (B1 §4).
	CodeSuperseded Code = "Superseded"
	// CodeInternal reports an unexpected server fault.
	CodeInternal Code = "Internal"
)

var httpStatus = map[Code]int{
	CodeInvalidRequest:       http.StatusBadRequest,
	CodeVersionUnsupported:   http.StatusBadRequest,
	CodePayloadTooLarge:      http.StatusRequestEntityTooLarge,
	CodeUnauthenticated:      http.StatusUnauthorized,
	CodeTicketInvalid:        http.StatusUnauthorized,
	CodeDenied:               http.StatusForbidden,
	CodeNotFound:             http.StatusNotFound,
	CodeRevisionMismatch:     http.StatusConflict,
	CodeIdempotencyConflict:  http.StatusConflict,
	CodeTicketConsumed:       http.StatusConflict,
	CodeVersionConflict:      http.StatusConflict,
	CodeChannelGone:          http.StatusGone,
	CodeReadinessRequired:    http.StatusPreconditionFailed,
	CodePreconditionRequired: http.StatusPreconditionRequired,
	CodeResourceExhausted:    http.StatusTooManyRequests,
	CodeTargetNotReady:       http.StatusServiceUnavailable,
	CodeAccessOffline:        http.StatusServiceUnavailable,
	CodeStoreUnavailable:     http.StatusServiceUnavailable,
	CodeStoreBusy:            http.StatusServiceUnavailable,
	CodeShutdown:             http.StatusServiceUnavailable,
}

// HTTPStatus returns the HTTP status for codes that appear in API responses
// and 500 for any other code.
func (c Code) HTTPStatus() int {
	if s, ok := httpStatus[c]; ok {
		return s
	}
	return http.StatusInternalServerError
}

var abortReasons = map[Code]uint16{
	CodeUnauthenticated:         0x0101,
	CodeDenied:                  0x0102,
	CodeRevisionChanged:         0x0103,
	CodeTargetNotReady:          0x0104,
	CodeAccessOffline:           0x0105,
	CodeRevoked:                 0x0106,
	CodeTargetConnectFailed:     0x0201,
	CodeLocalDenied:             0x0202,
	CodeTargetAddressNotAllowed: 0x0203,
	CodeResourceExhausted:       0x0204,
	CodePayloadTooLarge:         0x0301,
	CodeProtocolError:           0x0302,
	CodeChannelInterrupted:      0x0401,
	CodeLicenseExpired:          0x0402,
	CodeIdleTimeout:             0x0403,
	CodeClosedByClient:          0x0404,
	CodePrepareTimeout:          0x0405,
	CodeShutdown:                0x0406,
}

var abortCodes = func() map[uint16]Code {
	m := make(map[uint16]Code, len(abortReasons))
	for c, v := range abortReasons {
		m[v] = c
	}
	return m
}()

// AbortReason returns the 16-bit ABORT reason for c and whether c may be
// carried by an ABORT frame.
func AbortReason(c Code) (uint16, bool) {
	v, ok := abortReasons[c]
	return v, ok
}

// AbortCode maps a received ABORT reason back to its code.
func AbortCode(v uint16) (Code, bool) {
	c, ok := abortCodes[v]
	return c, ok
}

// Error is a coded failure that crosses process boundaries unchanged.
type Error struct {
	Code    Code
	Message string
	// ConfigVersion is the current version reported with VersionConflict.
	ConfigVersion int64
}

func (e *Error) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Message
}

// Errorf builds a coded error.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf returns the code carried by err, or CodeInternal.
func CodeOf(err error) Code {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return CodeInternal
}
