package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func g2JointTestStats(t *testing.T, mode string, order *[]string, requestCtx *context.Context) (*g2Env, *http.Client) {
	t.Helper()
	ois, oic := resourceTestStats()
	ois.Observation = &protocol.ObservationRuntimeStats{ExporterActive: 2, ExporterYoungestActiveAgeNS: 41}
	oic.Observation = &protocol.ObservationRuntimeStats{ProbeJobs: 16, ExporterActive: 1, ExporterYoungestActiveAgeNS: 29}
	if mode == "invalid_scheduler" {
		oic.Observation.ProbeJobs = 15
	}
	makeClient := func(role string, stats any) *http.Client {
		raw, err := json.Marshal(stats)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			*order = append(*order, role)
			*requestCtx = r.Context()
			switch mode {
			case role + "_error":
				return nil, io.ErrUnexpectedEOF
			case role + "_panic":
				panic("original panic")
			case role + "_goexit":
				runtime.Goexit()
			}
			var body io.ReadCloser = io.NopCloser(strings.NewReader(string(raw)))
			if role == "oic" && mode == "partial_body" {
				body = &g2DiagnosticBody{}
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body}, nil
		})}
	}
	g := &g2Env{env: &env{t: t, api: "ois.invalid", adminTok: "fixture-only",
		ois:   &proc{cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, exited: make(chan struct{})},
		stats: makeClient("ois", ois), oics: map[string]*oicProc{"lab-a": {diag: "oic.invalid"}}}}
	return g, makeClient("oic", oic)
}

func TestG2JointDiagnosticsRoleBoundaries(t *testing.T) {
	for _, mode := range []string{"success", "ois_error", "oic_error", "partial_body", "invalid_scheduler", "ois_panic", "oic_panic", "oic_goexit"} {
		t.Run(mode, func(t *testing.T) {
			var order []string
			var requestCtx context.Context
			g, hc := g2JointTestStats(t, mode, &order, &requestCtx)
			d := &g2JointCritical{}
			var gotErr error
			var panicValue any
			returned := false
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { panicValue = recover() }()
				_, gotErr = d.readStats(g, hc, false)
				returned = true
			}()
			<-done
			r := d.BeforeStats
			if r == nil || r.OISCall == nil || r.OISCall.StartedNS < r.StartedNS {
				t.Fatal("original first request missing")
			}
			expected := []string{"ois", "oic"}
			if strings.HasPrefix(mode, "ois_") {
				expected = []string{"ois"}
			}
			if !reflect.DeepEqual(order, expected) || d.AfterStats != nil || d.AfterStatsAttempts != 0 {
				t.Fatal("request count/order or unexecuted role changed", order)
			}
			abnormal := strings.HasSuffix(mode, "panic") || strings.HasSuffix(mode, "goexit")
			last := r.OICCall
			if len(expected) == 1 {
				last = r.OISCall
				if r.OICCall != nil || r.OIC != nil || requestCtx.Err() != nil {
					t.Fatal("absent OIC request invented/background canceled")
				}
			} else {
				if last == nil || r.OISCall.ReturnedNS == nil || *r.OISCall.ReturnedNS > last.StartedNS || !errors.Is(requestCtx.Err(), context.Canceled) {
					t.Fatal("role boundary/cancel changed")
				}
				deadline, ok := requestCtx.Deadline()
				if !ok || last.ContextDeadlineNS == nil || *last.ContextDeadlineNS != int64(deadline.Sub(resourceOrigin)) {
					t.Fatal("actual original deadline not retained")
				}
			}
			if abnormal {
				if returned || last.ReturnedNS != nil || last.ErrorClass != nil || last.ContextState != nil || r.ReturnedNS != 0 || r.ErrorClass != "interrupted" || d.capturedNS == nil {
					t.Fatal("abnormal exit became normal return", r)
				}
				if strings.HasSuffix(mode, "panic") && panicValue != "original panic" {
					t.Fatal("panic swallowed", panicValue)
				}
			} else if !returned || last.ReturnedNS == nil || *last.ReturnedNS > r.ReturnedNS || last.ContextState == nil || *last.ContextState != "active" {
				t.Fatal("actual call return lost")
			}
			if r.ContextState != "active" || r.OISCall.ContextDeadlineNS != nil {
				t.Fatal("state observed after cleanup or invented background deadline")
			}
			if mode == "success" {
				if gotErr != nil || d.capturedNS != nil || *last.ErrorClass != "none" {
					t.Fatal("success marked failure", gotErr)
				}
			} else if d.capturedNS == nil || (!abnormal && gotErr == nil) {
				t.Fatal("failure not frozen")
			}
			if mode == "invalid_scheduler" && (*last.ErrorClass != "none" || r.OIC == nil || r.ErrorClass == "none") {
				t.Fatal("scheduler rejection misattributed to HTTP")
			}
		})
	}
}

