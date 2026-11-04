package relay

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func currentSession(e *env) *session {
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	return e.agent.sess
}
func controlBarrier(t *testing.T, e *env) {
	t.Helper()
	e.ctl.send(&protocol.Heartbeat{LicenseTTLMS: 30000})
	expectMsg[*protocol.HeartbeatAck](e.ctl)
}

func TestObservationBudgetsIncludePriorStateWait(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	a, s, b := e.agent, currentSession(e), currentBinding(e)
	received := time.Now().Add(-2 * time.Second)
	if err := a.queryReceived(s, &protocol.ObservationQuery{QueryID: newCheckID(), Binding: b, DeadlineMS: 1000}, received); err != nil {
		t.Fatal(err)
	}
	if err := a.admissionReceived(s.ctx, s, &protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckPreflight, Binding: b, EffectiveAdmission: protocol.AdmissionBestEffort, DeadlineMS: 1000}, received); err != nil {
		t.Fatal(err)
	}
	controlBarrier(t, e) // no stale snapshot or positive check was written before it
	if d := a.Stats().Observation; d.PendingChecks != 0 || d.PendingQueries != 0 {
		t.Fatalf("expired work retained slots %+v", d)
	}
	select {
	case <-e.tgt.conns:
		t.Fatal("expired check dialed target")
	default:
	}
}

