package protocol

import (
	"encoding/json"
)

// Control message types of oi.control.v1 (B1 §4).
const (
	MsgHello          = "hello"
	MsgWelcome        = "welcome"
	MsgRegister       = "register"
	MsgRegisterAck    = "register_ack"
	MsgPrepareChannel = "prepare_channel"
	MsgPrepareFailed  = "prepare_failed"
	MsgCloseChannel   = "close_channel"
	MsgHeartbeat      = "heartbeat"
	MsgHeartbeatAck   = "heartbeat_ack"
	MsgError          = "error"
)

// Header is common to every control message; seq increases from 1 per
// sender and is used for diagnostics and acknowledgements.
type Header struct {
	Type string `json:"type"`
	Seq  uint64 `json:"seq"`
}

func (h *Header) header() *Header { return h }

// Message is any control message.
type Message interface{ header() *Header }

// Hello is the first OIC message. Identity comes only from mTLS.
type Hello struct {
	Header
	OICVersion    string `json:"oic_version"`
	LastEpochSeen int64  `json:"last_epoch_seen"`
}

// MaxEpoch bounds how far a hello may raise the epoch: OIS refuses a
// last_epoch_seen above its own epoch and at or beyond this value, so one
// Access cannot push the epoch near int64 overflow (B3 §6). Epochs OIS
// issued itself are valid whatever their size.
const MaxEpoch int64 = 1 << 62

// WelcomeLimits are the OIS limits an OIC needs.
type WelcomeLimits struct {
	DataMax      int   `json:"data_max"`
	CloseGraceMS int64 `json:"close_grace_ms"`
	DescMax      int   `json:"desc_max"`
	MaxExposures int   `json:"max_exposures"`
}

// Welcome assigns the connection its epoch and generation.
type Welcome struct {
	Header
	Epoch               int64         `json:"epoch"`
	Generation          int64         `json:"generation"`
	RunID               string        `json:"run_id"`
	HeartbeatIntervalMS int64         `json:"heartbeat_interval_ms"`
	LicenseTTLMS        int64         `json:"license_ttl_ms"`
	Limits              WelcomeLimits `json:"limits"`
}

// RegisterExposure is one registered exposure; it never carries a target.
type RegisterExposure struct {
	ExposureID       string `json:"exposure_id"`
	Revision         string `json:"revision"`
	Protocol         string `json:"protocol"`
	TransportProfile string `json:"transport_profile"`
	AdmissionMode    string `json:"admission_mode"`
	Description      string `json:"description"`
}

// Register is a full snapshot that replaces the Access's previous one.
type Register struct {
	Header
	Exposures []RegisterExposure `json:"exposures"`
}

// Rejection explains why one registered entry was refused.
type Rejection struct {
	ExposureID string `json:"exposure_id"`
	Code       Code   `json:"code"`
}

// RegisterAck lists accepted and rejected entries.
type RegisterAck struct {
	Header
	Accepted []string    `json:"accepted"`
	Rejected []Rejection `json:"rejected"`
}

// PrepareChannel asks the OIC to re-check locally and dial.
type PrepareChannel struct {
	Header
	ChannelID      string `json:"channel_id"`
	ExposureID     string `json:"exposure_id"`
	Revision       string `json:"revision"`
	Subject        string `json:"subject"`
	Epoch          int64  `json:"epoch"`
	Generation     int64  `json:"generation"`
	PrepareTTLMS   int64  `json:"prepare_ttl_ms"`
	ProviderTicket string `json:"provider_ticket"`
}

// PrepareFailed reports a local re-check, dial or quota failure.
type PrepareFailed struct {
	Header
	ChannelID string `json:"channel_id"`
	Code      Code   `json:"code"`
}

// CloseChannel is idempotent; unknown channels are ignored.
type CloseChannel struct {
	Header
	ChannelID string `json:"channel_id"`
	Code      Code   `json:"code"`
}

// Heartbeat renews the OIC's local license.
type Heartbeat struct {
	Header
	LicenseTTLMS int64 `json:"license_ttl_ms"`
}