func TestG2JointDiagnosticsObserverCleanup(t *testing.T) {
	for _, mode := range []string{"observer_panic", "observer_goexit", "ois_error_observer_panic", "ois_error_observer_goexit"} {
		t.Run(mode, func(t *testing.T) {
			var order []string
			var requestCtx context.Context
			oisError := strings.HasPrefix(mode, "ois_error_")
			input := "oic_panic"
			if oisError {
				input = "ois_error"
			}
			g, hc := g2JointTestStats(t, input, &order, &requestCtx)
			var observedActive bool
			var observedClasses []string
			var observedErrors []error
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { _ = recover() }()
				g2TryCompetitionSampleObserved(g, hc, func(ctx context.Context, read g2PreparationRead, err error) {
					observedClasses = append(observedClasses, read.ErrorClass)
					observedErrors = append(observedErrors, err)
					if oisError {
						observedActive = ctx.Err() == nil && read.OISCall != nil && read.OISCall.ReturnedNS != nil && read.OICCall == nil
					} else {
						observedActive = ctx.Err() == nil && read.OICCall != nil && read.OICCall.ReturnedNS == nil
					}
					if strings.HasSuffix(mode, "goexit") {
						runtime.Goexit()
					}
					panic("observer panic")
				})
			}()
			<-done
			if oisError {
				if !observedActive || requestCtx == nil || requestCtx.Err() != nil || !reflect.DeepEqual(order, []string{"ois"}) || !reflect.DeepEqual(observedClasses, []string{"eof"}) || len(observedErrors) != 1 || !errors.Is(observedErrors[0], io.ErrUnexpectedEOF) {
					t.Fatal("OIS observer notified again or original failure changed", observedClasses, order)
				}
			} else if !observedActive || requestCtx == nil || !errors.Is(requestCtx.Err(), context.Canceled) || !reflect.DeepEqual(order, []string{"ois", "oic"}) || !reflect.DeepEqual(observedClasses, []string{"interrupted"}) {
				t.Fatal("observer failure skipped original cancel or changed requests")
			}
		})
	}
}

func TestG2JointDiagnosticsHistoryProjection(t *testing.T) {
	for _, mode := range []string{"observed", "missing_observation", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			ois, oic := resourceTestStats()
			if mode != "missing_observation" {
				ois.Observation = &protocol.ObservationRuntimeStats{ExporterActive: 0}
				oic.Observation = &protocol.ObservationRuntimeStats{ExporterActive: 2, ExporterYoungestActiveAgeNS: 31}
			} else {
				ois.Observation, oic.Observation = nil, nil
			}
			d := &g2PreparationFailure{}
			count := 1
			if mode == "overflow" {
				count = 513
			}
			for i := 0; i < count; i++ {
				d.Attempts++
				d.beginHistory(11, 12)
				call := g2BeginJointCall(context.Background())
				call.returned(context.Background(), nil)
				d.historyRead(g2PreparationRead{OIS: &ois, OIC: &oic, OISCall: call})
			}
			first := d.history[0].Sample
			if first.OIS.PID != 11 || first.OIC.PID != 12 || first.OISCall == nil || first.OICCall != nil {
				t.Fatal("sample identity or missing call altered")
			}
			if mode == "missing_observation" {
				if first.OIS.ExporterActive != nil || first.OIC.ExporterYoungestNS != nil {
					t.Fatal("missing observation became zero")
				}
			} else if first.OIS.ExporterActive == nil || *first.OIS.ExporterActive != 0 || *first.OIS.ExporterYoungestNS != 0 || *first.OIC.ExporterActive != 2 || *first.OIC.ExporterYoungestNS != 31 {
				t.Fatal("same-sample values changed")
			}
			if mode == "overflow" && (d.historyCount != 512 || d.historyOmitted != 1 || !d.historyOverflow || !d.historyIncomplete || d.current != nil) {
				t.Fatal("diagnostic capacity changed")
			}
		})
	}
}

type g2JointFatal struct{}

func (g2JointFatal) Helper()               {}
func (g2JointFatal) Fatal(...any)          { runtime.Goexit() }
func (g2JointFatal) Fatalf(string, ...any) { runtime.Goexit() }

type g2JointTestStream struct {
	mode              string
	d                 *g2JointCritical
	order             *[]string
	closeBeforeFreeze bool
}

