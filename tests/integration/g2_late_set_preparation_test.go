package integration

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wyw-ai/loom-oi/tests/fixture"
)

// Queue/registration and the real 32-arm loop have their own existing tests.
// This fixture starts at their accepted boundary, before any exporter input.
func g2LateSetInput(t *testing.T, prepare time.Time) (*g2PreparationFailure, *[]string, func(string, bool) uint64) {
	t.Helper()
	d := &g2PreparationFailure{active: true, StartedNS: resourceNS(), DeadlineNS: int64(prepare.Sub(resourceOrigin))}
	var calls []string
	set := func(id string, hold bool) uint64 {
		ids := g2CompetitionSourceIDs()
		if len(calls) >= len(ids) || id != ids[len(calls)] || hold != (len(calls) >= 2) {
			t.Fatalf("source reset/order/role changed: %q hold=%t after %v", id, hold, calls)
		}
		calls = append(calls, id)
		return uint64(100 + len(calls))
	}
	if err := d.preloadSources(set); err != nil {
		t.Fatal(err)
	}
	d.dropArmStartedNS, d.dropArmCompletedNS = g2DiagnosticPtr(resourceNS()), g2DiagnosticPtr(resourceNS())
	d.dropArmCount = 32
	d.Phase = "cycle0_export_input"
	return d, &calls, set
}

func TestG2LateSetSelection(t *testing.T) {
	for _, mode := range []string{"qualified", "prepare_first", "connect_first", "expired_before_selection", "duplicate_selection", "later_cycle_nil"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// The bubble starts in 2000; advance only its virtual clock so
				// resourceOrigin-relative connection timestamps are positive.
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				start := time.Now()
				prepare, connect := start.Add(3*time.Second), start.Add(time.Second)
				if mode == "prepare_first" {
					prepare, connect = connect, prepare
				}
				d, calls, set := g2LateSetInput(t, prepare)
				originalDeadline := d.DeadlineNS
				connected := false
				selected := 0
				f := &g2CompetitionFinalSelection{preparation: d, deadline: prepare}
				f.slowInput = func(deadline time.Time) error {
					if !connected || !deadline.Equal(start.Add(time.Second)) {
						t.Fatal("last Set ran before connection or renewed the earlier deadline")
					}
					return d.finishSlowSources(set, deadline)
				}
				f.beforeInput = func(context.Context) error { return nil }
				f.selectInput = func(deadline time.Time) {
					selected++
					if len(*calls) != 16 || len(d.inputVersions) != 16 || d.SlowAppliedNS == nil || d.Phase != "slow_full" || !deadline.Equal(start.Add(time.Second)) {
						t.Fatal("final predicate ran without the complete original source batch")
					}
				}
				// Until the cycle supplies its connected beforeWrite boundary, only the
				// original preload exists. A later cycle supplies nil instead.
				if len(*calls) != 8 || d.finalSetStartedNS != nil || d.SlowAppliedNS != nil {
					t.Fatal("preload fabricated late source input")
				}
				if mode == "later_cycle_nil" {
					var later *g2CompetitionFinalSelection
					if later.selectFinal(connect) != nil || len(*calls) != 8 {
						t.Fatal("later cycle rebuilt slow input")
					}
					return
				}
				connected = true
				ctx, cancel := context.WithDeadline(context.Background(), f.inputDeadline(connect))
				defer cancel()
				slow := &g2SlowScrapes{writes: []g2SlowWrite{{ConnectedNS: int64(start.Sub(resourceOrigin))}, {ConnectedNS: int64(start.Sub(resourceOrigin))}}}
				if mode == "expired_before_selection" {
					time.Sleep(time.Until(start.Add(time.Second)))
				}
				err := f.prepareBeforeWrite(ctx, slow)
				if err == nil {
					err = f.selectFinal(connect)
				}
				if mode == "expired_before_selection" {
					if err == nil || selected != 0 || len(*calls) != 8 || d.finalSetStartedNS != nil || d.SlowAppliedNS != nil {
						t.Fatal("expired input performed late Set or final selection")
					}
				} else if err != nil || selected != 1 {
					t.Fatal("qualified input was not selected exactly once", err, selected)
				}
				if mode == "duplicate_selection" {
					if f.selectFinal(connect) == nil || selected != 1 || len(*calls) != 16 || d.finishSlowSources(set, connect) == nil {
						t.Fatal("same cycle reset a source or repeated selection")
					}
				}
				if d.DeadlineNS != originalDeadline || f.deadline != prepare {
					t.Fatal("stored original preparation deadline changed")
				}
			})
		})
	}
}

