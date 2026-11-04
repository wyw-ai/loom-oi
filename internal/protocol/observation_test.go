package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func wireBinding() ObservationBinding {
	return ObservationBinding{AccessID: "lab-a", Epoch: 1, Generation: 2, ExposureID: "lab-a/http", Revision: "rv_" + strings.Repeat("a", 26), ObservationRevision: "ob_1", ContractDigest: strings.Repeat("b", 64), SourceID: "local", ContractVersion: 1}
}
func wireUnsupported() ObservationBinding {
	b := wireBinding()
	b.SourceID = ""
	b.ContractVersion = 0
	b.ContractDigest = NullObservationDigest
	return b
}
func wireLimits() *ObservationLimits {
	return &ObservationLimits{MessageMax: 4096, PendingChecks: 32, QuerySlots: 8, CheckMS: 1000, QueryMS: 1000, WriteMS: 100, RefreshMS: 1000}
}
func wireSample() *uint64 { s := uint64(1); return &s }
func wireSnapshot() *ObservationSnapshot {
	return &ObservationSnapshot{QueryID: strings.Repeat("1", 32), Binding: wireBinding(), UpdateSeq: 3, Attempt: ObservationAttempt{Seq: 1, State: "idle", Outcome: "matched_ready_status", DurationMS: 10}, SampleSeq: wireSample(), Readiness: ReadinessReady, Reason: "matched_ready_status", RemainingMS: 1000}
}
func wireResult() *AdmissionResult {
	return &AdmissionResult{CheckID: strings.Repeat("2", 32), Binding: wireBinding(), ChannelID: "ch_" + strings.Repeat("b", 26), DecisionID: strings.Repeat("3", 32), EffectiveAdmission: AdmissionRequireReady, Readiness: ReadinessReady, Reason: "matched_ready_status", SampleSeq: wireSample(), RemainingMS: 1000}
}
func wireMessages() []Message {
	b := wireBinding()
	p := validPrepare()
	p.ChannelID = "ch_" + strings.Repeat("b", 26)
	p.Generation = 2
	p.Binding = &b
	p.EffectiveAdmission = AdmissionRequireReady
	return []Message{
		&Hello{OICVersion: "test", LastEpochSeen: 0},
		&Welcome{Epoch: 1, Generation: 2, RunID: "run", HeartbeatIntervalMS: 1000, LicenseTTLMS: 3000, Limits: WelcomeLimits{DataMax: 65536, CloseGraceMS: 1000, DescMax: 4096, MaxExposures: 16}, ObservationLimits: wireLimits()},
		&Register{Exposures: []RegisterExposure{{ExposureID: b.ExposureID, Revision: b.Revision, Protocol: "tcp", TransportProfile: TransportTCP, AdmissionMode: AdmissionBestEffort, Description: "test", ProbeSupport: ProbeSupported, Binding: &b}}},
		&RegisterAck{Accepted: []string{b.ExposureID}}, p,
		&PrepareFailed{ChannelID: p.ChannelID, Code: CodeReadinessCheckFailed, Reason: "timeout"},
		&CloseChannel{ChannelID: p.ChannelID, Code: CodeRevoked}, &Heartbeat{LicenseTTLMS: 3000}, &HeartbeatAck{AckSeq: 1}, &ControlError{Code: CodeProtocolError},
		&ObservationChanged{Binding: b, UpdateSeq: 1}, &ObservationQuery{QueryID: strings.Repeat("1", 32), Binding: b, DeadlineMS: 500}, wireSnapshot(),
		&AdmissionCheck{CheckID: strings.Repeat("2", 32), Stage: CheckFinal, Binding: b, EffectiveAdmission: AdmissionRequireReady, DeadlineMS: 500, ChannelID: p.ChannelID}, wireResult(),
	}
}
func wireObject(t testing.TB, m Message) map[string]any {
	t.Helper()
	var o map[string]any
	if err := json.Unmarshal(EncodeControl(m, 1), &o); err != nil {
		t.Fatal(err)
	}
	return o
}
func wireBytes(t testing.TB, o map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func wireReject(t testing.TB, data []byte) {
	t.Helper()
	if m, err := DecodeControlForVersion(data, ControlSubprotocolV2); m != nil || CodeOf(err) != CodeProtocolError {
		t.Fatalf("accepted invalid wire: %s -> %#v %v", data, m, err)
	}
}
func wireParent(o map[string]any, path string) (map[string]any, string) {
	p := strings.Split(path, ".")
	for _, k := range p[:len(p)-1] {
		if k == "exposures" {
			o = o[k].([]any)[0].(map[string]any)
		} else {
			o = o[k].(map[string]any)
		}
	}
	return o, p[len(p)-1]
}

func TestControlV2RoundTripAndOpaqueExtensions(t *testing.T) {
	for _, m := range wireMessages() {
		t.Run(typeOf(m), func(t *testing.T) {
			data := EncodeControl(m, math.MaxUint64)
			got, err := DecodeControlForVersion(data, ControlSubprotocolV2)
			if err != nil || !reflect.DeepEqual(m, got) || ControlHeader(got).Seq != math.MaxUint64 {
				t.Fatalf("%#v -> %#v %v", m, got, err)
			}
			o := wireObject(t, m)
			o["future"] = map[string]any{"SEQ": nil, "binding": []any{"opaque"}}
			if b, ok := o["binding"].(map[string]any); ok {
				b["future"] = map[string]any{"ACCESS_ID": "ignored"}
			}
			got, err = DecodeControlForVersion(wireBytes(t, o), ControlSubprotocolV2)
			if err != nil || !reflect.DeepEqual(m, got) {
				t.Fatalf("unknown extension changed known fields: %#v %v", got, err)
			}
		})
	}
}

func TestControlV2RequiredPresenceAndCanonicalNames(t *testing.T) {
	required := map[string][]string{
		MsgWelcome:        {"observation_limits", "observation_limits.message_max", "observation_limits.pending_checks", "observation_limits.query_slots", "observation_limits.check_ms", "observation_limits.query_ms", "observation_limits.write_ms", "observation_limits.refresh_ms"},
		MsgRegister:       {"exposures", "exposures.binding", "exposures.probe_support", "exposures.admission_mode"},
		MsgPrepareChannel: {"binding", "effective_admission"}, MsgObservationChanged: {"binding", "update_seq"},
		MsgObservationQuery:    {"query_id", "binding", "deadline_ms"},
		MsgObservationSnapshot: {"query_id", "binding", "update_seq", "attempt", "attempt.seq", "attempt.state", "attempt.outcome", "attempt.duration_ms", "readiness", "reason", "remaining_ms"},
		MsgAdmissionCheck:      {"check_id", "stage", "binding", "effective_admission", "deadline_ms"},
		MsgAdmissionResult:     {"check_id", "binding", "effective_admission", "readiness", "reason", "remaining_ms"},
	}
	for _, m := range wireMessages() {
		fields := append([]string{"type", "seq"}, required[typeOf(m)]...)
		for _, prefix := range []string{"binding", "exposures.binding"} {
			o := wireObject(t, m)
			present := o["binding"] != nil
			if prefix == "exposures.binding" {
				present = typeOf(m) == MsgRegister
			}
			if !present {
				continue
			}
			for _, f := range []string{"access_id", "epoch", "generation", "exposure_id", "revision", "observation_revision", "contract_digest", "source_id", "contract_version"} {
				fields = append(fields, prefix+"."+f)
			}
		}
		for _, field := range fields {
			for _, mutation := range []string{"missing", "null", "wrong_type", "alias_only", "alias_override"} {
				t.Run(typeOf(m)+"/"+field+"/"+mutation, func(t *testing.T) {
					o := wireObject(t, m)
					p, k := wireParent(o, field)
					old := p[k]
					switch mutation {
					case "missing":
						delete(p, k)
					case "null":
						p[k] = nil
					case "wrong_type":
						p[k] = true
					case "alias_only":
						delete(p, k)
						p[strings.ToUpper(k)] = old
					case "alias_override":
						p[strings.ToUpper(k)] = old
					}
					wireReject(t, wireBytes(t, o))
				})
			}
		}
	}
	// Legacy envelope fields are canonical too: a valid alias must not overwrite
	// a separately validated canonical numeric value, including nested structs.
	for _, tc := range []struct {
		index int
		path  string
	}{{0, "last_epoch_seen"}, {1, "epoch"}, {1, "limits.data_max"}, {2, "exposures.revision"}, {5, "code"}} {
		m := wireMessages()[tc.index]
		o := wireObject(t, m)
		p, k := wireParent(o, tc.path)
		p[strings.ToUpper(k)] = p[k]
		wireReject(t, wireBytes(t, o))
	}
}

func TestControlV2OptionalNullAndMalformedJSON(t *testing.T) {
	for _, tc := range []struct {
		m      Message
		fields []string
	}{
		{wireSnapshot(), []string{"sample_seq", "observed_at"}},
		{wireMessages()[5], []string{"reason"}},
		{&AdmissionCheck{CheckID: strings.Repeat("2", 32), Stage: CheckPreflight, Binding: wireBinding(), EffectiveAdmission: AdmissionBestEffort, DeadlineMS: 1}, []string{"channel_id"}},
		{wireResult(), []string{"channel_id", "decision_id", "sample_seq", "code"}},
	} {
		for _, f := range tc.fields {
			t.Run(typeOf(tc.m)+"/"+f, func(t *testing.T) { o := wireObject(t, tc.m); o[f] = nil; wireReject(t, wireBytes(t, o)) })
		}
	}
	base := EncodeControl(wireSnapshot(), 1)
	for _, data := range [][]byte{nil, []byte(`[]`), []byte(`null`), []byte(`true`), []byte(`{"type":"unknown","seq":1}`), append(append([]byte{}, base...), []byte(` {}`)...), bytes.Replace(base, []byte(`"seq":1`), []byte(`"seq":1,"seq":2`), 1), bytes.Replace(base, []byte(`"seq":1`), []byte(`"seq":18446744073709551616`), 1), bytes.Replace(base, []byte(`"seq":1`), []byte(`"seq":1.5`), 1)} {
		wireReject(t, data)
	}
	if _, err := DecodeControlForVersion(base, "oi.control.v999"); CodeOf(err) != CodeVersionUnsupported {
		t.Fatal(err)
	}
}

func TestControlV2ObservationCombinations(t *testing.T) {
	// Running and failed attempts preserve the independent valid previous sample.
	for _, state := range []string{ReadinessReady, ReadinessNotReady, ReadinessUnknown} {
		for _, a := range []ObservationAttempt{{Seq: 0, State: "idle"}, {Seq: 2, State: "running"}, {Seq: 2, State: "idle", Outcome: "timeout", DurationMS: 123}} {
			s := wireSnapshot()
			s.Attempt = a
			s.Readiness = state
			switch state {
			case ReadinessNotReady:
				s.Reason = "matched_not_ready_status"
			case ReadinessUnknown:
				s.SampleSeq = nil
				s.RemainingMS = 0
				s.Reason = "expired"
			}
			if a.Seq == 0 && state != ReadinessUnknown {
				continue
			}
			if _, err := DecodeControlForVersion(EncodeControl(s, 1), ControlSubprotocolV2); err != nil {
				t.Fatalf("valid independent attempt/sample rejected: %#v %v", s, err)
			}
		}
	}
	for _, outcome := range []string{"matched_ready_status", "matched_not_ready_status", "timeout", "permission_error", "tls_error", "address_denied", "connect_error", "status_unmapped", "malformed_response", "source_invalid"} {
		s := wireSnapshot()
		s.Attempt = ObservationAttempt{Seq: 2, State: "idle", Outcome: outcome}
		if _, err := DecodeControlForVersion(EncodeControl(s, 1), ControlSubprotocolV2); err != nil {
			t.Fatal(outcome, err)
		}
	}
	for _, edit := range []func(*ObservationSnapshot){
		func(s *ObservationSnapshot) { s.Readiness = "Ready" }, func(s *ObservationSnapshot) { s.Reason = "timeout" }, func(s *ObservationSnapshot) { s.RemainingMS = 0 }, func(s *ObservationSnapshot) { s.RemainingMS = -1 }, func(s *ObservationSnapshot) { s.SampleSeq = nil }, func(s *ObservationSnapshot) { *s.SampleSeq = 0 }, func(s *ObservationSnapshot) { s.Binding = wireUnsupported() },
		func(s *ObservationSnapshot) { s.Readiness = ReadinessUnknown; s.Reason = "expired"; s.RemainingMS = 0 }, func(s *ObservationSnapshot) {
			s.Readiness = ReadinessUnknown
			s.Reason = "timeout"
			s.SampleSeq = nil
			s.RemainingMS = 0
		},
		func(s *ObservationSnapshot) { s.UpdateSeq = 0 }, func(s *ObservationSnapshot) { s.QueryID = "bad" }, func(s *ObservationSnapshot) { s.Attempt.State = "unknown" }, func(s *ObservationSnapshot) { s.Attempt.DurationMS = -1 }, func(s *ObservationSnapshot) { s.Attempt.Outcome = "random_error" }, func(s *ObservationSnapshot) { s.Attempt.State = "running" }, func(s *ObservationSnapshot) { s.Attempt = ObservationAttempt{State: "running"} }, func(s *ObservationSnapshot) { s.Attempt = ObservationAttempt{State: "idle", Outcome: "timeout"} },
	} {
		s := wireSnapshot()
		edit(s)
		wireReject(t, EncodeControl(s, 1))
	}
	s := wireSnapshot()
	s.Binding = wireUnsupported()
	s.Attempt = ObservationAttempt{State: "idle"}
	s.SampleSeq = nil
	s.Readiness = ReadinessUnknown
	s.Reason = "unsupported"
	s.RemainingMS = 0
	if _, err := DecodeControlForVersion(EncodeControl(s, 1), ControlSubprotocolV2); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"no_sample", "expired", "source_invalid"} {
		s.Reason = reason
		wireReject(t, EncodeControl(s, 1))
	}
	s = wireSnapshot()
	now := time.Unix(123, 0).UTC()
	s.ObservedAt = &now
	if _, err := DecodeControlForVersion(EncodeControl(s, 1), ControlSubprotocolV2); err != nil {
		t.Fatal(err)
	}
}