func TestAdmissionUsesCurrentReducerTime(t *testing.T) {
	e := probeEnv(t)
	a, s := e.agent, currentSession(e)
	// This sample was valid at reception, but expired before reduction. The
	// received time only fixes the request budget; it must not resurrect it.
	a.mu.Lock()
	b := a.bindings["lab-a/http"]
	now := time.Now()
	started := now.Add(-15*time.Second - 50*time.Millisecond)
	token, _, err := a.observations.Begin(b, started)
	if err == nil {
		_, err = a.observations.Finish(token, observation.Outcome{Readiness: observation.Ready, Reason: observation.MatchedReady}, started)
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	id := newCheckID()
	if err := a.admissionReceived(s.ctx, s, &protocol.AdmissionCheck{CheckID: id, Stage: protocol.CheckPreflight, Binding: wireBinding(b), EffectiveAdmission: protocol.AdmissionRequireReady, DeadlineMS: 1000}, now.Add(-100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	r := expectMsg[*protocol.AdmissionResult](e.ctl)
	if r.CheckID != id || r.Code != protocol.CodeReadinessRequired || r.Readiness != protocol.ReadinessUnknown || r.Reason != "expired" || r.SampleSeq != nil || r.DecisionID != "" {
		t.Fatalf("past reception time revived Ready %+v", r)
	}
	controlBarrier(t, e)
}

func TestLateFinalCannotRecheckOrDisconnect(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	p, tc, leg := boundReservation(t, e)
	r := finalCheck(t, e, p)
	late := func(id string) {
		e.ctl.send(&protocol.AdmissionCheck{CheckID: id, Stage: protocol.CheckFinal, Binding: *p.Binding, ChannelID: p.ChannelID, EffectiveAdmission: p.EffectiveAdmission, DeadlineMS: 1000})
		controlBarrier(t, e)
	}
	late(r.CheckID)
	late(newCheckID())
	e.agent.mu.Lock()
	same := e.agent.channels[p.ChannelID].decisionID == r.DecisionID
	e.agent.mu.Unlock()
	if !same {
		t.Fatal("second final replaced decision")
	}
	legSend(t, leg, protocol.ReadyFrame())
	waitFor(t, "running", func() bool {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		return e.agent.channels[p.ChannelID].running
	})
	late(newCheckID())
	e.ctl.send(&protocol.CloseChannel{ChannelID: p.ChannelID, Code: protocol.CodeRevoked})
	expectRST(t, tc)
	waitFor(t, "closed channel", func() bool { return e.agent.Stats().Channels == 0 })
	late(newCheckID())
	if !e.agent.Stats().Registered {
		t.Fatal("late final ended valid session")
	}
}

func TestRejectedFinalStaysRejectedAfterNewHealth(t *testing.T) {
	e := probeEnv(t)
	p, _, _ := boundReservation(t, e)
	localSample(t, e, observation.NotReady)
	q := &protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckFinal, Binding: *p.Binding, ChannelID: p.ChannelID, EffectiveAdmission: p.EffectiveAdmission, DeadlineMS: 1000}
	e.ctl.send(q)
	r := expectMsg[*protocol.AdmissionResult](e.ctl)
	if r.Code != protocol.CodeTargetNotReady || r.DecisionID != "" {
		t.Fatal(r)
	}
	localSample(t, e, observation.Ready)
	q.CheckID = newCheckID()
	e.ctl.send(q)
	controlBarrier(t, e)
	e.agent.mu.Lock()
	ch := e.agent.channels[p.ChannelID]
	valid := ch != nil && ch.decisionID == "" && ch.finalCheckID == r.CheckID
	e.agent.mu.Unlock()
	if !valid {
		t.Fatal("rejected reservation revived")
	}
}

func TestQueuedFinalPreservesRevocation(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	p, tc, _ := boundReservation(t, e)
	s := currentSession(e)
	release, first := blockWriter(t, s.writer)
	done := make(chan error, 1)
	go func() {
		done <- e.agent.admission(s.ctx, s, &protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckFinal, Binding: *p.Binding, ChannelID: p.ChannelID, EffectiveAdmission: p.EffectiveAdmission, DeadlineMS: 1000})
	}()
	waitFor(t, "frozen final queued", func() bool {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		return e.agent.channels[p.ChannelID].decisionID != ""
	})
	e.agent.abort(p.ChannelID, protocol.CodeRevoked)
	expectRST(t, tc)
	release()
	expectMsg[*protocol.Hello](e.ctl)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	r := expectMsg[*protocol.AdmissionResult](e.ctl)
	if r.Code != protocol.CodeRevoked || r.DecisionID != "" || r.Reason != "unsupported" {
		t.Fatalf("revocation replaced by generic observation error %+v", r)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	controlBarrier(t, e)
}

func TestProbeDialFailureKeepsFiniteCause(t *testing.T) {
	for _, reason := range []observation.Reason{observation.PermissionError, observation.AddressDenied} {
		t.Run(string(reason), func(t *testing.T) {
			u := "http://127.0.0.1:1/health"
			if reason == observation.AddressDenied {
				u = "http://probe.test:80/health"
			}
			e := newEnv(t, func(m map[string]any) {
				m["exposures"].([]any)[0].(map[string]any)["probe"] = map[string]any{"kind": "http_status_v1", "url": u, "allowed_cidrs": []string{"127.0.0.0/8"}, "ready_statuses": []int{200}, "not_ready_statuses": []int{503}, "interval_ms": 86400000, "timeout_ms": 1000, "validity_ms": 15000, "jitter_ms": 0}
			})
			if reason == observation.PermissionError {
				e.agent.dialer.DialContext = func(context.Context, string, string) (net.Conn, error) {
					return nil, &os.PathError{Op: "connect", Path: "private endpoint", Err: os.ErrPermission}
				}
			}
			e.connect(1, 30000, true)
			waitFor(t, "finite failed attempt", func() bool {
				e.agent.mu.Lock()
				defer e.agent.mu.Unlock()
				s, _, _ := e.agent.observations.Snapshot(e.agent.bindings["lab-a/http"], time.Now())
				return s.Attempt.Outcome == reason
			})
			id := newCheckID()
			e.ctl.send(&protocol.ObservationQuery{QueryID: id, Binding: currentBinding(e), DeadlineMS: 1000})
			s := expectMsg[*protocol.ObservationSnapshot](e.ctl)
			if s.Readiness != protocol.ReadinessUnknown || s.SampleSeq != nil || s.Attempt.Outcome != string(reason) {
				t.Fatal(s)
			}
		})
	}
}

func TestUnknownAndExpiredCheckCannotDial(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	for _, expired := range []bool{false, true} {
		if expired {
			e.agent.license.Store(&license{gen: 1, deadline: time.Now().Add(-time.Second)})
		}
		e.ctl.send(&protocol.AdmissionCheck{CheckID: newCheckID(), Stage: protocol.CheckPreflight, Binding: currentBinding(e), EffectiveAdmission: protocol.AdmissionRequireReady, DeadlineMS: 1000})
		r := expectMsg[*protocol.AdmissionResult](e.ctl)
		want := protocol.CodeReadinessRequired
		if expired {
			want = protocol.CodeLicenseExpired
		}
		if r.Code != want || r.DecisionID != "" {
			t.Fatalf("%+v", r)
		}
	}
	select {
	case <-e.tgt.conns:
		t.Fatal("rejected check dialed")
	default:
	}
}

func TestControlWriterStopReleasesQueuedQueryContexts(t *testing.T) {
	w, _ := writerPair(t, 2, 3, 2, 4096)
	release, first := blockWriter(t, w)
	for _, key := range []string{"query:one", "query:two"} {
		if _, err := w.offerUntil(key, context.Background(), time.Now().Add(time.Hour), func(time.Time) (protocol.Message, error) { t.Error("cancelled telemetry executed"); return nil, nil }); err != nil {
			t.Fatal(err)
		}
	}
	w.mu.Lock()
	contexts := []context.Context{w.telemetry["query:one"].ctx, w.telemetry["query:two"].ctx}
	w.mu.Unlock()
	w.stop()
	release()
	if err := <-first; err == nil {
		t.Fatal("stopped writer succeeded")
	}
	for _, ctx := range contexts {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("query context not cancelled: %v", ctx.Err())
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queries)+len(w.telemetry)+len(w.order) != 0 {
		t.Fatal("stopped writer retained pending queries")
	}
}
