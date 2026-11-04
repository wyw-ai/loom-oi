package relay

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func wireBinding(b observation.Binding) protocol.ObservationBinding {
	return protocol.ObservationBinding{AccessID: b.AccessID, Epoch: b.Epoch, Generation: b.Generation, ExposureID: b.ExposureID, Revision: b.Revision, ObservationRevision: b.ObservationRevision, ContractDigest: b.ContractDigest, SourceID: b.SourceID, ContractVersion: b.ContractVersion}
}
func localBinding(b protocol.ObservationBinding) observation.Binding {
	return observation.Binding{AccessID: b.AccessID, Epoch: b.Epoch, Generation: b.Generation, ExposureID: b.ExposureID, Revision: b.Revision, ObservationRevision: b.ObservationRevision, ContractDigest: b.ContractDigest, SourceID: b.SourceID, ContractVersion: b.ContractVersion}
}

func narrowerMode(a, b string) observation.Mode {
	if a == protocol.AdmissionRequireReady || b == protocol.AdmissionRequireReady {
		return observation.RequireReady
	}
	return observation.BestEffort
}

func (a *Agent) effectMetrics(e observation.Effect) {
	if e.SampleCreated != "" {
		a.metrics.Add(metrics.Samples, string(e.SampleCreated))
	}
	if e.SampleExpired {
		a.metrics.Add(metrics.SampleExpired, "")
	}
}

// resetObservations is called under Agent.mu while adopting a new session.
func (a *Agent) resetObservations(s *session, w *protocol.Welcome) error {
	if _, err := a.observations.Reset(observation.Session{AccessID: a.cfg.AccessID, Epoch: w.Epoch, Generation: w.Generation}); err != nil {
		return err
	}
	a.bindings = make(map[string]observation.Binding)
	a.accepted = make(map[string]observation.Binding)
	for _, e := range a.cfg.Exposures {
		if !e.IsEnabled() {
			continue
		}
		b, effect, err := a.observations.Set(e.ID, e.Revision, e.ProbePlan(), time.Now())
		a.effectMetrics(effect)
		if err != nil {
			return err
		}
		a.bindings[e.ID] = b
	}
	return nil
}

func (a *Agent) newScheduler(s *session) (*observation.Scheduler, error) {
	return observation.NewScheduler(observation.SchedulerOptions{
		Workers: s.limits.ProbeWorkers, Queue: s.limits.ProbeQueue, MaxExposures: a.cfg.Limits.MaxExposures,
		OnStart: func(b observation.Binding, started time.Time, wait time.Duration) (observation.WorkToken, bool) {
			a.mu.Lock()
			if a.sess != s || a.bindings[b.ExposureID] != b {
				a.mu.Unlock()
				return observation.WorkToken{}, false
			}
			token, effect, err := a.observations.Begin(b, started)
			a.effectMetrics(effect)
			a.scheduleExpiryLocked(s)
			a.mu.Unlock()
			if effect.EndSession {
				s.cancel()
				_ = s.c.CloseNow()
			}
			if err != nil {
				return token, false
			}
			a.metrics.Observe(metrics.ProbeQueueWait, "", wait)
			a.markDirty(s, b)
			return token, true
		},
		OnFinish: func(token observation.WorkToken, outcome observation.Outcome, finished time.Time) {
			a.metrics.Observe(metrics.ProbeDuration, string(outcome.Reason), outcome.Duration)
			a.mu.Lock()
			if a.sess != s {
				a.mu.Unlock()
				return
			}
			effect, _ := a.observations.Finish(token, outcome, finished)
			a.effectMetrics(effect)
			a.scheduleExpiryLocked(s)
			a.mu.Unlock()
			if effect.EndSession {
				s.cancel()
				_ = s.c.CloseNow()
			}
			if effect.Dirty {
				a.markDirty(s, token.Binding)
			}
		},
	})
}

