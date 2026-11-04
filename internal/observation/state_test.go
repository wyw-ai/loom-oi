package observation

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func testOptions() PlanOptions {
	return PlanOptions{URL: "http://127.0.0.1:8080/ready", AllowedCIDRs: []string{"127.0.0.0/8"},
		ReadyStatuses: []int{200}, NotReadyStatuses: []int{503}, Interval: 500 * time.Millisecond,
		Timeout: 200 * time.Millisecond, Validity: time.Second, Jitter: 50 * time.Millisecond}
}

func testPlan(t *testing.T) *Plan {
	t.Helper()
	p, err := NewPlan(testOptions())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func testTable(t *testing.T) (*Table, Binding, time.Time) {
	t.Helper()
	table, err := NewTable(2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = table.Reset(Session{"lab-a", 1, 1}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	b, _, err := table.Set("lab-a/http", "rv_one", testPlan(t), now)
	if err != nil {
		t.Fatal(err)
	}
	return table, b, now
}

func TestObservationAttemptSample(t *testing.T) {
	for _, state := range []Readiness{Ready, NotReady} {
		t.Run(string(state), func(t *testing.T) {
			table, b, start := testTable(t)
			token, _, err := table.Begin(b, start)
			if err != nil {
				t.Fatal(err)
			}
			reason := MatchedReady
			if state == NotReady {
				reason = MatchedNotReady
			}
			created, finishErr := table.Finish(token, Outcome{Readiness: state, Reason: reason}, start.Add(20*time.Millisecond))
			if finishErr != nil || created.SampleCreated != state || created.SampleExpired {
				t.Fatalf("creation event %+v %v", created, finishErr)
			}
			if replay, err := table.Finish(token, Outcome{Readiness: state, Reason: reason}, start.Add(21*time.Millisecond)); err != nil || replay.SampleCreated != "" || replay.SampleExpired {
				t.Fatalf("late callback duplicated metrics %+v %v", replay, err)
			}
			s, _, _ := table.Snapshot(b, start.Add(100*time.Millisecond))
			if s.Readiness != state || s.Remaining != 900*time.Millisecond || s.LastSample.Seq != 1 {
				t.Fatalf("initial sample %+v", s)
			}
			deadline := s.LastSample.ExpiresAt
			token, _, err = table.Begin(b, start.Add(200*time.Millisecond))
			if err != nil {
				t.Fatal(err)
			}
			s, _, _ = table.Snapshot(b, start.Add(200*time.Millisecond))
			if !s.Attempt.Running || s.Readiness != state || s.Attempt.Seq != 2 {
				t.Fatalf("running erased sample: %+v", s)
			}
			if _, err = table.Finish(token, Outcome{Readiness: Unknown, Reason: Timeout}, start.Add(300*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			s, _, _ = table.Snapshot(b, start.Add(300*time.Millisecond))
			if s.Readiness != state || s.Remaining != 700*time.Millisecond || s.LastSample.ExpiresAt != deadline || s.Attempt.Outcome != Timeout {
				t.Fatalf("failure renewed/replaced evidence: %+v", s)
			}
			s, fx, err := table.Snapshot(b, start.Add(time.Second-time.Microsecond))
			if err != nil {
				t.Fatal(err)
			}
			if s.Readiness != Unknown || s.Reason != Expired || s.LastSample.Readiness != state || !fx.Dirty || !fx.SampleExpired {
				t.Fatalf("expiry %+v %+v", s, fx)
			}
			again, fx, err := table.Snapshot(b, start.Add(2*time.Second))
			if err != nil || fx.Dirty || fx.SampleExpired || again.UpdateSeq != s.UpdateSeq {
				t.Fatalf("repeated expiry: %+v %+v %v", again, fx, err)
			}
		})
	}
	t.Run("late_complete", func(t *testing.T) {
		table, b, start := testTable(t)
		token, _, _ := table.Begin(b, start)
		_, err := table.Finish(token, Outcome{Readiness: Ready, Reason: MatchedReady}, start.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		s, _, _ := table.Snapshot(b, start.Add(time.Second))
		if s.Readiness != Unknown || s.Reason != Expired || s.LastSample != nil {
			t.Fatalf("late sample revived: %+v", s)
		}
	})
	t.Run("invalid_outcome", func(t *testing.T) {
		table, b, start := testTable(t)
		token, _, _ := table.Begin(b, start)
		before := table.entries[b.ExposureID]
		for _, out := range []Outcome{{Readiness: NotReady, Reason: Timeout}, {Readiness: Ready, Reason: MatchedNotReady}, {Readiness: Unknown, Reason: MatchedReady}} {
			if _, err := table.Finish(token, out, start); err == nil {
				t.Fatal("contradictory outcome accepted")
			}
			if !reflect.DeepEqual(before, table.entries[b.ExposureID]) {
				t.Fatal("invalid outcome changed state")
			}
		}
		if _, err := table.Finish(token, Outcome{Readiness: Unknown, Reason: Timeout}, start.Add(-time.Second)); err == nil {
			t.Fatal("backwards time accepted")
		}
	})
}

func TestObservationExpiryEffectsAcrossEvents(t *testing.T) {
	for _, event := range []string{"begin", "finish"} {
		t.Run(event, func(t *testing.T) {
			table, b, start := testTable(t)
			first, _, _ := table.Begin(b, start)
			_, _ = table.Finish(first, Outcome{Readiness: Ready, Reason: MatchedReady}, start.Add(time.Millisecond))
			var effect Effect
			var err error
			if event == "begin" {
				_, effect, err = table.Begin(b, start.Add(time.Second))
			} else {
				second, _, _ := table.Begin(b, start.Add(500*time.Millisecond))
				effect, err = table.Finish(second, Outcome{Readiness: NotReady, Reason: MatchedNotReady}, start.Add(time.Second))
				if effect.SampleCreated != NotReady {
					t.Fatal("new negative sample event missing")
				}
			}
			if err != nil || !effect.SampleExpired {
				t.Fatalf("expiry not reported at %s: %+v %v", event, effect, err)
			}
			_, again, _ := table.Snapshot(b, start.Add(time.Second))
			if again.SampleExpired {
				t.Fatal("same expiry reported twice")
			}
		})
	}
}

func TestObservationWorkIdentity(t *testing.T) {
	table, b, start := testTable(t)
	old, _, _ := table.Begin(b, start)
	options := testOptions()
	options.ReadyStatuses = []int{201}
	planB, err := NewPlan(options)
	if err != nil {
		t.Fatal(err)
	}
	b2, fx, err := table.Set(b.ExposureID, b.Revision, planB, start)
	if err != nil || b2.ObservationRevision != "ob_2" || fx.CancelWork == nil || *fx.CancelWork != old || !fx.InvalidatePreparing {
		t.Fatalf("change: %+v %+v %v", b2, fx, err)
	}
	b3, _, err := table.Set(b.ExposureID, b.Revision, testPlan(t), start)
	if err != nil || b3.ObservationRevision != "ob_3" || b3.ContractDigest != b.ContractDigest {
		t.Fatalf("A-B-A %+v %v", b3, err)
	}
	current, _, _ := table.Begin(b3, start)
	if fx, err := table.Finish(old, Outcome{Readiness: Ready, Reason: MatchedReady}, start); err != nil || fx != (Effect{}) {
		t.Fatalf("old work applied %+v %v", fx, err)
	}
	if _, _, err = table.Begin(b3, start); !errors.Is(err, ErrBusy) {
		t.Fatal("stale worker released current slot")
	}
	if _, err = table.Finish(current, Outcome{Readiness: Ready, Reason: MatchedReady}, start); err != nil {
		t.Fatal(err)
	}
	s, _, _ := table.Snapshot(b3, start)
	if s.Attempt.Seq != 2 || s.LastSample.Seq != 1 {
		t.Fatalf("counters reset: %+v", s)
	}
	b4, fx, err := table.Set(b.ExposureID, "rv_two", testPlan(t), start)
	if err != nil || b4.ObservationRevision != b3.ObservationRevision || b4.ContractDigest != b3.ContractDigest || !fx.InvalidatePreparing {
		t.Fatalf("target-only changed probe version: %+v", b4)
	}
	if _, _, err = table.Snapshot(b3, start); !errors.Is(err, ErrBinding) {
		t.Fatal("old target binding accepted")
	}
	s, _, _ = table.Snapshot(b4, start)
	if s.Readiness != Unknown || s.Reason != SourceInvalid || s.LastSample == nil || s.Attempt.Seq != 2 {
		t.Fatalf("invalidated history lost: %+v", s)
	}
	if _, err = table.Remove(b.ExposureID); err != nil {
		t.Fatal(err)
	}
	b5, _, err := table.Set(b.ExposureID, "rv_two", testPlan(t), start)
	if err != nil || b5.ObservationRevision != "ob_4" {
		t.Fatalf("reinstatement reused version: %+v", b5)
	}
	s, _, _ = table.Snapshot(b5, start)
	if s.Attempt.Seq != 2 || s.LastSample.Seq != 1 {
		t.Fatal("tombstone lost counters/history")
	}
	if _, _, err = table.Set("lab-a/second", "rv", nil, start); err != nil {
		t.Fatal(err)
	}
	if _, err = table.Remove("lab-a/second"); err != nil {
		t.Fatal(err)
	}
	if _, fx, err = table.Set("lab-a/third", "rv", nil, start); !errors.Is(err, ErrCapacity) || !fx.EndSession || table.Identities() != 2 {
		t.Fatal("unbounded identity churn accepted")
	}
}

func TestObservationSessionAndAdmission(t *testing.T) {
	if _, err := NewTable(0); err == nil {
		t.Fatal("unbounded/zero table accepted")
	}
	table, b, start := testTable(t)
	if _, err := table.Reset(Session{"lab-a", 1, 1}); !errors.Is(err, ErrBinding) {
		t.Fatal("same generation reset allowed")
	}
	if _, err := table.Reset(Session{}); !errors.Is(err, ErrBinding) {
		t.Fatal("empty session accepted")
	}
	for _, mode := range []Mode{BestEffort, RequireReady} {
		d, _, err := table.Evaluate(b, mode, start)
		if err != nil || d.Allowed != (mode == BestEffort) {
			t.Fatalf("Unknown %s: %+v %v", mode, d, err)
		}
	}
	if _, _, err := table.Evaluate(b, "invalid", start); err == nil {
		t.Fatal("invalid mode accepted")
	}
	token, _, _ := table.Begin(b, start)
	effects := table.Disconnect()
	if len(effects) != 1 || effects[0].CancelWork == nil || len(table.Disconnect()) != 0 {
		t.Fatal("disconnect cleanup missing or duplicated")
	}
	if _, _, err := table.Snapshot(b, start); !errors.Is(err, ErrBinding) {
		t.Fatal("disconnected evidence accepted")
	}
	if _, err := table.Reset(Session{"lab-a", 1, 2}); err != nil {
		t.Fatal(err)
	}
	b2, _, _ := table.Set(b.ExposureID, b.Revision, nil, start)
	if b2.ObservationRevision != "ob_1" || b2.SourceID != "" || b2.ContractVersion != 0 || b2.ContractDigest != NullDigest {
		t.Fatalf("Unsupported binding %+v", b2)
	}
	if _, _, err := table.Begin(b2, start); !errors.Is(err, ErrNoProbe) {
		t.Fatal("unsupported started a probe")
	}
	if fx, err := table.Finish(token, Outcome{Readiness: Ready, Reason: MatchedReady}, start); err != nil || fx != (Effect{}) {
		t.Fatal("old session revived")
	}
	s, _, _ := table.Snapshot(b2, start)
	if s.Reason != Unsupported || s.LastSample != nil {
		t.Fatalf("old sample resurrected: %+v", s)
	}
	if _, fx, err := table.Set(b2.ExposureID, b2.Revision, nil, start); err != nil || fx != (Effect{}) {
		t.Fatal("unchanged config churned state")
	}
	if _, err := table.Remove("missing"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := table.Set("", "rv", nil, start); !errors.Is(err, ErrBinding) {
		t.Fatal("empty identity accepted")
	}
}

func TestObservationCounterOverflow(t *testing.T) {
	for _, name := range []string{"attempt", "update_begin", "work", "observation", "update_set", "sample_finish", "update_finish", "update_expire", "update_remove", "session"} {
		t.Run(name, func(t *testing.T) {
			table, b, start := testTable(t)
			var token WorkToken
			if name == "sample_finish" || name == "update_finish" || name == "update_expire" {
				token, _, _ = table.Begin(b, start)
			}
			if name == "update_expire" {
				_, _ = table.Finish(token, Outcome{Readiness: Ready, Reason: MatchedReady}, start)
			}
			e := table.entries[b.ExposureID]
			switch name {
			case "attempt":
				e.attempt.Seq = math.MaxUint64
			case "work":
				table.workSeq = math.MaxUint64
			case "observation":
				e.observation = math.MaxUint64
			case "sample_finish":
				e.sampleSeq = math.MaxUint64
			case "session":
				table.incarnation = math.MaxUint64
			default:
				e.updateSeq = math.MaxUint64
			}
			table.entries[b.ExposureID] = e
			before := table.entries[b.ExposureID]
			work := table.workSeq
			var fx Effect
			var err error
			switch name {
			case "observation", "update_set":
				_, fx, err = table.Set(b.ExposureID, b.Revision, nil, start)
			case "sample_finish", "update_finish":
				fx, err = table.Finish(token, Outcome{Readiness: Ready, Reason: MatchedReady}, start)
			case "update_expire":
				_, fx, err = table.Snapshot(b, start.Add(2*time.Second))
			case "update_remove":
				fx, err = table.Remove(b.ExposureID)
			case "session":
				var effects []Effect
				effects, err = table.Reset(Session{"lab-a", 1, 2})
				if len(effects) == 1 {
					fx = effects[0]
				}
			default:
				_, fx, err = table.Begin(b, start)
			}
			if !errors.Is(err, ErrOverflow) || !fx.EndSession || !reflect.DeepEqual(before, table.entries[b.ExposureID]) || work != table.workSeq {
				t.Fatalf("overflow partially committed: %+v %v", fx, err)
			}
		})
	}
}
