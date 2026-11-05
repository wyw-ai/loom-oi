package relay

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const (
	PendingIdentityHeader    = "X-OI-Diagnostic-Sample"
	PendingIdentitySchema    = "g2-pending-identity-v1"
	PendingIdentityMessage   = "observation_pending_snapshot"
	PendingIdentityMaxBytes  = 64 << 10
	pendingIdentityQueryCap  = 8
	pendingIdentitySourceCap = 16
)

// PendingIdentitySnapshot is a finite debugging record, not an HTTP response or
// an additional readiness/dispatch condition. Every coordinator value belongs
// to the same lock acquisition as the accompanying Stats counts.
type PendingIdentitySnapshot struct {
	Schema              string                  `json:"schema"`
	SampleID            string                  `json:"sample_id"`
	PID                 int                     `json:"pid"`
	RunID               *string                 `json:"run_id"`
	Epoch               int64                   `json:"epoch"`
	QuerySlots          int                     `json:"query_slots"`
	QueryMS             int64                   `json:"query_ms"`
	RefreshMS           int64                   `json:"refresh_ms"`
	ObservationCapacity int                     `json:"observation_capacity"`
	ObservationBacklog  int                     `json:"observation_backlog"`
	PendingQueries      int                     `json:"pending_queries"`
	DirtyMarked         int                     `json:"dirty_marked"`
	Complete            bool                    `json:"complete"`
	Reasons             []string                `json:"reasons"`
	Omitted             PendingIdentityOmitted  `json:"omitted"`
	Pending             []PendingIdentityQuery  `json:"pending"`
	Sources             []PendingIdentitySource `json:"sources"`
	QueryOrder          []*string               `json:"query_order"`
	QueryCursor         int                     `json:"query_cursor"`
}

type PendingIdentityOmitted struct {
	Pending    int `json:"pending"`
	Sources    int `json:"sources"`
	QueryOrder int `json:"query_order"`
}

type PendingIdentityQuery struct {
	QueryID        *string                      `json:"query_id"`
	Binding        *protocol.ObservationBinding `json:"binding"`
	AgeNS          *int64                       `json:"age_ns"`
	RemainingNS    *int64                       `json:"remaining_ns"`
	BindingCurrent bool                         `json:"binding_current"`
	SessionCurrent bool                         `json:"session_current"`
	Online         *bool                        `json:"online"`
}

type PendingIdentitySource struct {
	Binding            *protocol.ObservationBinding `json:"binding"`
	QueryID            *string                      `json:"query_id"`
	Dirty              bool                         `json:"dirty"`
	PeriodicOnly       bool                         `json:"periodic_only"`
	Online             *bool                        `json:"online"`
	ControlVersion     *string                      `json:"control_version"`
	CurrentGeneration  *int64                       `json:"current_generation"`
	RefreshAtSet       bool                         `json:"refresh_at_set"`
	RefreshRemainingNS *int64                       `json:"refresh_remaining_ns"`
}

func ValidPendingIdentitySampleID(id string) bool {
	return len(id) == 32 && protocol.ValidControlID(id)
}

// StatsWithPendingIdentity preserves Stats' original process/atomic read order.
// Its caller must apply the existing debug-level and authenticated-route gates.
// Invalid correlation IDs fall back to the ordinary path without a clock read.
func (h *Hub) StatsWithPendingIdentity(sampleID string) (protocol.OISStats, *PendingIdentitySnapshot) {
	if !ValidPendingIdentitySampleID(sampleID) {
		return h.Stats(), nil
	}
	s, d := h.statsPendingIdentity(sampleID)
	// Sort owned copies only, after releasing the coordinator lock. Keep the
	// original query order/cursor unchanged, including on overflow.
	slices.SortFunc(d.Pending, func(a, b PendingIdentityQuery) int {
		return strings.Compare(pendingString(a.QueryID), pendingString(b.QueryID))
	})
	slices.SortFunc(d.Sources, func(a, b PendingIdentitySource) int {
		return strings.Compare(pendingExposure(a.Binding), pendingExposure(b.Binding))
	})
	return s, d
}

func pendingString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func pendingExposure(b *protocol.ObservationBinding) string {
	if b == nil {
		return ""
	}
	return b.ExposureID
}

