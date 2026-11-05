package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type g2DiagnosticTransport struct {
	calls    int
	response *http.Response
	err      error
}

func (d *g2DiagnosticTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls++
	return d.response, d.err
}

type g2DiagnosticBody struct{ read bool }

func (b *g2DiagnosticBody) Read(p []byte) (int, error) {
	if b.read {
		return 0, io.ErrUnexpectedEOF
	}
	b.read = true
	return copy(p, "partial"), nil
}
func (*g2DiagnosticBody) Close() error { return nil }

func TestG2FailureDiagnosticsCallBoundaries(t *testing.T) {
	for _, mode := range []string{"complete", "no_response", "partial_body", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			body, err := metrics.Render((&metrics.Registry{}).Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			transport := &g2DiagnosticTransport{response: &http.Response{StatusCode: 200,
				Header: http.Header{"Content-Type": []string{metrics.ContentType}}, Body: io.NopCloser(strings.NewReader(string(body)))}}
			sentinel := errors.New("private-token-must-not-be-copied")
			switch mode {
			case "no_response":
				transport.response, transport.err = nil, sentinel
			case "partial_body":
				transport.response.Body = &g2DiagnosticBody{}
			case "canceled":
				transport.response, transport.err = nil, context.Canceled
			}
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "canceled" {
				cancel()
			}
			x, call, gotErr := g2ObserveExposition(ctx, func() (g2Exposition, error) {
				return g2ReadExposition(ctx, &http.Client{Transport: transport}, "127.0.0.1:1")
			})
			cancel()
			if transport.calls != 1 || call.StartedNS <= 0 || call.ReturnedNS < call.StartedNS || x.StartedNS < call.StartedNS {
				t.Fatalf("call sequence/timestamps changed: %+v calls=%d", call, transport.calls)
			}
			if mode == "complete" && (gotErr != nil || x.Status != 200 || len(x.Families) != 13 || call.ErrorClass != "none" || call.ContextState != "active") {
				t.Fatalf("complete response: %+v %v", call, gotErr)
			}
			if mode == "no_response" && (!errors.Is(gotErr, sentinel) || x.CompletedNS != 0 || x.Status != 0 || call.ErrorClass != "other") {
				t.Fatalf("response fabricated: %+v %v", x, gotErr)
			}
			if mode == "partial_body" && (!errors.Is(gotErr, io.ErrUnexpectedEOF) || x.CompletedNS == 0 || x.Body != "partial" || call.ErrorClass != "eof") {
				t.Fatalf("partial response changed: %+v %v", x, gotErr)
			}
			if mode == "canceled" && (!errors.Is(gotErr, context.Canceled) || call.ContextState != "canceled" || call.ErrorClass != "canceled") {
				t.Fatalf("cancel result changed: %+v %v", call, gotErr)
			}
			raw, _ := json.Marshal(call)
			if strings.Contains(string(raw), "private-token") {
				t.Fatal("error text entered diagnostic")
			}
		})
	}
}

func g2FailureExposition(timeout, writeError float64) g2Exposition {
	label, family, kind := "result", "oi_metrics_exports_total", dto.MetricType_COUNTER
	values := []*dto.Metric{}
	for _, x := range []struct {
		name  string
		value float64
	}{{"timeout", timeout}, {"write_error", writeError}} {
		values = append(values, &dto.Metric{Label: []*dto.LabelPair{{Name: &label, Value: g2DiagnosticPtr(x.name)}}, Counter: &dto.Counter{Value: g2DiagnosticPtr(x.value)}})
	}
	return g2Exposition{Status: 200, StartedNS: 11, CompletedNS: 12, Body: "original exposition\n",
		Families: map[string]*dto.MetricFamily{family: {Name: &family, Type: &kind, Metric: values}}}
}

