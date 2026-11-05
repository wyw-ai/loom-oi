// The methods below are mechanically extracted from cc653e0554c57fd13857e74e431e011c4e219dfd.
// TestObservationPeriodicWorkTraceEquivalence verifies their exact derivation.
package relay

import (
	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"time"
)

func (h *Hub) periodicLegacyPumpQueries(now time.Time) {
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

func (h *Hub) periodicLegacyOnObservationChanged(s *session, m *protocol.ObservationChanged, now time.Time) {
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
	h.periodicLegacyPumpQueries(now)
}

func (h *Hub) periodicLegacyOnObservationSnapshot(s *session, m *protocol.ObservationSnapshot, now time.Time) {
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
		h.periodicLegacyPumpQueries(now)
		return
	}
	d := &exp.observation
	if old := d.record; old != nil {
		if m.UpdateSeq < old.UpdateSeq {
			h.dropQuery(q)
			h.metrics.Observe(metrics.QueryRTT, "rejected", max(0, now.Sub(q.started)))
			h.periodicLegacyPumpQueries(now)
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
			h.periodicLegacyPumpQueries(now)
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
	h.periodicLegacyPumpQueries(now)
}

func (h *Hub) periodicLegacySweepObservations(now time.Time) {
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
	h.periodicLegacyPumpQueries(now)
}

func (h *Hub) periodicLegacyOnRegister(s *session, m *protocol.Register, now time.Time) {
	a := h.accesses[s.accessID]
	limit := min(a.rec.MaxExposures, h.lim.MaxExposures)
	ack := &protocol.RegisterAck{Accepted: []string{}, Rejected: []protocol.Rejection{}}
	next := map[string]*exposure{}
	for _, e := range m.Exposures {
		if code := h.checkEntry(s.accessID, e, next, limit); code != "" {
			ack.Rejected = append(ack.Rejected, protocol.Rejection{ExposureID: e.ExposureID, Code: code})
			continue
		}
		if s.version == protocol.ControlSubprotocolV2 {
			if e.Binding == nil || e.Binding.AccessID != s.accessID || e.Binding.Epoch != s.epoch || e.Binding.Generation != s.gen || e.Binding.ExposureID != e.ExposureID || e.Binding.Revision != e.Revision {
				h.endSession(s, causeLost, protocol.CodeProtocolError, "register binding does not match this session")
				return
			}
			if e.Binding.SourceID != "" && e.Binding.ContractVersion != 1 {
				ack.Rejected = append(ack.Rejected, protocol.Rejection{ExposureID: e.ExposureID, Code: protocol.CodeVersionUnsupported})
				continue
			}
			if !h.acceptObservationIdentity(s, *e.Binding, limit) {
				return
			}
		}
		n := &exposure{gen: s.gen, rec: store.Exposure{
			ExposureID: e.ExposureID, AccessID: s.accessID, Revision: e.Revision, Protocol: e.Protocol,
			TransportProfile: e.TransportProfile, AdmissionMode: e.AdmissionMode, Description: e.Description,
			UpdatedAt: now.UTC().Truncate(time.Microsecond),
		}}
		if s.version == protocol.ControlSubprotocolV2 {
			binding := *e.Binding
			n.binding = &binding
			n.probeSupport = e.ProbeSupport
			n.observation.dirty = true
			if old := h.exposures[e.ExposureID]; old != nil {
				if old.binding != nil && *old.binding == binding {
					n.observation = old.observation
				} else {
					n.observation.history = old.observation.history
				}
			}
		}
		next[e.ExposureID] = n
		ack.Accepted = append(ack.Accepted, e.ExposureID)
	}
	old := map[string]*exposure{}
	for id, e := range h.exposures {
		if e.rec.AccessID == s.accessID {
			old[id] = e
		}
	}
	var audits []store.AuditEntry
	changed := false
	for id, o := range old {
		n := next[id]
		if o.observation.queryID != "" && (n == nil || n.binding == nil || o.binding == nil || *n.binding != *o.binding) {
			h.dropQuery(h.queries[o.observation.queryID])
		}
		switch {
		case n == nil:
			audits = append(audits, regAudit(now, s.accessID, "exposure_removed", id, o.rec.Revision, ""))
		case n.rec.Revision != o.rec.Revision:
			audits = append(audits, regAudit(now, s.accessID, "exposure_revision_changed", id, o.rec.Revision, n.rec.Revision))
		case sameEntry(n.rec, o.rec):
			n.rec.UpdatedAt = o.rec.UpdatedAt
			continue
		}
		changed = true
	}
	for id, n := range next {
		if old[id] == nil {
			audits = append(audits, regAudit(now, s.accessID, "exposure_added", id, "", n.rec.Revision))
			changed = true
		}
	}
	for _, ch := range h.channels {
		if ch.accessID != s.accessID {
			continue
		}
		if n := next[ch.exposureID]; n == nil || n.rec.Revision != ch.revision {
			h.terminate(ch, protocol.CodeRevisionChanged, now)
		} else if ch.state == statePreparing && ch.binding != nil && (n.binding == nil || *n.binding != *ch.binding) {
			h.terminateReadiness(ch, protocol.CodeReadinessCheckFailed, "observation_changed", now)
		}
	}
	for id := range old {
		delete(h.exposures, id)
	}
	for id, n := range next {
		h.exposures[id] = n
	}
	if changed {
		h.regSeq++
		p := h.regDirty[s.accessID]
		if p == nil {
			p = &regPending{}
			h.regDirty[s.accessID] = p
		}
		p.seq = h.regSeq
		p.audits = append(p.audits, audits...)
		if over := len(p.audits) - maxPendingAudits; over > 0 {
			p.audits = p.audits[over:]
		}
		h.kickPersist()
	}
	s.registered = true
	s.send(ack)
	h.observationOrder()
	for op := range h.operations {
		if op.sess == s {
			if _, _, err := h.openPreconditions(op.creator, op.req, op, now); err != nil {
				h.finishOpen(op, nil, err)
			}
		}
	}
	h.periodicLegacyPumpQueries(now)
}