func pendingValue[T any](v T) *T { return &v }

func (d *PendingIdentitySnapshot) incomplete(reason string) {
	d.Complete = false
	if !slices.Contains(d.Reasons, reason) {
		d.Reasons = append(d.Reasons, reason)
	}
}

func (d *PendingIdentitySnapshot) queryID(id string) *string {
	if !ValidPendingIdentitySampleID(id) {
		d.incomplete("invalid_identity")
		return nil
	}
	return pendingValue(id)
}

func (d *PendingIdentitySnapshot) binding(b protocol.ObservationBinding) *protocol.ObservationBinding {
	// Check byte bounds before invoking syntax validators or copying strings.
	if len(b.AccessID) > 63 || len(b.ExposureID) > 127 || len(b.Revision) != 29 || len(b.ObservationRevision) > 23 ||
		len(b.ContractDigest) != 64 || len(b.SourceID) > 5 || b.SourceID != "local" || b.ContractVersion != 1 ||
		!protocol.ValidObservationBinding(b, false) {
		d.incomplete("invalid_identity")
		return nil
	}
	return pendingValue(b)
}

func pendingRunID(id string) bool {
	if len(id) != 30 || !strings.HasPrefix(id, "run_") {
		return false
	}
	for _, c := range id[4:] {
		if !(c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// pendingIdentityLocked only copies bounded values; it does not encode, log,
// expire, allocate query IDs, or materialize periodic work.
func (h *Hub) pendingIdentityLocked(sampleID string, stats *protocol.ObservationRuntimeStats, sources []*exposure, now time.Time) *PendingIdentitySnapshot {
	d := &PendingIdentitySnapshot{Schema: PendingIdentitySchema, SampleID: sampleID, PID: os.Getpid(), Epoch: h.epoch,
		QuerySlots: h.oblim.QuerySlots, QueryMS: h.oblim.QueryMS, RefreshMS: h.oblim.RefreshMS,
		ObservationCapacity: stats.ObservationCapacity, ObservationBacklog: stats.ObservationBacklog,
		PendingQueries: stats.PendingQueries, DirtyMarked: stats.DirtyMarked,
		Complete: true, Reasons: []string{}, Pending: make([]PendingIdentityQuery, 0, pendingIdentityQueryCap),
		Sources: make([]PendingIdentitySource, 0, pendingIdentitySourceCap), QueryOrder: make([]*string, 0, pendingIdentitySourceCap), QueryCursor: h.queryCursor}
	if pendingRunID(h.runID) {
		d.RunID = pendingValue(h.runID)
	} else {
		d.incomplete("invalid_identity")
	}
	if h.epoch <= 0 || h.oblim.QuerySlots <= 0 || h.oblim.QueryMS <= 0 || h.oblim.RefreshMS <= 0 {
		d.incomplete("invalid_state")
	}
	for key, q := range h.queries {
		if len(d.Pending) == pendingIdentityQueryCap {
			break
		}
		x := PendingIdentityQuery{}
		if q == nil {
			d.incomplete("invalid_state")
			d.Pending = append(d.Pending, x)
			continue
		}
		x.QueryID, x.Binding = d.queryID(q.id), d.binding(q.binding)
		if key != q.id || q.sess == nil {
			d.incomplete("invalid_state")
		}
		if q.started.IsZero() || q.deadline.IsZero() {
			d.incomplete("invalid_time")
		} else {
			x.AgeNS, x.RemainingNS = pendingValue(now.Sub(q.started).Nanoseconds()), pendingValue(q.deadline.Sub(now).Nanoseconds())
			if *x.AgeNS < 0 || q.deadline.Before(q.started) {
				d.incomplete("invalid_time")
			}
		}
		if x.Binding != nil {
			if e := h.exposures[q.binding.ExposureID]; e != nil {
				x.BindingCurrent = e.binding != nil && *e.binding == q.binding
			}
			if a := h.accesses[q.binding.AccessID]; a != nil {
				x.SessionCurrent = q.sess != nil && a.sess == q.sess
				x.Online = pendingValue(a.online())
			}
		}
		d.Pending = append(d.Pending, x)
	}
	for _, e := range sources {
		x := PendingIdentitySource{Binding: d.binding(*e.binding), Dirty: e.observation.dirty,
			PeriodicOnly: e.observation.periodicOnly, RefreshAtSet: !e.observation.refreshAt.IsZero()}
		if e.observation.queryID != "" {
			x.QueryID = d.queryID(e.observation.queryID)
		}
		if x.RefreshAtSet {
			x.RefreshRemainingNS = pendingValue(e.observation.refreshAt.Sub(now).Nanoseconds())
		}
		if len(e.rec.AccessID) > 63 || !protocol.ValidAccessID(e.rec.AccessID) || e.rec.AccessID != e.binding.AccessID || e.rec.ExposureID != e.binding.ExposureID {
			d.incomplete("invalid_identity")
		} else if a := h.accesses[e.rec.AccessID]; a != nil {
			x.Online, x.CurrentGeneration = pendingValue(a.online()), pendingValue(a.gen)
			if a.gen < 0 {
				d.incomplete("invalid_state")
			}
			if a.sess != nil {
				if a.sess.version == protocol.ControlSubprotocol || a.sess.version == protocol.ControlSubprotocolV2 {
					x.ControlVersion = pendingValue(a.sess.version)
				} else {
					d.incomplete("invalid_identity")
				}
			}
		}
		d.Sources = append(d.Sources, x)
	}
	for _, id := range h.queryOrder[:min(len(h.queryOrder), pendingIdentitySourceCap)] {
		if len(id) > 127 {
			d.incomplete("invalid_identity")
			d.QueryOrder = append(d.QueryOrder, nil)
			continue
		}
		if _, _, ok := protocol.SplitExposureID(id); !ok {
			d.incomplete("invalid_identity")
			d.QueryOrder = append(d.QueryOrder, nil)
		} else {
			d.QueryOrder = append(d.QueryOrder, pendingValue(id))
		}
	}
	d.Omitted = PendingIdentityOmitted{Pending: stats.PendingQueries - len(d.Pending), Sources: stats.ObservationCapacity - len(d.Sources), QueryOrder: len(h.queryOrder) - len(d.QueryOrder)}
	if d.Omitted.Pending > 0 || d.Omitted.Sources > 0 || d.Omitted.QueryOrder > 0 {
		d.incomplete("capacity")
	}
	if h.queryCursor < 0 || (len(h.queryOrder) == 0 && h.queryCursor != 0) || (len(h.queryOrder) > 0 && h.queryCursor >= len(h.queryOrder)) {
		d.incomplete("invalid_state")
	}
	return d
}

// JSONPayload runs after unlocking. The copied identity values have already
// passed fixed byte bounds. An unexpected encoding/size failure emits only a
// finite incomplete summary; never the oversized original payload.
func (d *PendingIdentitySnapshot) JSONPayload() json.RawMessage {
	raw, err := json.Marshal(d)
	if err == nil && len(raw) <= PendingIdentityMaxBytes {
		return raw
	}
	summary := PendingIdentitySnapshot{Schema: PendingIdentitySchema, PID: d.PID, Epoch: d.Epoch,
		QuerySlots: d.QuerySlots, QueryMS: d.QueryMS, RefreshMS: d.RefreshMS,
		ObservationCapacity: d.ObservationCapacity, ObservationBacklog: d.ObservationBacklog,
		PendingQueries: d.PendingQueries, DirtyMarked: d.DirtyMarked, QueryCursor: d.QueryCursor,
		Reasons: []string{"payload"}, Pending: []PendingIdentityQuery{}, Sources: []PendingIdentitySource{}, QueryOrder: []*string{},
		Omitted: PendingIdentityOmitted{Pending: d.PendingQueries, Sources: d.ObservationCapacity, QueryOrder: d.Omitted.QueryOrder + len(d.QueryOrder)}}
	if ValidPendingIdentitySampleID(d.SampleID) {
		summary.SampleID = d.SampleID
	}
	if d.RunID != nil && pendingRunID(*d.RunID) {
		summary.RunID = pendingValue(*d.RunID)
	}
	raw, _ = json.Marshal(&summary)
	return raw
}
