package integration

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/tests/fixture"
)

func TestG2SixSourcePreparationOrder(t *testing.T) {
	for _, mode := range []string{"complete", "preload_panic", "final_panic"} {
		t.Run(mode, func(t *testing.T) {
			d := &g2PreparationFailure{StartedNS: 101, DeadlineNS: 101 + int64(3*time.Second)}
			ids := g2CompetitionSourceIDs()
			var called []string
			holds := map[string]bool{}
			set := func(id string, hold bool) uint64 {
				called = append(called, id)
				if _, exists := holds[id]; exists {
					t.Fatal("source was set again, releasing its prior gate", id)
				}
				if (mode == "preload_panic" && len(called) == 4) || (mode == "final_panic" && len(called) == 12) {
					panic("set failed")
				}
				holds[id] = hold
				return uint64(100 + len(called))
			}
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				d.preloadSources(set)
				if len(called) != 8 || d.Phase != "preload_joint" || d.SlowAppliedNS != nil || len(d.inputVersions) != 8 {
					t.Fatal("preload unexpectedly established fourteen-source input")
				}
				d.Attempts = 7
				d.acceptedPreload()
				d.finishSlowSources(set)
			}()
			if d.StartedNS != 101 || d.DeadlineNS != 101+int64(3*time.Second) {
				t.Fatal("phase switch renewed original deadline")
			}
			for i, id := range called {
				if id != ids[i] {
					t.Fatal("source order changed", called)
				}
				if hold, exists := holds[id]; exists && hold != (i >= 2) {
					t.Fatal("fixed health/slow role changed", id)
				}
			}
			switch mode {
			case "complete":
				if recovered != nil || len(called) != 16 || len(d.inputVersions) != 16 || d.Phase != "slow_full" || *d.preloadAcceptedAttempt != 7 ||
					*d.preloadSetStartedNS > *d.preloadSetCompletedNS || *d.preloadSetCompletedNS > *d.preloadAcceptedNS || *d.preloadAcceptedNS > *d.finalSetStartedNS || *d.finalSetStartedNS > *d.SlowAppliedNS {
					t.Fatal("incorrect real two-batch boundaries")
				}
				fields := d.preparationFields(map[string]any{})
				d.inputVersions[ids[0]] = 999
				if fields["input_versions"].(map[string]uint64)[ids[0]] != 101 {
					t.Fatal("record aliases mutable version map")
				}
			case "preload_panic":
				if recovered != "set failed" || len(called) != 4 || len(d.inputVersions) != 3 || d.preloadSetCompletedNS != nil || d.preloadAcceptedNS != nil || d.finalSetStartedNS != nil || d.SlowAppliedNS != nil {
					t.Fatal("partial preload fabricated completion")
				}
			case "final_panic":
				if recovered != "set failed" || len(called) != 12 || len(d.inputVersions) != 11 || d.preloadAcceptedNS == nil || d.finalSetStartedNS == nil || d.SlowAppliedNS != nil {
					t.Fatal("partial final batch fabricated completion")
				}
			}
			if _, err := json.Marshal(d.fields()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestG2CompetitionQueryWitness(t *testing.T) {
	t.Run("original_snapshot_owned", func(t *testing.T) {
		now := resourceNS()
		events := preparationHistoryQueries(now)
		for i := range events {
			events[i].ConnID = 9
		}
		// A newer, not-yet-written event must not replace an original selected
		// query even if the live wire gets its write completion afterwards.
		future := events[7]
		future.ID, future.WrittenNS = 9, 0
		events = append(events, future)
		snapshot := g2ProxySnapshot{Events: events}
		snapshotReturned := resourceNS()
		w := g2ObserveCompetitionQueries(snapshot.Events, resourceNS(), 300*time.Millisecond)
		x, _ := preparationHistoryInput()
		w.bindSample(x, 210, snapshot, snapshotReturned, 101, 102)
		frozen := append([]g2PreparationQuery(nil), w.Queries...)
		events[7].WrittenNS = resourceNS()
		events[8].WrittenNS = resourceNS()
		if !w.Result || len(w.Queries) != 8 || w.Queries[0].EventID != 8 || w.Queries[0].ConnID != 9 ||
			w.WireEventCount != 9 || w.WireLastEventID != 9 || w.WireSnapshotReturnedNS != snapshotReturned ||
			!reflect.DeepEqual(w.Queries, frozen) || w.Attempt != nil || w.Sample != nil || w.CallbackReturnedNS != nil || w.OISPID != 101 || w.OICPID != 102 {
			t.Fatal("query witness was inferred from later wire or another phase")
		}
	})
	t.Run("preload_final_independent", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1, Phase: "preload_joint"}
		x, read := preparationHistoryInput()
		d.lastSample, d.lastSampleReturnedNS = x, read.ReturnedNS
		d.beginHistory(101, 102)
		now := resourceNS()
		events := preparationHistoryQueries(now)
		if !d.young(events, now) {
			t.Fatal("valid preload")
		}
		d.returned(true, g2ProxySnapshot{Events: events}, now-1)
		d.acceptedPreload()
		preload := d.preloadWitness
		x.OIC.Observation.ProbeActive = 0
		x.OIC.Closed["changed"] = 1
		d.lastSample, d.Attempts, d.Phase = x, 2, "slow_full"
		d.beginHistory(201, 202)
		events[7].ReceivedNS = now - int64(200*time.Millisecond) - 1
		if d.young(events, now) {
			t.Fatal("stale final accepted using preload witness")
		}
		d.returned(false, g2ProxySnapshot{Events: events}, now-1)
		if preload == d.lastQueryWitness || !preload.Result || d.lastQueryWitness.Result ||
			*preload.Attempt != 1 || *d.lastQueryWitness.Attempt != 2 || preload.Sample.OIC.Observation.ProbeActive != 2 || preload.Sample.OIC.Closed["changed"] != 0 ||
			preload.OISPID != 101 || preload.OICPID != 102 || d.lastQueryWitness.OISPID != 201 || d.lastQueryWitness.OICPID != 202 {
			t.Fatal("preload copied later sample/result or final reused preload")
		}
	})
	t.Run("short_circuit_null", func(t *testing.T) {
		d := &g2PreparationFailure{Attempts: 1}
		x, _ := preparationHistoryInput()
		x.OIC.Observation.ProbeQueued = 0
		d.beginHistory(1, 2)
		ok := g2CompetitionFull(x) && d.young(preparationHistoryQueries(resourceNS()), resourceNS())
		d.returned(ok, g2ProxySnapshot{}, resourceNS())
		if ok || d.lastQueryWitness != nil {
			t.Fatal("unevaluated query gate fabricated witness")
		}
	})
	t.Run("history_overflow_keeps_actual_witness", func(t *testing.T) {
		d := &g2PreparationFailure{historyCount: 512, Attempts: 513}
		d.beginHistory(1, 2)
		now := resourceNS()
		events := preparationHistoryQueries(now)
		ok := d.young(events, now)
		if !d.returned(ok, g2ProxySnapshot{Events: events}, now-1) || d.lastQueryWitness == nil || *d.lastQueryWitness.Attempt != 513 || !d.historyOverflow ||
			d.lastQueryWitness.OISPID != 1 || d.lastQueryWitness.OICPID != 2 {
			t.Fatal("diagnostic capacity changed original result or lost successful evidence")
		}
	})
	for _, tc := range []struct {
		name  string
		limit time.Duration
		late  int64
	}{
		{"prepare_exact", 200 * time.Millisecond, 0}, {"prepare_late", 200 * time.Millisecond, 1},
		{"critical_exact", 300 * time.Millisecond, 0}, {"critical_late", 300 * time.Millisecond, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := int64(time.Second)
			events := preparationHistoryQueries(now)
			events[7].ReceivedNS = now - int64(tc.limit) - tc.late
			w := g2ObserveCompetitionQueries(events, now, tc.limit)
			if w.Result != (tc.late == 0) || w.MaxAgeMS != tc.limit.Milliseconds() || w.EvaluatedNS != now || w.Queries[0].ReceivedNS != events[7].ReceivedNS {
				t.Fatal("original age boundary or actual evaluated time changed")
			}
		})
	}
}

func TestG2CompetitionTargetExerciseWindow(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		start, canceled, header int64
		version                 uint64
		slow                    bool
		attempts, timeouts      int
		bad                     bool
	}{
		{"preload_only", 90, 95, 0, 2, true, 0, 0, false},
		{"preload_cancels_in_window", 99, 150, 0, 2, true, 0, 0, false},
		{"late_cancel", 150, 201, 0, 2, true, 1, 0, false},
		{"exact_start_and_end", 100, 200, 0, 2, true, 1, 1, false},
		{"at_end_not_completed", 200, 201, 0, 2, true, 1, 0, false},
		{"after_window", 201, 202, 0, 2, true, 0, 0, false},
		{"headers_in_window", 150, 0, 160, 2, true, 0, 0, true},
		{"headers_after_window", 150, 0, 201, 2, true, 1, 0, false},
		{"wrong_version", 150, 160, 0, 3, true, 0, 0, true},
		{"healthy_headers", 150, 0, 160, 2, false, 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixture.HealthSnapshot{PID: 1, Version: 2, Records: []fixture.HealthRecord{{ID: 1, Version: tc.version, StartedNS: tc.start, CancelledNS: tc.canceled, HeaderStartedNS: tc.header}}}
			a, n, err := g2CompetitionTargetCounts(h, 2, 100, 200, tc.slow)
			if (err != nil) != tc.bad || (!tc.bad && (a != tc.attempts || n != tc.timeouts)) {
				t.Fatalf("got %d/%d %v", a, n, err)
			}
		})
	}
	t.Run("preload_and_real_timeout", func(t *testing.T) {
		h := fixture.HealthSnapshot{PID: 1, Version: 2, Records: []fixture.HealthRecord{
			{ID: 1, Version: 2, StartedNS: 90, CancelledNS: 95},
			{ID: 2, Version: 2, StartedNS: 100, CancelledNS: 200},
			{ID: 3, Version: 2, StartedNS: 150, CancelledNS: 201},
		}}
		a, n, err := g2CompetitionTargetCounts(h, 2, 100, 200, true)
		if err != nil || a != 2 || n != 1 || len(h.Records) != 3 {
			t.Fatal("preload/late cancellation polluted count or original history was removed", a, n, err)
		}
	})
	t.Run("timeout_cannot_hide_headers", func(t *testing.T) {
		h := fixture.HealthSnapshot{PID: 1, Version: 2, Records: []fixture.HealthRecord{
			{ID: 1, Version: 2, StartedNS: 100, CancelledNS: 150},
			{ID: 2, Version: 2, StartedNS: 160, HeaderStartedNS: 170},
		}}
		if _, _, err := g2CompetitionTargetCounts(h, 2, 100, 200, true); err == nil {
			t.Fatal("one legitimate timeout masked released slow input")
		}
	})
	for _, mode := range []string{"overflow", "missing_pid", "snapshot_version", "reversed_window"} {
		t.Run(mode, func(t *testing.T) {
			h := fixture.HealthSnapshot{PID: 1, Version: 2}
			start, end := int64(100), int64(200)
			switch mode {
			case "overflow":
				h.Overflow = true
			case "missing_pid":
				h.PID = 0
			case "snapshot_version":
				h.Version = 3
			case "reversed_window":
				end = start - 1
			}
			if _, _, err := g2CompetitionTargetCounts(h, 2, start, end, true); err == nil {
				t.Fatal("malformed source/window accepted")
			}
		})
	}
}