// reconcileProbes is ordered with reload/registration by regMu. It never
// calls scheduler methods under the state lock (callbacks may take that lock).
func (a *Agent) reconcileProbes(s *session) error {
	a.regMu.Lock()
	defer a.regMu.Unlock()
	a.mu.Lock()
	if a.sess != s || s.scheduler == nil {
		a.mu.Unlock()
		return nil
	}
	jobs := map[string]observation.Job{}
	access := a.cfg.AccessID
	for _, e := range a.cfg.Exposures {
		b, ok := a.bindings[e.ID]
		if !ok || e.ProbePlan() == nil {
			continue
		}
		tgt := e.Probe.Target
		jobs[e.ID] = observation.Job{Binding: b, Plan: e.ProbePlan(), Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != net.JoinHostPort(tgt.Host, strconv.Itoa(int(tgt.Port))) {
				return nil, observation.ErrAddress
			}
			c, err := a.dialer.Dial(ctx, tgt)
			if protocol.CodeOf(err) == protocol.CodeTargetAddressNotAllowed {
				return nil, observation.ErrAddress
			}
			return c, err
		}}
	}
	known := make([]string, 0, len(s.probeIDs))
	for id := range s.probeIDs {
		known = append(known, id)
	}
	s.probeIDs = make(map[string]bool, len(jobs))
	for id := range jobs {
		s.probeIDs[id] = true
	}
	a.mu.Unlock()
	for _, id := range known {
		if _, ok := jobs[id]; !ok {
			s.scheduler.Remove(access, id)
		}
	}
	for _, job := range jobs {
		if err := s.scheduler.Update(job); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) scheduleExpiryLocked(s *session) {
	if a.sess != s {
		return
	}
	var next time.Time
	for _, b := range a.bindings {
		snap, effect, err := a.observations.Snapshot(b, time.Now())
		a.effectMetrics(effect)
		if effect.Dirty {
			a.queueDirtyLocked(s, b)
		}
		if err == nil && snap.Readiness != observation.Unknown && snap.LastSample != nil && (next.IsZero() || snap.LastSample.ExpiresAt.Before(next)) {
			next = snap.LastSample.ExpiresAt
		}
	}
	if s.expiry != nil {
		s.expiry.Stop()
		s.expiry = nil
	}
	if next.IsZero() {
		return
	}
	// Millisecond wire time is conservative; emit expiry when less than a
	// whole millisecond remains, matching the Table's actual validity rule.
	d := max(time.Nanosecond, time.Until(next)-time.Millisecond+time.Nanosecond)
	s.expiry = time.AfterFunc(d, func() {
		a.mu.Lock()
		if a.sess != s {
			a.mu.Unlock()
			return
		}
		var dirty []observation.Binding
		for _, b := range a.bindings {
			_, effect, _ := a.observations.Snapshot(b, time.Now())
			a.effectMetrics(effect)
			if effect.Dirty {
				dirty = append(dirty, b)
			}
		}
		a.scheduleExpiryLocked(s)
		a.mu.Unlock()
		for _, b := range dirty {
			a.markDirty(s, b)
		}
	})
}

func (a *Agent) queueDirtyLocked(s *session, b observation.Binding) {
	s.pendingDirty[b.ExposureID] = b
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (a *Agent) publicationLoop(s *session) {
	defer close(s.publicationDone)
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.notify:
			a.mu.Lock()
			pending := s.pendingDirty
			s.pendingDirty = make(map[string]observation.Binding)
			a.mu.Unlock()
			for _, b := range pending {
				a.markDirty(s, b)
			}
		}
	}
}

func (a *Agent) markDirty(s *session, b observation.Binding) {
	a.mu.Lock()
	current := a.sess == s && a.accepted[b.ExposureID] == b
	a.mu.Unlock()
	if !current {
		return
	}
	merged, err := s.writer.offer("dirty:"+b.ExposureID, s.ctx, func(now time.Time) (protocol.Message, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.sess != s || a.accepted[b.ExposureID] != b || a.bindings[b.ExposureID] != b {
			return nil, nil
		}
		snap, effect, err := a.observations.Snapshot(b, time.Now())
		a.effectMetrics(effect)
		if err != nil {
			return nil, nil
		}
		return &protocol.ObservationChanged{Binding: wireBinding(b), UpdateSeq: snap.UpdateSeq}, nil
	})
	if merged {
		a.metrics.Add(metrics.DirtyCoalesced, "")
	}
	if err != nil {
		s.cancel()
		_ = s.c.CloseNow()
	}
}

