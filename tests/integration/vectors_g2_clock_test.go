package integration

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Ordinary integration deliberately cannot enter this instrument. The dedicated
// workflow checks all 20 terminal leaf results and all 400 shift/restore pairs.
func TestTVG2_04_RealClock(t *testing.T) {
	g2ClockEnabled(t)
	for _, scenario := range []string{"ready", "not_ready", "preflight", "final", "total_prepare"} {
		t.Run(scenario, func(t *testing.T) {
			for _, direction := range []string{"forward", "backward"} {
				t.Run(direction, func(t *testing.T) {
					each(t, func(t *testing.T, be string) {
						clock := newG2Clock(t)
						g := newG2Env(t, be, "G2-04-clock", g2Options{proxy: true})
						delta := 30 * time.Second
						if direction == "backward" {
							delta = -delta
						}
						g2ClockCertificates(g)
						identities := g2ClockIdentities(g)
						sessions := g2ClockSessions(g)
						for round := range 20 {
							var facts map[string]any
							if scenario == "ready" || scenario == "not_ready" {
								facts = g2ClockSampleRound(g, clock, scenario, delta)
							} else {
								facts = g2ClockCheckRound(g, clock, scenario, delta)
							}
							if got := g2ClockIdentities(g); got != identities {
								t.Fatalf("clock round changed product identities: %v -> %v", identities, got)
							}
							if got := g2ClockSessions(g); got != sessions {
								t.Fatalf("clock round changed epoch/generation: %v -> %v", sessions, got)
							}
							facts["round"], facts["scenario"], facts["direction"], facts["delta_ns"] = round, scenario, direction, int64(delta)
							facts["product_pids"], facts["guard_pid"] = identities, clock.guardPID
							facts["session_identity"] = sessions
							// Each round uses the same pre-fault profile. Restore, idle socket
							// closure and healthy inputs all consume this original cleanup D.
							g.settled()
							facts["resource_completed_ns"] = resourceNS()
							g.event("real_clock_round", facts)
						}
						g2RecordWire(g)
						g.recordHealth("lab-a/http")
					})
				})
			}
		})
	}
}

func g2ClockIdentities(g *g2Env) [3]int {
	return [3]int{g.ois.cmd.Process.Pid, g.oics["lab-a"].proc.cmd.Process.Pid, g.oics["lab-b"].proc.cmd.Process.Pid}
}

type g2ClockSessionIdentity struct {
	OISEpoch    int64 `json:"ois_epoch"`
	AEpoch      int64 `json:"lab_a_epoch"`
	AGeneration int64 `json:"lab_a_generation"`
	BEpoch      int64 `json:"lab_b_epoch"`
	BGeneration int64 `json:"lab_b_generation"`
}

func g2ClockSessions(g *g2Env) g2ClockSessionIdentity {
	o, a, b := g.oisStats(), g.oicStats("lab-a"), g.oicStats("lab-b")
	if !a.Connected || !a.Registered || !b.Connected || !b.Registered {
		g.t.Fatal("clock identity lost online/registered topology")
	}
	return g2ClockSessionIdentity{int64(o.Epoch), int64(a.Epoch), int64(a.Generation), int64(b.Epoch), int64(b.Generation)}
}

func g2ClockCertificates(g *g2Env) {
	g.t.Helper()
	now := time.Now()
	certs := []*x509.Certificate{g.ca.Cert, g.oics["lab-a"].leaf.Cert, g.oics["lab-b"].leaf.Cert}
	for _, cert := range certs {
		if !now.Add(-time.Minute).After(cert.NotBefore) || !now.Add(time.Hour).Before(cert.NotAfter) {
			g.t.Fatal("pre-generated identity does not cover clock offsets/test window")
		}
	}
	g.event("clock_preconditions", map[string]any{"product_pids": g2ClockIdentities(g), "certificate_before": certs[0].NotBefore, "certificate_after": certs[0].NotAfter, "tokens": "fixture_nonexpiring", "clock_changed": false})
}

func g2ClockFresh(g *g2Env, state string) {
	g.t.Helper()
	status := 200
	if state == "not_ready" {
		status = 503
	}
	metric := fmt.Sprintf(`oi_observation_samples_total{result="%s"}`, state)
	previous := g2Metric(g, metric)
	g.health["lab-a/http"].Set(status, false)
	g2WaitMetric(g, metric, previous, time.Second)
	g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == state })
}

