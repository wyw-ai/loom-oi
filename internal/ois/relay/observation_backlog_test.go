package relay

import (
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"testing"
)

func TestObservationBacklogCountsSourceSlotsOnce(t *testing.T) {
	e := newEnv(t)
	b := protocol.ObservationBinding{}
	e.h.mu.Lock()
	for name, d := range map[string]directoryObservation{
		"dirty": {dirty: true}, "query": {queryID: "q1"}, "both": {dirty: true, queryID: "q2"}, "idle": {},
	} {
		e.h.exposures[name] = &exposure{binding: &b, probeSupport: protocol.ProbeSupported, observation: d}
	}
	e.h.exposures["unsupported"] = &exposure{binding: &b, probeSupport: protocol.ProbeUnsupported, observation: directoryObservation{dirty: true}}
	e.h.exposures["legacy"] = &exposure{probeSupport: protocol.ProbeSupported, observation: directoryObservation{dirty: true}}
	e.h.mu.Unlock()
	s := e.h.Stats().Observation
	if s.ObservationCapacity != 4 || s.ObservationBacklog != 3 || s.DirtyMarked != 2 {
		t.Fatalf("source slot union: %+v", s)
	}
}
