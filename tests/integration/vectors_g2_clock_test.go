package integration

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
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
								facts = g2ClockSampleRound(g, clock, scenario, delta, round)
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

func g2ClockSampleRound(g *g2Env, clock *g2Clock, state string, delta time.Duration, round int) map[string]any {
	t := g.t
	p := g.paths["lab-a"]
	facts := map[string]any{"round": round, "scenario": state, "delta_ns": int64(delta), "direction": "forward"}
	if delta < 0 {
		facts["direction"] = "backward"
	}
	stage, completed := "preparation", false
	defer func() {
		if !completed {
			facts["failure_stage"] = stage
			facts["failure_observed_ns"] = resourceNS()
			g.event("real_clock_sample_failure", facts)
		}
	}()
	if len(g.health) != 1 || g.health["lab-a/http"] == nil {
		t.Fatal("clock reduction witness requires exactly the one declared probe")
	}
	metricsAddr := g2ExporterAddresses(g)["ois"]
	g2ClockFresh(g, state)
	status := 200
	if state == "not_ready" {
		status = 503
	}
	stopVersion := g.health["lab-a/http"].Set(status, true)
	facts["stopped_health_version"] = stopVersion
	queryGate := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgObservationQuery})
	query := g2WaitBarrier(t, queryGate)
	facts["query"] = query
	message, err := protocol.DecodeControlForVersion(query.Raw, protocol.ControlSubprotocolV2)
	if err != nil {
		t.Fatal(err)
	}
	q, ok := message.(*protocol.ObservationQuery)
	if !ok || q.DeadlineMS <= 0 || q.DeadlineMS > 500 {
		t.Fatal("invalid clock query budget", message)
	}
	// This fixture upper bound uses the real encoded remainder; success below
	// also proves the OIS accepted the response under its own earlier absolute D.
	reduction := &g2ClockQueryReduction{QueryID: q.QueryID, DeadlineNS: query.ReceivedNS + q.DeadlineMS*int64(time.Millisecond), Polls: []g2Exposition{}}
	facts["query_reduction"] = reduction
	ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(reduction.DeadlineNS)))
	defer cancel()
	read := func(ctx context.Context) (g2Exposition, error) {
		return g2ReadExposition(ctx, g2MetricsClient(g), metricsAddr)
	}
	responseGate := g2SnapshotBarrier(t, p, query.ConnID)
	stage = "shift"
	shift := clock.shift(delta)
	facts["shift"] = shift
	queryGate.Release()
	response := g2WaitBarrier(t, responseGate)
	facts["response"] = response
	sample := g2DecodeSnapshot(t, response)
	if sample.Readiness != state || sample.SampleSeq == nil || sample.RemainingMS < 200 {
		t.Fatalf("clock sample fixture not live: %+v", sample)
	}
	stage = "reduction_baseline"
	guard := g2SnapshotBarrier(t, p, query.ConnID)
	facts["next_snapshot_guard_armed_ns"] = resourceNS()
	reduction.Before, err = read(ctx)
	if err != nil {
		facts["reduction_error"] = err.Error()
		t.Fatal(err)
	}
	if _, err = g2ClockQueryCount(reduction.Before); err != nil {
		facts["reduction_error"] = err.Error()
		t.Fatal(err)
	}
	facts["response_release_requested_ns"] = resourceNS()
	responseGate.Release()
	written := g2WaitWritten(t, p, response)
	facts["response_written"] = written
	stage = "reduction_wait"
	if err = g2ClockAwaitReduction(ctx, reduction, read); err != nil {
		facts["reduction_error"] = err.Error()
		t.Fatal(err)
	}
	stage = "first_view"
	viewStart := resourceNS()
	facts["view_started_ns"] = viewStart
	view := g.view("lab-a/http")
	viewEnd := resourceNS()
	upper := max(int64(0), sample.RemainingMS-(viewStart-query.ReceivedNS)/int64(time.Millisecond))
	facts["view"], facts["view_started_ns"], facts["view_ended_ns"] = view, viewStart, viewEnd
	facts["remaining_upper_bound_ms"] = upper
	if viewStart > reduction.DeadlineNS || view.Readiness != state || view.ReadinessView == nil || view.ReadinessView.RemainingMS <= 0 || view.ReadinessView.RemainingMS > upper || view.ReadinessView.LastReported == nil || view.ReadinessView.LastReported.SampleSeq != *sample.SampleSeq {
		t.Fatalf("clock jump cleared/extended live sample: %+v upper=%d", view, upper)
	}
	liveWitness := clock.witness()
	facts["live_witness"] = liveWitness
	stage = "expiry_trajectory"
	var expired protocol.ExposureView
	var trajectory []map[string]any
	remaining := view.ReadinessView.RemainingMS
	expiryLower, expiryUpper := g2ClockAuthorityWindow(viewStart, viewEnd, remaining)
	// Authority ends when the floored remaining_ms reaches zero: at D-1ms.
	// Both bounds therefore subtract one unit, with no new timing allowance.
	facts["expiry_lower_ns"], facts["expiry_upper_ns"] = expiryLower, expiryUpper
	deadline := response.ReceivedNS + sample.RemainingMS*int64(time.Millisecond) + int64(time.Second)
	for {
		pollStart := resourceNS()
		expired = g.view("lab-a/http")
		pollEnd := resourceNS()
		trajectory = append(trajectory, map[string]any{"started_ns": pollStart, "ended_ns": pollEnd, "view": expired})
		facts["expiry_trajectory"] = trajectory
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
	facts["expired"], facts["directory_expired_ns"] = expired, expiredAt
	if expired.ReadinessView == nil || expired.ReadinessView.RemainingMS != 0 || expired.ReadinessView.LastReported == nil || expired.ReadinessView.LastReported.SampleSeq != *sample.SampleSeq {
		t.Fatal("clock expiration lost historical sample", expired)
	}
	// Release a genuinely held later response. It may be stale and must not
	// restore cache authority; subsequent real source output must itself expire.
	stage = "source_expiry"
	guard.Cancel()
	mark := g2Mark(p)
	var source g2WireEvent
	for {
		source = g2WaitWire(t, p, mark, g2Match{ConnID: query.ConnID, Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
		facts["source_expired"] = source
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
	facts["terminal_ns"], facts["cleanup_origin_ns"] = terminal, terminal
	expiredWitness := clock.witness()
	stage = "restore"
	restore := clock.restore()
	facts["terminal_witness"], facts["restore"] = expiredWitness, restore
	facts["stopped_health_version"] = stopVersion
	facts["terminal_ns"], facts["cleanup_origin_ns"] = terminal, terminal
	completed = true
	return facts
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
		check := g2ClockFindCheck(t, p, held)
		shift := clock.shift(delta)
		upper, released, releaseWitness, late := g2ClockReleaseExpired(g, clock, p, check, held, gate)
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
		zero := g2WaitSlots(g, 0, 0)
		if zero.Channels.Preparing+zero.Channels.Open+zero.Channels.Closing != 0 || zero.Tickets != 0 || len(g.t1.Conns()) != after {
			t.Fatal("late preflight created Channel/target")
		}
		return map[string]any{"shift": shift, "terminal_witness": witness, "restore": restore, "check": check, "held": held, "deadline_upper_ns": upper, "release_requested_ns": released, "late_release_witness": releaseWitness, "late_written": late, "open_outcome": out, "http_reason": pe.Reason, "terminal_ns": terminal, "cleanup_origin_ns": terminal, "after": zero, "target": g.t1.Conns()[after:]}
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
	// Observe immediately and retain the original arrival time while the main
	// goroutine keeps the positive result held. Sleeping first and then reading
	// would conceal a premature ABORT behind the instrument's own delay.
	type observed struct {
		frame protocol.Frame
		at    int64
		err   error
	}
	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelRead()
	observedTerminal := make(chan observed, 1)
	go func() {
		typ, raw, err := leg.Read(readCtx)
		at := resourceNS()
		var frame protocol.Frame
		if err == nil && typ != websocket.MessageBinary {
			err = fmt.Errorf("nonbinary terminal")
		}
		if err == nil {
			frame, err = protocol.DecodeFrame(raw, 1<<20)
		}
		observedTerminal <- observed{frame, at, err}
	}()
	upper, released, releaseWitness, late := g2ClockReleaseExpired(g, clock, p, check, held, gate)
	observedResult := <-observedTerminal
	if observedResult.err != nil {
		t.Fatal(observedResult.err)
	}
	f, terminal := observedResult.frame, observedResult.at
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
	return map[string]any{"shift": shift, "terminal_witness": witness, "restore": restore, "open_outcome": opened, "attach_started_ns": attachStart, "check": check, "held": held, "deadline_upper_ns": upper, "release_requested_ns": released, "late_release_witness": releaseWitness, "late_written": late, "consumer_abort": f, "channel": cv, "target": g2NoPayload(g, after), "terminal_ns": terminal, "cleanup_origin_ns": terminal}
}

func g2ClockReleaseExpired(g *g2Env, clock *g2Clock, p *g2Proxy, check, held g2WireEvent, gate *g2Barrier) (int64, int64, map[string]any, g2WireEvent) {
	g.t.Helper()
	var body protocol.AdmissionCheck
	if err := json.Unmarshal(check.Raw, &body); err != nil {
		g.t.Fatal(err)
	}
	if body.DeadlineMS <= 0 || body.DeadlineMS > 500 || check.ReceivedNS <= 0 {
		g.t.Fatal("invalid original check deadline")
	}
	// The producer floors its integer remaining milliseconds; this is an
	// upper bound on its original deadline, not an extra product grace period.
	upper := check.ReceivedNS + (body.DeadlineMS+1)*int64(time.Millisecond)
	if wait := time.Duration(upper - resourceNS()); wait > 0 {
		time.Sleep(wait)
	}
	witness := clock.witness()
	released := resourceNS()
	if released < upper {
		g.t.Fatal("positive release preceded conservative deadline")
	}
	gate.Release()
	late := g2WaitWritten(g.t, p, held)
	if late.WriteStartedNS < upper {
		g.t.Fatal("positive actually forwarded before original check expired")
	}
	return upper, released, witness, late
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
