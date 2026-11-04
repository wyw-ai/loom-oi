package relay

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type openWaiter struct {
	principal Principal
	original  bool
	ctx       context.Context
}
type openOperation struct {
	creator           Principal
	req               protocol.OpenRequest
	mode              string
	sess              *session
	binding           *protocol.ObservationBinding
	started, deadline time.Time
	waiters           map[*openWaiter]struct{}
	check             *admissionCheck
	done              chan struct{}
	finished          bool
	ch                *channel
	err               error
}
type admissionCheck struct {
	id, stage, mode   string
	sess              *session
	binding           protocol.ObservationBinding
	started, deadline time.Time
	op                *openOperation
	ch                *channel
}

func readinessError(code protocol.Code, reason string) *protocol.Error {
	return &protocol.Error{Code: code, Reason: reason, Message: "readiness check did not permit this channel"}
}
func commitConditionError(code protocol.Code) error {
	if code == protocol.CodeReadinessCheckFailed {
		return readinessError(code, "observation_changed")
	}
	return protocol.Errorf(code, "commit condition changed")
}
func sameOpen(a, b protocol.OpenRequest) bool {
	return a.ExposureID == b.ExposureID && a.ExpectedRevision == b.ExpectedRevision && a.Admission == b.Admission
}

// OpenContext holds no coordinator lock while waiting for a remote check. A
// waiter never replaces the creator credential or extends the operation's D.
func (h *Hub) OpenContext(ctx context.Context, p Principal, req protocol.OpenRequest) (result OpenResult, err error) {
	metric := metrics.OpenBestEffort
	if req.Admission == protocol.AdmissionRequireReady {
		metric = metrics.OpenRequireReady
	}
	defer func() {
		key := "rejected"
		if err == nil {
			key = "created"
			if result.Replay {
				key = "reused"
			}
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			key = "cancelled"
		}
		h.metrics.Add(metric, key)
	}()
	if err = validOpen(req); err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	h.mu.Lock()
	now := h.now()
	if err = h.stillValid(p, now); err != nil {
		h.mu.Unlock()
		return
	}
	key := idemKey{p.Subject, req.ClientRequestID}
	if req.ClientRequestID != "" {
		if ch := h.idem[key]; ch != nil {
			if ch.exposureID != req.ExposureID || ch.revision != req.ExpectedRevision || ch.reqAdmission != req.Admission {
				err = protocol.Errorf(protocol.CodeIdempotencyConflict, "client_request_id was used with other parameters")
			} else {
				result = h.openResult(ch, now, true)
			}
			h.mu.Unlock()
			return
		}
	}
	op := h.pending[key]
	if req.ClientRequestID == "" {
		op = nil
	}
	if op != nil {
		h.pruneWaiters(op)
		if op.finished {
			op = nil
		}
	}
	w := &openWaiter{principal: p, ctx: ctx}
	if op != nil {
		if !sameOpen(op.req, req) {
			h.mu.Unlock()
			return OpenResult{}, protocol.Errorf(protocol.CodeIdempotencyConflict, "client_request_id is pending with other parameters")
		}
		if len(op.waiters) >= limits.ObservationMaxWaiters {
			h.mu.Unlock()
			return OpenResult{}, protocol.Errorf(protocol.CodeResourceExhausted, "pending Open waiter limit reached")
		}
		op.waiters[w] = struct{}{}
	} else {
		exp, a, preErr := h.openPreconditions(p, req, nil, now)
		if preErr != nil {
			h.mu.Unlock()
			return OpenResult{}, preErr
		}
		if len(h.operations) >= h.oblim.PendingChecks {
			h.mu.Unlock()
			return OpenResult{}, protocol.Errorf(protocol.CodeResourceExhausted, "pending readiness checks exhausted")
		}
		w.original = true
		op = &openOperation{creator: p, req: req, mode: protocol.StricterAdmission(exp.rec.AdmissionMode, req.Admission), sess: a.sess, started: now, deadline: now.Add(limits.D(h.lim.PrepareMS)), waiters: map[*openWaiter]struct{}{w: {}}, done: make(chan struct{})}
		if exp.binding != nil {
			binding := *exp.binding
			op.binding = &binding
		}
		h.operations[op] = struct{}{}
		if req.ClientRequestID != "" {
			h.pending[key] = op
		}
		if op.sess.version == protocol.ControlSubprotocolV2 {
			if op.binding == nil {
				h.finishOpen(op, nil, readinessError(protocol.CodeReadinessCheckFailed, "unavailable"))
			} else {
				h.startAdmission(op, nil, now)
			}
		} else {
			if op.mode == protocol.AdmissionRequireReady {
				h.finishOpen(op, nil, readinessError(protocol.CodeReadinessRequired, "unsupported"))
			} else {
				h.createPrepared(op, protocol.ReadinessUnknown, now)
			}
		}
	}
	if op.mode == protocol.AdmissionRequireReady {
		metric = metrics.OpenRequireReady
	}
	deadline := op.deadline
	if op.check != nil {
		deadline = op.check.deadline
	}
	h.mu.Unlock()
	timer := time.NewTimer(max(0, deadline.Sub(h.now())))
	defer timer.Stop()
	select {
	case <-op.done:
	case <-ctx.Done():
		h.mu.Lock()
		delete(op.waiters, w)
		if !op.finished && len(op.waiters) == 0 {
			h.finishOpen(op, nil, ctx.Err())
		}
		h.mu.Unlock()
		return OpenResult{}, ctx.Err()
	case <-h.ctx.Done():
		h.mu.Lock()
		if !op.finished {
			h.finishOpen(op, nil, protocol.Errorf(protocol.CodeShutdown, "OIS is stopping"))
		}
		h.mu.Unlock()
	case <-timer.C:
		h.mu.Lock()
		if !op.finished {
			// Waiting for the coordinator never freezes authorization or total D.
			now = h.now()
			if now.Before(deadline) {
				now = deadline
			}
			h.timeoutAdmission(op.check, now)
		}
		h.mu.Unlock()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(op.waiters, w)
	if err = h.stillValid(p, h.now()); err != nil {
		return OpenResult{}, err
	}
	if op.err != nil {
		return OpenResult{}, op.err
	}
	if op.ch.admission == protocol.AdmissionRequireReady {
		metric = metrics.OpenRequireReady
	}
	return h.openResult(op.ch, h.now(), !w.original), nil
}

// The preflight order intentionally differs from commitCheck: applied
// credential/visibility/version changes precede a late remote result or D.
func (h *Hub) openPreconditions(p Principal, req protocol.OpenRequest, op *openOperation, now time.Time) (*exposure, *access, error) {
	if err := h.stillValid(p, now); err != nil {
		return nil, nil, err
	}
	if h.shutting {
		return nil, nil, protocol.Errorf(protocol.CodeShutdown, "OIS is stopping")
	}
	exp := h.exposures[req.ExposureID]
	if exp == nil || !h.allowed(p.Subject, req.ExposureID, protocol.ActionDiscover) {
		return nil, nil, protocol.Errorf(protocol.CodeNotFound, "exposure not found")
	}
	if !h.allowed(p.Subject, req.ExposureID, protocol.ActionOpen) {
		return nil, nil, protocol.Errorf(protocol.CodeDenied, "open is not granted")
	}
	if exp.rec.Revision != req.ExpectedRevision {
		return nil, nil, protocol.Errorf(protocol.CodeRevisionMismatch, "expected_revision is not current")
	}
	a := h.accesses[exp.rec.AccessID]
	if a == nil || !a.rec.Enabled || !a.online() || (op != nil && a.sess != op.sess) {
		return nil, nil, protocol.Errorf(protocol.CodeAccessOffline, "access is offline or replaced")
	}
	if op != nil && op.binding != nil && (exp.binding == nil || *exp.binding != *op.binding) {
		return nil, nil, readinessError(protocol.CodeReadinessCheckFailed, "observation_changed")
	}
	return exp, a, nil
}

func (h *Hub) finishOpen(op *openOperation, ch *channel, err error) {
	if op == nil || op.finished {
		return
	}
	op.finished, op.ch, op.err = true, ch, err
	delete(h.operations, op)
	key := idemKey{op.creator.Subject, op.req.ClientRequestID}
	if h.pending[key] == op {
		delete(h.pending, key)
	}
	if op.check != nil {
		h.endAdmission(op.check, err, h.now())
	}
	close(op.done)
}

func (h *Hub) createPrepared(op *openOperation, readiness string, now time.Time) {
	h.pruneWaiters(op)
	if op.finished {
		return
	}
	if current := h.now(); current.After(now) {
		now = current
	}
	exp, a, err := h.openPreconditions(op.creator, op.req, op, now)
	if err == nil && op.deadline.Sub(now) < time.Millisecond {
		err = protocol.Errorf(protocol.CodePrepareTimeout, "total preparation budget exhausted")
	}
	if err == nil && op.check != nil && !now.Before(op.check.deadline) {
		err = readinessError(protocol.CodeReadinessCheckFailed, "timeout")
	}
	if err == nil && !h.quota.admit(h.lim, a.rec.AccessID, op.creator.Subject) {
		err = protocol.Errorf(protocol.CodeResourceExhausted, "channel quota exhausted")
	}
	if err == nil && !h.storeOK {
		err = protocol.Errorf(protocol.CodeStoreUnavailable, "store unavailable: new channels are refused")
	}
	if err == nil && h.meta.Maintenance {
		err = protocol.Errorf(protocol.CodeMaintenance, "maintenance mode: new channels are refused until an administrator resumes")
	}
	if err != nil {
		h.finishOpen(op, nil, err)
		return
	}
	ch := &channel{id: protocol.NewID("ch_"), exposureID: exp.rec.ExposureID, accessID: a.rec.AccessID, revision: exp.rec.Revision, subject: op.creator.Subject, tokenID: op.creator.TokenID,
		admission: op.mode, readiness: readiness, reqAdmission: op.req.Admission, requestID: op.req.ClientRequestID, epoch: op.sess.epoch, gen: op.sess.gen, created: op.started, deadline: op.deadline, binding: op.binding}
	ch.setState(statePreparing)
	for _, role := range []Role{Consumer, Provider} {
		ch.ticketHash[role] = sha256.Sum256([]byte(h.ticket(role, ch.id)))
	}
	h.quota.take(ch)
	h.channels[ch.id] = ch
	if ch.requestID != "" {
		h.idem[idemKey{ch.subject, ch.requestID}] = ch
	}
	h.finishOpen(op, ch, nil)
	message := &protocol.PrepareChannel{ChannelID: ch.id, ExposureID: ch.exposureID, Revision: ch.revision, Subject: ch.subject, Epoch: ch.epoch, Generation: ch.gen, PrepareTTLMS: ms(ch.deadline.Sub(now)), ProviderTicket: h.ticket(Provider, ch.id)}
	if ch.binding != nil {
		message.Binding = ch.binding
		message.EffectiveAdmission = ch.admission
	}
	op.sess.sendChecked(message, func(sent time.Time) bool {
		if ch.state != statePreparing {
			return false
		}
		if code := h.commitCheck(ch, sent); code != "" {
			h.terminateCommit(ch, code, sent)
			return false
		}
		message.PrepareTTLMS = ms(ch.deadline.Sub(sent))
		return message.PrepareTTLMS > 0
	})
	h.log.Info("open", "channel", ch.id, "exposure", ch.exposureID, "subject", ch.subject, "admission", ch.admission, "readiness", ch.readiness)
}

// A caller can cancel while its goroutine is waiting to acquire Hub.mu. Check
// that already-observable fact before consuming capacity or creating a Channel,
// rather than letting scheduling order retain a disconnected waiter slot.
func (h *Hub) pruneWaiters(op *openOperation) {
	if op == nil || op.finished {
		return
	}
	for w := range op.waiters {
		if w.ctx.Err() != nil {
			delete(op.waiters, w)
		}
	}
	if len(op.waiters) == 0 {
		h.finishOpen(op, nil, context.Canceled)
	}
}

func (h *Hub) startAdmission(op *openOperation, ch *channel, now time.Time) {
	var s *session
	var binding protocol.ObservationBinding
	var mode, stage, channelID string
	var total time.Time
	if op != nil {
		s, binding, mode, stage, total = op.sess, *op.binding, op.mode, protocol.CheckPreflight, op.deadline
	} else {
		a := h.accesses[ch.accessID]
		if a == nil || a.sess == nil {
			h.terminate(ch, protocol.CodeAccessOffline, now)
			return
		}
		s, binding, mode, stage, total = a.sess, *ch.binding, ch.admission, protocol.CheckFinal, ch.deadline
		channelID = ch.id
	}
	id, err := s.ids.Next()
	if err != nil {
		h.endSession(s, causeLost, "", "")
		return
	}
	deadline := now.Add(limits.D(h.oblim.CheckMS))
	if total.Before(deadline) {
		deadline = total
	}
	check := &admissionCheck{id: id, stage: stage, mode: mode, sess: s, binding: binding, started: now, deadline: deadline, op: op, ch: ch}
	h.admissions[id] = check
	if op != nil {
		op.check = check
	} else {
		ch.check = check
	}
	m := &protocol.AdmissionCheck{CheckID: id, Stage: stage, Binding: binding, EffectiveAdmission: mode, DeadlineMS: ms(deadline.Sub(now)), ChannelID: channelID}
	s.sendChecked(m, func(sent time.Time) bool {
		if h.admissions[id] != check {
			return false
		}
		if !sent.Before(check.deadline) {
			h.timeoutAdmission(check, sent)
			return false
		}
		m.DeadlineMS = ms(check.deadline.Sub(sent))
		if m.DeadlineMS <= 0 {
			h.timeoutAdmission(check, check.deadline)
			return false
		}
		return true
	})
}

func (h *Hub) timeoutAdmission(check *admissionCheck, now time.Time) {
	if check == nil || h.admissions[check.id] != check {
		return
	}
	code, reason := protocol.CodeReadinessCheckFailed, "timeout"
	if check.op != nil {
		if _, _, err := h.openPreconditions(check.op.creator, check.op.req, check.op, now); err != nil {
			h.finishOpen(check.op, nil, err)
			return
		}
		if !now.Before(check.op.deadline) {
			code = protocol.CodePrepareTimeout
		}
		h.finishOpen(check.op, nil, readinessError(code, reason))
	} else {
		if problem := h.commitCheck(check.ch, now); problem != "" {
			code = problem
			reason = checkReason(problem)
			if problem == protocol.CodeReadinessCheckFailed {
				reason = "observation_changed"
			}
		}
		h.terminateReadiness(check.ch, code, reason, now)
	}
}

func (h *Hub) onAdmissionResult(s *session, m *protocol.AdmissionResult, now time.Time) {
	check := h.admissions[m.CheckID]
	if check == nil {
		return
	}
	channelID := ""
	if check.ch != nil {
		channelID = check.ch.id
	}
	if check.sess != s || check.binding != m.Binding || m.ChannelID != channelID || protocol.StricterAdmission(check.mode, m.EffectiveAdmission) != m.EffectiveAdmission ||
		(check.stage == protocol.CheckPreflight && m.DecisionID != "") || (check.stage == protocol.CheckFinal && m.Code == "" && !protocol.ValidControlID(m.DecisionID)) || m.RemainingMS > int64(24*time.Hour/time.Millisecond) {
		h.endSession(s, causeLost, protocol.CodeProtocolError, "admission result does not match its pending check")
		return
	}
	var err error
	if check.op != nil {
		_, _, err = h.openPreconditions(check.op.creator, check.op.req, check.op, now)
	} else if code := h.commitCheck(check.ch, now); code != "" {
		err = commitConditionError(code)
	}
	if err == nil && check.ch != nil {
		exp := h.exposures[check.ch.exposureID]
		if exp == nil || exp.binding == nil || *exp.binding != check.binding {
			err = readinessError(protocol.CodeReadinessCheckFailed, "observation_changed")
		}
	}
	var total time.Time
	if check.op != nil {
		total = check.op.deadline
	} else {
		total = check.ch.deadline
	}
	if err == nil && !now.Before(total) {
		err = protocol.Errorf(protocol.CodePrepareTimeout, "total preparation budget exhausted")
	}
	if err == nil && !now.Before(check.deadline) {
		err = readinessError(protocol.CodeReadinessCheckFailed, "timeout")
	}
	if err == nil && m.Code != "" {
		err = readinessError(m.Code, m.Reason)
	}
	safe := limits.D(m.RemainingMS) - now.Sub(check.started)
	if err == nil && m.EffectiveAdmission == protocol.AdmissionRequireReady && (m.Readiness != protocol.ReadinessReady || safe < time.Millisecond) {
		err = readinessError(protocol.CodeReadinessRequired, "expired")
	}
	if check.op != nil {
		if err != nil {
			h.finishOpen(check.op, nil, err)
			return
		}
		check.op.mode = m.EffectiveAdmission
		h.createPrepared(check.op, m.Readiness, now)
		return
	}
	ch := check.ch
	ch.decision = &protocol.ReadinessDecision{EffectiveAdmission: m.EffectiveAdmission, Readiness: m.Readiness, Source: "local_check", SampleSeq: cloneSeq(m.SampleSeq), Reason: m.Reason}
	// No metrics lock or encoding work separates the final time guards from
	// the channel transition. This local decision never reads directory health.
	if current := h.now(); current.After(now) {
		now = current
	}
	if err == nil {
		if code := h.commitCheck(ch, now); code != "" {
			err = commitConditionError(code)
		}
	}
	if err == nil && !now.Before(check.deadline) {
		err = readinessError(protocol.CodeReadinessCheckFailed, "timeout")
	}
	if err == nil && m.EffectiveAdmission == protocol.AdmissionRequireReady && limits.D(m.RemainingMS)-now.Sub(check.started) < time.Millisecond {
		err = readinessError(protocol.CodeReadinessRequired, "expired")
	}
	if err != nil {
		ch.decision.Reason = decisionReason(err)
		h.endAdmission(check, err, now)
		h.terminate(ch, protocol.CodeOf(err), now)
		return
	}
	ch.admission = m.EffectiveAdmission
	// The final reply fixes this observation decision. Never consult the
	// independent directory cache here, even if it already holds NotReady.
	h.commitReady(ch, now)
	h.endAdmission(check, nil, now)
}

func (h *Hub) endAdmission(check *admissionCheck, err error, now time.Time) {
	if check == nil || h.admissions[check.id] != check {
		return
	}
	delete(h.admissions, check.id)
	if check.op != nil && check.op.check == check {
		check.op.check = nil
	}
	if check.ch != nil && check.ch.check == check {
		check.ch.check = nil
	}
	result, reason := "success", "allowed"
	if err != nil {
		reason = admissionReason(err)
		result = "rejected"
		if reason == "timeout" || reason == "prepare_timeout" {
			result = "timeout"
		} else if reason == "cancelled" {
			result = "cancelled"
		}
	}
	h.metrics.Add(metrics.Checks, reason)
	h.metrics.Observe(metrics.CheckDuration, result, max(0, now.Sub(check.started)))
}

// Public final diagnostics preserve the precise finite local cause; metric
// labels remain the smaller, fixed outcome taxonomy in admissionReason.
func decisionReason(err error) string {
	var e *protocol.Error
	if errors.As(err, &e) {
		if e.Code == protocol.CodeReadinessRequired {
			switch e.Reason {
			case "unsupported", "no_sample", "expired", "source_invalid":
				return e.Reason
			}
		}
		if e.Code == protocol.CodeTargetNotReady && e.Reason == "matched_not_ready_status" {
			return e.Reason
		}
	}
	return admissionReason(err)
}

func admissionReason(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "cancelled"
	}
	var e *protocol.Error
	if errors.As(err, &e) && e.Code == protocol.CodeReadinessCheckFailed {
		switch e.Reason {
		case "timeout", "observation_changed", "unavailable":
			return e.Reason
		}
	}
	return checkReason(protocol.CodeOf(err))
}

func checkReason(code protocol.Code) string {
	switch code {
	case protocol.CodeTargetNotReady:
		return "not_ready"
	case protocol.CodeReadinessRequired:
		return "readiness_required"
	case protocol.CodeRevoked, protocol.CodeDenied, protocol.CodeUnauthenticated:
		return "revoked"
	case protocol.CodeRevisionChanged, protocol.CodeRevisionMismatch:
		return "revision_changed"
	case protocol.CodeAccessOffline:
		return "access_offline"
	case protocol.CodeLicenseExpired:
		return "license_expired"
	case protocol.CodePrepareTimeout:
		return "prepare_timeout"
	case protocol.CodeProtocolError:
		return "protocol_error"
	default:
		return "unavailable"
	}
}

func cloneSeq(p *uint64) *uint64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
