package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func preparationHistoryInput() (g2CompetitionStats, g2PreparationRead) {
	ois, oic := resourceTestStats()
	ois.Observation = &protocol.ObservationRuntimeStats{ObservationCapacity: 16, ObservationBacklog: 16, PendingQueries: 8}
	oic.Observation = &protocol.ObservationRuntimeStats{ProbeJobs: 16, ProbeActive: 2, ProbeQueued: 4, MaxProbeActive: 2, MaxProbeQueued: 4}
	x := g2CompetitionStats{StartedNS: 100, CompletedNS: 200, OIS: ois, OIC: oic}
	r := g2PreparationRead{StartedNS: 100, CompletedNS: g2DiagnosticPtr(int64(200)), ReturnedNS: 210,
		OIS: &x.OIS, OIC: &x.OIC, CompletedRoles: []string{"ois", "oic"}, ErrorClass: "none", ContextState: "active"}
	return x, r
}

func preparationHistoryQueries(now int64) []g2WireEvent {
	var events []g2WireEvent
	for i := 0; i < 8; i++ {
		q := protocol.ObservationQuery{QueryID: fmt.Sprintf("q%d", i), DeadlineMS: 500,
			Binding: protocol.ObservationBinding{ExposureID: fmt.Sprintf("exposure-%d", i)}}
		raw, _ := json.Marshal(q)
		events = append(events, g2WireEvent{ID: uint64(i + 1), Type: protocol.MsgObservationQuery,
			Direction: g2OISToOIC, Raw: raw, QueryID: q.QueryID, ReceivedNS: now - 10, WriteStartedNS: now - 9, WrittenNS: now - 8})
	}
	return events
}

func TestG2PreparationHistorySamples(t *testing.T) {
	t.Run("two_phases_keep_values", func(t *testing.T) {
		d := &g2PreparationFailure{Phase: "healthy_backlog", Attempts: 1}
		x, read := preparationHistoryInput()
		d.beginHistory(101, 102)
		d.historyRead(read)
		d.full(x)
		if !d.returned(true, g2ProxySnapshot{}, 220) {
			t.Fatal("changed original true")
		}
		d.acceptedHealthy()
		d.slowSetStartedNS = g2DiagnosticPtr(resourceNS())
		d.SlowAppliedNS = g2DiagnosticPtr(resourceNS())
		// The next decoded observation and its nested maps may change. The old
		// row must contain only its own scalar projection, not those references.
		x.OIS.Epoch = 99
		x.OIS.Observation.ObservationBacklog = 0
		x.OIC.Generation = 88
		x.OIC.Observation.ProbeActive = 0
		read.OIS, read.OIC = &x.OIS, &x.OIC
		d.Phase, d.Attempts = "slow_full", 2
		d.beginHistory(101, 102)
		d.historyRead(read)
		d.full(x)
		d.returned(false, g2ProxySnapshot{}, 230)
		a, b := &d.history[0], &d.history[1]
		if a.Attempt != 1 || a.Phase != "healthy_backlog" || b.Attempt != 2 || b.Phase != "slow_full" ||
			a.Sample.OIS.PID != 101 || a.Sample.OIC.PID != 102 || a.Sample.OIS.Backlog != 16 || a.Sample.OIC.Active != 2 ||
			!a.Full.Backlog16 || b.Full.Backlog16 || *d.healthyAcceptedAttempt != 1 || *d.healthyAcceptedNS > *d.slowSetStartedNS || *d.slowSetStartedNS > *d.SlowAppliedNS {
			t.Fatal("history identity, values, or original phase switch lost")
		}
		if a.Sample.OIS.Epoch == 99 || a.Sample.OIC.Generation == 88 || a.Sample.OIS == b.Sample.OIS {
			t.Fatal("aliased later statistics")
		}
	})
	t.Run("short_circuit_stays_null", func(t *testing.T) {
		d := &g2PreparationFailure{Phase: "slow_full", Attempts: 1}
		x, read := preparationHistoryInput()
		x.OIS.Observation.ObservationBacklog = 0
		d.beginHistory(1, 2)
		d.historyRead(read)
		d.full(x)
		ok := g2CompetitionFull(x) && d.young(preparationHistoryQueries(1000), 1000)
		d.returned(ok, g2ProxySnapshot{}, 1001)
		r := &d.history[0]
		if ok || r.Young != nil || r.YoungEvaluatedNS != nil || r.Queries != nil || r.Full.Backlog16 || r.PredicateReturned == nil || *r.PredicateReturned {
			t.Fatal("unevaluated young fabricated")
		}
	})
	t.Run("young_exact_original_order", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1}
		d.beginHistory(1, 2)
		now := int64(time.Second)
		events := preparationHistoryQueries(now)
		if !d.young(events, now) {
			t.Fatal("original young input")
		}
		d.returned(true, g2ProxySnapshot{Events: events}, now-1)
		before := d.history[0].Queries[0]
		d.Queries[0].ReceivedNS = 0
		if len(d.history[0].Queries) != 8 || d.history[0].Queries[0] != before || before.EventID != 8 || *d.history[0].YoungEvaluatedNS != now {
			t.Fatal("query projection is not its original order/value")
		}
	})
	t.Run("young_rejection_keeps_checked_prefix", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1}
		d.beginHistory(1, 2)
		now := int64(time.Second)
		events := preparationHistoryQueries(now)
		events[7].ReceivedNS = now - int64(200*time.Millisecond) - 1
		if d.young(events, now) || len(d.history[0].Queries) != 1 || d.history[0].Queries[0].EventID != 8 {
			t.Fatal("replaced failing query with younger batch")
		}
	})
	t.Run("partial_role_is_null", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1}
		d.beginHistory(1, 2)
		_, read := preparationHistoryInput()
		read.OIC = nil
		read.CompletedNS = nil
		read.FailedRole = g2DiagnosticPtr("oic")
		d.historyRead(read)
		b, err := json.Marshal(d.history[0])
		if err != nil {
			t.Fatal(err)
		}
		if d.history[0].Sample.OIS == nil || d.history[0].Sample.OIC != nil || !strings.Contains(string(b), `"oic":null`) || d.history[0].Full != nil {
			t.Fatal("missing role filled in", string(b))
		}
	})
	t.Run("deadline_not_clamped", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1, DeadlineNS: 199}
		d.beginHistory(1, 2)
		_, read := preparationHistoryInput()
		d.historyRead(read)
		d.returned(false, g2ProxySnapshot{}, 220)
		if *d.history[0].Sample.CompletedNS != 200 || d.history[0].Sample.ReturnedNS != 210 || *d.history[0].PredicateReturned || *d.history[0].WireSnapshotReturnedNS != 220 {
			t.Fatal("late observation rewritten")
		}
	})
}

