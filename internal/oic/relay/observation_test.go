package relay

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func probeEnv(t *testing.T) *env {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	t.Cleanup(srv.Close)
	e := newEnv(t, func(m map[string]any) {
		m["exposures"].([]any)[0].(map[string]any)["probe"] = map[string]any{
			"kind": "http_status_v1", "url": srv.URL + "/ready", "allowed_cidrs": []string{"127.0.0.0/8"}, "ready_statuses": []int{200}, "not_ready_statuses": []int{503}, "interval_ms": 86400000, "timeout_ms": 1000, "validity_ms": 15000, "jitter_ms": 0,
		}
	})
	e.connect(1, 30000, true)
	waitFor(t, "real HTTP probe", func() bool {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		s, _, err := e.agent.observations.Snapshot(e.agent.bindings["lab-a/http"], time.Now())
		return err == nil && s.Readiness == observation.Ready
	})
	return e
}

func currentBinding(e *env) protocol.ObservationBinding {
	e.agent.mu.Lock()
	defer e.agent.mu.Unlock()
	return wireBinding(e.agent.bindings["lab-a/http"])
}

func newCheckID() string { return must(must(protocol.NewControlIDs()).Next()) }

func localSample(t *testing.T, e *env, ready observation.Readiness) {
	t.Helper()
	a := e.agent
	a.mu.Lock()
	b := a.bindings["lab-a/http"]
	token, _, err := a.observations.Begin(b, time.Now())
	if err != nil {
		a.mu.Unlock()
		t.Fatal(err)
	}
	reason := observation.MatchedReady
	if ready == observation.NotReady {
		reason = observation.MatchedNotReady
	}
	effect, err := a.observations.Finish(token, observation.Outcome{Readiness: ready, Reason: reason}, time.Now())
	a.effectMetrics(effect)
	a.scheduleExpiryLocked(a.sess)
	s := a.sess
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	a.markDirty(s, b)
}

func boundReservation(t *testing.T, e *env) (*protocol.PrepareChannel, net.Conn, *websocket.Conn) {
	t.Helper()
	p := e.prepareMsg("lab-a/http", e.rev, 1)
	e.ctl.send(p)
	tc := e.tgt.next(t)
	leg := e.ois.attach(t)
	e.waitBound(p.ChannelID)
	return p, tc, leg.c
}

func finalCheck(t *testing.T, e *env, p *protocol.PrepareChannel) *protocol.AdmissionResult {
	t.Helper()
	id := newCheckID()
	e.ctl.send(&protocol.AdmissionCheck{CheckID: id, Stage: protocol.CheckFinal, Binding: *p.Binding, ChannelID: p.ChannelID, EffectiveAdmission: p.EffectiveAdmission, DeadlineMS: 1000})
	r := expectMsg[*protocol.AdmissionResult](e.ctl)
	if r.CheckID != id || r.Code != "" || r.DecisionID == "" {
		t.Fatalf("final result %+v", r)
	}
	return r
}

func TestObservationRealProbeQueryAndAdmission(t *testing.T) {
	e := probeEnv(t)
	query := newCheckID()
	e.ctl.send(&protocol.ObservationQuery{QueryID: query, Binding: currentBinding(e), DeadlineMS: 1000})
	snap := expectMsg[*protocol.ObservationSnapshot](e.ctl)
	if snap.QueryID != query || snap.Readiness != protocol.ReadinessReady || snap.SampleSeq == nil || snap.RemainingMS <= 0 || snap.RemainingMS > 15000 || snap.Attempt.Seq != 1 || snap.Attempt.Outcome != "matched_ready_status" {
		t.Fatalf("snapshot %+v", snap)
	}
	encoded, _ := json.Marshal(snap)
	if strings.Contains(string(encoded), "http://") || strings.Contains(string(encoded), "127.0.0.1") || strings.Contains(string(encoded), "tls_ca_file") {
		t.Fatal("local probe address leaked")
	}
	for _, tc := range []struct {
		state observation.Readiness
		mode  string
		code  protocol.Code
	}{{observation.Ready, protocol.AdmissionBestEffort, ""}, {observation.Ready, protocol.AdmissionRequireReady, ""}, {observation.NotReady, protocol.AdmissionBestEffort, protocol.CodeTargetNotReady}, {observation.NotReady, protocol.AdmissionRequireReady, protocol.CodeTargetNotReady}} {
		localSample(t, e, tc.state)
		id := newCheckID()
		e.ctl.send(&protocol.AdmissionCheck{CheckID: id, Stage: protocol.CheckPreflight, Binding: currentBinding(e), EffectiveAdmission: tc.mode, DeadlineMS: 1000})
		r := expectMsg[*protocol.AdmissionResult](e.ctl)
		if r.CheckID != id || r.Code != tc.code || r.DecisionID != "" || r.EffectiveAdmission != tc.mode {
			t.Fatalf("preflight %+v", r)
		}
		select {
		case <-e.tgt.conns:
			t.Fatal("preflight dialed a business target")
		default:
		}
	}
}

