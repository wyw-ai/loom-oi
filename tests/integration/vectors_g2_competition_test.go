package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type g2CompetitionStats struct {
	StartedNS   int64             `json:"started_ns"`
	CompletedNS int64             `json:"completed_ns"`
	OIS         protocol.OISStats `json:"ois"`
	OIC         protocol.OICStats `json:"oic"`
}

func g2CompetitionSample(g *g2Env, hc *http.Client) g2CompetitionStats {
	g.t.Helper()
	x, _, err := g2TryCompetitionSample(g, hc)
	if err != nil {
		g.t.Fatal(err)
	}
	return x
}

func g2CompetitionFull(s g2CompetitionStats) bool {
	o := s.OIS.Observation
	p := s.OIC.Observation
	return o != nil && p != nil && o.ObservationCapacity == 16 && o.ObservationBacklog == 16 && o.PendingQueries == 8 && p.ProbeActive == 2 && p.ProbeQueued == 4
}

// This is only a preparation phase gate. The before/after real full snapshots
// remain mandatory, so selecting a young query batch cannot turn a drained
// backlog into passing evidence. No query deadline is moved or renewed.
func g2CompetitionYoungQueries(events []g2WireEvent, now int64, maxAge time.Duration) bool {
	return g2CompetitionYoungQueriesObserved(events, now, maxAge, nil)
}

func g2CompetitionYoungQueriesObserved(events []g2WireEvent, now int64, maxAge time.Duration, observed *[]g2PreparationQuery) bool {
	seen := map[string]bool{}
	sources := map[string]bool{}
	for i := len(events) - 1; i >= 0 && len(seen) < 8; i-- {
		e := events[i]
		if e.Type != protocol.MsgObservationQuery || e.Direction != g2OISToOIC || e.Injected || e.Dropped || e.WrittenNS == 0 {
			continue
		}
		var q protocol.ObservationQuery
		decodeErr := json.Unmarshal(e.Raw, &q)
		if observed != nil {
			*observed = append(*observed, g2PreparationQuery{EventID: e.ID, QueryID: q.QueryID, Binding: q.Binding,
				ReceivedNS: e.ReceivedNS, WriteStartedNS: e.WriteStartedNS, WrittenNS: e.WrittenNS,
				DeadlineMS: q.DeadlineMS, Injected: e.Injected, Dropped: e.Dropped, Decoded: decodeErr == nil})
		}
		if decodeErr != nil || q.QueryID == "" || seen[q.QueryID] || q.Binding.ExposureID == "" || sources[q.Binding.ExposureID] || q.DeadlineMS < 400 || q.DeadlineMS > 500 || e.ReceivedNS <= 0 || now < e.ReceivedNS || now-e.ReceivedNS > int64(maxAge) {
			return false
		}
		seen[q.QueryID] = true
		sources[q.Binding.ExposureID] = true
	}
	return len(seen) == 8
}

type g2CompetitionVictim struct {
	stream      *client.Stream
	token       protocol.TokenCreated
	payload     string
	targetIndex int
}

