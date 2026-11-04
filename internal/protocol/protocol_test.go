package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHTTPStatusTable(t *testing.T) {
	cases := map[Code]int{
		CodeInvalidRequest: 400, CodeVersionUnsupported: 400, CodePayloadTooLarge: 413,
		CodeUnauthenticated: 401, CodeTicketInvalid: 401, CodeDenied: 403, CodeNotFound: 404,
		CodeRevisionMismatch: 409, CodeIdempotencyConflict: 409, CodeTicketConsumed: 409,
		CodeVersionConflict: 409, CodeChannelGone: 410, CodeReadinessRequired: 412,
		CodePreconditionRequired: 428, CodeResourceExhausted: 429, CodeTargetNotReady: 503,
		CodeAccessOffline: 503, CodeStoreUnavailable: 503, CodeStoreBusy: 503,
		CodeRevoked: http.StatusInternalServerError, CodeInternal: 500,
	}
	for c, want := range cases {
		if got := c.HTTPStatus(); got != want {
			t.Errorf("%s: got %d want %d", c, got, want)
		}
	}
}

func TestAbortTableRoundTrip(t *testing.T) {
	want := map[Code]uint16{
		CodeUnauthenticated: 0x0101, CodeDenied: 0x0102, CodeRevisionChanged: 0x0103,
		CodeTargetNotReady: 0x0104, CodeAccessOffline: 0x0105, CodeRevoked: 0x0106,
		CodeTargetConnectFailed: 0x0201, CodeLocalDenied: 0x0202, CodeTargetAddressNotAllowed: 0x0203,
		CodeResourceExhausted: 0x0204, CodePayloadTooLarge: 0x0301, CodeProtocolError: 0x0302,
		CodeChannelInterrupted: 0x0401, CodeLicenseExpired: 0x0402, CodeIdleTimeout: 0x0403,
		CodeClosedByClient: 0x0404, CodePrepareTimeout: 0x0405, CodeShutdown: 0x0406,
	}
	if len(want) != len(abortReasons) {
		t.Fatalf("table has %d entries, B1 §7 lists %d", len(abortReasons), len(want))
	}
	for c, v := range want {
		got, ok := AbortReason(c)
		if !ok || got != v {
			t.Errorf("AbortReason(%s)=%#x,%v", c, got, ok)
		}
		back, ok := AbortCode(v)
		if !ok || back != c {
			t.Errorf("AbortCode(%#x)=%s,%v", v, back, ok)
		}
	}
	if _, ok := AbortReason(CodeNotFound); ok {
		t.Error("NotFound must not be an ABORT reason")
	}
	if _, ok := AbortCode(0xffff); ok {
		t.Error("unknown reason accepted")
	}
}

func TestErrorHelpers(t *testing.T) {
	e := Errorf(CodeDenied, "subject %s", "a")
	if e.Error() != "Denied: subject a" {
		t.Fatal(e.Error())
	}
	if (&Error{Code: CodeDenied}).Error() != "Denied" {
		t.Fatal("bare code text")
	}
	if CodeOf(fmt.Errorf("wrap: %w", e)) != CodeDenied {
		t.Fatal("CodeOf through wrap")
	}
	if CodeOf(errors.New("plain")) != CodeInternal {
		t.Fatal("plain error must be Internal")
	}
}