func TestFinalSnapshotIgnoresLaterHealth(t *testing.T) {
	e := probeEnv(t)
	p, tc, leg := boundReservation(t, e)
	decision := finalCheck(t, e, p)
	localSample(t, e, observation.NotReady)
	legSend(t, leg, protocol.ReadyFrame())
	legSend(t, leg, protocol.DataFrame([]byte("after-final")))
	_ = tc.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 11)
	if _, err := io.ReadFull(tc, buf); err != nil || string(buf) != "after-final" {
		t.Fatalf("frozen decision was recomputed: %q %v", buf, err)
	}
	e.agent.mu.Lock()
	ch := e.agent.channels[p.ChannelID]
	ok := ch != nil && ch.running && ch.decisionID == decision.DecisionID
	e.agent.mu.Unlock()
	if !ok {
		t.Fatal("same final decision did not enter Running")
	}
}

func TestProbeReloadREADYOrdering(t *testing.T) {
	for _, runningFirst := range []bool{false, true} {
		name := "config_first"
		if runningFirst {
			name = "running_first"
		}
		t.Run(name, func(t *testing.T) {
			e := probeEnv(t)
			p, tc, leg := boundReservation(t, e)
			finalCheck(t, e, p)
			if runningFirst {
				legSend(t, leg, protocol.ReadyFrame())
				waitFor(t, "Running", func() bool {
					e.agent.mu.Lock()
					defer e.agent.mu.Unlock()
					return e.agent.channels[p.ChannelID].running
				})
			}
			old := currentBinding(e)
			probe := e.cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)
			probe["validity_ms"] = 16000
			e.writeConfig()
			e.agent.reload(context.Background())
			next := currentBinding(e)
			if old.ObservationRevision == next.ObservationRevision || old.Revision != next.Revision {
				t.Fatal("probe changed wrong revision")
			}
			if !runningFirst {
				failure := expectMsg[*protocol.PrepareFailed](e.ctl)
				if failure.ChannelID != p.ChannelID || failure.Code != protocol.CodeReadinessCheckFailed || failure.Reason != "observation_changed" {
					t.Fatalf("%+v", failure)
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				_ = leg.Write(ctx, websocket.MessageBinary, protocol.ReadyFrame())
				cancel()
				expectRST(t, tc)
				waitFor(t, "cancelled reservation", func() bool { return e.agent.Stats().Channels == 0 })
			} else {
				reg := expectMsg[*protocol.Register](e.ctl)
				if len(reg.Exposures) == 0 {
					t.Fatal("empty register")
				}
				legSend(t, leg, protocol.DataFrame([]byte("kept")))
				_ = tc.SetReadDeadline(time.Now().Add(wait))
				buf := make([]byte, 4)
				if _, err := io.ReadFull(tc, buf); err != nil || string(buf) != "kept" {
					t.Fatalf("probe reload killed Running: %q %v", buf, err)
				}
			}
		})
	}
}

