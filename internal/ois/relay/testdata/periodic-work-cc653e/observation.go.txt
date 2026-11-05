package relay

import (
	"slices"
	"strconv"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func isObservationTelemetry(m protocol.Message) bool {
	switch m.(type) {
	case *protocol.ObservationChanged, *protocol.ObservationQuery, *protocol.ObservationSnapshot:
		return true
	}
	return false
}

func (h *Hub) acceptObservationIdentity(s *session, b protocol.ObservationBinding, limit int) bool {
	if !protocol.ValidObservationBinding(b, false) {
		h.endSession(s, causeLost, protocol.CodeProtocolError, "invalid registered observation binding")
		return false
	}
	if old, seen := s.identities[b.ExposureID]; seen {
		before, _ := strconv.ParseUint(old.ObservationRevision[3:], 10, 64)
		after, _ := strconv.ParseUint(b.ObservationRevision[3:], 10, 64)
		present := h.exposures[b.ExposureID] != nil && h.exposures[b.ExposureID].gen == s.gen
		if after < before || (after == before && (old.ContractDigest != b.ContractDigest || old.SourceID != b.SourceID || old.ContractVersion != b.ContractVersion || !present)) {
			h.endSession(s, causeLost, protocol.CodeProtocolError, "observation revision was reused or moved backwards")
			return false
		}
	} else if len(s.identities) >= limit {
		h.endSession(s, causeLost, protocol.CodeResourceExhausted, "observation identity capacity exhausted")
		return false
	}
	s.identities[b.ExposureID] = b
	return true
}

type directoryObservation struct {
	record     *protocol.ObservationSnapshot
	lastSample *protocol.ObservationSnapshot
	history    *protocol.ReadinessReport
	deadline   time.Time
	queryID    string
	dirty      bool
	refreshAt  time.Time
}
type directoryQuery struct {
	id                string
	sess              *session
	binding           protocol.ObservationBinding
	started, deadline time.Time
}

func wireObservationLimits(o limits.Observation) *protocol.ObservationLimits {
	return &protocol.ObservationLimits{MessageMax: o.MessageMax, PendingChecks: o.PendingChecks, QuerySlots: o.QuerySlots, CheckMS: o.CheckMS, QueryMS: o.QueryMS, WriteMS: o.WriteMS, RefreshMS: o.RefreshMS}
}

func (h *Hub) observationOrder() {
	h.queryOrder = h.queryOrder[:0]
	for id, e := range h.exposures {
		if e.binding != nil && e.probeSupport == protocol.ProbeSupported {
			h.queryOrder = append(h.queryOrder, id)
		}
	}
	slices.Sort(h.queryOrder)
	if h.queryCursor >= len(h.queryOrder) {
		h.queryCursor = 0
	}
}

func (h *Hub) dropQuery(q *directoryQuery) {
	if q == nil || h.queries[q.id] != q {
		return
	}
	delete(h.queries, q.id)
	if exp := h.exposures[q.binding.ExposureID]; exp != nil && exp.observation.queryID == q.id {
		exp.observation.queryID = ""
	}
}

func (h *Hub) pumpQueries(now time.Time) {
	if h.shutting {
		return
	}
	for visited := 0; visited < len(h.queryOrder) && len(h.queries) < h.oblim.QuerySlots; visited++ {
		id := h.queryOrder[h.queryCursor]
		h.queryCursor = (h.queryCursor + 1) % len(h.queryOrder)
		exp := h.exposures[id]
		if exp == nil || exp.binding == nil || exp.probeSupport != protocol.ProbeSupported || exp.observation.queryID != "" {
			continue
		}
		a := h.accesses[exp.rec.AccessID]
		if a == nil || !a.online() || a.sess.version != protocol.ControlSubprotocolV2 {
			continue
		}
		if !exp.observation.dirty && now.Before(exp.observation.refreshAt) {
			continue
		}
		s := a.sess
		id, err := s.ids.Next()
		if err != nil {
			h.endSession(s, causeLost, "", "")
			continue
		}
		q := &directoryQuery{id: id, sess: s, binding: *exp.binding, started: now, deadline: now.Add(limits.D(h.oblim.QueryMS))}
		h.queries[id] = q
		exp.observation.queryID = id
		exp.observation.dirty = false
		exp.observation.refreshAt = now.Add(limits.D(h.oblim.RefreshMS))
		m := &protocol.ObservationQuery{QueryID: id, Binding: q.binding, DeadlineMS: h.oblim.QueryMS}
		if !s.sendTelemetry(m, func(sent time.Time) bool {
			if h.queries[id] != q {
				return false
			}
			if !sent.Before(q.deadline) || ms(q.deadline.Sub(sent)) <= 0 {
				h.dropQuery(q)
				return false
			}
			m.DeadlineMS = ms(q.deadline.Sub(sent))
			return true
		}) {
			h.dropQuery(q)
			exp.observation.dirty = true
		}
	}
}

func (h *Hub) onObservationChanged(s *session, m *protocol.ObservationChanged, now time.Time) {
	if m.Binding.AccessID != s.accessID || m.Binding.Epoch != s.epoch || m.Binding.Generation != s.gen {
		h.endSession(s, causeLost, protocol.CodeProtocolError, "observation notification has another session identity")
		return
	}
	exp := h.exposures[m.Binding.ExposureID]
	if exp == nil || exp.binding == nil || *exp.binding != m.Binding {
		return
	}
	if exp.observation.record != nil && m.UpdateSeq <= exp.observation.record.UpdateSeq {
		return
	}
	if exp.observation.dirty {
		h.metrics.Add(metrics.DirtyCoalesced, "")
	}
	exp.observation.dirty = true
	h.pumpQueries(now)
}

func equalSample(a, b *protocol.ObservationSnapshot) bool {
	if (a.SampleSeq == nil) != (b.SampleSeq == nil) {
		return false
	}
	if a.SampleSeq != nil && *a.SampleSeq != *b.SampleSeq {
		return false
	}
	if (a.ObservedAt == nil) != (b.ObservedAt == nil) {
		return false
	}
	return a.Readiness == b.Readiness && a.Reason == b.Reason && (a.ObservedAt == nil || a.ObservedAt.Equal(*b.ObservedAt))
}
func cloneSnapshot(m *protocol.ObservationSnapshot) *protocol.ObservationSnapshot {
	copy := *m
	copy.SampleSeq = cloneSeq(m.SampleSeq)
	if m.ObservedAt != nil {
		at := *m.ObservedAt
		copy.ObservedAt = &at
	}
	return &copy
}

func (h *Hub) onObservationSnapshot(s *session, m *protocol.ObservationSnapshot, now time.Time) {
	q := h.queries[m.QueryID]
	if q == nil {
		return
	}
	if q.sess != s || q.binding != m.Binding || m.RemainingMS > int64(24*time.Hour/time.Millisecond) {
		h.endSession(s, causeLost, protocol.CodeProtocolError, "snapshot does not match its pending query")
		return
	}
	exp := h.exposures[q.binding.ExposureID]
	if exp == nil || exp.binding == nil || *exp.binding != q.binding {
		h.dropQuery(q)
		return
	}
	if !now.Before(q.deadline) {
		h.dropQuery(q)
		h.metrics.Observe(metrics.QueryRTT, "timeout", max(0, now.Sub(q.started)))
		h.pumpQueries(now)
		return
	}
	d := &exp.observation
	if old := d.record; old != nil {
		if m.UpdateSeq < old.UpdateSeq {
			h.dropQuery(q)
			h.metrics.Observe(metrics.QueryRTT, "rejected", max(0, now.Sub(q.started)))
			h.pumpQueries(now)
			return
		}
		if (m.UpdateSeq == old.UpdateSeq && (!equalSample(m, old) || m.Attempt != old.Attempt || m.RemainingMS > old.RemainingMS)) || m.Attempt.Seq < old.Attempt.Seq ||
			(m.Attempt.Seq == old.Attempt.Seq && old.Attempt.State == "idle" && m.Attempt != old.Attempt) {
			h.endSession(s, causeLost, protocol.CodeProtocolError, "snapshot update conflicts with prior immutable state")
			return
		}
	}
	if m.SampleSeq != nil && d.lastSample != nil {
		if *m.SampleSeq < *d.lastSample.SampleSeq {
			h.dropQuery(q)
			h.metrics.Observe(metrics.QueryRTT, "rejected", max(0, now.Sub(q.started)))
			h.pumpQueries(now)
			return
		}
		if *m.SampleSeq == *d.lastSample.SampleSeq && !equalSample(m, d.lastSample) {
			h.endSession(s, causeLost, protocol.CodeProtocolError, "same sample has conflicting immutable content")
			return
		}
	}
	if m.SampleSeq != nil {
		deadline := now.Add(max(0, limits.D(m.RemainingMS)-now.Sub(q.started)))
		if d.lastSample != nil && *d.lastSample.SampleSeq == *m.SampleSeq && !d.deadline.IsZero() && d.deadline.Before(deadline) {
			deadline = d.deadline
		}
		d.deadline = deadline
		d.lastSample = cloneSnapshot(m)
		d.history = &protocol.ReadinessReport{Readiness: m.Readiness, SampleSeq: *m.SampleSeq, ObservedAt: d.lastSample.ObservedAt}
	}
	d.record = cloneSnapshot(m)
	h.dropQuery(q)
	h.metrics.Observe(metrics.QueryRTT, "success", max(0, now.Sub(q.started)))
	h.pumpQueries(now)
}

func (e *exposure) directoryView(now time.Time) (string, *protocol.ReadinessView) {
	if e.binding == nil {
		return protocol.ReadinessUnknown, nil
	}
	b := e.binding
	v := &protocol.ReadinessView{Authority: "directory_snapshot", Revision: b.Revision, Generation: b.Generation, ObservationRevision: b.ObservationRevision, ContractDigest: b.ContractDigest, Reason: "no_sample", LastAttempt: protocol.ObservationAttempt{State: "idle"}}
	if e.probeSupport == protocol.ProbeUnsupported {
		v.Reason = "unsupported"
	} else {
		v.Source = "http_status_v1"
	}
	d := &e.observation
	if d.history != nil {
		copy := *d.history
		v.LastReported = &copy
	}
	readiness := protocol.ReadinessUnknown
	if d.record != nil {
		v.LastAttempt = d.record.Attempt
		v.Reason = d.record.Reason
		if d.record.Readiness != protocol.ReadinessUnknown {
			v.RemainingMS = max(0, ms(d.deadline.Sub(now)))
			if v.RemainingMS > 0 {
				readiness = d.record.Readiness
				v.SampleSeq = cloneSeq(d.record.SampleSeq)
			} else {
				v.Reason = "expired"
			}
		}
	}
	return readiness, v
}

func (h *Hub) sweepObservations(now time.Time) {
	for op := range h.operations {
		if _, _, err := h.openPreconditions(op.creator, op.req, op, now); err != nil {
			h.finishOpen(op, nil, err)
		} else if !now.Before(op.deadline) {
			h.finishOpen(op, nil, protocol.Errorf(protocol.CodePrepareTimeout, "total preparation budget exhausted"))
		}
	}
	for _, check := range h.admissions {
		if !now.Before(check.deadline) {
			h.timeoutAdmission(check, now)
		}
	}
	for _, q := range h.queries {
		if !now.Before(q.deadline) {
			h.dropQuery(q)
			h.metrics.Observe(metrics.QueryRTT, "timeout", max(0, now.Sub(q.started)))
		}
	}
	h.pumpQueries(now)
}
