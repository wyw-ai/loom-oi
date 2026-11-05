package integration

import (
	"fmt"
	"maps"
	"time"

	"github.com/wyw-ai/loom-oi/tests/fixture"
)

const g2CompetitionPreparationStrategy = "six-source-early-arm-pre-dial-fresh-v1"

func g2CompetitionSourceIDs() [16]string {
	ids := [16]string{"lab-a/http", "lab-a/alt"}
	for i := 2; i < len(ids); i++ {
		ids[i] = fmt.Sprintf("lab-a/competition-%02d", i)
	}
	return ids
}

// Each source is set once. In particular the second batch never releases the
// six already-held targets, and neither batch creates or renews a deadline.
func (d *g2PreparationFailure) preloadSources(set func(string, bool) uint64) error {
	if d.preloadSetStartedNS != nil || d.dropArmCompletedNS == nil || d.dropArmCount != 32 || resourceNS() >= d.DeadlineNS {
		return fmt.Errorf("preload source input must start once after the sole complete arm batch inside original deadline")
	}
	d.Phase = "preload_probe"
	d.preloadSetStartedNS = g2DiagnosticPtr(resourceNS())
	d.inputVersions = make(map[string]uint64, 16)
	ids := g2CompetitionSourceIDs()
	for i, id := range ids[:8] {
		if resourceNS() >= d.DeadlineNS {
			return fmt.Errorf("preload sources exceeded original preparation deadline")
		}
		d.inputVersions[id] = set(id, i >= 2)
	}
	d.preloadSetCompletedNS = g2DiagnosticPtr(resourceNS())
	if *d.preloadSetCompletedNS >= d.DeadlineNS {
		return fmt.Errorf("preload source completion exceeded original preparation deadline")
	}
	return nil
}

func (d *g2PreparationFailure) finishSlowSources(set func(string, bool) uint64, deadline time.Time) error {
	deadlineNS := min(d.DeadlineNS, int64(deadline.Sub(resourceOrigin)))
	if d.dropArmCompletedNS == nil || d.dropArmCount != 32 || d.finalSetStartedNS != nil || resourceNS() >= deadlineNS {
		return fmt.Errorf("final source input requires the completed sole Drop arm batch inside original deadline")
	}
	// Cycle zero has not entered the socket helper. This bound never replaces
	// the original three-second preparation deadline.
	d.Phase = "final_slow_set"
	d.finalSetStartedNS = g2DiagnosticPtr(resourceNS())
	ids := g2CompetitionSourceIDs()
	for _, id := range ids[8:] {
		if resourceNS() >= deadlineNS {
			return fmt.Errorf("final sources exceeded original preparation deadline")
		}
		d.inputVersions[id] = set(id, true)
	}
	d.SlowAppliedNS = g2DiagnosticPtr(resourceNS())
	if *d.SlowAppliedNS >= deadlineNS {
		return fmt.Errorf("final source input completed after original preparation deadline")
	}
	d.Phase = "slow_full"
	return nil
}

// The projections belong to the original Snapshot and the original young
// evaluation. No later wire history is consulted to manufacture a witness.
type g2QueryWitness struct {
	OISPID                 int                  `json:"ois_pid"`
	OICPID                 int                  `json:"oic_pid"`
	SampleStartedNS        int64                `json:"sample_started_ns"`
	SampleCompletedNS      int64                `json:"sample_completed_ns"`
	SampleReturnedNS       int64                `json:"sample_returned_ns"`
	WireEventCount         int                  `json:"wire_event_count"`
	WireLastEventID        uint64               `json:"wire_last_event_id"`
	WireSnapshotReturnedNS int64                `json:"wire_snapshot_returned_ns"`
	EvaluatedNS            int64                `json:"evaluated_ns"`
	ReturnedNS             int64                `json:"returned_ns"`
	MaxAgeMS               int64                `json:"max_age_ms"`
	Result                 bool                 `json:"result"`
	Queries                []g2PreparationQuery `json:"queries"`
	Attempt                *int                 `json:"attempt,omitempty"`
	CallbackReturnedNS     *int64               `json:"callback_returned_ns,omitempty"`
	Sample                 *g2CompetitionStats  `json:"sample,omitempty"`
}