func TestG2FailureDiagnosticsBeforeClose(t *testing.T) {
	for _, mode := range []string{"close_only_failures", "only_429", "spent_stop", "insufficient_before", "no_after", "qualified_before"} {
		t.Run(mode, func(t *testing.T) {
			d := newG2SlowFailure("ois", 0)
			before, after := g2FailureExposition(0, 0), g2FailureExposition(2, 0)
			d.Before = &before
			if mode != "spent_stop" {
				x := g2Exposition{Status: 429, StartedNS: 11, CompletedNS: 12}
				if mode == "insufficient_before" {
					x = g2FailureExposition(1, 0)
				}
				if mode == "qualified_before" {
					x = g2FailureExposition(2, 0)
				}
				index := d.observe("failure_before_close", x, nil, g2DiagnosticCall{StartedNS: 10, ReturnedNS: 13, ErrorClass: "none", ContextState: "active"})
				if mode == "qualified_before" {
					d.BoundedFailure, d.BoundedFailureIndex = &x, index
				}
			}
			d.freeze("condition")
			at := *d.CapturedNS
			if mode != "no_after" {
				d.observe("recovery", after, nil, g2DiagnosticCall{})
				d.After = &after
			}
			d.freeze("later_cleanup")
			if *d.CapturedNS != at || *d.Class != "condition" {
				t.Fatal("later cleanup replaced failure")
			}
			if mode == "qualified_before" {
				if d.BoundedFailure == nil || d.BoundedFailureIndex == nil || d.Observations[*d.BoundedFailureIndex].Phase != "failure_before_close" {
					t.Fatal("real earlier response lost")
				}
			} else if d.BoundedFailure != nil || d.BoundedFailureIndex != nil {
				t.Fatal("after-close counts promoted into bounded proof")
			}
			raw, _ := json.Marshal(d.fields())
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded["native"] != nil || decoded["close_started_ns"] != nil || decoded["input_stop_ns"] != nil {
				t.Fatal("unexecuted actions fabricated")
			}
			if mode == "no_after" && decoded["after"] != nil {
				t.Fatal("missing after became a response")
			}
		})
	}
}

func TestG2FailureDiagnosticsBoundedStorage(t *testing.T) {
	d := newG2SlowFailure("oic", 1)
	x := g2FailureExposition(0, 0)
	for i := 0; i < 128; i++ {
		if d.observe("rejection", x, nil, g2DiagnosticCall{}) == nil {
			t.Fatal("early overflow")
		}
	}
	first := d.Observations[0]
	if d.observe("failure_before_close", x, nil, g2DiagnosticCall{}) != nil || !d.Overflow {
		t.Fatal("input overflow not recorded")
	}
	for i := 0; i < 256; i++ {
		if d.observe("recovery", x, nil, g2DiagnosticCall{}) == nil {
			t.Fatal("recovery capacity borrowed")
		}
	}
	if d.observe("recovery", x, nil, g2DiagnosticCall{}) != nil || len(d.Observations) != 384 || !reflect.DeepEqual(first, d.Observations[0]) {
		t.Fatal("bounded records replaced or grew")
	}
	if d.BoundedFailure != nil {
		t.Fatal("overflow invented acceptance")
	}
}

func TestG2FailureDiagnosticsCleanupOrdering(t *testing.T) {
	for _, mode := range []string{"return", "goexit", "panic", "cleanup_panic"} {
		t.Run(mode, func(t *testing.T) {
			steps := make(chan string, 5)
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() {
					if recover() != nil {
						steps <- "panic_preserved"
					}
				}()
				defer func() {
					p := recover()
					stamp := g2FailureStamp{}
					stamp.freeze("original")
					steps <- "frozen"
					g2FinishDiagnostic(func() {
						steps <- "cleanup_joined"
						if mode == "cleanup_panic" {
							panic("cleanup")
						}
					}, func() {
						if stamp.CapturedNS == nil {
							panic("unfrozen")
						}
						steps <- "emitted"
					})
					if p != nil {
						panic(p)
					}
				}()
				switch mode {
				case "goexit":
					runtime.Goexit()
				case "panic":
					panic("original")
				}
			}()
			<-done
			close(steps)
			var got []string
			for x := range steps {
				got = append(got, x)
			}
			want := []string{"frozen", "cleanup_joined", "emitted"}
			if mode == "panic" || mode == "cleanup_panic" {
				want = append(want, "panic_preserved")
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("order=%v", got)
			}
		})
	}
}

