package protocol

import (
	"encoding/binary"
	"unicode/utf8"
)

// Tunnel frame types of oi.tunnel.v1 (B1 §5). Each WebSocket binary
// message carries exactly one frame: a type byte followed by its payload.
const (
	FrameReady byte = 0x01
	FrameData  byte = 0x02
	FrameFin   byte = 0x03
	FrameAbort byte = 0x04

	// MaxAbortDetail bounds the UTF-8 explanation carried by ABORT.
	MaxAbortDetail = 128
)

// Frame is a decoded tunnel frame. Data aliases the decoded message.
type Frame struct {
	Type   byte
	Data   []byte
	Reason Code   // ABORT only; empty when ReasonValue is unknown
	Value  uint16 // ABORT only: raw reason value
	Detail string // ABORT only
}

// ReadyFrame encodes READY.
func ReadyFrame() []byte { return []byte{FrameReady} }

// FinFrame encodes FIN.
func FinFrame() []byte { return []byte{FrameFin} }

// DataFrame encodes DATA; callers keep len(p) within 1..F.
func DataFrame(p []byte) []byte {
	b := make([]byte, 1+len(p))
	b[0] = FrameData
	copy(b[1:], p)
	return b
}

// AbortFrame encodes ABORT. Codes without an ABORT value are sent as
// ProtocolError; detail is cut to MaxAbortDetail bytes on a rune boundary.
func AbortFrame(code Code, detail string) []byte {
	v, ok := AbortReason(code)
	if !ok {
		v, _ = AbortReason(CodeProtocolError)
	}
	detail = TruncateUTF8(detail, MaxAbortDetail)
	b := make([]byte, 3+len(detail))
	b[0] = FrameAbort
	binary.BigEndian.PutUint16(b[1:3], v)
	copy(b[3:], detail)
	return b
}

// TruncateUTF8 cuts s to at most n bytes without splitting a rune and
// replaces invalid sequences, so the result is always valid UTF-8.
func TruncateUTF8(s string, n int) string {
	if !utf8.ValidString(s) {
		s = string([]rune(s))
	}
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// DecodeFrame validates one binary message against B1 §5. It returns a
// *Error with CodePayloadTooLarge when DATA exceeds dataMax and
// CodeProtocolError for every other violation.
func DecodeFrame(msg []byte, dataMax int) (Frame, error) {
	if len(msg) == 0 {
		return Frame{}, Errorf(CodeProtocolError, "empty frame")
	}
	f := Frame{Type: msg[0]}
	payload := msg[1:]
	switch f.Type {
	case FrameReady, FrameFin:
		if len(payload) != 0 {
			return Frame{}, Errorf(CodeProtocolError, "frame 0x%02x carries a payload", f.Type)
		}
	case FrameData:
		if len(payload) == 0 {
			return Frame{}, Errorf(CodeProtocolError, "empty DATA")
		}
		if len(payload) > dataMax {
			return Frame{}, Errorf(CodePayloadTooLarge, "DATA of %d bytes exceeds %d", len(payload), dataMax)
		}
		f.Data = payload
	case FrameAbort:
		if len(payload) < 2 || len(payload)-2 > MaxAbortDetail || !utf8.Valid(payload[2:]) {
			return Frame{}, Errorf(CodeProtocolError, "malformed ABORT")
		}
		f.Value = binary.BigEndian.Uint16(payload[:2])
		f.Reason, _ = AbortCode(f.Value)
		f.Detail = string(payload[2:])
	default:
		return Frame{}, Errorf(CodeProtocolError, "unknown frame type 0x%02x", f.Type)
	}
	return f, nil
}
