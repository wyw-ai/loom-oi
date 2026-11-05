package relay

import (
	"context"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestObservationControlBoundsAndNamespace(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	a, s, b := e.agent, currentSession(e), currentBinding(e)
	for _, kind := range []string{"query_limit", "check_limit", "query_other_access", "check_other_access"} {
		t.Run(kind, func(t *testing.T) {
			binding, deadline := b, int64(1000)
			if kind == "query_limit" || kind == "check_limit" {
				deadline = 1001
			} else {
				binding.AccessID, binding.ExposureID = "lab-b", "lab-b/http"
			}
			var err error
			if kind == "query_limit" || kind == "query_other_access" {
				err = a.query(s, &protocol.ObservationQuery{QueryID: newCheckID(), Binding: binding, DeadlineMS: deadline})
			} else {
				err = a.admission(s.ctx, s, &protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckPreflight, Binding: binding, EffectiveAdmission: protocol.AdmissionBestEffort, DeadlineMS: deadline})
			}
			if protocol.CodeOf(err) != protocol.CodeProtocolError {
				t.Fatalf("%v", err)
			}
		})
	}
	b.ObservationRevision = "ob_999"
	if err := a.query(s, &protocol.ObservationQuery{QueryID: newCheckID(), Binding: b, DeadlineMS: 1000}); err != nil {
		t.Fatal(err)
	}
	controlBarrier(t, e)
	if d := a.Stats().Observation; d.PendingChecks+d.PendingQueries != 0 {
		t.Fatalf("refusal retained work %+v", d)
	}
}

func TestQueryReplacedWhileQueuedDoesNotPublishOldBinding(t *testing.T) {
	e := probeEnv(t)
	s, old := currentSession(e), currentBinding(e)
	release, first := blockWriter(t, s.writer)
	if err := e.agent.query(s, &protocol.ObservationQuery{QueryID: newCheckID(), Binding: old, DeadlineMS: 1000}); err != nil {
		t.Fatal(err)
	}
	e.cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)["validity_ms"] = 16000
	e.writeConfig()
	done := make(chan struct{})
	go func() { e.agent.reload(context.Background()); close(done) }()
	waitFor(t, "binding replaced", func() bool { return currentBinding(e) != old })
	release()
	expectMsg[*protocol.Hello](e.ctl)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	expectMsg[*protocol.Register](e.ctl)
	<-done
	controlBarrier(t, e)
	waitFor(t, "obsolete query slot released", func() bool { return e.agent.Stats().Observation.PendingQueries == 0 })
}

func TestProbeRemovalAndExposureDisableRetireJobs(t *testing.T) {
	for _, disable := range []bool{false, true} {
		name := "remove_probe_keeps_running"
		if disable {
			name = "disable_exposure_closes_running"
		}
		t.Run(name, func(t *testing.T) {
			e := probeEnv(t)
			p, tc, _ := e.open(1)
			x := e.cfg["exposures"].([]any)[0].(map[string]any)
			if disable {
				x["enabled"] = false
			} else {
				delete(x, "probe")
			}
			e.writeConfig()
			e.agent.reload(context.Background())
			reg := expectMsg[*protocol.Register](e.ctl)
			waitFor(t, "probe job removed", func() bool { return e.agent.Stats().Observation.ProbeJobs == 0 })
			if disable {
				expectRST(t, tc)
				waitFor(t, "disabled channel closed", func() bool { return e.agent.Stats().Channels == 0 })
			} else {
				found := false
				for _, r := range reg.Exposures {
					if r.ExposureID == p.ExposureID {
						found = true
						if r.ProbeSupport != protocol.ProbeUnsupported || r.Binding.SourceID != "" {
							t.Fatalf("%+v", r)
						}
					}
				}
				if !found || e.agent.Stats().Channels != 1 {
					t.Fatal("probe removal killed running business channel")
				}
			}
		})
	}
}

func TestREADYNeedsFrozenDecisionAndLiveLicense(t *testing.T) {
	for _, licensed := range []bool{true, false} {
		name := "missing_final"
		if !licensed {
			name = "license_expired_before_ready"
		}
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, nil)
			e.connect(1, 30000, true)
			p, tc, leg := boundReservation(t, e)
			if !licensed {
				finalCheck(t, e, p)
				e.agent.license.Store(&license{gen: 1, deadline: time.Now().Add(-time.Second)})
			}
			legSend(t, leg, protocol.ReadyFrame())
			r := expectMsg[*protocol.PrepareFailed](e.ctl)
			want := protocol.CodeReadinessCheckFailed
			if !licensed {
				want = protocol.CodeLicenseExpired
			}
			if r.ChannelID != p.ChannelID || r.Code != want {
				t.Fatal(r)
			}
			expectRST(t, tc)
		})
	}
}