func g2ClockSampleRound(g *g2Env, clock *g2Clock, state string, delta time.Duration) map[string]any {
	t := g.t
	p := g.paths["lab-a"]
	g2ClockFresh(g, state)
	status := 200
	if state == "not_ready" {
		status = 503
	}
	stopVersion := g.health["lab-a/http"].Set(status, true)
	queryGate := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgObservationQuery})
	query := g2WaitBarrier(t, queryGate)
	responseGate := g2SnapshotBarrier(t, p, query.ConnID)
	shift := clock.shift(delta)
	queryGate.Release()
	response := g2WaitBarrier(t, responseGate)
	sample := g2DecodeSnapshot(t, response)
	if sample.Readiness != state || sample.SampleSeq == nil || sample.RemainingMS < 200 {
		t.Fatalf("clock sample fixture not live: %+v", sample)
	}
	guard := g2SnapshotBarrier(t, p, query.ConnID)
	responseGate.Release()
	written := g2WaitWritten(t, p, response)
	viewStart := resourceNS()
	view := g.view("lab-a/http")
	viewEnd := resourceNS()
	upper := max(int64(0), sample.RemainingMS-(viewStart-query.ReceivedNS)/int64(time.Millisecond))
	if view.Readiness != state || view.ReadinessView == nil || view.ReadinessView.RemainingMS <= 0 || view.ReadinessView.RemainingMS > upper || view.ReadinessView.LastReported == nil || view.ReadinessView.LastReported.SampleSeq != *sample.SampleSeq {
		t.Fatalf("clock jump cleared/extended live sample: %+v upper=%d", view, upper)
	}
	liveWitness := clock.witness()
	var expired protocol.ExposureView
	var trajectory []map[string]any
	remaining := view.ReadinessView.RemainingMS
	expiryLower := viewStart + remaining*int64(time.Millisecond)
	// The view's integer duration is floored. Adding precisely one unit bounds
	// that representation loss; it is not a new product timing allowance.
	expiryUpper := viewEnd + (remaining+1)*int64(time.Millisecond)
	deadline := response.ReceivedNS + sample.RemainingMS*int64(time.Millisecond) + int64(time.Second)
	for {
		pollStart := resourceNS()
		expired = g.view("lab-a/http")
		pollEnd := resourceNS()
		trajectory = append(trajectory, map[string]any{"started_ns": pollStart, "ended_ns": pollEnd, "view": expired})
		if len(trajectory) > 256 {
			t.Fatal("clock TTL observation bound exceeded")
		}
		if expired.ReadinessView == nil {
			t.Fatal("clock TTL view lost observation")
		}
		bound := max(int64(0), remaining-(pollStart-viewEnd)/int64(time.Millisecond))
		if expired.ReadinessView.RemainingMS > bound {
			t.Fatal("clock TTL countdown renewed between real polls")
		}
		if expired.Readiness == protocol.ReadinessUnknown {
			if pollEnd < expiryLower {
				t.Fatal("clock jump prematurely expired the sample after the first live view")
			}
			break
		}
		if pollStart >= expiryUpper {
			t.Fatal("directory stayed authoritative beyond its original monotonic expiry")
		}
		if resourceNS() > deadline {
			t.Fatal("original monotonic sample failed to expire")
		}
		time.Sleep(10 * time.Millisecond)
	}
	expiredAt := resourceNS()
	if expired.ReadinessView == nil || expired.ReadinessView.RemainingMS != 0 || expired.ReadinessView.LastReported == nil || expired.ReadinessView.LastReported.SampleSeq != *sample.SampleSeq {
		t.Fatal("clock expiration lost historical sample", expired)
	}
	// Release a genuinely held later response. It may be stale and must not
	// restore cache authority; subsequent real source output must itself expire.
	guard.Cancel()
	mark := g2Mark(p)
	var source g2WireEvent
	for {
		source = g2WaitWire(t, p, mark, g2Match{ConnID: query.ConnID, Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
		current := g2DecodeSnapshot(t, source)
		if current.Binding != sample.Binding {
			t.Fatal("clock changed source binding")
		}
		if current.Readiness == protocol.ReadinessUnknown && current.Reason == "expired" && current.RemainingMS == 0 {
			break
		}
		if current.SampleSeq != nil && *current.SampleSeq != *sample.SampleSeq {
			t.Fatal("new explicit sample invalidates expiry experiment")
		}
		if resourceNS() > deadline {
			t.Fatal("OIC source sample did not expire")
		}
		mark = source.ID
	}
	terminal := resourceNS()
	g.cleanupStartNS = terminal
	expiredWitness := clock.witness()
	restore := clock.restore()
	return map[string]any{"shift": shift, "live_witness": liveWitness, "terminal_witness": expiredWitness, "restore": restore, "query": query, "response": response, "response_written": written, "source_expired": source, "stopped_health_version": stopVersion, "view": view, "view_started_ns": viewStart, "view_ended_ns": viewEnd, "remaining_upper_bound_ms": upper, "expiry_lower_ns": expiryLower, "expiry_upper_ns": expiryUpper, "expiry_trajectory": trajectory, "expired": expired, "directory_expired_ns": expiredAt, "terminal_ns": terminal, "cleanup_origin_ns": terminal}
}

func g2ClockCheckRound(g *g2Env, clock *g2Clock, scenario string, delta time.Duration) map[string]any {
	t := g.t
	p := g.paths["lab-a"]
	c := g.consumer("alice")
	g2FreshReady(g)
	view := g.view("lab-a/http")
	after := len(g.t1.Conns())
	mark := g2Mark(p)
	req := protocol.OpenRequest{ExposureID: view.ExposureID, ExpectedRevision: view.Revision, Admission: protocol.AdmissionRequireReady}
	if scenario == "preflight" {
		gate := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckPreflight})
		done := g2AsyncOpen(c, context.Background(), req)
		held := g2WaitBarrier(t, gate)
		if positive := g2Result(g, held); positive.Code != "" {
			t.Fatal("clock preflight fixture not positive", positive)
		}
		shift := clock.shift(delta)
		out := g2AwaitOpen(t, done)
		terminal := out.CompletedNS
		g.cleanupStartNS = terminal
		var pe *protocol.Error
		if out.Status != 503 || out.Code != protocol.CodeReadinessCheckFailed || !errors.As(out.err, &pe) || pe.Reason != "timeout" || out.Open.ChannelID != "" {
			t.Fatalf("clock preflight terminal: %+v", out)
		}
		if terminal < out.StartedNS+int64(500*time.Millisecond) {
			t.Fatal("wall-clock jump prematurely expired the check")
		}
		witness := clock.witness()
		restore := clock.restore()
		gate.Release()
		late := g2WaitWritten(t, p, held)
		zero := g2WaitSlots(g, 0, 0)
		if zero.Channels.Preparing+zero.Channels.Open+zero.Channels.Closing != 0 || zero.Tickets != 0 || len(g.t1.Conns()) != after {
			t.Fatal("late preflight created Channel/target")
		}
		check := g2ClockFindCheck(t, p, held)
		return map[string]any{"shift": shift, "terminal_witness": witness, "restore": restore, "check": check, "held": held, "late_written": late, "open_outcome": out, "http_reason": pe.Reason, "terminal_ns": terminal, "cleanup_origin_ns": terminal, "after": zero, "target": g.t1.Conns()[after:]}
	}
	opened := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
	if opened.err != nil {
		t.Fatal(opened.err)
	}
	o := opened.Open
	gate := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
	abortGate := g2Arm(t, p, g2Match{Role: g2RoleProvider, Direction: g2OICToOIS, Type: "ABORT", ChannelID: o.ChannelID})
	if scenario == "total_prepare" {
		// This is an explicit measured input delay, not a guessed event. Healthy
		// periodic probes remain active so the final still has a live sample.
		if remain := time.Duration(opened.StartedNS + int64(1700*time.Millisecond) - resourceNS()); remain > 0 {
			time.Sleep(remain)
		}
	}
	attachStart := resourceNS()
	leg := g2Leg(g, c, o)
	held := g2WaitBarrier(t, gate)
	positive := g2Result(g, held)
	if positive.Code != "" || positive.DecisionID == "" {
		t.Fatal("clock final not a real positive", positive)
	}
	check := g2ClockFindCheck(t, p, held)
	var checkBody protocol.AdmissionCheck
	if err := json.Unmarshal(check.Raw, &checkBody); err != nil {
		t.Fatal(err)
	}
	if scenario == "total_prepare" && (checkBody.DeadlineMS <= 0 || checkBody.DeadlineMS >= 400) {
		t.Fatalf("final did not start in original total-D tail: %d", checkBody.DeadlineMS)
	}
	shift := clock.shift(delta)
	f := g2Frame(g, leg)
	terminal := resourceNS()
	g.cleanupStartNS = terminal
	want := protocol.CodeReadinessCheckFailed
	if scenario == "total_prepare" {
		want = protocol.CodePrepareTimeout
	}
	if f.Type != protocol.FrameAbort || f.Reason != want {
		t.Fatalf("clock final terminal %+v want %s", f, want)
	}
	lower := attachStart + int64(500*time.Millisecond)
	if scenario == "total_prepare" {
		lower = opened.StartedNS + int64(2*time.Second)
	}
	if terminal < lower {
		t.Fatal("wall-clock jump prematurely ended the original monotonic budget")
	}
	witness := clock.witness()
	restore := clock.restore()
	abortGate.Release()
	gate.Release()
	late := g2WaitWritten(t, p, held)
	leg.CloseNow()
	cv := g2CommitCleanup(g, o, after, true, terminal)
	if cv.CloseReason != want || cv.ReadinessDecision == nil || (scenario == "final" && cv.ReadinessDecision.Reason != "timeout") {
		t.Fatal("clock first terminal reason changed", cv)
	}
	for _, e := range p.Snapshot().Events {
		if e.ID > mark && e.ChannelID == o.ChannelID && e.Type == "READY" {
			t.Fatal("late positive emitted READY")
		}
	}
	return map[string]any{"shift": shift, "terminal_witness": witness, "restore": restore, "open_outcome": opened, "attach_started_ns": attachStart, "check": check, "held": held, "late_written": late, "consumer_abort": f, "channel": cv, "target": g2NoPayload(g, after), "terminal_ns": terminal, "cleanup_origin_ns": terminal}
}

func g2ClockFindCheck(t *testing.T, p *g2Proxy, result g2WireEvent) g2WireEvent {
	t.Helper()
	for _, e := range p.Snapshot().Events {
		if e.ConnID == result.ConnID && e.Type == protocol.MsgAdmissionCheck && e.CheckID == result.CheckID {
			return e
		}
	}
	t.Fatal("clock result has no original check")
	return g2WireEvent{}
}