func TestG2FailureDiagnosticsPreparationQueries(t *testing.T) {
	for _, mode := range []string{"exact", "duplicate", "future", "incomplete_batch", "filtered_event", "latest_failure"} {
		t.Run(mode, func(t *testing.T) {
			now := int64(time.Second)
			events := []g2WireEvent{}
			for i := range 8 {
				q := protocol.ObservationQuery{QueryID: fmt.Sprintf("%032x", i+1), Binding: protocol.ObservationBinding{ExposureID: fmt.Sprintf("lab-a/%d", i)}, DeadlineMS: 499}
				raw, _ := json.Marshal(q)
				events = append(events, g2WireEvent{ID: uint64(i + 1), Type: protocol.MsgObservationQuery, Direction: g2OISToOIC, ReceivedNS: now - 1000, WriteStartedNS: now - 900, WrittenNS: now - 800, Raw: raw})
			}
			d := g2PreparationFailure{Round: 2}
			switch mode {
			case "duplicate":
				events[0].Raw = events[1].Raw
			case "future":
				events[0].ReceivedNS = now + 1
			case "incomplete_batch":
				events = events[:7]
			case "filtered_event":
				events[0].Injected = true
			case "latest_failure":
				if !d.young(events, now) {
					t.Fatal("valid setup")
				}
				events[7].ReceivedNS = now - int64(time.Second)
			}
			actual := d.young(events, now)
			if actual != g2CompetitionYoungQueries(events, now, 200*time.Millisecond) || actual != (mode == "exact") {
				t.Fatalf("decision changed %v", actual)
			}
			if len(d.Queries) > 8 || d.Young == nil || *d.Young != actual || *d.EvaluatedNS != now {
				t.Fatal("wrong observation")
			}
			if mode == "latest_failure" && (len(d.Queries) != 1 || d.Queries[0].ReceivedNS != events[7].ReceivedNS) {
				t.Fatal("older successful input substituted")
			}
			if len(d.Queries) > 0 && d.Queries[0].EventID != events[len(events)-1].ID {
				t.Fatal("identity/order replaced")
			}
		})
	}
}

func TestG2FailureDiagnosticsActiveSuccessShape(t *testing.T) {
	x := g2ExportSlotInput{Schema: "g2-export-slot-input-v1", Samples: []g2ExportSlotObservation{{StartedNS: 1, CompletedNS: 2, ReturnedNS: 3, ErrorClass: "none", ContextState: "active"}}, SelectedIndex: g2DiagnosticPtr(0), FailureCapturedNS: g2DiagnosticPtr(int64(4))}
	raw, _ := json.Marshal(x)
	for _, key := range []string{"call_returned_ns", "selected_index", "failure_captured_ns", "context_state"} {
		if strings.Contains(string(raw), key) {
			t.Fatal("success shape changed", key)
		}
	}
	failure, _ := json.Marshal(x.failureFields())
	for _, key := range []string{"call_returned_ns", "selected_index", "failure_captured_ns", "context_state"} {
		if !strings.Contains(string(failure), key) {
			t.Fatal("failure fact missing", key)
		}
	}
}

func TestG2FailureDiagnosticsEntityNoSecrets(t *testing.T) {
	d := g2EntityFailure{Stage: "token_create"}
	d.begin("token_create")
	err := protocol.Errorf(protocol.CodeStoreUnavailable, "private-token-sql-dsn")
	d.returned(context.Background(), g2DiagnosticPtr(503), err)
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "private-token") || d.Call.HTTPStatus == nil || *d.Call.HTTPStatus != 503 || d.Call.Code == nil || *d.Call.Code != protocol.CodeStoreUnavailable || d.CapturedNS == nil {
		t.Fatalf("invalid finite API diagnostic %s", raw)
	}
}

