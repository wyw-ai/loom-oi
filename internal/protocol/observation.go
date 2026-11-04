package protocol

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ControlSubprotocolV2   = "oi.control.v2"
	ProbeSupported         = "Supported"
	ProbeUnsupported       = "Unsupported"
	CheckPreflight         = "preflight"
	CheckFinal             = "final"
	MsgObservationChanged  = "observation_changed"
	MsgObservationQuery    = "observation_query"
	MsgObservationSnapshot = "observation_snapshot"
	MsgAdmissionCheck      = "admission_check"
	MsgAdmissionResult     = "admission_result"
	// NullObservationDigest is SHA256 of the canonical JSON null contract.
	NullObservationDigest = "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b"
)

// ObservationBinding is complete on every v2 message. It is comparable and
// never filled from a previous registration by the decoder.
type ObservationBinding struct {
	AccessID            string `json:"access_id"`
	Epoch               int64  `json:"epoch"`
	Generation          int64  `json:"generation"`
	ExposureID          string `json:"exposure_id"`
	Revision            string `json:"revision"`
	ObservationRevision string `json:"observation_revision"`
	ContractDigest      string `json:"contract_digest"`
	SourceID            string `json:"source_id,omitempty"`
	ContractVersion     int    `json:"contract_version,omitempty"`
}

var controlIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
var contractDigestRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ValidControlID(id string) bool { return controlIDRe.MatchString(id) }

func ValidObservationRevision(s string) bool {
	if !strings.HasPrefix(s, "ob_") {
		return false
	}
	n, err := strconv.ParseUint(s[3:], 10, 64)
	return err == nil && n > 0 && s == "ob_"+strconv.FormatUint(n, 10)
}

// ValidObservationBinding checks structure. Unknown positive contract versions
// are allowed only during registration, where the receiver rejects that entry.
// Session identity and the original exposure fields must also be checked there.
func ValidObservationBinding(b ObservationBinding, registering bool) bool {
	a, _, ok := SplitExposureID(b.ExposureID)
	if !ok || a != b.AccessID || b.Epoch <= 0 || b.Generation <= 0 ||
		!ValidRevision(b.Revision) || !ValidObservationRevision(b.ObservationRevision) ||
		!contractDigestRe.MatchString(b.ContractDigest) {
		return false
	}
	if b.SourceID == "" {
		return b.ContractVersion == 0 && b.ContractDigest == NullObservationDigest
	}
	return b.SourceID == "local" && b.ContractVersion > 0 && (registering || b.ContractVersion == 1)
}

type ObservationLimits struct {
	MessageMax    int   `json:"message_max"`
	PendingChecks int   `json:"pending_checks"`
	QuerySlots    int   `json:"query_slots"`
	CheckMS       int64 `json:"check_ms"`
	QueryMS       int64 `json:"query_ms"`
	WriteMS       int64 `json:"write_ms"`
	RefreshMS     int64 `json:"refresh_ms"`
}

func (l ObservationLimits) Valid() bool {
	return l.MessageMax > 0 && l.MessageMax <= 4096 && l.PendingChecks > 0 &&
		l.QuerySlots > 0 && l.CheckMS > 0 && l.QueryMS > 0 &&
		l.WriteMS > 0 && l.WriteMS <= 100 && l.RefreshMS > 0 && l.RefreshMS <= 1000
}

// ObservationAttempt keeps attempt status separate from a still-valid sample.
// The first idle state has seq=0 and outcome=""; all issued attempts have seq>0.
type ObservationAttempt struct {
	Seq        uint64 `json:"seq"`
	State      string `json:"state"`
	Outcome    string `json:"outcome"`
	DurationMS int64  `json:"duration_ms"`
}