func (s *g2JointTestStream) ChannelID() string { return "recorded-channel" }
func (s *g2JointTestStream) Write(p []byte) (int, error) {
	*s.order = append(*s.order, "write")
	if s.mode == "write" {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func (s *g2JointTestStream) CloseWrite() error {
	*s.order = append(*s.order, "close_write")
	if s.mode == "close_write" {
		return io.ErrClosedPipe
	}
	return nil
}
func (s *g2JointTestStream) Read(p []byte) (int, error) {
	*s.order = append(*s.order, "read")
	if s.mode == "panic" {
		panic("native panic")
	}
	if s.mode == "goexit" {
		runtime.Goexit()
	}
	if s.mode == "read" {
		return 0, io.ErrUnexpectedEOF
	}
	return 0, io.EOF
}
func (s *g2JointTestStream) Close() error {
	*s.order = append(*s.order, "close")
	s.closeBeforeFreeze = s.mode != "success" && s.d.capturedNS == nil
	return nil
}

func TestG2JointDiagnosticsNativeExit(t *testing.T) {
	for _, mode := range []string{"success", "dial", "write", "close_write", "read", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			d := &g2JointCritical{Native: &g2JointNative{g2JointCall: *g2BeginJointCall(bg)}}
			var order []string
			s := &g2JointTestStream{mode: mode, d: d, order: &order}
			returned := false
			var panicked any
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { panicked = recover() }()
				g2JointRequest(g2JointFatal{}, d, func() (g2JointStream, error) {
					order = append(order, "dial")
					if mode == "dial" {
						return nil, io.ErrUnexpectedEOF
					}
					return s, nil
				}, "fixture", "payload")
				d.Native.ReturnedNS = g2DiagnosticPtr(resourceNS())
				returned = true
			}()
			<-done
			if mode == "success" {
				if !returned || d.capturedNS != nil || d.Native.ReturnedNS == nil || !reflect.DeepEqual(order, []string{"dial", "write", "close_write", "read", "close"}) {
					t.Fatal("success sequence changed", order)
				}
			} else {
				if returned || d.capturedNS == nil || d.Native.ReturnedNS != nil || s.closeBeforeFreeze {
					t.Fatal("failure cleanup preceded freeze or invented return", order)
				}
				if mode != "panic" && mode != "goexit" && (d.Native.Phase == nil || *d.Native.Phase != mode || d.Native.ErrorObservedNS == nil) {
					t.Fatal("actual failed phase absent")
				}
				if mode == "dial" && !reflect.DeepEqual(order, []string{"dial"}) {
					t.Fatal("failed dial closed nonexistent stream")
				}
			}
			if mode == "panic" && panicked != "native panic" {
				t.Fatal("panic changed", panicked)
			}
			if d.Revoke != nil || d.ProviderWait != nil || d.AfterStats != nil {
				t.Fatal("future actions fabricated")
			}
		})
	}
}

func TestG2JointDiagnosticsRevokeCalls(t *testing.T) {
	for _, mode := range []string{"success", "get_error", "delete_error", "delete_panic", "get_goexit"} {
		t.Run(mode, func(t *testing.T) {
			d := &g2JointCritical{Revoke: &g2JointRevoke{StartedNS: resourceNS()}}
			var methods []string
			var gotMatch string
			var panicked any
			returned := false
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() { panicked = recover() }()
				g2JointRevokeToken(g2JointFatal{}, d, func(ctx context.Context, method, path, match string, in, out any) (int, error) {
					methods = append(methods, method)
					if ctx != context.Background() || in != nil {
						panic("changed context/body")
					}
					if method == "GET" {
						if path != "/v1/admin/config" || match != "" {
							panic("changed GET")
						}
						if mode == "get_goexit" {
							runtime.Goexit()
						}
						if mode == "get_error" {
							return 503, protocol.Errorf(protocol.CodeStoreUnavailable, "private")
						}
						out.(*protocol.ConfigView).ConfigVersion = 73
						return 200, nil
					}
					gotMatch = match
					if path != "/v1/admin/tokens/victim" {
						panic("changed DELETE")
					}
					if mode == "delete_panic" {
						panic("delete panic")
					}
					if mode == "delete_error" {
						return 409, protocol.Errorf(protocol.CodeVersionConflict, "private")
					}
					*out.(*json.RawMessage) = json.RawMessage(`{"config_version":74}`)
					return 200, nil
				}, "victim")
				d.Revoke.ReturnedNS = g2DiagnosticPtr(resourceNS())
				returned = true
			}()
			<-done
			want := []string{"GET", "DELETE"}
			if strings.HasPrefix(mode, "get_") {
				want = []string{"GET"}
			}
			if !reflect.DeepEqual(methods, want) || (len(want) == 2 && gotMatch != "73") {
				t.Fatal("request/IfMatch changed", methods, gotMatch)
			}
			if (mode == "success") != returned || (mode == "success") != (d.capturedNS == nil) {
				t.Fatal("failure became success")
			}
			if mode != "success" && d.Revoke.ReturnedNS != nil {
				t.Fatal("Fatal/Goexit became returned helper")
			}
			if mode == "get_goexit" && (d.Revoke.ConfigGet.ReturnedNS != nil || d.Revoke.ConfigGet.ErrorClass != nil) {
				t.Fatal("unreturned GET invented success")
			}
			if len(want) == 1 && d.Revoke.TokenDelete != nil {
				t.Fatal("unexecuted DELETE recorded")
			}
			if mode == "delete_panic" && (panicked != "delete panic" || d.Revoke.TokenDelete.ReturnedNS != nil) {
				t.Fatal("panic/return changed")
			}
			raw, err := json.Marshal(d)
			if err != nil || strings.Contains(string(raw), "private") {
				t.Fatal("raw error text escaped", err)
			}
		})
	}
}