func TestControlV2AdmissionCombinations(t *testing.T) {
	for _, mode := range []string{AdmissionBestEffort, AdmissionRequireReady} {
		for _, stage := range []string{CheckPreflight, CheckFinal} {
			c := &AdmissionCheck{CheckID: strings.Repeat("2", 32), Stage: stage, Binding: wireBinding(), EffectiveAdmission: mode, DeadlineMS: 1}
			if stage == CheckFinal {
				c.ChannelID = NewID("ch_")
			}
			if _, err := DecodeControlForVersion(EncodeControl(c, 1), ControlSubprotocolV2); err != nil {
				t.Fatal(err)
			}
			for _, edit := range []func(*AdmissionCheck){func(c *AdmissionCheck) { c.Stage = "future" }, func(c *AdmissionCheck) { c.EffectiveAdmission = "" }, func(c *AdmissionCheck) { c.DeadlineMS = 0 }, func(c *AdmissionCheck) {
				if stage == CheckFinal {
					c.ChannelID = ""
				} else {
					c.ChannelID = NewID("ch_")
				}
			}, func(c *AdmissionCheck) { c.CheckID = strings.Repeat("A", 32) }} {
				bad := *c
				edit(&bad)
				wireReject(t, EncodeControl(&bad, 1))
			}
		}
	}
	for _, code := range []Code{"", CodeTargetNotReady, CodeReadinessRequired, CodeReadinessCheckFailed, CodeRevoked, CodeRevisionChanged, CodeAccessOffline, CodeLicenseExpired, CodePrepareTimeout, CodeLocalDenied} {
		for _, final := range []bool{false, true} {
			r := wireResult()
			r.Code = code
			if !final {
				r.ChannelID = ""
			}
			if code != "" || !final {
				r.DecisionID = ""
			}
			switch code {
			case CodeTargetNotReady:
				r.Readiness = ReadinessNotReady
				r.Reason = "matched_not_ready_status"
			case CodeReadinessRequired:
				r.Readiness = ReadinessUnknown
				r.Reason = "no_sample"
				r.SampleSeq = nil
				r.RemainingMS = 0
			case CodeReadinessCheckFailed:
				r.Readiness = ReadinessUnknown
				r.Reason = "timeout"
				r.SampleSeq = nil
				r.RemainingMS = 0
			}
			if _, err := DecodeControlForVersion(EncodeControl(r, 1), ControlSubprotocolV2); err != nil {
				t.Fatalf("valid %s final=%v: %v", code, final, err)
			}
			if code != "" {
				r.DecisionID = strings.Repeat("3", 32)
				wireReject(t, EncodeControl(r, 1))
			}
		}
	}
	for _, reason := range []string{"timeout", "unavailable", "observation_changed"} {
		r := wireResult()
		r.Code = CodeReadinessCheckFailed
		r.DecisionID = ""
		r.SampleSeq = nil
		r.RemainingMS = 0
		r.Readiness = ReadinessUnknown
		r.Reason = reason
		if _, err := DecodeControlForVersion(EncodeControl(r, 1), ControlSubprotocolV2); err != nil {
			t.Fatal(err)
		}
	}
	for _, edit := range []func(*AdmissionResult){func(r *AdmissionResult) { r.DecisionID = "" }, func(r *AdmissionResult) { r.ChannelID = "" }, func(r *AdmissionResult) { r.ChannelID = "bad" }, func(r *AdmissionResult) { r.DecisionID = "bad" }, func(r *AdmissionResult) { r.EffectiveAdmission = "future" }, func(r *AdmissionResult) { r.Code = "future"; r.DecisionID = "" }, func(r *AdmissionResult) { r.Readiness = ReadinessNotReady; r.Reason = "matched_not_ready_status" }, func(r *AdmissionResult) {
		r.Readiness = ReadinessUnknown
		r.Reason = "no_sample"
		r.SampleSeq = nil
		r.RemainingMS = 0
	}, func(r *AdmissionResult) { r.Code = CodeReadinessCheckFailed; r.DecisionID = "" }, func(r *AdmissionResult) { r.Code = CodeReadinessRequired; r.DecisionID = "" }, func(r *AdmissionResult) { r.Code = CodeTargetNotReady; r.DecisionID = "" }} {
		r := wireResult()
		edit(r)
		wireReject(t, EncodeControl(r, 1))
	}
	r := wireResult()
	r.Binding = wireUnsupported()
	r.Readiness = ReadinessUnknown
	r.Reason = "unsupported"
	r.SampleSeq = nil
	r.RemainingMS = 0
	r.EffectiveAdmission = AdmissionBestEffort
	if _, err := DecodeControlForVersion(EncodeControl(r, 1), ControlSubprotocolV2); err != nil {
		t.Fatal("unsupported best effort final must remain possible", err)
	}
	r.Code = CodeReadinessRequired
	r.DecisionID = ""
	wireReject(t, EncodeControl(r, 1))
}

