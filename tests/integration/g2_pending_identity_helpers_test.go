package integration

import (
	"bytes"
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

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestG2PendingIdentityOptIn(t *testing.T) {
	for _, tc := range []struct {
		name, vector, value string
		enabled             bool
	}{
		{"default", "TV-G2-11", "", false}, {"one", "TV-G2-11", "1", true},
		{"other_vector", "TV-G2-12", "1", false}, {"true", "TV-G2-11", "true", false},
		{"whitespace", "TV-G2-11", " 1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"existing": 1}
			enabled := g2PendingEnabled(tc.vector, tc.value)
			g2PendingConfigure(cfg, enabled)
			if enabled != tc.enabled || (cfg["log_level"] == "debug") != enabled || len(cfg) != 1+map[bool]int{false: 0, true: 1}[enabled] {
				t.Fatal("opt-in altered unrelated/default configuration", cfg)
			}
			d := &g2PreparationFailure{}
			if _, ok := d.pendingFields(map[string]any{})["pending_identity_diagnostics"]; ok {
				t.Fatal("default metadata appeared")
			}
		})
	}
}

func g2PendingTestEnv(t *testing.T, enabled bool, mode string) (*g2Env, *http.Client, *[]string, *int, *context.Context) {
	t.Helper()
	ois, oic := resourceTestStats()
	ois.Observation = &protocol.ObservationRuntimeStats{ObservationCapacity: 16, PendingQueries: 8}
	oic.Observation = &protocol.ObservationRuntimeStats{ProbeJobs: 16}
	oisJSON, err := json.Marshal(ois)
	if err != nil {
		t.Fatal(err)
	}
	oicJSON, err := json.Marshal(oic)
	if err != nil {
		t.Fatal(err)
	}
	headers, calls := []string{}, 0
	var ctx context.Context
	response := func(raw []byte) *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}
	}
	g := &g2Env{pendingDiagnostics: enabled, vector: "TV-G2-11", env: &env{t: t, be: "sqlite", api: "ois.invalid", adminTok: "fixture-only",
		ois:  &proc{cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, exited: make(chan struct{})},
		oics: map[string]*oicProc{"lab-a": {diag: "oic.invalid"}}}}
	g.stats = &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		headers = append(headers, r.Header.Get(g2PendingHeader))
		ctx = r.Context()
		switch mode {
		case "ois_error":
			return nil, io.ErrUnexpectedEOF
		case "ois_panic":
			panic("original-http-panic")
		case "ois_goexit":
			runtime.Goexit()
		case "partial_body":
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &g2DiagnosticBody{}}, nil
		}
		return response(oisJSON), nil
	})}
	hc := &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get(g2PendingHeader) != "" {
			t.Error("OIC request was tagged")
		}
		ctx = r.Context()
		if mode == "oic_error" {
			return nil, io.ErrUnexpectedEOF
		}
		return response(oicJSON), nil
	})}
	return g, hc, &headers, &calls, &ctx
}

func TestG2PendingIdentityOriginalRequests(t *testing.T) {
	for _, mode := range []string{"disabled", "prepare", "non_prepare", "entropy_failure", "overflow", "ois_error", "oic_error", "partial_body"} {
		t.Run(mode, func(t *testing.T) {
			g, hc, headers, calls, actualCtx := g2PendingTestEnv(t, mode != "disabled", mode)
			d := &g2PreparationFailure{Round: 2, Phase: "pre_dial_full"}
			if mode == "entropy_failure" || mode == "overflow" {
				d.pendingRequest(g)
				d.pendingDiagnostics.Requests = d.pendingDiagnostics.Requests[:0]
				if mode == "entropy_failure" {
					d.pendingDiagnostics.entropy = strings.NewReader("")
				}
				if mode == "overflow" {
					d.pendingDiagnostics.Requests = d.pendingDiagnostics.Requests[:512]
				}
			}
			var err error
			if mode == "non_prepare" {
				_, _, err = g2TryCompetitionSample(g, hc)
			} else {
				_, err = d.sampleContext(context.Background(), g, hc)
			}
			failed := mode == "ois_error" || mode == "oic_error" || mode == "partial_body"
			if (err != nil) != failed {
				t.Fatal("original result changed", err)
			}
			wantCalls := 2
			if mode == "ois_error" || mode == "partial_body" {
				wantCalls = 1
			}
			if *calls != wantCalls || len(*headers) != 1 {
				t.Fatal("original request count changed", *calls)
			}
			tagged := mode != "disabled" && mode != "non_prepare" && mode != "entropy_failure"
			if (len((*headers)[0]) == 32) != tagged {
				t.Fatal("wrong header scope", *headers)
			}
			if mode == "non_prepare" || mode == "disabled" {
				if d.pendingDiagnostics != nil {
					t.Fatal("non-prepare/default allocated diagnostics")
				}
				return
			}
			if mode == "overflow" {
				if len(d.pendingDiagnostics.Requests) != 512 || d.pendingDiagnostics.Omitted != 1 || !d.pendingDiagnostics.Overflow {
					t.Fatal("overflow changed bounds")
				}
				return
			}
			r := d.pendingDiagnostics.Requests[0]
			if r.OISCall != d.Read.OISCall || r.OISCall.ReturnedNS == nil {
				t.Fatal("lost original OIS call")
			}
			if (r.OISStats != nil) != (mode != "ois_error" && mode != "partial_body") {
				t.Fatal("fabricated complete HTTP response")
			}
			if mode == "entropy_failure" && (r.IDError == nil || *r.IDError != "entropy_error" || r.SampleID != nil) {
				t.Fatal("entropy failure not bounded")
			}
			if failed && (d.CapturedNS == nil || d.Read.ContextState != "active") {
				t.Fatal("failure was not frozen before cancel")
			}
			if mode == "oic_error" && !errors.Is((*actualCtx).Err(), context.Canceled) {
				t.Fatal("original OIC cleanup missing")
			}
		})
	}
}