// HeartbeatAck acknowledges a heartbeat by its seq.
type HeartbeatAck struct {
	Header
	AckSeq uint64 `json:"ack_seq"`
}

// ControlError reports a protocol error; the sender then closes.
type ControlError struct {
	Header
	Code   Code   `json:"code"`
	Detail string `json:"detail,omitempty"`
}

var messageTypes = map[string]func() Message{
	MsgHello:          func() Message { return &Hello{} },
	MsgWelcome:        func() Message { return &Welcome{} },
	MsgRegister:       func() Message { return &Register{} },
	MsgRegisterAck:    func() Message { return &RegisterAck{} },
	MsgPrepareChannel: func() Message { return &PrepareChannel{} },
	MsgPrepareFailed:  func() Message { return &PrepareFailed{} },
	MsgCloseChannel:   func() Message { return &CloseChannel{} },
	MsgHeartbeat:      func() Message { return &Heartbeat{} },
	MsgHeartbeatAck:   func() Message { return &HeartbeatAck{} },
	MsgError:          func() Message { return &ControlError{} },
}

func typeOf(m Message) string {
	switch m.(type) {
	case *Hello:
		return MsgHello
	case *Welcome:
		return MsgWelcome
	case *Register:
		return MsgRegister
	case *RegisterAck:
		return MsgRegisterAck
	case *PrepareChannel:
		return MsgPrepareChannel
	case *PrepareFailed:
		return MsgPrepareFailed
	case *CloseChannel:
		return MsgCloseChannel
	case *Heartbeat:
		return MsgHeartbeat
	case *HeartbeatAck:
		return MsgHeartbeatAck
	default:
		return MsgError
	}
}

// EncodeControl stamps type and seq on m and marshals it. The message types
// contain only strings, numbers and slices, so marshalling cannot fail.
func EncodeControl(m Message, seq uint64) []byte {
	h := m.header()
	h.Type = typeOf(m)
	h.Seq = seq
	b, _ := json.Marshal(m)
	return b
}

// DecodeControl decodes one control message leniently (unknown fields are
// ignored, B1 §2 rule 2) and validates its structure. Unknown message types
// are protocol errors: new semantics require a new subprotocol.
func DecodeControl(data []byte) (Message, error) {
	var h Header
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, Errorf(CodeProtocolError, "control message is not a JSON object")
	}
	mk, ok := messageTypes[h.Type]
	if !ok {
		return nil, Errorf(CodeProtocolError, "unknown control message type %q", h.Type)
	}
	m := mk()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, Errorf(CodeProtocolError, "malformed %s message", h.Type)
	}
	if h.Seq == 0 {
		return nil, Errorf(CodeProtocolError, "%s without seq", h.Type)
	}
	if !validControl(m) {
		return nil, Errorf(CodeProtocolError, "invalid %s message", h.Type)
	}
	return m, nil
}

func validControl(m Message) bool {
	switch v := m.(type) {
	case *Hello:
		return len(v.OICVersion) <= 64 && v.LastEpochSeen >= 0
	case *Welcome:
		return v.Epoch > 0 && v.Generation > 0 && v.HeartbeatIntervalMS > 0 && v.LicenseTTLMS > 0 && v.Limits.DataMax > 0
	case *PrepareChannel:
		_, _, ok := SplitExposureID(v.ExposureID)
		return ok && ValidChannelID(v.ChannelID) && ValidRevision(v.Revision) && ValidSubject(v.Subject) &&
			v.Epoch > 0 && v.Generation > 0 && v.PrepareTTLMS > 0 && v.ProviderTicket != "" && len(v.ProviderTicket) <= 128
	case *PrepareFailed:
		return ValidChannelID(v.ChannelID) && v.Code != ""
	case *CloseChannel:
		return ValidChannelID(v.ChannelID)
	case *Heartbeat:
		return v.LicenseTTLMS > 0
	case *HeartbeatAck:
		return v.AckSeq > 0
	case *ControlError:
		return v.Code != ""
	default: // *Register, *RegisterAck: entries are validated by the receiver
		return true
	}
}