func TestUnknownFinalKeepsModeAfterLocalTightening(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	p, tc, leg := boundReservation(t, e)
	r := finalCheck(t, e, p)
	if r.Readiness != protocol.ReadinessUnknown {
		t.Fatal(r)
	}
	e.cfg["exposures"].([]any)[0].(map[string]any)["admission_mode"] = protocol.AdmissionRequireReady
	e.writeConfig()
	e.agent.reload(context.Background())
	expectMsg[*protocol.Register](e.ctl)
	legSend(t, leg, protocol.ReadyFrame())
	legSend(t, leg, protocol.DataFrame([]byte("ok")))
	_ = tc.SetReadDeadline(time.Now().Add(wait))
	buf := make([]byte, 2)
	if _, err := io.ReadFull(tc, buf); err != nil || string(buf) != "ok" {
		t.Fatal("late mode change revoked frozen Unknown", err)
	}
}

func TestFinalDeadlineCancelsWithoutREADY(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	p, tc, _ := boundReservation(t, e)
	id := newCheckID()
	e.ctl.send(&protocol.AdmissionCheck{CheckID: id, Stage: protocol.CheckFinal, Binding: *p.Binding, ChannelID: p.ChannelID, EffectiveAdmission: protocol.AdmissionBestEffort, DeadlineMS: 30})
	r := expectMsg[*protocol.AdmissionResult](e.ctl)
	if r.Code != "" {
		t.Fatal(r)
	}
	failure := expectMsg[*protocol.PrepareFailed](e.ctl)
	if failure.Code != protocol.CodeReadinessCheckFailed || failure.Reason != "timeout" {
		t.Fatal(failure)
	}
	expectRST(t, tc)
	waitFor(t, "deadline cleanup", func() bool { return e.agent.Stats().Channels == 0 })
}

func TestObservationStaleCheckCannotReviveBinding(t *testing.T) {
	e := probeEnv(t)
	old := currentBinding(e)
	probe := e.cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)
	probe["validity_ms"] = 16000
	e.writeConfig()
	e.agent.reload(context.Background())
	expectMsg[*protocol.Register](e.ctl)
	id := newCheckID()
	e.ctl.send(&protocol.AdmissionCheck{CheckID: id, Stage: protocol.CheckPreflight, Binding: old, EffectiveAdmission: protocol.AdmissionBestEffort, DeadlineMS: 1000})
	r := expectMsg[*protocol.AdmissionResult](e.ctl)
	if r.Code != protocol.CodeReadinessCheckFailed || r.Reason != "observation_changed" || r.Binding != old {
		t.Fatal(r)
	}
	e.ctl.send(&protocol.Heartbeat{LicenseTTLMS: 30000})
	expectMsg[*protocol.HeartbeatAck](e.ctl)
	select {
	case <-e.tgt.conns:
		t.Fatal("stale binding dialed business target")
	default:
	}
}

func TestQueryDuringAttemptAndFailurePreservesSample(t *testing.T) {
	e := probeEnv(t)
	localSample(t, e, observation.NotReady)
	a := e.agent
	a.mu.Lock()
	b := a.bindings["lab-a/http"]
	token, _, err := a.observations.Begin(b, time.Now())
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	query := func() *protocol.ObservationSnapshot {
		t.Helper()
		id := newCheckID()
		e.ctl.send(&protocol.ObservationQuery{QueryID: id, Binding: wireBinding(b), DeadlineMS: 1000})
		return expectMsg[*protocol.ObservationSnapshot](e.ctl)
	}
	running := query()
	if running.Attempt.State != "running" || running.Readiness != protocol.ReadinessNotReady || running.SampleSeq == nil {
		t.Fatalf("running overwrote evidence %+v", running)
	}
	a.mu.Lock()
	effect, err := a.observations.Finish(token, observation.Outcome{Readiness: observation.Unknown, Reason: observation.Timeout, Duration: 50 * time.Millisecond}, time.Now())
	a.effectMetrics(effect)
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	failed := query()
	if failed.Attempt.Outcome != "timeout" || failed.Readiness != protocol.ReadinessNotReady || *failed.SampleSeq != *running.SampleSeq || failed.RemainingMS > running.RemainingMS {
		t.Fatalf("failed attempt renewed or erased sample: %+v / %+v", running, failed)
	}
}