func TestG2PendingIdentityUniqueImmutable(t *testing.T) {
	g, hc, headers, _, _ := g2PendingTestEnv(t, true, "")
	d := &g2PreparationFailure{Round: 1, Phase: "preload_joint"}
	if _, err := d.sampleContext(context.Background(), g, hc); err != nil {
		t.Fatal(err)
	}
	first := d.pendingFields(map[string]any{})["pending_identity_diagnostics"].(g2PendingDiagnostics)
	before, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	d.Read.OIS.Observation.PendingQueries = 999
	d.pendingDiagnostics.Requests[0].OISStats.Observation.PendingQueries = 998
	*d.pendingDiagnostics.Requests[0].OISCall.ReturnedNS = -1
	*d.pendingDiagnostics.Requests[0].SampleID = "changed"
	if _, err := d.sampleContext(context.Background(), g, hc); err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || len(*headers) != 2 || (*headers)[0] == (*headers)[1] {
		t.Fatal("old emitted metadata mutated or ID reused")
	}
	if len(first.Requests) != 1 || len(d.pendingDiagnostics.Requests) != 2 {
		t.Fatal("snapshot alias")
	}
}

func TestG2PendingIdentityAbnormalOwnership(t *testing.T) {
	for _, mode := range []string{"ois_panic", "ois_goexit", "observer_panic", "observer_goexit"} {
		t.Run(mode, func(t *testing.T) {
			transportMode := mode
			if strings.HasPrefix(mode, "observer_") {
				transportMode = "ois_error"
			}
			g, hc, _, calls, _ := g2PendingTestEnv(t, true, transportMode)
			d := &g2PreparationFailure{Round: 0, Phase: "slow_full", Attempts: 1}
			r := d.pendingRequest(g)
			done := make(chan struct{})
			notifications := 0
			normal := false
			var recovered any
			var captured g2PreparationRead
			go func() {
				defer close(done)
				defer func() { recovered = recover() }()
				_, _, _ = g2TryCompetitionSamplePending(context.Background(), g, hc, func(ctx context.Context, read g2PreparationRead, err error) {
					notifications++
					captured = read
					if ctx.Err() != nil {
						t.Error("first observation canceled")
					}
					if mode == "observer_panic" {
						panic("observer")
					}
					if mode == "observer_goexit" {
						runtime.Goexit()
					}
				}, r)
				normal = true
			}()
			<-done
			if normal || notifications != 1 || *calls != 1 || r.OISStats != nil {
				t.Fatal("abnormal path fabricated return/repeated request", normal, notifications, *calls)
			}
			if strings.HasPrefix(mode, "observer_") {
				if captured.ErrorClass != "eof" || r.OISCall.ReturnedNS == nil {
					t.Fatal("first HTTP error overwritten")
				}
			} else if captured.ErrorClass != "interrupted" || r.OISCall.ReturnedNS != nil {
				t.Fatal("interrupted HTTP return fabricated")
			}
			want := any(nil)
			if mode == "ois_panic" {
				want = "original-http-panic"
			}
			if mode == "observer_panic" {
				want = "observer"
			}
			if !reflect.DeepEqual(recovered, want) {
				t.Fatal("panic changed", recovered)
			}
		})
	}
}
