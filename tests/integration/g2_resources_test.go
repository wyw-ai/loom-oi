package integration

import (
	"context"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestG2ResourceBaselineRejectsActiveProbe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active int
		queued int
		absent bool
		want   bool
	}{{"idle", 0, 0, false, true}, {"socket_in_flight", 1, 0, false, false}, {"queued_probe", 0, 1, false, false}, {"missing_observation", 0, 0, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := resourceSnapshot{OICs: map[string]resourceProcess[protocol.OICStats]{}}
			for _, access := range []string{"lab-a", "lab-b"} {
				x := resourceProcess[protocol.OICStats]{Stats: protocol.OICStats{Observation: &protocol.ObservationRuntimeStats{}}}
				if access == "lab-b" {
					x.Stats.Observation.ProbeActive, x.Stats.Observation.ProbeQueued = tc.active, tc.queued
					if tc.absent {
						x.Stats.Observation = nil
					}
				}
				s.OICs[access] = x
			}
			if got := g2ProbeIdle(s); got != tc.want {
				t.Fatalf("all process probe sockets must be idle: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestG2ResourceDeadlineIncludesPreparation(t *testing.T) {
	// An already exhausted absolute budget must be cancelled immediately;
	// creating a fresh relative timeout would grant an additional interval.
	deadline := resourceNS() - 1
	ctx, cancel := resourceDeadlineContext(deadline)
	defer cancel()
	if ctx.Err() != context.DeadlineExceeded {
		t.Fatal("preparation time was discarded", ctx.Err())
	}
	got, ok := ctx.Deadline()
	if !ok || !got.Equal(resourceOrigin.Add(time.Duration(deadline))) {
		t.Fatal("original monotonic deadline changed", got)
	}
}