func TestFrameEncodeDecode(t *testing.T) {
	f, err := DecodeFrame(ReadyFrame(), 8)
	if err != nil || f.Type != FrameReady {
		t.Fatalf("ready: %+v %v", f, err)
	}
	f, err = DecodeFrame(FinFrame(), 8)
	if err != nil || f.Type != FrameFin {
		t.Fatalf("fin: %+v %v", f, err)
	}
	f, err = DecodeFrame(DataFrame([]byte("abc")), 8)
	if err != nil || f.Type != FrameData || string(f.Data) != "abc" {
		t.Fatalf("data: %+v %v", f, err)
	}
	f, err = DecodeFrame(AbortFrame(CodeRevoked, "policy"), 8)
	if err != nil || f.Type != FrameAbort || f.Reason != CodeRevoked || f.Value != 0x0106 || f.Detail != "policy" {
		t.Fatalf("abort: %+v %v", f, err)
	}
	// Non-ABORT codes are sent as ProtocolError.
	f, _ = DecodeFrame(AbortFrame(CodeNotFound, ""), 8)
	if f.Reason != CodeProtocolError {
		t.Fatalf("fallback reason %s", f.Reason)
	}
	// Unknown reason values decode with an empty Reason but keep the value.
	f, err = DecodeFrame([]byte{FrameAbort, 0x7f, 0x01}, 8)
	if err != nil || f.Reason != "" || f.Value != 0x7f01 {
		t.Fatalf("unknown reason: %+v %v", f, err)
	}
}

func TestFrameViolations(t *testing.T) {
	long := append([]byte{FrameAbort, 0x01, 0x01}, bytes.Repeat([]byte("x"), MaxAbortDetail+1)...)
	cases := []struct {
		name string
		msg  []byte
		code Code
	}{
		{"empty", nil, CodeProtocolError},
		{"unknown", []byte{0x09}, CodeProtocolError},
		{"ready payload", []byte{FrameReady, 1}, CodeProtocolError},
		{"fin payload", []byte{FrameFin, 1}, CodeProtocolError},
		{"empty data", []byte{FrameData}, CodeProtocolError},
		{"oversize data", DataFrame(make([]byte, 9)), CodePayloadTooLarge},
		{"short abort", []byte{FrameAbort, 1}, CodeProtocolError},
		{"long abort", long, CodeProtocolError},
		{"bad utf8 abort", []byte{FrameAbort, 1, 1, 0xff}, CodeProtocolError},
	}
	for _, c := range cases {
		_, err := DecodeFrame(c.msg, 8)
		if CodeOf(err) != c.code {
			t.Errorf("%s: got %v want %s", c.name, err, c.code)
		}
	}
}

func TestTruncateUTF8(t *testing.T) {
	if got := TruncateUTF8("abc", 10); got != "abc" {
		t.Fatal(got)
	}
	s := strings.Repeat("中", 50) // 3 bytes each
	got := TruncateUTF8(s, 128)
	if len(got) != 126 || !utf8.ValidString(got) {
		t.Fatalf("len %d valid %v", len(got), utf8.ValidString(got))
	}
	if got := TruncateUTF8("a\xffb", 10); !utf8.ValidString(got) {
		t.Fatalf("invalid input not repaired: %q", got)
	}
	d, err := DecodeFrame(AbortFrame(CodeShutdown, s), 1)
	if err != nil || len(d.Detail) > MaxAbortDetail {
		t.Fatalf("abort detail not bounded: %v %d", err, len(d.Detail))
	}
}

func TestIdentifiers(t *testing.T) {
	ok := []struct {
		s string
		f func(string) bool
	}{
		{"lab-a", ValidAccessID}, {"http", ValidName}, {"pol-1", ValidPolicyID},
		{"rv_abcdefghijklmnopqrstuvwxyz", ValidRevision}, {"ch_abcdefghijklmnopqrstuv2345", ValidChannelID},
		{"alice@example.com", ValidSubject}, {"http", ValidProtocol}, {"0123456789abcdef", ValidTokenID},
		{"req-1:2", ValidClientRequestID},
	}
	for _, c := range ok {
		if !c.f(c.s) {
			t.Errorf("%q rejected", c.s)
		}
	}
	for _, s := range []string{"", "-a", "A", strings.Repeat("a", 64)} {
		if ValidAccessID(s) {
			t.Errorf("access id %q accepted", s)
		}
	}
	if ValidRevision("rv_ABC") || ValidChannelID("ch_1") || ValidTokenID("xyz") || ValidSubject("") ||
		ValidProtocol("HTTP") || ValidClientRequestID("") || ValidClientRequestID("a b") || ValidName("/x") {
		t.Error("invalid identifier accepted")
	}
	a, n, ok2 := SplitExposureID("lab-a/http")
	if !ok2 || a != "lab-a" || n != "http" {
		t.Fatal("split")
	}
	for _, s := range []string{"lab-a", "lab-a/", "/http", "LAB/http", "lab-a/http/x"} {
		if _, _, ok := SplitExposureID(s); ok {
			t.Errorf("%q split", s)
		}
	}
	if !ValidAdmission(AdmissionBestEffort) || !ValidAdmission(AdmissionRequireReady) || ValidAdmission("x") {
		t.Error("admission validation")
	}
	if StricterAdmission(AdmissionBestEffort, AdmissionBestEffort) != AdmissionBestEffort ||
		StricterAdmission(AdmissionBestEffort, AdmissionRequireReady) != AdmissionRequireReady ||
		StricterAdmission(AdmissionRequireReady, "") != AdmissionRequireReady {
		t.Error("stricter admission")
	}
	id := NewID("ch_")
	if !ValidChannelID(id) || NewID("ch_") == id {
		t.Fatalf("NewID %q", id)
	}
	if len(RandomBytes(32)) != 32 {
		t.Fatal("RandomBytes")
	}
}