func g2ObserveCompetitionQueries(events []g2WireEvent, now int64, maxAge time.Duration) *g2QueryWitness {
	queries := make([]g2PreparationQuery, 0, 8)
	ok := g2CompetitionYoungQueriesObserved(events, now, maxAge, &queries)
	returned := resourceNS()
	return &g2QueryWitness{EvaluatedNS: now, ReturnedNS: returned, MaxAgeMS: maxAge.Milliseconds(), Result: ok, Queries: queries}
}

func (w *g2QueryWitness) bindSample(sample g2CompetitionStats, returned int64, snapshot g2ProxySnapshot, snapshotReturned int64, oisPID, oicPID int) {
	w.OISPID, w.OICPID = oisPID, oicPID
	w.SampleStartedNS, w.SampleCompletedNS, w.SampleReturnedNS = sample.StartedNS, sample.CompletedNS, returned
	w.WireEventCount, w.WireSnapshotReturnedNS = len(snapshot.Events), snapshotReturned
	if len(snapshot.Events) > 0 {
		w.WireLastEventID = snapshot.Events[len(snapshot.Events)-1].ID
	}
}

func g2CopyCompetitionSample(s g2CompetitionStats) g2CompetitionStats {
	if s.OIS.Observation != nil {
		s.OIS.Observation = g2DiagnosticPtr(*s.OIS.Observation)
	}
	if s.OIC.Observation != nil {
		s.OIC.Observation = g2DiagnosticPtr(*s.OIC.Observation)
	}
	s.OIC.Closed = maps.Clone(s.OIC.Closed)
	return s
}

func (d *g2PreparationFailure) acceptedPreload() error {
	w := d.lastQueueWitness
	accepted := resourceNS()
	if w == nil || !w.Result || w.CallbackReturnedNS == nil || accepted >= d.DeadlineNS {
		return fmt.Errorf("probe queue observation did not complete inside original preparation deadline")
	}
	d.preloadAcceptedAttempt, d.preloadAcceptedNS = g2DiagnosticPtr(d.Attempts), g2DiagnosticPtr(accepted)
	d.preloadQueueWitness = w
	return nil
}

func (d *g2PreparationFailure) preparationFields(fields map[string]any) map[string]any {
	fields["preparation_strategy"] = g2CompetitionPreparationStrategy
	fields["preload_set_started_ns"], fields["preload_set_completed_ns"] = d.preloadSetStartedNS, d.preloadSetCompletedNS
	fields["preload_accepted_ns"], fields["final_set_started_ns"] = d.preloadAcceptedNS, d.finalSetStartedNS
	fields["drop_arm_started_ns"], fields["drop_arm_completed_ns"], fields["drop_arm_count"] = d.dropArmStartedNS, d.dropArmCompletedNS, d.dropArmCount
	fields["drop_arm_before_event_id"], fields["drop_arm_before_snapshot_returned_ns"] = d.dropArmBeforeEventID, d.dropArmBeforeSnapshotReturnedNS
	fields["input_versions"] = maps.Clone(d.inputVersions)
	fields["pre_dial_context_deadline_ns"], fields["pre_dial_accepted_ns"] = d.preDialContextDeadlineNS, d.preDialAcceptedNS
	return fields
}

// Server-side cancellation without headers is the existing timeout evidence;
// it is not a new claim about the executor's complete 200ms duration.
func g2CompetitionTargetCounts(h fixture.HealthSnapshot, version uint64, start, end int64, slow bool) (attempts, timeouts int, err error) {
	if h.Overflow || h.PID <= 0 || version == 0 || h.Version != version || start <= 0 || end < start {
		return 0, 0, fmt.Errorf("invalid competition health identity/window")
	}
	for _, r := range h.Records {
		if r.StartedNS < start || r.StartedNS > end {
			continue
		}
		if r.Version != version {
			return 0, 0, fmt.Errorf("competition target changed version inside exercise window")
		}
		attempts++
		if slow && r.HeaderStartedNS > 0 && r.HeaderStartedNS <= end {
			return 0, 0, fmt.Errorf("slow competition target released headers inside exercise window")
		}
		if r.CancelledNS > r.StartedNS && r.CancelledNS <= end && r.HeaderStartedNS == 0 {
			timeouts++
		}
	}
	return attempts, timeouts, nil
}
