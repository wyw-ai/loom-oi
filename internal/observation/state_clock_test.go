package observation

import (
	"testing"
	"time"
)

// This changes only diagnostic wall metadata. It deliberately does not claim
// to adjust the machine clock or fabricate time.Time's private monotonic bits.
// StartedAt/ExpiresAt originate from time.Now (with its monotonic component).
func TestObservedAtDoesNotControlLocalValidity(t *testing.T) {
	for _, state := range []Readiness{Ready, NotReady} {
		for _, skew := range []time.Duration{-365 * 24 * time.Hour, 365 * 24 * time.Hour} {
			t.Run(string(state)+"/"+skew.String(), func(t *testing.T) {
				table, b, start := testTable(t)
				work, _, err := table.Begin(b, start)
				if err != nil {
					t.Fatal(err)
				}
				reason := MatchedReady
				if state == NotReady {
					reason = MatchedNotReady
				}
				if _, err = table.Finish(work, Outcome{Readiness: state, Reason: reason}, start.Add(20*time.Millisecond)); err != nil {
					t.Fatal(err)
				}
				entry := table.entries[b.ExposureID]
				deadline := entry.last.ExpiresAt
				// Round removes monotonic data from a display timestamp, as Finish does;
				// changing this field cannot extend either local admission or a snapshot.
				displayed := entry.last.ObservedAt.Add(skew).Round(0)
				entry.last.ObservedAt = displayed
				table.entries[b.ExposureID] = entry
				for _, mode := range []Mode{BestEffort, RequireReady} {
					before, fx, err := table.Evaluate(b, mode, start.Add(250*time.Millisecond))
					if err != nil || before.Allowed != (state == Ready) || before.Snapshot.Readiness != state || before.Snapshot.Remaining != 750*time.Millisecond || fx.SampleExpired || before.Snapshot.LastSample.ExpiresAt != deadline || !before.Snapshot.LastSample.ObservedAt.Equal(displayed) {
						t.Fatalf("metadata changed effective decision: %+v %+v %v", before, fx, err)
					}
				}
				expired, fx, err := table.Evaluate(b, RequireReady, deadline)
				if err != nil || expired.Allowed || expired.Snapshot.Readiness != Unknown || expired.Snapshot.Remaining != 0 || expired.Snapshot.Reason != Expired || !fx.SampleExpired || !expired.Snapshot.LastSample.ObservedAt.Equal(displayed) {
					t.Fatalf("metadata changed expiry: %+v %+v %v", expired, fx, err)
				}
				best, fx, err := table.Evaluate(b, BestEffort, deadline.Add(time.Second))
				if err != nil || !best.Allowed || best.Snapshot.Readiness != Unknown || best.Snapshot.Remaining != 0 || fx.SampleExpired {
					t.Fatalf("metadata changed expired C1 algebra: %+v %+v %v", best, fx, err)
				}
			})
		}
	}
}