func TestControlV2RegistrationAndBinding(t *testing.T) {
	b := wireBinding()
	unsupported := wireUnsupported()
	future := b
	future.ExposureID = "lab-a/other"
	future.ContractVersion = 2
	reg := &Register{Exposures: []RegisterExposure{{ExposureID: b.ExposureID, Revision: b.Revision, AdmissionMode: AdmissionBestEffort, ProbeSupport: ProbeSupported, Binding: &b}, {ExposureID: future.ExposureID, Revision: future.Revision, AdmissionMode: AdmissionBestEffort, ProbeSupport: ProbeSupported, Binding: &future}, {ExposureID: unsupported.ExposureID, Revision: unsupported.Revision, AdmissionMode: AdmissionBestEffort, ProbeSupport: ProbeUnsupported, Binding: &unsupported}}}
	m, err := DecodeControlForVersion(EncodeControl(reg, 1), ControlSubprotocolV2)
	if err != nil || !reflect.DeepEqual(reg, m) {
		t.Fatalf("unknown positive version must reach per-entry receiver rejection, without discarding siblings: %v", err)
	}
	for _, edit := range []func(*ObservationBinding){func(b *ObservationBinding) { b.AccessID = "other" }, func(b *ObservationBinding) { b.ExposureID = "bad" }, func(b *ObservationBinding) { b.Epoch = 0 }, func(b *ObservationBinding) { b.Generation = 0 }, func(b *ObservationBinding) { b.Revision = "bad" }, func(b *ObservationBinding) { b.ObservationRevision = "ob_01" }, func(b *ObservationBinding) { b.ContractDigest = strings.Repeat("A", 64) }, func(b *ObservationBinding) { b.SourceID = "remote" }, func(b *ObservationBinding) { b.ContractVersion = 0 }, func(b *ObservationBinding) { b.SourceID = "" }} {
		bad := b
		edit(&bad)
		if ValidObservationBinding(bad, true) {
			t.Fatalf("accepted %#v", bad)
		}
	}
	if ValidObservationBinding(future, false) || !ValidObservationBinding(unsupported, false) {
		t.Fatal("registration-only version exception lost")
	}
	for _, field := range []string{"source_id", "contract_version"} {
		o := wireObject(t, &ObservationChanged{Binding: unsupported, UpdateSeq: 1})
		o["binding"].(map[string]any)[field] = nil
		wireReject(t, wireBytes(t, o))
	}
	for _, edit := range []func(*RegisterExposure){func(e *RegisterExposure) { e.ProbeSupport = "unknown" }, func(e *RegisterExposure) { e.ProbeSupport = ProbeUnsupported }, func(e *RegisterExposure) { e.ExposureID = "lab-a/other" }, func(e *RegisterExposure) { e.Revision = "rv_" + strings.Repeat("b", 26) }, func(e *RegisterExposure) { e.AdmissionMode = "" }} {
		e := reg.Exposures[0]
		edit(&e)
		wireReject(t, EncodeControl(&Register{Exposures: []RegisterExposure{e}}, 1))
	}
	for _, value := range []string{"", "ob_0", "ob_01", "ob_+1", "OB_1", "ob_18446744073709551616", "ob_-1"} {
		if ValidObservationRevision(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"ob_1", "ob_18446744073709551615"} {
		if !ValidObservationRevision(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"", strings.Repeat("1", 31), strings.Repeat("A", 32), strings.Repeat("1", 33)} {
		if ValidControlID(value) {
			t.Fatal(value)
		}
	}
	for _, edit := range []func(*PrepareChannel){func(p *PrepareChannel) { p.Generation++ }, func(p *PrepareChannel) { p.Epoch++ }, func(p *PrepareChannel) { p.ExposureID = "lab-a/other" }, func(p *PrepareChannel) { p.Revision = "rv_" + strings.Repeat("b", 26) }, func(p *PrepareChannel) { p.EffectiveAdmission = "unknown" }} {
		p := wireMessages()[4].(*PrepareChannel)
		edit(p)
		wireReject(t, EncodeControl(p, 1))
	}
}

func TestObservationLimitsValidation(t *testing.T) {
	for _, field := range []string{"message_max", "pending_checks", "query_slots", "check_ms", "query_ms", "write_ms", "refresh_ms"} {
		for _, bad := range []int{0, -1} {
			w := wireMessages()[1]
			o := wireObject(t, w)
			o["observation_limits"].(map[string]any)[field] = bad
			wireReject(t, wireBytes(t, o))
		}
	}
	for field, value := range map[string]int{"message_max": 4097, "write_ms": 101, "refresh_ms": 1001} {
		w := wireMessages()[1]
		o := wireObject(t, w)
		o["observation_limits"].(map[string]any)[field] = value
		wireReject(t, wireBytes(t, o))
	}
}

func TestControlV1IgnoresV2FieldsExactlyAsUnknownExtensions(t *testing.T) {
	for _, tc := range []struct {
		index  int
		fields []string
	}{{1, []string{"observation_limits"}}, {2, []string{"exposures.binding", "exposures.probe_support"}}, {4, []string{"binding", "effective_admission"}}, {5, []string{"reason"}}} {
		m := wireMessages()[tc.index]
		base := wireObject(t, m)
		for _, field := range tc.fields {
			p, k := wireParent(base, field)
			delete(p, k)
		}
		expected, err := DecodeControl(wireBytes(t, base))
		if err != nil {
			t.Fatal(err)
		}
		for _, field := range tc.fields {
			for _, value := range []any{nil, true, 23, "unknown", []any{true}, map[string]any{"source_id": "remote"}} {
				for _, alias := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%T/alias=%v", typeOf(m), field, value, alias), func(t *testing.T) {
						o := wireObject(t, expected)
						p, k := wireParent(o, field)
						if alias {
							k = strings.ToUpper(k)
						}
						p[k] = value
						for _, decode := range []func([]byte) (Message, error){DecodeControl, func(b []byte) (Message, error) { return DecodeControlForVersion(b, ControlSubprotocol) }} {
							got, err := decode(wireBytes(t, o))
							if err != nil || !reflect.DeepEqual(expected, got) {
								t.Fatalf("v1 extension was used or rejected: %#v != %#v: %v", expected, got, err)
							}
						}
					})
				}
			}
		}
	}
	// Existing v1 known-field case/duplicate handling remains last-wins, unlike v2.
	data := []byte(`{"type":"heartbeat","seq":1,"license_ttl_ms":5,"LICENSE_TTL_MS":7,"license_ttl_ms":9}`)
	got, err := DecodeControl(data)
	if err != nil || got.(*Heartbeat).LicenseTTLMS != 9 {
		t.Fatal(got, err)
	}
	for _, raw := range []string{`{"type":"register","seq":1}`, `{"type":"register","seq":1,"exposures":null}`, `{"type":"register","seq":1,"exposures":[]}`, `{"type":"register","seq":1,"exposures":[null]}`} {
		m, err := DecodeControl([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeControl(EncodeControl(m, 1)); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range wireMessages()[10:] {
		if _, err := DecodeControlForVersion(EncodeControl(m, 1), ControlSubprotocol); CodeOf(err) != CodeProtocolError {
			t.Fatalf("v1 accepted %T", m)
		}
	}
	if _, err := DecodeControl([]byte(`{"type":"register","seq":1,"exposures":true}`)); CodeOf(err) != CodeProtocolError {
		t.Fatal("legacy known-field wrong type accepted", err)
	}
}

// Called for every input by the existing FuzzDecodeControl runner, before its
// v1 branch can return. All legacy seeds and properties remain in that root.
func assertControlV2FuzzProperties(t *testing.T, data []byte) {
	t.Helper()
	m, err := DecodeControlForVersion(data, ControlSubprotocolV2)
	if err != nil {
		if m != nil || CodeOf(err) != CodeProtocolError {
			t.Fatalf("partial/incorrect error: %#v %v", m, err)
		}
		return
	}
	if ControlHeader(m).Seq == 0 || !validV2Message(m) {
		t.Fatal("accepted invalid message")
	}
	again, err := DecodeControlForVersion(EncodeControl(m, ControlHeader(m).Seq), ControlSubprotocolV2)
	if err != nil || !reflect.DeepEqual(m, again) {
		t.Fatalf("accepted message cannot round trip: %s %#v %v", data, again, err)
	}
}