func validPrepare() *PrepareChannel {
	return &PrepareChannel{ChannelID: NewID("ch_"), ExposureID: "lab-a/http", Revision: "rv_" + strings.Repeat("a", 26),
		Subject: "alice", Epoch: 1, Generation: 1, PrepareTTLMS: 1000, ProviderTicket: "tk_x"}
}

func TestControlRoundTrip(t *testing.T) {
	msgs := []Message{
		&Hello{OICVersion: "v1", LastEpochSeen: 3},
		&Welcome{Epoch: 2, Generation: 1, RunID: "run_x", HeartbeatIntervalMS: 10, LicenseTTLMS: 30, Limits: WelcomeLimits{DataMax: 32}},
		&Register{Exposures: []RegisterExposure{{ExposureID: "lab-a/http"}}},
		&RegisterAck{Accepted: []string{"lab-a/http"}, Rejected: []Rejection{{ExposureID: "x", Code: CodeDenied}}},
		validPrepare(),
		&PrepareFailed{ChannelID: NewID("ch_"), Code: CodeTargetConnectFailed},
		&CloseChannel{ChannelID: NewID("ch_"), Code: CodeRevoked},
		&Heartbeat{LicenseTTLMS: 30000},
		&HeartbeatAck{AckSeq: 7},
		&ControlError{Code: CodeSuperseded, Detail: "newer connection"},
	}
	for i, m := range msgs {
		data := EncodeControl(m, uint64(i+1))
		got, err := DecodeControl(data)
		if err != nil {
			t.Fatalf("%T: %v (%s)", m, err, data)
		}
		if got.header().Seq != uint64(i+1) || got.header().Type != m.header().Type {
			t.Fatalf("%T header %+v", m, got.header())
		}
		a, _ := json.Marshal(m)
		b, _ := json.Marshal(got)
		if !bytes.Equal(a, b) {
			t.Fatalf("%T round trip\n%s\n%s", m, a, b)
		}
	}
}

func TestControlLenientUnknownFields(t *testing.T) {
	m, err := DecodeControl([]byte(`{"type":"heartbeat","seq":1,"license_ttl_ms":5,"future":"x"}`))
	if err != nil || m.(*Heartbeat).LicenseTTLMS != 5 {
		t.Fatalf("%v %v", m, err)
	}
}