type ObservationChanged struct {
	Header
	Binding   ObservationBinding `json:"binding"`
	UpdateSeq uint64             `json:"update_seq"`
}
type ObservationQuery struct {
	Header
	QueryID    string             `json:"query_id"`
	Binding    ObservationBinding `json:"binding"`
	DeadlineMS int64              `json:"deadline_ms"`
}
type ObservationSnapshot struct {
	Header
	QueryID     string             `json:"query_id"`
	Binding     ObservationBinding `json:"binding"`
	UpdateSeq   uint64             `json:"update_seq"`
	Attempt     ObservationAttempt `json:"attempt"`
	SampleSeq   *uint64            `json:"sample_seq,omitempty"`
	Readiness   string             `json:"readiness"`
	Reason      string             `json:"reason"`
	RemainingMS int64              `json:"remaining_ms"`
	ObservedAt  *time.Time         `json:"observed_at,omitempty"`
}
type AdmissionCheck struct {
	Header
	CheckID            string             `json:"check_id"`
	Stage              string             `json:"stage"`
	Binding            ObservationBinding `json:"binding"`
	EffectiveAdmission string             `json:"effective_admission"`
	DeadlineMS         int64              `json:"deadline_ms"`
	ChannelID          string             `json:"channel_id,omitempty"`
}
type AdmissionResult struct {
	Header
	CheckID            string             `json:"check_id"`
	Binding            ObservationBinding `json:"binding"`
	ChannelID          string             `json:"channel_id,omitempty"`
	DecisionID         string             `json:"decision_id,omitempty"`
	EffectiveAdmission string             `json:"effective_admission"`
	Readiness          string             `json:"readiness"`
	Reason             string             `json:"reason"`
	SampleSeq          *uint64            `json:"sample_seq,omitempty"`
	RemainingMS        int64              `json:"remaining_ms"`
	Code               Code               `json:"code,omitempty"`
}

type ReadinessView struct {
	Authority           string             `json:"authority"`
	Source              string             `json:"source,omitempty"`
	Revision            string             `json:"revision"`
	Generation          int64              `json:"generation"`
	ObservationRevision string             `json:"observation_revision,omitempty"`
	ContractDigest      string             `json:"contract_digest,omitempty"`
	SampleSeq           *uint64            `json:"sample_seq,omitempty"`
	Reason              string             `json:"reason"`
	RemainingMS         int64              `json:"remaining_ms"`
	LastAttempt         ObservationAttempt `json:"last_attempt"`
	LastReported        *ReadinessReport   `json:"last_reported,omitempty"`
}