func TestG2PreparationHistoryCallbackExit(t *testing.T) {
	for _, mode := range []string{"true", "false", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			d := &g2PreparationFailure{Attempts: 1}
			d.beginHistory(1, 2)
			done := make(chan struct{})
			var recovered any
			go func() {
				defer close(done)
				defer func() { recovered = recover() }()
				predicate := func() bool {
					if mode == "panic" {
						panic("original")
					}
					if mode == "goexit" {
						runtime.Goexit()
					}
					return mode == "true"
				}
				ok := predicate()
				d.returned(ok, g2ProxySnapshot{}, 123)
			}()
			<-done
			r := &d.history[0]
			if mode == "panic" || mode == "goexit" {
				if r.PredicateReturned != nil || r.CallbackReturnedNS != nil || r.WireEventCount != nil {
					t.Fatal("abnormal exit became false/returned")
				}
				if mode == "panic" && recovered != "original" {
					t.Fatal("panic changed")
				}
			} else if r.PredicateReturned == nil || *r.PredicateReturned != (mode == "true") || r.CallbackReturnedNS == nil {
				t.Fatal("normal return changed")
			}
		})
	}
}

func TestG2PreparationHistoryBounds(t *testing.T) {
	d := &g2PreparationFailure{Phase: "healthy_backlog"}
	for i := 1; i <= 514; i++ {
		d.Attempts = i
		d.beginHistory(1, 2)
		want := i%2 == 0
		if d.returned(want, g2ProxySnapshot{}, int64(i)) != want {
			t.Fatal("capacity changed predicate")
		}
	}
	if d.historyCount != 512 || d.historyOmitted != 2 || !d.historyOverflow || !d.historyIncomplete || d.history[0].Attempt != 1 || d.history[511].Attempt != 512 {
		t.Fatal("wrong bounded prefix")
	}
	if _, err := json.Marshal(d.fields()); err != nil {
		t.Fatal(err)
	}
}