func TestObservationExpiryPublishesWithoutAnotherProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	e := newEnv(t, func(m map[string]any) {
		m["metrics_enabled"] = true
		m["exposures"].([]any)[0].(map[string]any)["probe"] = map[string]any{"kind": "http_status_v1", "url": srv.URL, "allowed_cidrs": []string{"127.0.0.0/8"}, "ready_statuses": []int{200}, "not_ready_statuses": []int{503}, "interval_ms": 86400000, "timeout_ms": 1000, "validity_ms": 100, "jitter_ms": 0}
	})
	e.connect(1, 30000, true)
	// Observe the actual publication first. Table.Snapshot is a reducer: polling
	// it here would expire the sample and consume the effect before the real
	// timer can publish it, creating a test-induced missing notification.
	last := uint64(0)
	for last < 4 {
		notice := expectMsg[*protocol.ObservationChanged](e.ctl)
		if notice.Binding.ExposureID == "lab-a/http" {
			last = notice.UpdateSeq
		}
	}
	id := newCheckID()
	e.ctl.send(&protocol.ObservationQuery{QueryID: id, Binding: currentBinding(e), DeadlineMS: 1000})
	snap := expectMsg[*protocol.ObservationSnapshot](e.ctl)
	if snap.Readiness != protocol.ReadinessUnknown || snap.Reason != "expired" || snap.SampleSeq != nil || snap.ObservedAt != nil || snap.RemainingMS != 0 || snap.Attempt.Seq != 1 {
		t.Fatal(snap)
	}
	r := must(http.Get("http://" + e.agent.DiagAddr() + "/metrics"))
	body := must(io.ReadAll(r.Body))
	_ = r.Body.Close()
	if r.StatusCode != 200 || !strings.Contains(string(body), "oi_observation_sample_expired_total 1\n") {
		t.Fatalf("expiry metric status=%d %s", r.StatusCode, body)
	}
	diag := e.agent.Stats().Observation
	if diag == nil || diag.ProbeStarted != 1 || diag.ProbeFinished != 1 || diag.MaxProbeActive != 1 || diag.ProbeActive != 0 {
		t.Fatalf("real scheduler diagnostic %+v", diag)
	}
}

func TestReloadCancelsOldHTTPWorkBeforeNewAttempt(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	e := newEnv(t, func(m map[string]any) {
		m["exposures"].([]any)[0].(map[string]any)["probe"] = map[string]any{"kind": "http_status_v1", "url": srv.URL + "/old", "allowed_cidrs": []string{"127.0.0.0/8"}, "ready_statuses": []int{200}, "not_ready_statuses": []int{503}, "interval_ms": 86400000, "timeout_ms": 30000, "validity_ms": 15000, "jitter_ms": 0}
	})
	e.connect(1, 30000, true)
	select {
	case <-entered:
	case <-time.After(wait):
		t.Fatal("probe never started")
	}
	old := currentBinding(e)
	e.cfg["exposures"].([]any)[0].(map[string]any)["probe"].(map[string]any)["url"] = srv.URL + "/new"
	e.writeConfig()
	e.agent.reload(context.Background())
	expectMsg[*protocol.Register](e.ctl)
	select {
	case <-cancelled:
	case <-time.After(wait):
		t.Fatal("old HTTP request survived replacement")
	}
	waitFor(t, "new valid sample", func() bool {
		e.agent.mu.Lock()
		defer e.agent.mu.Unlock()
		b := e.agent.bindings["lab-a/http"]
		s, _, _ := e.agent.observations.Snapshot(b, time.Now())
		return b.ObservationRevision != old.ObservationRevision && s.Readiness == observation.Ready && s.Attempt.Seq == 2
	})
	d := e.agent.Stats().Observation
	if d.ProbeStarted != 2 || d.MaxProbeActive != 1 {
		t.Fatalf("replacement overlapped old execution %+v", d)
	}
}

func TestMetricsDisabledAndOptInRoute(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	r := must(http.Get("http://" + e.agent.DiagAddr() + "/metrics"))
	_ = r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatal("default exporter enabled", r.StatusCode)
	}
}