func snapshotWire(query string, snap observation.Snapshot) *protocol.ObservationSnapshot {
	attempt := protocol.ObservationAttempt{Seq: snap.Attempt.Seq, State: "idle", Outcome: string(snap.Attempt.Outcome), DurationMS: snap.Attempt.Duration.Milliseconds()}
	if snap.Attempt.Running {
		attempt.State = "running"
	}
	m := &protocol.ObservationSnapshot{QueryID: query, Binding: wireBinding(snap.Binding), UpdateSeq: snap.UpdateSeq, Attempt: attempt, Readiness: string(snap.Readiness), Reason: string(snap.Reason), RemainingMS: observation.RemainingMS(snap.Remaining)}
	if snap.Readiness != observation.Unknown && snap.LastSample != nil {
		seq := snap.LastSample.Seq
		at := snap.LastSample.ObservedAt
		m.SampleSeq = &seq
		m.ObservedAt = &at
	}
	return m
}

func (a *Agent) query(s *session, q *protocol.ObservationQuery) error {
	return a.queryReceived(s, q, time.Now())
}

// received is captured before waiting for the state lock. It fixes the one
// absolute request budget; the reducer still observes time inside its lock.
func (a *Agent) queryReceived(s *session, q *protocol.ObservationQuery, received time.Time) error {
	deadline := received.Add(limits.D(q.DeadlineMS))
	b := localBinding(q.Binding)
	if q.DeadlineMS > s.limits.QueryMS {
		return protocol.Errorf(protocol.CodeProtocolError, "query deadline exceeds negotiated bound")
	}
	a.mu.Lock()
	if a.sess != s || b.AccessID != a.cfg.AccessID || b.Epoch != a.epoch || b.Generation != a.gen {
		a.mu.Unlock()
		return protocol.Errorf(protocol.CodeProtocolError, "query binding is not registered")
	}
	if a.bindings[b.ExposureID] != b || a.accepted[b.ExposureID] != b {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	if !time.Now().Before(deadline) {
		return nil
	}
	_, err := s.writer.offerUntil("query:"+q.QueryID, s.ctx, deadline, func(_ time.Time) (protocol.Message, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		now := time.Now()
		if !now.Before(deadline) {
			return nil, nil
		}
		if a.sess != s || a.bindings[b.ExposureID] != b {
			return nil, nil
		}
		snap, effect, err := a.observations.Snapshot(b, now)
		a.effectMetrics(effect)
		if err != nil {
			return nil, nil
		}
		return snapshotWire(q.QueryID, snap), nil
	})
	return err
}

func checkCode(d observation.Decision) protocol.Code {
	if d.Allowed {
		return ""
	}
	if d.Snapshot.Readiness == observation.NotReady {
		return protocol.CodeTargetNotReady
	}
	return protocol.CodeReadinessRequired
}

func (a *Agent) admission(ctx context.Context, s *session, q *protocol.AdmissionCheck) error {
	return a.admissionReceived(ctx, s, q, time.Now())
}

func (a *Agent) admissionReceived(ctx context.Context, s *session, q *protocol.AdmissionCheck, started time.Time) error {
	deadline := started.Add(limits.D(q.DeadlineMS))
	a.mu.Lock()
	s.checks++
	a.mu.Unlock()
	defer func() { a.mu.Lock(); s.checks--; a.mu.Unlock() }()
	defer func() { a.metrics.Observe(metrics.CriticalControlDuration, "check", time.Since(started)) }()
	b := localBinding(q.Binding)
	if q.DeadlineMS > s.limits.CheckMS {
		return protocol.Errorf(protocol.CodeProtocolError, "check deadline exceeds negotiated bound")
	}
	a.mu.Lock()
	if a.sess != s || b.AccessID != a.cfg.AccessID || b.Epoch != a.epoch || b.Generation != a.gen {
		a.mu.Unlock()
		return protocol.Errorf(protocol.CodeProtocolError, "check binding is not registered")
	}
	if a.bindings[b.ExposureID] != b || a.accepted[b.ExposureID] != b {
		a.mu.Unlock()
		wctx, cancel := context.WithDeadline(ctx, deadline)
		defer cancel()
		return checkWriteError(s, s.send(wctx, &protocol.AdmissionResult{CheckID: q.CheckID, Binding: q.Binding, ChannelID: q.ChannelID, EffectiveAdmission: q.EffectiveAdmission, Readiness: protocol.ReadinessUnknown, Reason: "observation_changed", Code: protocol.CodeReadinessCheckFailed}))
	}
	exp := a.cfg.Find(b.ExposureID)
	if exp == nil || !exp.IsEnabled() {
		a.mu.Unlock()
		return protocol.Errorf(protocol.CodeProtocolError, "check exposure is unavailable")
	}
	mode := narrowerMode(exp.AdmissionMode, q.EffectiveAdmission)
	var ch *channel
	if q.Stage == protocol.CheckFinal {
		ch = a.channels[q.ChannelID]
		if ch == nil || ch.running || ch.finalCheckID != "" || ch.aborted != "" {
			a.mu.Unlock()
			return nil // completed/cancelled final checks cannot revive a reservation
		}
		if ch.binding != b {
			a.mu.Unlock()
			return protocol.Errorf(protocol.CodeProtocolError, "final check has no matching reservation")
		}
		mode = narrowerMode(string(mode), string(ch.mode))
		deadline = minTime(deadline, ch.deadline)
	}
	now := time.Now()
	if !now.Before(deadline) {
		a.mu.Unlock()
		return nil
	}
	if ch != nil {
		ch.finalCheckID = q.CheckID
	}
	d, effect, err := a.observations.Evaluate(b, mode, now)
	a.effectMetrics(effect)
	if err != nil {
		a.mu.Unlock()
		return err
	}
	result := &protocol.AdmissionResult{CheckID: q.CheckID, Binding: q.Binding, ChannelID: q.ChannelID, EffectiveAdmission: string(mode), Readiness: string(d.Snapshot.Readiness), Reason: string(d.Snapshot.Reason), RemainingMS: observation.RemainingMS(d.Snapshot.Remaining), Code: checkCode(d)}
	var sampleExpiry time.Time
	if d.Snapshot.Readiness != observation.Unknown && d.Snapshot.LastSample != nil {
		seq := d.Snapshot.LastSample.Seq
		result.SampleSeq = &seq
		sampleExpiry = d.Snapshot.LastSample.ExpiresAt
	}
	if code := a.allowFor(b.Generation)(); code != "" {
		result.Code = code
	}
	if result.Code == "" && ch != nil {
		id, idErr := s.ids.Next()
		if idErr != nil {
			a.mu.Unlock()
			return idErr
		}
		result.DecisionID = id
		ch.decisionID, ch.decisionDeadline, ch.sampleDeadline, ch.mode = id, deadline, sampleExpiry, mode
		end := deadline
		if !sampleExpiry.IsZero() {
			end = minTime(end, sampleExpiry)
		}
		ch.decisionTimer = time.AfterFunc(max(0, time.Until(end)), func() { a.cancelAwaiting(ch, s) })
	}
	a.scheduleExpiryLocked(s)
	a.mu.Unlock()
	if effect.Dirty {
		a.markDirty(s, b)
	}
	key := "allowed"
	terminal := "success"
	if result.Code != "" {
		terminal = "rejected"
		switch result.Code {
		case protocol.CodeTargetNotReady:
			key = "not_ready"
		case protocol.CodeReadinessRequired:
			key = "readiness_required"
		case protocol.CodeLicenseExpired:
			key = "license_expired"
		default:
			key = "unavailable"
		}
	}
	a.metrics.Add(metrics.Checks, key)
	a.metrics.Observe(metrics.CheckDuration, terminal, time.Since(started))
	// Keep the immutable t_check health decision, but subtract every local
	// queue/write delay from its original sample lifetime before serialization.
	wctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	err = s.writer.sendBuilt(wctx, func(_ time.Time) (protocol.Message, error) {
		out := *result
		a.mu.Lock()
		now := time.Now()
		bindingChanged := a.sess != s || a.bindings[b.ExposureID] != b
		invalid := bindingChanged || (ch != nil && ch.aborted != "")
		var abortCode protocol.Code
		if ch != nil {
			abortCode = ch.aborted
		}
		a.mu.Unlock()
		if !now.Before(deadline) {
			return nil, nil
		}
		if invalid {
			out.Readiness = protocol.ReadinessUnknown
			out.Reason = "observation_changed"
			out.SampleSeq = nil
			out.RemainingMS = 0
			out.DecisionID = ""
			out.Code = protocol.CodeReadinessCheckFailed
			switch abortCode {
			case "":
			case protocol.CodeReadinessCheckFailed:
				if !bindingChanged && !sampleExpiry.IsZero() && observation.RemainingMS(sampleExpiry.Sub(now)) == 0 {
					out.Reason = "timeout"
				}
			case protocol.CodeReadinessRequired:
				out.Code, out.Reason = abortCode, "expired"
			case protocol.CodeRevoked, protocol.CodeRevisionChanged, protocol.CodeAccessOffline, protocol.CodeLicenseExpired, protocol.CodePrepareTimeout, protocol.CodeLocalDenied:
				out.Code, out.Reason = abortCode, "source_invalid"
				if out.Binding.SourceID == "" {
					out.Reason = "unsupported"
				}
			default:
				return nil, nil // an already terminated operation has no new final verdict
			}
			return &out, nil
		}
		if code := a.allowFor(b.Generation)(); code != "" {
			out.Code, out.DecisionID = code, ""
		}
		if out.SampleSeq != nil {
			out.RemainingMS = observation.RemainingMS(sampleExpiry.Sub(now))
			if out.RemainingMS == 0 {
				out.Readiness = protocol.ReadinessUnknown
				out.Reason = "timeout"
				out.SampleSeq = nil
				out.DecisionID = ""
				out.Code = protocol.CodeReadinessCheckFailed
			}
		}
		return &out, nil
	})
	return checkWriteError(s, err)
}

// An expired request queued behind other work is abandoned without ending an
// otherwise healthy session. A timed-out actual WebSocket write still closes
// the writer/transport and the normal session owner performs its teardown.
func checkWriteError(s *session, err error) error {
	if (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) && s.ctx.Err() == nil {
		return nil
	}
	return err
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func (a *Agent) cancelAwaiting(ch *channel, s *session) {
	a.mu.Lock()
	if a.sess != s || a.channels[ch.id] != ch || ch.running || ch.aborted != "" {
		a.mu.Unlock()
		return
	}
	code := protocol.CodeReadinessCheckFailed
	if ch.mode == observation.RequireReady && !ch.sampleDeadline.IsZero() && !time.Now().Before(ch.sampleDeadline) {
		code = protocol.CodeReadinessRequired
	}
	a.markCancelledLocked(ch, code)
	a.mu.Unlock()
	a.closeCancelled(ch)
	_ = s.send(s.ctx, &protocol.PrepareFailed{ChannelID: ch.id, Code: code, Reason: "timeout"})
}

// READY and observation reload use precisely this same state lock. Once
// Running wins, later probe-only changes must leave the stream running.
func (a *Agent) allowReady(ch *channel) protocol.Code {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ch.aborted != "" {
		return ch.aborted
	}
	if a.channels[ch.id] != ch || a.bindings[ch.exposureID] != ch.binding {
		return protocol.CodeReadinessCheckFailed
	}
	if code := a.allowFor(ch.gen)(); code != "" {
		return code
	}
	if ch.decisionID == "" || !time.Now().Before(ch.decisionDeadline) {
		return protocol.CodeReadinessCheckFailed
	}
	if (ch.mode == observation.RequireReady && ch.sampleDeadline.IsZero()) || (!ch.sampleDeadline.IsZero() && observation.RemainingMS(time.Until(ch.sampleDeadline)) == 0) {
		if ch.mode == observation.RequireReady {
			return protocol.CodeReadinessRequired
		}
		return protocol.CodeReadinessCheckFailed
	}
	ch.running = true
	if ch.decisionTimer != nil {
		ch.decisionTimer.Stop()
	}
	return ""
}

func (a *Agent) markCancelledLocked(ch *channel, code protocol.Code) {
	if ch.aborted == "" {
		ch.aborted = code
	}
	if ch.decisionTimer != nil {
		ch.decisionTimer.Stop()
	}
}
func (a *Agent) closeCancelled(ch *channel) {
	a.mu.Lock()
	b, code := ch.bridge, ch.aborted
	a.mu.Unlock()
	ch.cancel()
	if b != nil {
		b.Abort(code)
	}
}
