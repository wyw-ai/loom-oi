package limits

import (
	"fmt"
	"math"
	"time"
)

const ObservationMaxWaiters = 16

// Observation contains the adopted G2 bounded control and local probe budgets.
// It is separate from B4 limits so old configurations retain their defaults.
type Observation struct {
	MessageMax    int   `json:"message_max"`
	PendingChecks int   `json:"pending_checks"`
	QuerySlots    int   `json:"query_slots"`
	CheckMS       int64 `json:"check_ms"`
	QueryMS       int64 `json:"query_ms"`
	WriteMS       int64 `json:"write_ms"`
	RefreshMS     int64 `json:"refresh_ms"`
	ProbeWorkers  int   `json:"probe_workers"`
	ProbeQueue    int   `json:"probe_queue"`
}

// ObservationDefaults narrows counts and check times to the existing local
// B4 limits, including deliberately smaller controlled test configurations.
func ObservationDefaults(l Limits) Observation {
	return Observation{MessageMax: 4096, PendingChecks: min(32, l.NOIS), QuerySlots: min(8, l.NOIS),
		CheckMS: min(1000, l.PrepareMS), QueryMS: min(1000, l.PrepareMS), WriteMS: 100, RefreshMS: 1000,
		ProbeWorkers: min(4, l.MaxExposures), ProbeQueue: min(32, l.MaxExposures)}
}

func (o Observation) Validate(l Limits) error {
	for name, v := range map[string]int64{
		"message_max": int64(o.MessageMax), "pending_checks": int64(o.PendingChecks), "query_slots": int64(o.QuerySlots),
		"check_ms": o.CheckMS, "query_ms": o.QueryMS, "write_ms": o.WriteMS, "refresh_ms": o.RefreshMS,
		"probe_workers": int64(o.ProbeWorkers), "probe_queue": int64(o.ProbeQueue),
	} {
		if v <= 0 {
			return fmt.Errorf("observation_limits.%s must be positive", name)
		}
	}
	switch {
	case o.MessageMax > 4096:
		return fmt.Errorf("observation_limits.message_max exceeds 4096")
	case o.PendingChecks > l.NOIS || o.QuerySlots > l.NOIS:
		return fmt.Errorf("observation control slots exceed n_ois")
	case o.CheckMS > l.PrepareMS || o.QueryMS > l.PrepareMS:
		return fmt.Errorf("observation deadlines exceed prepare_ms")
	case o.CheckMS > math.MaxInt64/int64(time.Millisecond) || o.QueryMS > math.MaxInt64/int64(time.Millisecond):
		return fmt.Errorf("observation deadlines overflow time.Duration")
	case o.WriteMS > 100 || o.RefreshMS > 1000:
		return fmt.Errorf("observation write/refresh exceeds adopted bound")
	case o.ProbeWorkers > l.MaxExposures || o.ProbeQueue > l.MaxExposures:
		return fmt.Errorf("probe workers/queue exceed max_exposures")
	}
	return nil
}