func TestG2PreparationHistoryWire(t *testing.T) {
	drop := func(id uint64) g2WireEvent {
		return g2WireEvent{ID: id, Type: protocol.MsgObservationSnapshot, Direction: g2OICToOIS, QueryID: fmt.Sprint(id), ReceivedNS: int64(id), Dropped: true}
	}
	t.Run("incremental_prefix", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1}
		d.beginHistory(1, 2)
		events := []g2WireEvent{drop(1), {ID: 2, Type: protocol.MsgObservationQuery}}
		d.returned(false, g2ProxySnapshot{Events: events}, 10)
		d.Attempts = 2
		d.beginHistory(1, 2)
		events = append(events, drop(3))
		d.returned(false, g2ProxySnapshot{Events: events}, 20)
		if d.dropCount != 2 || d.totalDropped != 2 || d.wireCursor != 3 || *d.history[0].DroppedCount != 1 || *d.history[1].DroppedCount != 2 || d.drops[0].EventID != 1 || d.drops[1].EventID != 3 {
			t.Fatal("rescanned/duplicated original prefix")
		}
	})
	t.Run("later_completion_does_not_backfill", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1}
		d.beginHistory(1, 2)
		events := preparationHistoryQueries(1000)
		d.young(events, 1000)
		d.returned(true, g2ProxySnapshot{Events: events}, 1001)
		original := d.history[0].Queries[0]
		events[7].WrittenNS = 2000
		d.Attempts = 2
		d.beginHistory(1, 2)
		d.returned(false, g2ProxySnapshot{Events: events}, 2001)
		if d.history[0].Queries[0] != original || *d.history[0].WireSnapshotReturnedNS != 1001 {
			t.Fatal("future event state replaced snapshot")
		}
	})
	for _, mode := range []string{"id_gap", "shrinking", "proxy_overflow", "proxy_failure", "drop_overflow", "invalid_drop"} {
		t.Run(mode, func(t *testing.T) {
			d := &g2PreparationFailure{Attempts: 1}
			d.beginHistory(1, 2)
			s := g2ProxySnapshot{Events: []g2WireEvent{drop(1)}}
			switch mode {
			case "id_gap":
				s.Events[0].ID = 2
			case "shrinking":
				d.historyWire(s, 1)
				s.Events = nil
			case "proxy_overflow":
				s.Overflow = true
			case "proxy_failure":
				s.Failure = "fixture failure"
			case "drop_overflow":
				for i := 2; i <= 33; i++ {
					s.Events = append(s.Events, drop(uint64(i)))
				}
			case "invalid_drop":
				s.Events[0].WrittenNS = 5
			}
			if !d.returned(true, s, 10) || !d.historyIncomplete {
				t.Fatal("diagnostic corruption changed predicate or disappeared")
			}
			if mode == "drop_overflow" && (d.dropCount != 32 || d.totalDropped != 33 || !d.historyOverflow || *d.history[0].DroppedCount != 33) {
				t.Fatal("overflow concealed actual total")
			}
		})
	}
}

func TestG2PreparationHistoryStatsFreezeBeforeCancel(t *testing.T) {
	for _, mode := range []string{"oic_error", "success"} {
		t.Run(mode, func(t *testing.T) {
			x, _ := preparationHistoryInput()
			oisJSON, _ := json.Marshal(x.OIS)
			oicJSON, _ := json.Marshal(x.OIC)
			var order []string
			var reqCtx context.Context
			response := func(raw []byte) *http.Response {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}
			}
			ocs := &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				order = append(order, "ois")
				return response(oisJSON), nil
			})}
			sentinel := errors.New("original stats failure")
			hc := &http.Client{Transport: g2DiagnosticRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				order = append(order, "oic")
				reqCtx = r.Context()
				if mode == "oic_error" {
					return nil, sentinel
				}
				return response(oicJSON), nil
			})}
			g := &g2Env{env: &env{t: t, api: "ois.invalid", ois: &proc{cmd: &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}, exited: make(chan struct{})}, stats: ocs, oics: map[string]*oicProc{"lab-a": {diag: "oic.invalid"}}}}
			d := &g2PreparationFailure{Attempts: 1}
			d.beginHistory(os.Getpid(), 202)
			_, read, err := g2TryCompetitionSampleObserved(g, hc, func(ctx context.Context, partial g2PreparationRead, cause error) {
				d.historyRead(partial)
				d.freeze("stats_error")
				if ctx.Err() != nil || reqCtx.Err() != nil || !errors.Is(cause, sentinel) || d.history[0].Sample.ContextState != "active" || d.history[0].Sample.OIS == nil || d.history[0].Sample.OIC != nil {
					t.Fatal("history not frozen before original cancel")
				}
				order = append(order, "frozen")
			})
			if reqCtx == nil || reqCtx.Err() != context.Canceled {
				t.Fatal("original helper failed to cancel")
			}
			want := []string{"ois", "oic", "frozen"}
			if mode == "success" {
				want = []string{"ois", "oic"}
				d.historyRead(read)
				if err != nil || d.CapturedNS != nil || d.history[0].Sample.OIC == nil {
					t.Fatal("success fabricated failure")
				}
			} else if !errors.Is(err, sentinel) || d.CapturedNS == nil {
				t.Fatal("original failure changed")
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatal("request count/order changed", order)
			}
		})
	}
}