func TestG2FailureDiagnosticsConnectionCleanup(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	joined := make(chan error, 1)
	workerDone := make(chan struct{})
	t.Cleanup(func() { _ = listener.Close(); <-workerDone })
	go func() {
		defer close(workerDone)
		var first error
		for range 2 {
			c, e := listener.Accept()
			if e != nil {
				joined <- e
				return
			}
			_ = c.SetReadDeadline(time.Now().Add(time.Second))
			var b [1]byte
			_, e = c.Read(b[:])
			_ = c.Close()
			if !errors.Is(e, io.EOF) {
				first = e
			}
		}
		joined <- first
	}()
	sentinel := errors.New("original before-write failure")
	stamp := g2FailureStamp{}
	callbackCalls := 0
	s, got := g2BeginSlowScrapesDiagnostic(context.Background(), listener.Addr().String(), &net.Dialer{},
		func(context.Context, *g2SlowScrapes) error { return sentinel },
		func(s *g2SlowScrapes, e error) {
			callbackCalls++
			stamp.freeze("original")
			if !errors.Is(e, sentinel) || len(s.conns) != 2 {
				t.Error("wrong original failure")
			}
			for _, c := range s.conns {
				if e := c.SetReadDeadline(time.Time{}); e != nil {
					t.Error("connection closed before snapshot", e)
				}
			}
		})
	if !errors.Is(got, sentinel) || callbackCalls != 1 || stamp.CapturedNS == nil || s == nil {
		t.Fatal("failure changed", got)
	}
	if err := <-joined; err != nil {
		t.Fatal("owned peer did not close", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("writer cleanup was not joined", err)
	}
}

func TestG2FailureDiagnosticsErrorMetadata(t *testing.T) {
	for _, mode := range []string{"http_error", "abort_error", "uncoded_error", "success"} {
		t.Run(mode, func(t *testing.T) {
			var err error
			status := 0
			var want *protocol.Code
			switch mode {
			case "http_error":
				err, status = protocol.Errorf(protocol.CodeStoreUnavailable, "private detail"), 503
				want = g2DiagnosticPtr(protocol.CodeStoreUnavailable)
			case "abort_error":
				err = &client.AbortError{Code: protocol.CodeChannelInterrupted, Detail: "private detail"}
				want = g2DiagnosticPtr(protocol.CodeChannelInterrupted)
			case "uncoded_error":
				err = io.ErrUnexpectedEOF
			case "success":
				status = 200
			}
			d := g2EntityFailure{}
			d.begin("native")
			d.returned(context.Background(), g2DiagnosticHTTPStatus(status), err)
			if !reflect.DeepEqual(d.Call.Code, want) {
				t.Fatal("actual code lost or invented")
			}
			if (status == 0) != (d.Call.HTTPStatus == nil) {
				t.Fatal("missing HTTP response became a status")
			}
			raw, _ := json.Marshal(d)
			if strings.Contains(string(raw), "private detail") {
				t.Fatal("private detail copied")
			}
		})
	}
}

type g2DiagnosticRoundTripFunc func(*http.Request) (*http.Response, error)

func (f g2DiagnosticRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestG2FailureDiagnosticsStatsFreezeBeforeCancel(t *testing.T) {
	for _, mode := range []string{"ois_error", "oic_error", "partial_body", "success"} {
		t.Run(mode, func(t *testing.T) {
			ois, oic := resourceTestStats()
			oic.Observation = &protocol.ObservationRuntimeStats{ProbeJobs: 16}
			oisJSON, err := json.Marshal(ois)
			if err != nil {
				t.Fatal(err)
			}
			oicJSON, err := json.Marshal(oic)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("original statistics failure")
			var order []string
			var originalContext context.Context
			makeResponse := func(body io.ReadCloser) *http.Response {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
			}
			oisClient := &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				order = append(order, "ois")
				if mode == "ois_error" {
					originalContext = r.Context()
					return nil, sentinel
				}
				return makeResponse(io.NopCloser(strings.NewReader(string(oisJSON)))), nil
			})}
			oicClient := &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				order = append(order, "oic")
				originalContext = r.Context()
				if mode == "oic_error" {
					return nil, sentinel
				}
				if mode == "partial_body" {
					return makeResponse(&g2DiagnosticBody{}), nil
				}
				return makeResponse(io.NopCloser(strings.NewReader(string(oicJSON)))), nil
			})}
			// Same live-process fixture as TestResourceProcessMustBeAlive; this
			// invokes the real helper and both original HTTP seams, no child.
			g := &g2Env{env: &env{t: t, api: "ois.invalid", adminTok: "fixture-only",
				ois:   &proc{cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, exited: make(chan struct{})},
				stats: oisClient, oics: map[string]*oicProc{"lab-a": {diag: "oic.invalid"}}}}
			stamp := g2FailureStamp{}
			var captured g2PreparationRead
			calls := 0
			_, read, gotErr := g2TryCompetitionSampleObserved(g, oicClient, func(ctx context.Context, partial g2PreparationRead, cause error) {
				calls++
				if ctx.Err() != nil || originalContext == nil || originalContext.Err() != nil || partial.ContextState != "active" {
					t.Fatal("original context canceled before failure freeze")
				}
				if mode == "partial_body" {
					if !errors.Is(cause, io.ErrUnexpectedEOF) {
						t.Fatal("partial-body error changed", cause)
					}
				} else if !errors.Is(cause, sentinel) {
					t.Fatal("original error changed", cause)
				}
				captured = partial
				stamp.freeze("stats_error")
				order = append(order, "frozen")
			})
			wantOrder := []string{"ois", "oic", "frozen"}
			if mode == "ois_error" {
				wantOrder = []string{"ois", "frozen"}
			}
			if mode == "success" {
				wantOrder = []string{"ois", "oic"}
			}
			if !reflect.DeepEqual(order, wantOrder) {
				t.Fatalf("requests/freeze order changed: %v", order)
			}
			if mode == "success" {
				if gotErr != nil || calls != 0 || stamp.CapturedNS != nil || read.OIS == nil || read.OIC == nil {
					t.Fatal("success captured as failure", gotErr)
				}
			} else {
				if gotErr == nil || calls != 1 || stamp.CapturedNS == nil || !reflect.DeepEqual(captured, read) || read.ReturnedNS > *stamp.CapturedNS {
					t.Fatal("missing original failure snapshot", gotErr)
				}
				if read.OIC != nil || (mode == "ois_error") != (read.OIS == nil) {
					t.Fatal("partial roles fabricated")
				}
			}
			if originalContext == nil {
				t.Fatal("real request context absent")
			}
			if mode == "ois_error" {
				if originalContext.Err() != nil {
					t.Fatal("background context changed")
				}
			} else if !errors.Is(originalContext.Err(), context.Canceled) {
				t.Fatal("original OIC context not canceled after return")
			}
		})
	}
}