func TestControlRejects(t *testing.T) {
	bad := []string{
		`[]`, `{"type":"nope","seq":1}`, `{"type":"heartbeat","seq":"x"}`,
		`{"type":"heartbeat","license_ttl_ms":5}`, `{"type":"heartbeat","seq":1}`,
		`{"type":"hello","seq":1,"last_epoch_seen":-1}`,
		`{"type":"welcome","seq":1,"epoch":1,"generation":1,"heartbeat_interval_ms":1,"license_ttl_ms":1}`,
		`{"type":"prepare_failed","seq":1,"channel_id":"x","code":"A"}`,
		`{"type":"close_channel","seq":1,"channel_id":"bad"}`,
		`{"type":"heartbeat_ack","seq":1}`, `{"type":"error","seq":1}`,
	}
	for _, s := range bad {
		if _, err := DecodeControl([]byte(s)); CodeOf(err) != CodeProtocolError {
			t.Errorf("%s: %v", s, err)
		}
	}
	p := validPrepare()
	p.ProviderTicket = ""
	if _, err := DecodeControl(EncodeControl(p, 1)); err == nil {
		t.Error("prepare without ticket accepted")
	}
	p = validPrepare()
	p.ExposureID = "bad"
	if _, err := DecodeControl(EncodeControl(p, 1)); err == nil {
		t.Error("prepare with bad exposure accepted")
	}
}

type sample struct {
	A string `json:"a"`
	B []int  `json:"b"`
}

func TestDecodeStrict(t *testing.T) {
	var s sample
	if err := DecodeStrict(strings.NewReader(`{"a":"x","b":[1,2]}`), 100, &s); err != nil || s.A != "x" || len(s.B) != 2 {
		t.Fatalf("%v %+v", err, s)
	}
	cases := map[string]Code{
		`{"a":"x","c":1}`:       CodeInvalidRequest,
		`{"a":"x","a":"y"}`:     CodeInvalidRequest,
		`{"a":"x"} {"a":"y"}`:   CodeInvalidRequest,
		`{"a":`:                 CodeInvalidRequest,
		`{"b":[{"x":1,"x":2}]}`: CodeInvalidRequest,
		strings.Repeat("[", 40): CodeInvalidRequest,
		`{"a":"` + strings.Repeat("y", 200) + `"}`: CodePayloadTooLarge,
	}
	for body, code := range cases {
		if err := DecodeStrict(strings.NewReader(body), 100, &sample{}); CodeOf(err) != code {
			t.Errorf("%.30s: %v want %s", body, err, code)
		}
	}
	if err := DecodeStrict(errReader{}, 100, &s); CodeOf(err) != CodeInvalidRequest {
		t.Errorf("read error: %v", err)
	}
	if err := DecodeStrictBytes([]byte(`{"a":"x"}x`), &s); CodeOf(err) != CodeInvalidRequest {
		t.Errorf("trailing garbage: %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func FuzzDecodeFrame(f *testing.F) {
	f.Add(ReadyFrame())
	f.Add(DataFrame([]byte("hello")))
	f.Add(FinFrame())
	f.Add(AbortFrame(CodeRevoked, "x"))
	f.Fuzz(func(t *testing.T, msg []byte) {
		fr, err := DecodeFrame(msg, 64)
		if err != nil {
			if c := CodeOf(err); c != CodeProtocolError && c != CodePayloadTooLarge {
				t.Fatalf("unexpected code %s", c)
			}
			return
		}
		if fr.Type == FrameData && (len(fr.Data) == 0 || len(fr.Data) > 64) {
			t.Fatalf("data length %d accepted", len(fr.Data))
		}
		if fr.Type == FrameAbort && (len(fr.Detail) > MaxAbortDetail || !utf8.ValidString(fr.Detail)) {
			t.Fatalf("abort detail accepted: %q", fr.Detail)
		}
	})
}

func FuzzDecodeControl(f *testing.F) {
	f.Add(EncodeControl(&Hello{OICVersion: "v"}, 1))
	f.Add(EncodeControl(validPrepare(), 2))
	f.Add(EncodeControl(&Register{Exposures: []RegisterExposure{{ExposureID: "a/b"}}}, 3))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := DecodeControl(data)
		if err != nil {
			if CodeOf(err) != CodeProtocolError {
				t.Fatalf("unexpected code %v", err)
			}
			return
		}
		if m.header().Seq == 0 {
			t.Fatal("accepted message without seq")
		}
		again, err := DecodeControl(EncodeControl(m, m.header().Seq))
		if err != nil || again.header().Type != m.header().Type {
			t.Fatalf("re-encode failed: %v", err)
		}
	})
}