func TestFinalCannotExtendPrepareDeadline(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	p := e.prepareMsg("lab-a/http", e.rev, 1)
	e.ctl.send(p)
	tc := e.tgt.next(t)
	leg := e.ois.attach(t)
	defer leg.c.CloseNow()
	e.waitBound(p.ChannelID)
	e.agent.mu.Lock()
	ch := e.agent.channels[p.ChannelID]
	if ch == nil {
		e.agent.mu.Unlock()
		t.Fatal("reservation disappeared during preparation")
	}
	originalDeadline, generation := ch.deadline, ch.gen
	e.agent.mu.Unlock()
	if originalDeadline.IsZero() || generation != p.Generation {
		t.Fatal("invalid original reservation identity/deadline")
	}
	q := &protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckFinal, Binding: *p.Binding,
		ChannelID: p.ChannelID, EffectiveAdmission: p.EffectiveAdmission, DeadlineMS: 1000}
	// The fake OIS deliberately supplies a larger check budget to test the
	// OIC's defensive min. A real OIS must send the original D's remainder.
	if delay := time.Until(originalDeadline.Add(-200 * time.Millisecond)); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
	}
	e.agent.mu.Lock()
	sent := time.Now()
	remaining := originalDeadline.Sub(sent)
	valid := e.agent.channels[p.ChannelID] == ch && ch.gen == generation && ch.deadline.Equal(originalDeadline) &&
		!ch.running && ch.aborted == "" && remaining > 0 && remaining <= 200*time.Millisecond &&
		sent.Add(time.Second).After(originalDeadline)
	e.agent.mu.Unlock()
	if !valid {
		t.Fatalf("final tail-window input not established: remaining=%v original_deadline=%s", remaining, originalDeadline.Format(time.RFC3339Nano))
	}
	e.ctl.send(q)
	if !time.Now().Before(originalDeadline) {
		t.Fatal("final send completed after the original deadline; input not established")
	}
	result := expectMsg[*protocol.AdmissionResult](e.ctl)
	if result.CheckID != q.CheckID || result.Code != "" || result.DecisionID == "" {
		t.Fatalf("final was not positively reduced before the original deadline: %+v", result)
	}
	e.agent.mu.Lock()
	// Keep the actual object even if its timer has already removed it from
	// the map. A missing map entry must not make this assertion vacuous.
	valid = ch.gen == generation && ch.deadline.Equal(originalDeadline) && ch.finalCheckID == result.CheckID &&
		ch.decisionID == result.DecisionID && ch.decisionDeadline.Equal(originalDeadline)
	decisionDeadline := ch.decisionDeadline
	e.agent.mu.Unlock()
	if !valid {
		t.Fatalf("final changed reservation/deadline: check=%s decision=%s original=%s actual=%s", result.CheckID,
			result.DecisionID, originalDeadline.Format(time.RFC3339Nano), decisionDeadline.Format(time.RFC3339Nano))
	}
	t.Logf("final_sent=%s original_deadline=%s decision_deadline=%s check=%s", sent.Format(time.RFC3339Nano),
		originalDeadline.Format(time.RFC3339Nano), decisionDeadline.Format(time.RFC3339Nano), result.CheckID)
	r := expectMsg[*protocol.PrepareFailed](e.ctl)
	if r.ChannelID != p.ChannelID || r.Code != protocol.CodeReadinessCheckFailed || r.Reason != "timeout" || time.Now().Before(originalDeadline) {
		t.Fatal(r)
	}
	expectRST(t, tc)
}

func TestQueuedCheckCannotRenewExpiringSample(t *testing.T) {
	for _, kind := range []string{"preflight", "final_best_effort", "final_require_ready"} {
		t.Run(kind, func(t *testing.T) {
			e := probeEnv(t)
			s := currentSession(e)
			binding := currentBinding(e)
			q := &protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckPreflight, Binding: binding, EffectiveAdmission: protocol.AdmissionBestEffort, DeadlineMS: 1000}
			if kind != "preflight" {
				p, _, _ := boundReservation(t, e)
				q.Stage, q.ChannelID = protocol.CheckFinal, p.ChannelID
				if kind == "final_require_ready" {
					q.EffectiveAdmission = protocol.AdmissionRequireReady
				}
			}
			e.agent.mu.Lock()
			b := localBinding(binding)
			at := time.Now().Add(-15*time.Second + 400*time.Millisecond)
			token, _, err := e.agent.observations.Begin(b, at)
			if err == nil {
				_, err = e.agent.observations.Finish(token, observation.Outcome{Readiness: observation.Ready, Reason: observation.MatchedReady}, at)
			}
			e.agent.scheduleExpiryLocked(s)
			e.agent.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			release, first := blockWriter(t, s.writer)
			done := make(chan error, 1)
			go func() { done <- e.agent.admission(s.ctx, s, q) }()
			waitFor(t, "response queued", func() bool { return len(s.writer.critical) > 0 })
			waitFor(t, "original sample expired", func() bool {
				e.agent.mu.Lock()
				defer e.agent.mu.Unlock()
				if q.Stage == protocol.CheckFinal {
					return e.agent.channels[q.ChannelID] != nil && e.agent.channels[q.ChannelID].aborted != ""
				}
				v, _, _ := e.agent.observations.Snapshot(b, time.Now())
				return v.Reason == observation.Expired
			})
			release()
			expectMsg[*protocol.Hello](e.ctl)
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			r := expectMsg[*protocol.AdmissionResult](e.ctl)
			want, reason := protocol.CodeReadinessCheckFailed, "timeout"
			if kind == "final_require_ready" {
				want, reason = protocol.CodeReadinessRequired, "expired"
			}
			if r.Code != want || r.Reason != reason || r.SampleSeq != nil || r.RemainingMS != 0 || r.DecisionID != "" {
				t.Fatalf("expired queued sample used %+v", r)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