func TestG2FailureDiagnosticsNonFiniteCounters(t *testing.T) {
	for _, mode := range []string{"missing_family", "missing_label", "nan_value", "infinite_value", "finite_value"} {
		t.Run(mode, func(t *testing.T) {
			x := g2FailureExposition(2, 3)
			switch mode {
			case "missing_family":
				delete(x.Families, "oi_metrics_exports_total")
			case "missing_label":
				x.Families["oi_metrics_exports_total"].Metric = nil
			case "nan_value":
				x.Families["oi_metrics_exports_total"].Metric[0].Counter.Value = g2DiagnosticPtr(math.NaN())
			case "infinite_value":
				x.Families["oi_metrics_exports_total"].Metric[0].Counter.Value = g2DiagnosticPtr(math.Inf(1))
			}
			original := g2ExpositionCounter(x, "oi_metrics_exports_total", map[string]string{"result": "timeout"})
			d := newG2SlowFailure("ois", 0)
			d.observe("failure_before_close", x, nil, g2DiagnosticCall{})
			d.freeze("original_failure")
			raw, err := json.Marshal(d.fields())
			if err != nil {
				t.Fatal("failed event cannot be serialized", err)
			}
			var decoded struct {
				Observations []struct {
					Timeout    *float64 `json:"timeout"`
					WriteError *float64 `json:"write_error"`
				} `json:"observations"`
			}
			if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Observations) != 1 {
				t.Fatal("missing failure row", err)
			}
			observed := decoded.Observations[0]
			if mode == "finite_value" {
				if observed.Timeout == nil || *observed.Timeout != 2 || observed.WriteError == nil || *observed.WriteError != 3 {
					t.Fatal("finite count changed")
				}
			} else if observed.Timeout != nil || (!math.IsNaN(original) && !math.IsInf(original, 0)) {
				t.Fatal("unknown diagnostic counter became a number")
			}
			if (mode == "missing_family" || mode == "missing_label") && observed.WriteError != nil {
				t.Fatal("missing write-error count became a number")
			}
			if d.BoundedFailure != nil || d.BoundedFailureIndex != nil {
				t.Fatal("diagnostic repair changed qualification")
			}
		})
	}
}