func TestG2LateSetFailureOwnership(t *testing.T) {
	for _, mode := range []string{"export_not_qualified", "only_first_429", "partial_deadline", "last_return_at_deadline", "last_return_after_deadline", "set_panic", "set_goexit"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// The bubble starts in 2000; advance only its virtual clock so
				// resourceOrigin-relative connection timestamps are positive.
				time.Sleep(time.Until(resourceOrigin.Add(time.Second)))
				start := time.Now()
				prepare, connect := start.Add(3*time.Second), start.Add(time.Second)
				d, calls, set := g2LateSetInput(t, prepare)
				originalDeadline := d.DeadlineNS
				f := &g2CompetitionFinalSelection{preparation: d, deadline: prepare}
				f.selectInput = func(time.Time) { t.Error("failed late input reached final predicate") }
				f.slowInput = func(deadline time.Time) error {
					return d.finishSlowSources(func(id string, hold bool) uint64 {
						next := len(*calls) + 1
						if next == 11 && mode == "set_panic" {
							panic("Set did not return")
						}
						if next == 11 && mode == "set_goexit" {
							runtime.Goexit()
						}
						v := set(id, hold)
						if (next == 11 && mode == "partial_deadline") || (next == 16 && (mode == "last_return_at_deadline" || mode == "last_return_after_deadline")) {
							time.Sleep(time.Until(deadline))
							if mode == "last_return_after_deadline" {
								time.Sleep(time.Nanosecond)
							}
						}
						return v
					}, deadline)
				}
				f.beforeInput = func(context.Context) error { return nil }
				ctx, cancel := context.WithDeadline(context.Background(), f.inputDeadline(connect))
				defer cancel()
				slow := &g2SlowScrapes{writes: []g2SlowWrite{{ConnectedNS: int64(start.Sub(resourceOrigin))}, {ConnectedNS: int64(start.Sub(resourceOrigin))}}}
				var order []string
				var frozen []byte
				var recovered any
				var marshalErr error
				returned := false
				done := make(chan struct{})
				go func() {
					defer close(done)
					defer func() {
						recovered = recover()
						f.freezeBeforeCleanup(recovered, true)
						order = append(order, "freeze")
						frozen, marshalErr = json.Marshal(d.fields())
						g2FinishDiagnostic(func() {
							if d.CapturedNS == nil {
								order = append(order, "close-before-freeze")
							}
							order = append(order, "close", "join")
						}, func() { order = append(order, "emit") })
					}()
					// Last Set now precedes export. An incomplete export still must
					// not run final selection or fabricate its witness.
					err := f.prepareBeforeWrite(ctx, slow)
					if mode == "export_not_qualified" || mode == "only_first_429" {
						if err != nil {
							t.Error(err)
						}
						runtime.Goexit()
					}
					if err == nil {
						t.Error("incomplete or late Set became successful input")
					}
					returned = true
				}()
				<-done
				if marshalErr != nil || d.CapturedNS == nil || !reflect.DeepEqual(order, []string{"freeze", "close", "join", "emit"}) || d.DeadlineNS != originalDeadline {
					t.Fatal("failure ownership/order/deadline changed", order, marshalErr)
				}
				var fields map[string]any
				if err := json.Unmarshal(frozen, &fields); err != nil {
					t.Fatal(err)
				}
				wantVersions, wantStart, wantApplied := 8, false, false
				switch mode {
				case "export_not_qualified", "only_first_429":
					wantVersions, wantStart, wantApplied = 16, true, true
				case "partial_deadline":
					wantVersions, wantStart = 11, true
				case "set_panic", "set_goexit":
					wantVersions, wantStart = 10, true
				case "last_return_at_deadline", "last_return_after_deadline":
					wantVersions, wantStart, wantApplied = 16, true, true
				}
				if len(fields["input_versions"].(map[string]any)) != wantVersions || (fields["final_set_started_ns"] != nil) != wantStart || (fields["slow_input_applied_ns"] != nil) != wantApplied {
					t.Fatal("failure invented missing versions or erased an actually returned Set", string(frozen))
				}
				wantReturned := mode == "partial_deadline" || mode == "last_return_at_deadline" || mode == "last_return_after_deadline"
				if returned != wantReturned || (recovered != nil) != (mode == "set_panic") {
					t.Fatal("panic/Goexit/error return were conflated")
				}
				first := *d.CapturedNS
				d.finish(nil, true)
				if *d.CapturedNS != first {
					t.Fatal("outer cleanup replaced inner first failure")
				}
			})
		})
	}
}

func TestG2LateSetTargetWindow(t *testing.T) {
	for _, tc := range []struct {
		name               string
		records            []fixture.HealthRecord
		attempts, timeouts int
		bad                bool
	}{
		{"old_healthy_crosses_set", []fixture.HealthRecord{{ID: 1, Version: 1, StartedNS: 90, HeaderStartedNS: 110, FinishedNS: 220}}, 0, 0, false},
		{"preheated_timeout_cannot_fill_window", []fixture.HealthRecord{{ID: 1, Version: 2, StartedNS: 90, CancelledNS: 110, FinishedNS: 220}}, 0, 0, false},
		{"timeout_finishes_after_window", []fixture.HealthRecord{{ID: 1, Version: 2, StartedNS: 100, CancelledNS: 200, FinishedNS: 220}}, 1, 1, false},
		{"attempt_cancels_after_window", []fixture.HealthRecord{{ID: 1, Version: 2, StartedNS: 100, CancelledNS: 210, FinishedNS: 220}}, 1, 0, false},
		{"old_version_starts_at_set", []fixture.HealthRecord{{ID: 1, Version: 1, StartedNS: 100, HeaderStartedNS: 110, FinishedNS: 220}}, 0, 0, true},
		{"old_history_and_real_timeout", []fixture.HealthRecord{{ID: 1, Version: 1, StartedNS: 90, HeaderStartedNS: 110, FinishedNS: 220}, {ID: 2, Version: 2, StartedNS: 100, CancelledNS: 200, FinishedNS: 220}}, 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture.HealthSnapshot{PID: 1, Version: 2, Records: tc.records}
			original, _ := json.Marshal(h)
			a, n, err := g2CompetitionTargetCounts(h, 2, 100, 200, true)
			if (err != nil) != tc.bad || (err == nil && (a != tc.attempts || n != tc.timeouts)) {
				t.Fatalf("strict last-Set window: attempts=%d timeouts=%d error=%v", a, n, err)
			}
			after, _ := json.Marshal(h)
			if string(original) != string(after) {
				t.Fatal("old-version or late-finished original history was removed")
			}
		})
	}
}