func g2CompetitionPrepare(g *g2Env, round, cycle int) g2CompetitionVictim {
	g.t.Helper()
	v := g2CompetitionVictim{token: g.newToken("alice", 0), payload: fmt.Sprintf("competition-victim-%d", cycle), targetIndex: len(g.t1.Conns())}
	prepared := resourceNS()
	deadlineNS := prepared + int64(3*time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(deadlineNS)))
	defer cancel()
	consumer := g.clientFor(v.token.Token)
	var view protocol.ExposureView
	var lookupStarted, lookupCompleted, selected, remaining int64
	attempts := 0
	var inputErr error
	for ctx.Err() == nil {
		attempts++
		if attempts > 512 {
			inputErr = fmt.Errorf("victim input lookup capacity exhausted")
			break
		}
		lookupStarted = resourceNS()
		view, inputErr = consumer.Lookup(ctx, "lab-a/http")
		lookupCompleted = resourceNS()
		if inputErr != nil {
			break
		}
		selected = resourceNS()
		var eligible bool
		remaining, eligible = g2CompetitionVictimEligible(view, lookupStarted, lookupCompleted, selected, deadlineNS)
		if eligible {
			break
		}
		select {
		case <-ctx.Done():
			inputErr = ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if inputErr == nil {
		inputErr = ctx.Err()
	}
	fields := map[string]any{"round": round, "cycle": cycle, "token_id": v.token.TokenID, "preparation_started_ns": prepared, "deadline_ns": deadlineNS,
		"lookup_started_ns": lookupStarted, "lookup_completed_ns": lookupCompleted, "selected_ns": selected, "conservative_remaining_ns": remaining,
		"lookup_attempts": attempts, "view": view, "dial_started_ns": int64(0), "dial_completed_ns": int64(0), "channel_id": "", "code": ""}
	if inputErr != nil {
		fields["input_error"] = inputErr.Error()
		g.event("competition_victim_input", fields)
		g.t.Fatalf("competition victim Ready input absent within original 3s: %v", inputErr)
	}
	fields["dial_started_ns"] = resourceNS()
	stream, err := consumer.Dial(ctx, "lab-a/http")
	fields["dial_completed_ns"] = resourceNS()
	if err != nil {
		fields["code"] = string(abortCode(err))
	} else {
		fields["channel_id"] = stream.ChannelID()
	}
	g.event("competition_victim_input", fields)
	if err != nil {
		g.t.Fatalf("competition victim Dial failed: %v", err)
	}
	v.stream = stream
	g.t.Cleanup(func() { _ = v.stream.Close() })
	if _, err := v.stream.Write([]byte(v.payload)); err != nil {
		g.t.Fatal(err)
	}
	if !g.t1.Wait(time.Second, func(rs []fixture.Record) bool {
		return len(rs) > v.targetIndex && string(rs[v.targetIndex].Data) == v.payload
	}) {
		g.t.Fatal("victim native payload not observed before revocation")
	}
	return v
}

func g2CompetitionExportFailures(x g2Exposition) float64 {
	return g2ExpositionCounter(x, "oi_metrics_exports_total", map[string]string{"result": "timeout"}) + g2ExpositionCounter(x, "oi_metrics_exports_total", map[string]string{"result": "write_error"})
}

// A cycle retains both slow network writers until each exporter failure has
// actually been counted. It performs native work and revocation after a real
// 429, before waiting for those writes to time out.
func g2CompetitionCycle(g *g2Env, hc *http.Client, role, addr string, round, cycle int, v g2CompetitionVictim, requireFull bool, epoch, generation int64) {
	g.t.Helper()
	p := g.paths["lab-a"]
	before := g2ExporterRead(g.t, hc, addr)
	slotInput := g2ExportSlotInput{}
	connectFailure := g2FailureStamp{}
	var failedWrites []g2SlowWrite
	slow, err := g2BeginSlowScrapesDiagnostic(context.Background(), addr, g2CompetitionDialer(), func(ctx context.Context, slow *g2SlowScrapes) error {
		return slotInput.beforeWrite(ctx, g, hc, role, slow)
	}, func(s *g2SlowScrapes, err error) {
		connectFailure.freeze("connect_error")
		failedWrites = s.Snapshot()
	})
	if err != nil {
		fields := map[string]any{"round": round, "cycle": cycle, "role": role, "stage": "export_slot_zero", "export_slot_input": slotInput.failureFields(), "failure_captured_ns": connectFailure.CapturedNS, "error_class": g2DiagnosticError(err), "slow_writes": failedWrites}
		g.event("competition_failed_cycle", fields)
		g.t.Fatal(err)
	}
	connected := slow.Snapshot()
	if len(connected) != 2 || connected[0].ConnectedNS <= 0 || connected[1].ConnectedNS <= 0 {
		g.t.Fatal("slow input did not create both actual sockets")
	}
	deadline := resourceOrigin.Add(time.Duration(min(connected[0].ConnectedNS, connected[1].ConnectedNS)) + time.Second)
	var rejected g2Exposition
	var lastScrape g2Exposition
	inputGate := g2CompetitionRejectionGate{deadlineNS: int64(deadline.Sub(resourceOrigin))}
	var responseHeaders []g2CompetitionResponseHeader
	stage := "await_socket_writes"
	defer func() {
		p := recover()
		var failure map[string]any
		if g.t.Failed() || p != nil {
			var last, rejection any
			if lastScrape.StartedNS != 0 {
				last = lastScrape
			}
			if rejected.StartedNS != 0 {
				rejection = rejected
			}
			at := resourceNS()
			failure = map[string]any{"round": round, "cycle": cycle, "role": role, "stage": stage,
				"before_export": before, "last_scrape": last, "actual_rejection": rejection,
				"export_input_observations": inputGate.observations, "export_slot_input": slotInput.failureFields(),
				"slow_response_headers": responseHeaders, "slow_writes": slow.Snapshot(),
				"observed_before_close_ns": at, "failure_captured_ns": at}
			if slotInput.FailureCapturedNS != nil {
				failure["failure_captured_ns"] = slotInput.FailureCapturedNS
			}
		}
		g2FinishDiagnostic(func() {
			if err := slow.Close(); err != nil {
				g.t.Error(err)
			}
		}, func() {
			if failure != nil {
				g.event("competition_failed_cycle", failure)
			}
		})
		if p != nil {
			panic(p)
		}
	}()
	if err := g2CompetitionAwaitWrites(slow, deadline); err != nil {
		g.t.Fatalf("slow request writes did not complete in original bounded input window: %v", err)
	}
	stage = "await_first_response_headers"
	responseHeaders, err = g2CompetitionPeekHeaders(slow, deadline)
	if err != nil {
		g.t.Fatalf("both slow sockets must have a genuine first 200 header before observer: %v", err)
	}
	stage = "await_export_active"
	activeCtx, activeCancel := context.WithDeadline(context.Background(), deadline)
	err = slotInput.awaitActive(activeCtx, g, hc, role)
	activeCancel()
	if err != nil {
		g.t.Fatalf("non-export activity input did not qualify: %v", err)
	}
	// A first 429 can occur while two quick pipeline responses merely render
	// concurrently. Select a persistent actual rejection interval instead;
	// the original connection-origin deadline and pipeline bounds do not move.
	stage = "await_persistent_rejection"
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		x, err := g2ReadExposition(ctx, hc, addr)
		cancel()
		lastScrape = x
		if err != nil {
			g.t.Fatal(err)
		}
		ready, gateErr := inputGate.observe(x)
		if gateErr != nil {
			g.t.Fatal(gateErr)
		}
		if ready {
			rejected = x
			break
		}
		// This observer also consumes an exporter slot. Keep the same bounded
		// cadence as G2-12 instead of aggressively starving a slow socket.
		time.Sleep(10 * time.Millisecond)
	}
	if rejected.Status != 429 {
		g.t.Fatal("two slow requests never established a persistent actual rejection interval")
	}
	if !time.Now().Before(deadline) {
		g.t.Fatal("persistent input observation exhausted original one-second window")
	}
	stage = "critical"
	start := g2CompetitionSample(g, hc)
	if requireFull && !g2CompetitionFull(start) {
		g.event("competition_not_full", map[string]any{"cycle": cycle, "sample": start})
		g.t.Fatal("source backlog/probe queue not full during export blocking")
	}
	if requireFull {
		queryEvents := p.Snapshot().Events
		if !g2CompetitionYoungQueries(queryEvents, resourceNS(), 300*time.Millisecond) {
			g.event("competition_not_full", map[string]any{"cycle": cycle, "sample": start})
			g.t.Fatal("young query input phase did not survive actual exporter setup")
		}
	}
	nativeStart := resourceNS()
	got, channel := request(g.t, g.consumer("alice"), "lab-a/http", "competition-normal")
	nativeEnd := resourceNS()
	if got != "T1:competition-normal" {
		g.t.Fatal(got)
	}
	mark := g2Mark(p)
	controlGate := g2Arm(g.t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgCloseChannel, ChannelID: v.stream.ChannelID()})
	providerGate := g2Arm(g.t, p, g2Match{Role: g2RoleProvider, Direction: g2OISToOIC, Type: "ABORT", ChannelID: v.stream.ChannelID()})
	barrierArmed := resourceNS()
	defer controlGate.Cancel()
	defer providerGate.Cancel()
	revokeStart := resourceNS()
	g.write("DELETE", "/v1/admin/tokens/"+v.token.TokenID, nil)
	revokeReturn := resourceNS()
	readResult := make(chan struct {
		n    int
		code protocol.Code
		at   int64
	}, 1)
	go func() {
		b := make([]byte, 1)
		n, err := v.stream.Read(b)
		readResult <- struct {
			n    int
			code protocol.Code
			at   int64
		}{n, abortCode(err), resourceNS()}
	}()
	inputCtx, inputCancel := context.WithDeadline(context.Background(), deadline)
	defer inputCancel()
	providerAbort, err := providerGate.Wait(inputCtx)
	if err != nil {
		g.t.Fatalf("actual provider ABORT input not captured within original cycle: %v", err)
	}
	providerFrame, err := protocol.DecodeFrame(providerAbort.Raw, 1<<20)
	if err != nil || providerFrame.Type != protocol.FrameAbort || providerFrame.Reason != protocol.CodeRevoked {
		g.t.Fatalf("wrong provider revocation input: %+v %v", providerFrame, err)
	}
	if _, err := controlGate.Wait(inputCtx); err != nil {
		g.t.Fatalf("actual control revocation input not captured within original cycle: %v", err)
	}
	controlRelease := resourceNS()
	controlGate.Release()
	var abortAt int64
	select {
	case r := <-readResult:
		if r.n != 0 || r.code != protocol.CodeRevoked {
			g.t.Fatalf("revoke data/outcome %d/%s", r.n, r.code)
		}
		abortAt = r.at
	case <-time.After(time.Second):
		_ = v.stream.Close()
		g.t.Fatal("consumer did not receive real Revoked")
	}
	if n, err := v.stream.Write([]byte("forbidden-after-revoke")); n != 0 || err == nil {
		g.t.Fatal("revoked stream accepted more bytes")
	}
	_ = v.stream.Close()
	oicAbort, err := p.WaitEvent(inputCtx, mark, g2Match{Role: g2RoleProvider, Direction: g2OICToOIS, Type: "ABORT", ChannelID: v.stream.ChannelID()})
	if err != nil {
		g.t.Fatalf("real OIC first terminal not observed inside original cycle: %v", err)
	}
	oicFrame, err := protocol.DecodeFrame(oicAbort.Raw, 1<<20)
	if err != nil || oicFrame.Type != protocol.FrameAbort || oicFrame.Reason != protocol.CodeRevoked || oicAbort.Injected || oicAbort.Dropped || oicAbort.ConnID != providerAbort.ConnID {
		g.t.Fatalf("wrong OIC control-terminal witness: %+v %v", oicFrame, err)
	}
	providerRelease := resourceNS()
	providerGate.Release()
	var finished g2CompetitionStats
	g.eventually(500*time.Millisecond, "actual OIC revocation applied", func() bool {
		finished = g2CompetitionSample(g, hc)
		return finished.OIC.Closed[string(protocol.CodeRevoked)] == start.OIC.Closed[string(protocol.CodeRevoked)]+1
	})
	closeEvent := g2WaitWire(g.t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgCloseChannel, ChannelID: v.stream.ChannelID()})
	closeEvent = g2WaitWritten(g.t, p, closeEvent)
	var closeMessage protocol.CloseChannel
	if err := json.Unmarshal(closeEvent.Raw, &closeMessage); err != nil {
		g.t.Fatal(err)
	}
	if closeMessage.Code != protocol.CodeRevoked || finished.CompletedNS-closeEvent.ReceivedNS > int64(500*time.Millisecond) || finished.CompletedNS < closeEvent.ReceivedNS {
		g.t.Fatalf("revocation processing exceeded conservative 500ms: %+v %+v", closeEvent, finished)
	}
	if finished.OIC.Epoch != epoch || finished.OIC.Generation != generation {
		g.t.Fatal("control reconnected under competition")
	}
	if requireFull && !g2CompetitionFull(finished) {
		g.event("competition_lost_full", map[string]any{"cycle": cycle, "before": start, "after": finished})
		g.t.Fatal("backlog drained before measured critical processing")
	}
	var postCriticalRejection g2Exposition
	if requireFull {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		postCriticalRejection, err = g2ReadExposition(ctx, hc, addr)
		cancel()
		lastScrape = postCriticalRejection
		if err != nil || postCriticalRejection.Status != 429 {
			g.event("competition_overlap_input_lost", map[string]any{"cycle": cycle, "before": rejected, "after": postCriticalRejection, "critical_finished_ns": finished.CompletedNS})
			g.t.Fatalf("both exporter slots were not still occupied after critical processing: status=%d error=%v", postCriticalRejection.Status, err)
		}
	}
	var providerConnection g2ConnectionSnapshot
	for {
		snapshot := p.Snapshot()
		for _, e := range snapshot.Events {
			if e.ID == providerAbort.ID {
				providerAbort = e
			}
			if e.ID == oicAbort.ID {
				oicAbort = e
			}
		}
		for _, c := range snapshot.Connections {
			if c.ID == providerAbort.ConnID {
				providerConnection = c
			}
		}
		if providerConnection.Closed && providerConnection.Workers == 0 {
			break
		}
		select {
		case <-inputCtx.Done():
			g.t.Fatal("provider ABORT barrier did not close/join within original cycle")
		case <-time.After(time.Millisecond):
		}
	}
	providerGate.Cancel()
	controlGate.Cancel()
	barrierCancelled := resourceNS()
	r := waitTarget(g.t, g.t1, v.targetIndex)
	if string(r.Data) != v.payload {
		g.t.Fatalf("revoked target received unexpected payload %q", r.Data)
	}
	var boundedFailure g2Exposition
	stage = "await_export_failures"
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		x, err := g2ReadExposition(ctx, hc, addr)
		cancel()
		lastScrape = x
		if err != nil {
			g.t.Fatal(err)
		}
		if x.Status == 200 && g2CompetitionExportFailures(x)-g2CompetitionExportFailures(before) >= 2 {
			boundedFailure = x
			break
		}
		if x.Status != 200 && x.Status != 429 {
			g.t.Fatalf("competition completion scrape %d", x.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if boundedFailure.Status != 200 {
		g.t.Fatal("both blocked writes did not fail before instrument close")
	}
	closedAt := resourceNS()
	stage = "recovery"
	if err := slow.Close(); err != nil {
		g.t.Fatal(err)
	}
	after := g2ExporterRead(g.t, hc, addr)
	if after.CompletedNS-closedAt > int64(2*time.Second) {
		g.t.Fatal("exporter did not recover within 2s")
	}
	g.event("competition_cycle", map[string]any{"round": round, "cycle": cycle, "role": role, "full_backlog_required": requireFull, "before_export": before, "actual_rejection": rejected, "export_input_observations": inputGate.observations, "export_slot_input": slotInput, "slow_response_headers": responseHeaders, "post_critical_rejection": postCriticalRejection, "backlog_before": start, "native_started_ns": nativeStart, "native_completed_ns": nativeEnd, "native_response": got, "normal_channel_id": channel, "revoke_token_id": v.token.TokenID, "victim_channel_id": v.stream.ChannelID(), "revoke_started_ns": revokeStart, "revoke_returned_ns": revokeReturn, "consumer_abort_ns": abortAt, "revoke_wire": closeEvent, "backlog_after": finished, "oic_abort_witness": oicAbort, "provider_abort_barrier": map[string]any{"channel_id": v.stream.ChannelID(), "armed_ns": barrierArmed, "control_release_requested_ns": controlRelease, "release_requested_ns": providerRelease, "cancelled_ns": barrierCancelled, "event_id": providerAbort.ID}, "provider_abort": providerAbort, "provider_abort_connection": providerConnection, "target": r, "bounded_failure_before_close": boundedFailure, "scrapes_closed_ns": closedAt, "slow_writes": slow.Snapshot(), "after_export": after})
}

func TestTVG2_11_ControlExportCompetition(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for round := range 3 {
			t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
				preparation := &g2PreparationFailure{Round: round}
				var g *g2Env
				// Registered before the environment: emit only after its original cleanup.
				t.Cleanup(func() {
					if preparation.CapturedNS != nil && g != nil {
						g.event("competition_failed_preparation", preparation.fields())
					}
				})
				defer func() {
					p := recover()
					preparation.finish(p, t.Failed())
					if p != nil {
						panic(p)
					}
				}()
				ids := []string{"lab-a/http", "lab-a/alt"}
				for i := 2; i < 16; i++ {
					ids = append(ids, fmt.Sprintf("lab-a/competition-%02d", i))
				}
				g = newG2Env(t, be, "TV-G2-11", g2Options{proxy: true, probes: ids})
				hc := g2MetricsClient(g)
				initial := g2CompetitionSample(g, hc)
				victim := g2CompetitionPrepare(g, round, 0)
				started := resourceNS()
				prepareStart := time.Now()
				prepareDeadline := prepareStart.Add(3 * time.Second)
				prepareStartedNS := int64(prepareStart.Sub(resourceOrigin))
				prepareDeadlineNS := int64(prepareDeadline.Sub(resourceOrigin))
				preparation.StartedNS, preparation.DeadlineNS = prepareStartedNS, prepareDeadlineNS
				preparation.Phase, preparation.active = "healthy_backlog", true
				// Only actual snapshot responses are dropped; critical messages on
				// this ordered stream remain untouched. Every original query keeps
				// its 500ms absolute deadline. The input is capped at 32 responses.
				var drops []*g2Barrier
				for range 32 {
					b := g2Arm(t, g.paths["lab-a"], g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
					b.Drop()
					drops = append(drops, b)
				}
				// Build the source backlog while all probes still complete normally.
				// Starting fourteen slow targets first spreads their dirty notices
				// across a worker round, which need not coincide with a young full
				// query batch. This is input preparation, not a change to deadlines
				// or a retry: both stages share the original three-second window.
				g.eventually(time.Until(prepareDeadline), "actual healthy 16-source backlog and young query batch", func() bool {
					s := preparation.sample(g, hc)
					preparation.full(s)
					o := s.OIS.Observation
					snapshot := g.paths["lab-a"].Snapshot()
					snapshotNS := resourceNS()
					ok := o != nil && o.ObservationCapacity == 16 && o.ObservationBacklog == 16 && o.PendingQueries == 8 &&
						preparation.young(snapshot.Events, resourceNS())
					return preparation.returned(ok, snapshot, snapshotNS)
				})
				if !time.Now().Before(prepareDeadline) {
					t.Fatal("healthy backlog observation completed after original preparation deadline")
				}
				preparation.acceptedHealthy()
				versions := map[string]uint64{}
				preparation.slowSetStartedNS = g2DiagnosticPtr(resourceNS())
				for i, id := range ids {
					versions[id] = g.health[id].Set(200, i >= 2)
				}
				slowInputApplied := resourceNS()
				preparation.SlowAppliedNS = g2DiagnosticPtr(slowInputApplied)
				preparation.Phase = "slow_full"
				// Healthy backlog preparation is not time under the slow input.
				// Retain its original 3s deadline; observe all fourteen slow targets
				// for a separate complete 60s from the last actual Set returning.
				end := resourceOrigin.Add(time.Duration(slowInputApplied) + 60*time.Second)
				var full g2CompetitionStats
				g.eventually(time.Until(prepareDeadline), "actual 16-source backlog, P2/Q4 and young query batch", func() bool {
					full = preparation.sample(g, hc)
					preparation.full(full)
					snapshot := g.paths["lab-a"].Snapshot()
					snapshotNS := resourceNS()
					ok := g2CompetitionFull(full) && preparation.young(snapshot.Events, resourceNS())
					return preparation.returned(ok, snapshot, snapshotNS)
				})
				preparedNS := resourceNS()
				if preparedNS > prepareDeadlineNS {
					t.Fatal("full backlog observation completed after original preparation deadline")
				}
				preparation.active = false
				addresses := g2ExporterAddresses(g)
				g.event("competition_full", map[string]any{"round": round, "window_start_ns": started, "prepare_started_ns": prepareStartedNS, "prepare_deadline_ns": prepareDeadlineNS, "prepared_ns": preparedNS, "slow_input_applied_ns": slowInputApplied, "sample": full, "snapshot_drop_cap": 32})
				fullRole := "ois"
				if round%2 == 1 {
					fullRole = "oic"
				}
				g2CompetitionCycle(g, hc, fullRole, addresses[fullRole], round, 0, victim, true, initial.OIC.Epoch, initial.OIC.Generation)
				for _, b := range drops {
					b.Cancel()
				}
				drainMark := g2Mark(g.paths["lab-a"])
				for {
					e := g2WaitWire(t, g.paths["lab-a"], drainMark, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
					drainMark = e.ID
					if e.Dropped {
						continue
					}
					e = g2WaitWritten(t, g.paths["lab-a"], e)
					g.event("competition_drain", map[string]any{"round": round, "first_forwarded_snapshot": e, "sample": g2CompetitionSample(g, hc)})
					break
				}
				cycles := 1
				for time.Now().Before(end) {
					cycleStart := time.Now()
					v := g2CompetitionPrepare(g, round, cycles)
					role := "ois"
					if cycles%2 == 1 {
						role = "oic"
					}
					g2CompetitionCycle(g, hc, role, addresses[role], round, cycles, v, false, initial.OIC.Epoch, initial.OIC.Generation)
					cycles++
					if cycles > 512 {
						t.Fatal("bounded competition cycle capacity exhausted")
					}
					if remaining := time.Until(cycleStart.Add(500 * time.Millisecond)); remaining > 0 {
						time.Sleep(remaining)
					}
				}
				ended := resourceNS()
				if ended-slowInputApplied < int64(60*time.Second) || cycles < 20 {
					t.Fatal("competition window incomplete")
				}
				// Allow the final heartbeat's existing 500ms test budget to finish;
				// its measured interval still starts at the original proxy receipt.
				time.Sleep(500 * time.Millisecond)
				heartbeatEvidence := g2CompetitionHeartbeats(g, started, ended, round)
				var targetEvidence []map[string]any
				for i, id := range ids {
					hs := g.health[id].Snapshot()
					attempts, timeouts := 0, 0
					if hs.Overflow {
						t.Fatal("health evidence overflow")
					}
					for _, r := range hs.Records {
						if r.Version == versions[id] && r.StartedNS >= started && r.StartedNS <= ended {
							attempts++
							if r.CancelledNS > r.StartedNS && r.HeaderStartedNS == 0 {
								timeouts++
							}
						}
					}
					if attempts == 0 || (i >= 2 && timeouts == 0) {
						t.Fatalf("competition target not exercised %s %d/%d", id, attempts, timeouts)
					}
					targetEvidence = append(targetEvidence, map[string]any{"round": round, "exposure_id": id, "version": versions[id], "timeout_target": i >= 2, "attempts": attempts, "timeouts": timeouts, "health": hs})
				}
				g.event("competition_cleanup_requested", map[string]any{"round": round, "window_end_ns": ended, "requested_ns": resourceNS()})
				// The G2 wrapper restores probes and closes this cached metrics
				// transport inside the original absolute B4 cleanup window.
				g.settled()
				for _, e := range heartbeatEvidence {
					g.event("competition_heartbeats", e)
				}
				for _, e := range targetEvidence {
					g.event("competition_target", e)
				}
				g.event("competition_complete", map[string]any{"round": round, "window_start_ns": started, "window_end_ns": ended, "prepare_started_ns": prepareStartedNS, "prepare_deadline_ns": prepareDeadlineNS, "prepared_ns": preparedNS, "slow_input_applied_ns": slowInputApplied, "cycles": cycles, "targets": 16, "timeout_targets": 14})
				g2RecordWire(g)
			})
		}
	})
}