// LastReported is historical; it never grants a fresh current lifetime.
type ReadinessReport struct {
	Readiness  string     `json:"readiness"`
	SampleSeq  uint64     `json:"sample_seq"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}
type ReadinessDecision struct {
	EffectiveAdmission string  `json:"effective_admission"`
	Readiness          string  `json:"readiness"`
	Source             string  `json:"source"`
	SampleSeq          *uint64 `json:"sample_seq,omitempty"`
	Reason             string  `json:"reason"`
}

func ControlHeader(m Message) Header { return *m.header() }

func validAdmission(s string) bool { return s == AdmissionBestEffort || s == AdmissionRequireReady }

func validAttemptReason(s string) bool {
	switch s {
	case "matched_ready_status", "matched_not_ready_status", "timeout", "permission_error", "tls_error",
		"address_denied", "connect_error", "status_unmapped", "malformed_response", "source_invalid":
		return true
	}
	return false
}

func validAttempt(a ObservationAttempt) bool {
	if a.DurationMS < 0 {
		return false
	}
	if a.State == "running" {
		return a.Seq > 0 && a.Outcome == "" && a.DurationMS == 0
	}
	return a.State == "idle" && ((a.Seq == 0 && a.Outcome == "" && a.DurationMS == 0) ||
		(a.Seq > 0 && validAttemptReason(a.Outcome)))
}

func validReadiness(b ObservationBinding, readiness, reason string, sample *uint64, remaining int64) bool {
	if remaining < 0 {
		return false
	}
	switch readiness {
	case ReadinessReady, ReadinessNotReady:
		want := "matched_ready_status"
		if readiness == ReadinessNotReady {
			want = "matched_not_ready_status"
		}
		return b.SourceID == "local" && b.ContractVersion == 1 && sample != nil && *sample > 0 && remaining > 0 && reason == want
	case ReadinessUnknown:
		if sample != nil || remaining != 0 {
			return false
		}
		if b.SourceID == "" {
			return reason == "unsupported"
		}
		return reason == "no_sample" || reason == "expired" || reason == "source_invalid"
	}
	return false
}

func validV2Message(m Message) bool {
	switch v := m.(type) {
	case *Welcome:
		return v.ObservationLimits != nil && v.ObservationLimits.Valid()
	case *Register:
		for _, e := range v.Exposures {
			if e.Binding == nil || !ValidObservationBinding(*e.Binding, true) ||
				e.ExposureID != e.Binding.ExposureID || e.Revision != e.Binding.Revision || !validAdmission(e.AdmissionMode) {
				return false
			}
			if e.ProbeSupport != ProbeSupported && e.ProbeSupport != ProbeUnsupported {
				return false
			}
			if (e.ProbeSupport == ProbeSupported) != (e.Binding.SourceID != "") {
				return false
			}
		}
		return true
	case *PrepareChannel:
		return v.Binding != nil && ValidObservationBinding(*v.Binding, false) && validAdmission(v.EffectiveAdmission) &&
			v.ExposureID == v.Binding.ExposureID && v.Revision == v.Binding.Revision &&
			v.Epoch == v.Binding.Epoch && v.Generation == v.Binding.Generation
	case *ObservationChanged:
		return ValidObservationBinding(v.Binding, false) && v.UpdateSeq > 0
	case *ObservationQuery:
		return ValidObservationBinding(v.Binding, false) && ValidControlID(v.QueryID) && v.DeadlineMS > 0
	case *ObservationSnapshot:
		return ValidObservationBinding(v.Binding, false) && ValidControlID(v.QueryID) && v.UpdateSeq > 0 &&
			validAttempt(v.Attempt) && validReadiness(v.Binding, v.Readiness, v.Reason, v.SampleSeq, v.RemainingMS)
	case *AdmissionCheck:
		return ValidObservationBinding(v.Binding, false) && ValidControlID(v.CheckID) && validAdmission(v.EffectiveAdmission) && v.DeadlineMS > 0 &&
			((v.Stage == CheckPreflight && v.ChannelID == "") || (v.Stage == CheckFinal && ValidChannelID(v.ChannelID)))
	case *AdmissionResult:
		if !ValidObservationBinding(v.Binding, false) || !ValidControlID(v.CheckID) || !validAdmission(v.EffectiveAdmission) ||
			(v.ChannelID != "" && !ValidChannelID(v.ChannelID)) {
			return false
		}
		if v.Code == CodeReadinessCheckFailed {
			return v.DecisionID == "" && v.Readiness == ReadinessUnknown && v.SampleSeq == nil && v.RemainingMS == 0 &&
				(v.Reason == "timeout" || v.Reason == "unavailable" || v.Reason == "observation_changed")
		}
		if !validReadiness(v.Binding, v.Readiness, v.Reason, v.SampleSeq, v.RemainingMS) {
			return false
		}
		if v.Code != "" {
			return v.DecisionID == "" && ((v.Code == CodeTargetNotReady && v.Readiness == ReadinessNotReady) ||
				(v.Code == CodeReadinessRequired && v.Readiness == ReadinessUnknown && v.EffectiveAdmission == AdmissionRequireReady) ||
				v.Code == CodeRevoked || v.Code == CodeRevisionChanged || v.Code == CodeAccessOffline || v.Code == CodeLicenseExpired || v.Code == CodePrepareTimeout || v.Code == CodeLocalDenied)
		}
		if v.Readiness == ReadinessNotReady || (v.EffectiveAdmission == AdmissionRequireReady && v.Readiness != ReadinessReady) {
			return false
		}
		return (v.ChannelID == "" && v.DecisionID == "") || (v.ChannelID != "" && ValidControlID(v.DecisionID))
	}
	return true
}

// requiredObject checks exact field spelling, presence and JSON null separately
// from Go zero values. Extensions remain lenient; duplicate keys are rejected.
func requiredObject(data []byte, fields ...string) (map[string]json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil || obj == nil {
		return nil, false
	}
	for _, f := range fields {
		v, ok := obj[f]
		if !ok || string(v) == "null" {
			return nil, false
		}
	}
	return obj, true
}

// encoding/json accepts case-insensitive aliases and lets the last alias win.
// In v2 a noncanonical spelling of a known field is not an unknown extension:
// it must never override the canonical safety field that presence validated.
// Unknown extensions remain opaque; their contents do not become wire fields.
func canonicalControlFields(data []byte, typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() == reflect.Slice {
		var values []json.RawMessage
		if json.Unmarshal(data, &values) != nil {
			return false
		}
		for _, value := range values {
			if !canonicalControlFields(value, typ.Elem()) {
				return false
			}
		}
		return true
	}
	if typ.Kind() != reflect.Struct || typ == reflect.TypeFor[time.Time]() {
		return true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return false
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			if !canonicalControlFields(data, field.Type) {
				return false
			}
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		for key, value := range obj {
			if strings.EqualFold(key, name) && (key != name || !canonicalControlFields(value, field.Type)) {
				return false
			}
		}
	}
	return true
}

func optionalNonNull(o map[string]json.RawMessage, fields ...string) bool {
	for _, field := range fields {
		if value, exists := o[field]; exists && string(value) == "null" {
			return false
		}
	}
	return true
}

func bindingPresent(data []byte) bool {
	o, ok := requiredObject(data, "access_id", "epoch", "generation", "exposure_id", "revision", "observation_revision", "contract_digest")
	if !ok {
		return false
	}
	var b ObservationBinding
	if json.Unmarshal(data, &b) != nil {
		return false
	}
	_, source := o["source_id"]
	_, version := o["contract_version"]
	if b.SourceID == "" {
		return !source && !version
	}
	_, ok = requiredObject(data, "source_id", "contract_version")
	return ok
}

func v2Presence(data []byte, m Message) bool {
	o, ok := requiredObject(data, "type", "seq")
	if !ok {
		return false
	}
	require := func(fields ...string) bool { _, ok := requiredObject(data, fields...); return ok }
	switch m.(type) {
	case *Welcome:
		_, ok = requiredObject(o["observation_limits"], "message_max", "pending_checks", "query_slots", "check_ms", "query_ms", "write_ms", "refresh_ms")
		return ok
	case *Register:
		var es []json.RawMessage
		if !require("exposures") || json.Unmarshal(o["exposures"], &es) != nil {
			return false
		}
		for _, e := range es {
			x, yes := requiredObject(e, "binding", "probe_support", "admission_mode")
			if !yes || !bindingPresent(x["binding"]) {
				return false
			}
		}
	case *PrepareChannel:
		return require("binding", "effective_admission") && bindingPresent(o["binding"])
	case *PrepareFailed:
		return optionalNonNull(o, "reason")
	case *ObservationChanged:
		return require("binding", "update_seq") && bindingPresent(o["binding"])
	case *ObservationQuery:
		return require("query_id", "binding", "deadline_ms") && bindingPresent(o["binding"])
	case *ObservationSnapshot:
		_, ok = requiredObject(o["attempt"], "seq", "state", "outcome", "duration_ms")
		return ok && require("query_id", "binding", "update_seq", "attempt", "readiness", "reason", "remaining_ms") &&
			bindingPresent(o["binding"]) && optionalNonNull(o, "sample_seq", "observed_at")
	case *AdmissionCheck:
		return require("check_id", "stage", "binding", "effective_admission", "deadline_ms") && bindingPresent(o["binding"]) && optionalNonNull(o, "channel_id")
	case *AdmissionResult:
		return require("check_id", "binding", "effective_admission", "readiness", "reason", "remaining_ms") &&
			bindingPresent(o["binding"]) && optionalNonNull(o, "channel_id", "decision_id", "sample_seq", "code")
	}
	return true
}

// DecodeControlForVersion keeps v1 decoding and its message vocabulary frozen.
// Identity, negotiated limits, temporal validity and pending request ownership
// are checked by the session coordinator after this structural validation.
func DecodeControlForVersion(data []byte, version string) (Message, error) {
	if version == ControlSubprotocol {
		return DecodeControl(data)
	}
	if version != ControlSubprotocolV2 {
		return nil, Errorf(CodeVersionUnsupported, "unsupported control version")
	}
	if err := checkDuplicates(data); err != nil {
		return nil, Errorf(CodeProtocolError, "invalid control JSON")
	}
	var h Header
	if json.Unmarshal(data, &h) != nil {
		return nil, Errorf(CodeProtocolError, "malformed control message")
	}
	var m Message
	switch h.Type {
	case MsgObservationChanged:
		m = &ObservationChanged{}
	case MsgObservationQuery:
		m = &ObservationQuery{}
	case MsgObservationSnapshot:
		m = &ObservationSnapshot{}
	case MsgAdmissionCheck:
		m = &AdmissionCheck{}
	case MsgAdmissionResult:
		m = &AdmissionResult{}
	default:
		var err error
		m, err = decodeControl(data, false)
		if err != nil {
			return nil, err
		}
	}
	if json.Unmarshal(data, m) != nil || h.Seq == 0 || !canonicalControlFields(data, reflect.TypeOf(m)) || !v2Presence(data, m) || !validV2Message(m) {
		return nil, Errorf(CodeProtocolError, "invalid v2 %s message", h.Type)
	}
	return m, nil
}