func TestG2JointDiagnosticsCleanupOrder(t *testing.T) {
	for _, mode := range []string{"success", "early_return", "goexit", "panic", "provider_error", "control_error", "provider_panic", "control_goexit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
			defer cancel()
			d := &g2JointCritical{CycleDeadlineNS: 42, InputDeadlineNS: 39, inputCtx: ctx}
			var order []string
			var panicked any
			completed := false
			done := make(chan struct{})
			cleanup := func(name string) {
				if mode != "success" && d.capturedNS == nil {
					order = append(order, "unfrozen")
				}
				order = append(order, name)
			}
			go func() {
				defer close(done)
				defer func() { panicked = recover() }()
				defer g2JointCleanup(&d, &completed, func() bool { return false }, func() { cleanup("control") })
				defer g2JointCleanup(&d, &completed, func() bool { return false }, func() { cleanup("provider") })
				defer g2JointCleanup(&d, &completed, func() bool { return false }, func() { cleanup("context"); cancel() })
				if mode == "goexit" {
					runtime.Goexit()
				}
				if mode == "panic" {
					panic("original panic")
				}
				if mode == "early_return" {
					return
				}
				_, err := g2JointWait(d, &d.ProviderWait, ctx, func(got context.Context) (g2WireEvent, error) {
					if got != ctx {
						panic("new context")
					}
					if mode == "provider_error" {
						return g2WireEvent{}, io.ErrUnexpectedEOF
					}
					if mode == "provider_panic" {
						panic("provider panic")
					}
					return g2WireEvent{ID: 1}, nil
				})
				if err != nil {
					return
				}
				_, err = g2JointWait(d, &d.ControlWait, ctx, func(context.Context) (g2WireEvent, error) {
					if mode == "control_goexit" {
						runtime.Goexit()
					}
					if mode == "control_error" {
						return g2WireEvent{}, io.ErrUnexpectedEOF
					}
					return g2WireEvent{ID: 2}, nil
				})
				if err != nil {
					return
				}
				completed = true
			}()
			<-done
			if !reflect.DeepEqual(order, []string{"context", "provider", "control"}) {
				t.Fatal("original LIFO/freeze order changed", order)
			}
			if mode == "success" {
				if d.capturedNS != nil || d.ContextStateAtFailure != nil {
					t.Fatal("success captured")
				}
			} else {
				if d.capturedNS == nil || d.ContextStateAtFailure == nil || *d.ContextStateAtFailure != "active" || d.ContextDeadlineNS == nil {
					t.Fatal("state captured after cancel")
				}
				at := *d.capturedNS
				d.freeze()
				if *d.capturedNS != at || *d.ContextStateAtFailure != "active" {
					t.Fatal("later cleanup replaced frozen fact")
				}
			}
			if d.CycleDeadlineNS != 42 || d.InputDeadlineNS != 39 || !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("original D/cancel changed")
			}
			if (mode == "provider_error" || mode == "provider_panic") && d.ControlWait != nil {
				t.Fatal("unexecuted control wait fabricated")
			}
			if mode == "panic" && panicked != "original panic" {
				t.Fatal("panic swallowed")
			}
			if mode == "provider_panic" || mode == "control_goexit" {
				call := d.ProviderWait
				if mode == "control_goexit" {
					call = d.ControlWait
				} else if panicked != "provider panic" {
					t.Fatal("Wait panic changed")
				}
				if call == nil || call.ReturnedNS != nil || call.ErrorClass != nil || call.ContextState != nil || call.ContextDeadlineNS == nil {
					t.Fatal("interrupted Wait fabricated a return")
				}
			}
		})
	}
}