func g2CompetitionHeartbeats(g *g2Env, start, end int64, round int) []map[string]any {
	g.t.Helper()
	var evidence []map[string]any
	for access, p := range g.paths {
		s := p.Snapshot()
		if s.Overflow || s.Failure != "" {
			g.t.Fatal("incomplete control evidence")
		}
		acks := map[string]g2WireEvent{}
		for _, e := range s.Events {
			if e.Role != g2RoleControl || e.Direction != g2OICToOIS || e.Type != protocol.MsgHeartbeatAck {
				continue
			}
			var a protocol.HeartbeatAck
			if err := json.Unmarshal(e.Raw, &a); err != nil {
				g.t.Fatal(err)
			}
			key := fmt.Sprintf("%d/%d", e.ConnID, a.AckSeq)
			if _, ok := acks[key]; ok {
				g.t.Fatal("duplicate heartbeat acknowledgement")
			}
			acks[key] = e
		}
		pairs := []map[string]any{}
		for _, e := range s.Events {
			if e.Role != g2RoleControl || e.Direction != g2OISToOIC || e.Type != protocol.MsgHeartbeat || e.ReceivedNS < start || e.ReceivedNS > end {
				continue
			}
			var b protocol.Heartbeat
			if err := json.Unmarshal(e.Raw, &b); err != nil {
				g.t.Fatal(err)
			}
			a, ok := acks[fmt.Sprintf("%d/%d", e.ConnID, b.Seq)]
			if !ok || e.Injected || a.Injected || e.Dropped || a.Dropped || e.WrittenNS == 0 || a.WrittenNS == 0 || a.ReceivedNS < e.ReceivedNS || a.WrittenNS-e.ReceivedNS > int64(500*time.Millisecond) || b.LicenseTTLMS != 3000 {
				g.t.Fatalf("heartbeat ack/lease bound violated: %+v %+v", e, a)
			}
			pairs = append(pairs, map[string]any{"heartbeat": e, "ack": a, "elapsed_ns": a.WrittenNS - e.ReceivedNS})
		}
		if len(pairs) < 20 {
			g.t.Fatal("insufficient actual heartbeat interval coverage")
		}
		evidence = append(evidence, map[string]any{"round": round, "access_id": access, "window_start_ns": start, "window_end_ns": end, "pairs": pairs})
	}
	return evidence
}
