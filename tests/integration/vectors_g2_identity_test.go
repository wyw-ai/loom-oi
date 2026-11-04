package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type g2HTTPOutcome struct {
	Status                 int                   `json:"status"`
	Open                   protocol.OpenResponse `json:"open"`
	Code                   protocol.Code         `json:"code,omitempty"`
	Error                  string                `json:"error,omitempty"`
	StartedNS, CompletedNS int64
	err                    error
}

func g2AsyncOpen(c *client.Client, ctx context.Context, req protocol.OpenRequest) <-chan g2HTTPOutcome {
	done := make(chan g2HTTPOutcome, 1)
	go func() {
		out := g2HTTPOutcome{StartedNS: resourceNS()}
		out.Status, out.err = c.Do(ctx, "POST", "/v1/channels", "", req, &out.Open)
		out.CompletedNS = resourceNS()
		out.Code = protocol.CodeOf(out.err)
		if out.err != nil {
			out.Error = out.err.Error()
		}
		done <- out
	}()
	return done
}
func g2AwaitOpen(t *testing.T, done <-chan g2HTTPOutcome) g2HTTPOutcome {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("Open did not finish")
		return g2HTTPOutcome{}
	}
}
func g2WaitBarrier(t *testing.T, b *g2Barrier) g2WireEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := b.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func g2Arm(t *testing.T, p *g2Proxy, m g2Match) *g2Barrier {
	t.Helper()
	b, err := p.Arm(m)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Cancel)
	return b
}
func g2Mark(p *g2Proxy) uint64 {
	s := p.Snapshot()
	if len(s.Events) == 0 {
		return 0
	}
	return s.Events[len(s.Events)-1].ID
}
func g2CheckIDs(t *testing.T, p *g2Proxy, after uint64, want int) []string {
	t.Helper()
	var ids []string
	seen := map[string]bool{}
	for _, e := range p.Snapshot().Events {
		if e.ID > after && e.Type == protocol.MsgAdmissionCheck && !e.Injected {
			if e.Stage != protocol.CheckPreflight || e.CheckID == "" || seen[e.CheckID] {
				t.Fatal("unexpected final/reused check in preflight-only operation", e)
			}
			seen[e.CheckID] = true
			ids = append(ids, e.CheckID)
		}
	}
	if len(ids) != want {
		t.Fatalf("operation emitted %d checks, want %d: %v", len(ids), want, ids)
	}
	return ids
}

func g2WaitWire(t *testing.T, p *g2Proxy, after uint64, m g2Match) g2WireEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := p.WaitEvent(ctx, after, m)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func g2WaitWritten(t *testing.T, p *g2Proxy, e g2WireEvent) g2WireEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := p.WaitWritten(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func g2WaitSlots(g *g2Env, opens, waiters int) protocol.OISStats {
	g.t.Helper()
	var s protocol.OISStats
	g.eventually(2*time.Second, "exact pending Open/waiter counts", func() bool {
		s = g.oisStats()
		return s.Observation != nil && s.Observation.PendingOpens == opens && s.Observation.HTTPWaiters == waiters
	})
	return s
}
func g2RecordWire(g *g2Env) {
	for access, p := range g.paths {
		s := p.Snapshot()
		if s.Overflow || s.Failure != "" {
			g.t.Fatalf("proxy evidence incomplete: %+v", s)
		}
		g.event("wire_history", map[string]any{"access_id": access, "proxy": s})
	}
}
func g2CloseChannel(g *g2Env, o protocol.OpenResponse) {
	g.t.Helper()
	if o.ChannelID == "" {
		g.t.Fatal("missing created channel")
	}
	if _, err := g.consumer("alice").Close(bg, o.ChannelID); err != nil {
		g.t.Fatal(err)
	}
}

// G2-08d executes real HTTPS requests against one coordinator and captures the
// actual preflight messages. Each race is repeated with a fresh key, not replayed
// as a synthetic count. Cancellation never substitutes another credential.
func TestTVG2_08d_PendingOpenIdentity(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-08d", g2Options{proxy: true})
		p := g.paths["lab-a"]
		c := g.consumer("alice")
		var token protocol.TokenCreated
		if err := json.Unmarshal(g.write("POST", "/v1/admin/tokens", protocol.TokenCreate{Subject: "alice", Scope: protocol.ScopeConsumer}), &token); err != nil {
			t.Fatal(err)
		}
		other := g.clientFor(token.Token)
		revision := g.view("lab-a/http").Revision
		match := g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckPreflight}
		for round := range 20 {
			mergeMark := g2Mark(p)
			req := protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: revision, ClientRequestID: fmt.Sprintf("merge-%d", round)}
			b := g2Arm(t, p, match)
			ctx, cancel := context.WithCancel(context.Background())
			first := g2AsyncOpen(c, ctx, req)
			held := g2WaitBarrier(t, b)
			waiters := make([]<-chan g2HTTPOutcome, 0, 15)
			for i := range 15 {
				caller := c
				if i == 0 {
					caller = other
				}
				waiters = append(waiters, g2AsyncOpen(caller, context.Background(), req))
			}
			full := g2WaitSlots(g, 1, 16)
			overflow := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
			if overflow.Status != 429 || overflow.Code != protocol.CodeResourceExhausted {
				t.Fatalf("17th waiter: %+v", overflow)
			}
			different := req
			different.Admission = protocol.AdmissionRequireReady
			conflict := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), different))
			if conflict.Status != 409 || conflict.Code != protocol.CodeIdempotencyConflict {
				t.Fatalf("different params: %+v", conflict)
			}
			cancel()
			cancelled := g2AwaitOpen(t, first)
			if !errors.Is(cancelled.err, context.Canceled) {
				t.Fatalf("creator cancel: %+v", cancelled)
			}
			remaining := g2WaitSlots(g, 1, 15)
			b.Release()
			results := make([]g2HTTPOutcome, 0, 15)
			for _, d := range waiters {
				out := g2AwaitOpen(t, d)
				if out.err != nil || out.Status != 200 {
					t.Fatalf("joined waiter: %+v", out)
				}
				if len(results) > 0 && (out.Open.ChannelID != results[0].Open.ChannelID || out.Open.Attach == nil || out.Open.Attach.Ticket != results[0].Open.Attach.Ticket) {
					t.Fatal("merged request created different channel/ticket")
				}
				results = append(results, out)
			}
			o := results[0].Open
			if s, err := other.Attach(context.Background(), o); err == nil || protocol.CodeOf(err) != protocol.CodeTicketInvalid {
				if s != nil {
					s.Close()
				}
				t.Fatalf("foreign credential attached creator ticket: %v", err)
			}
			// A cancelled creator's still-valid credential remains the binding owner.
			replay := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
			if replay.err != nil || replay.Status != 200 || replay.Open.ChannelID != o.ChannelID {
				t.Fatalf("created replay: %+v", replay)
			}
			g2CloseChannel(g, o)
			g.event("pending_merge", map[string]any{"check_ids": g2CheckIDs(t, p, mergeMark, 1), "round": round, "request": req, "held": held, "full": full, "after_creator_cancel": remaining, "creator": cancelled, "waiters": results, "overflow": overflow, "conflict": conflict, "replay": replay, "after": g2WaitSlots(g, 0, 0)})

			// The last cancelled HTTP frees the key. Its queued old result is delivered
			// while the replacement is still pending, proving identity-bound cleanup.
			req.ClientRequestID = fmt.Sprintf("last-cancel-%d", round)
			cancelMark := g2Mark(p)
			oldBarrier := g2Arm(t, p, match)
			oldCtx, oldCancel := context.WithCancel(context.Background())
			oldHTTP := g2AsyncOpen(c, oldCtx, req)
			oldEvent := g2WaitBarrier(t, oldBarrier)
			oldCancel()
			oldOut := g2AwaitOpen(t, oldHTTP)
			if !errors.Is(oldOut.err, context.Canceled) {
				t.Fatal(oldOut)
			}
			zero := g2WaitSlots(g, 0, 0)
			nextBarrier := g2Arm(t, p, match)
			mark := g2Mark(p)
			nextHTTP := g2AsyncOpen(c, context.Background(), req)
			nextCheck := g2WaitWire(t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgAdmissionCheck, Stage: protocol.CheckPreflight})
			if oldEvent.CheckID == nextCheck.CheckID {
				t.Fatal("check ID reused")
			}
			oldBarrier.Release()
			g2WaitWritten(t, p, oldEvent)
			nextEvent := g2WaitBarrier(t, nextBarrier)
			live := g2WaitSlots(g, 1, 1)
			nextBarrier.Release()
			nextOut := g2AwaitOpen(t, nextHTTP)
			if nextOut.err != nil || nextOut.Status != 201 {
				t.Fatalf("replacement after stale response: %+v", nextOut)
			}
			g2CloseChannel(g, nextOut.Open)
			g.event("last_cancel_and_late_result", map[string]any{"check_ids": g2CheckIDs(t, p, cancelMark, 2), "round": round, "request": req, "old_result": oldEvent, "cancelled": oldOut, "released": zero, "next_check": nextCheck, "next_result": nextEvent, "replacement_pending": live, "replacement": nextOut, "after": g2WaitSlots(g, 0, 0)})

			// Cancellation after the atomic Channel creation cannot roll it back or
			// erase its idempotency entry. Hold its actual prepare to make the order exact.
			createdMark := g2Mark(p)
			req.ClientRequestID = fmt.Sprintf("created-cancel-%d", round)
			prepared := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgPrepareChannel})
			createdCtx, createdCancel := context.WithCancel(context.Background())
			createdHTTP := g2AsyncOpen(c, createdCtx, req)
			prepare := g2WaitBarrier(t, prepared)
			cancelledAt := resourceNS()
			createdCancel()
			created := g2AwaitOpen(t, createdHTTP)
			if created.err != nil && !errors.Is(created.err, context.Canceled) || created.err == nil && (created.Status != 201 || created.Open.ChannelID != prepare.ChannelID) {
				t.Fatalf("created before cancellation: %+v", created)
			}
			again := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
			if again.err != nil || again.Status != 200 || again.Open.ChannelID != prepare.ChannelID || again.Open.Attach == nil {
				t.Fatalf("post-create cancellation changed identity: %+v", again)
			}
			retained := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
			if retained.err != nil || retained.Status != 200 || retained.Open.ChannelID != again.Open.ChannelID || retained.Open.Attach == nil || retained.Open.Attach.Ticket != again.Open.Attach.Ticket || created.err == nil && created.Open.Attach.Ticket != again.Open.Attach.Ticket {
				t.Fatal("post-create cancellation changed retained ticket", retained)
			}
			prepared.Release()
			g2CloseChannel(g, again.Open)
			g.event("cancel_after_creation", map[string]any{"check_ids": g2CheckIDs(t, p, createdMark, 1), "round": round, "request": req, "prepare": prepare, "cancelled_ns": cancelledAt, "created": created, "replay": again, "retained": retained, "after": g2WaitSlots(g, 0, 0)})
		}
		// A concrete negative local result releases its key immediately, too.
		req := protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: revision, ClientRequestID: "failed-then-retry"}
		g.health[req.ExposureID].Set(503, false)
		g.waitView(req.ExposureID, func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessNotReady })
		failed := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
		if failed.Code != protocol.CodeTargetNotReady {
			t.Fatal(failed)
		}
		g2WaitSlots(g, 0, 0)
		g.health[req.ExposureID].Set(200, false)
		g.waitView(req.ExposureID, func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessReady })
		retried := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), req))
		if retried.err != nil || retried.Status != 201 {
			t.Fatal(retried)
		}
		g2CloseChannel(g, retried.Open)
		g.event("failed_key_retry", map[string]any{"request": req, "failed": failed, "retried": retried, "after": g2WaitSlots(g, 0, 0)})
		g.settled()
		g2RecordWire(g)
		g.recordHealth(req.ExposureID)
	})
}

func g2DecodeSnapshot(t *testing.T, e g2WireEvent) *protocol.ObservationSnapshot {
	t.Helper()
	m, err := protocol.DecodeControlForVersion(e.Raw, protocol.ControlSubprotocolV2)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := m.(*protocol.ObservationSnapshot)
	if !ok {
		t.Fatalf("not snapshot: %s", e.Raw)
	}
	return s
}
func g2PreviousSnapshot(t *testing.T, p *g2Proxy, conn uint64, before uint64) (g2WireEvent, *protocol.ObservationSnapshot) {
	t.Helper()
	events := p.Snapshot().Events
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.ID < before && e.ConnID == conn && e.Type == protocol.MsgObservationSnapshot && e.WrittenNS > 0 && !e.Injected && !e.Dropped {
			return e, g2DecodeSnapshot(t, e)
		}
	}
	t.Fatal("missing previously delivered snapshot")
	return g2WireEvent{}, nil
}
func g2InjectSnapshot(t *testing.T, p *g2Proxy, e g2WireEvent, m *protocol.ObservationSnapshot) []byte {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = p.Inject(ctx, e.ConnID, g2OICToOIS, e.MessageType, raw); err != nil {
		t.Fatal(err)
	}
	return raw
}
func g2SnapshotBarrier(t *testing.T, p *g2Proxy, conn uint64) *g2Barrier {
	return g2Arm(t, p, g2Match{ConnID: conn, Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot})
}
func g2ReadySample(g *g2Env, minSeq uint64) protocol.ExposureView {
	return g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
		return v.Readiness == protocol.ReadinessReady && v.ReadinessView != nil && v.ReadinessView.SampleSeq != nil && *v.ReadinessView.SampleSeq >= minSeq
	})
}

// G2-03 mutates only explicit test messages in a real, authenticated control
// connection. Envelope seq/query identity comes from the held real response;
// injected observations are always distinguishable from OIC-produced evidence.
func TestTVG2_03_SequenceAndBinding(t *testing.T) {
	for _, scenario := range []string{"duplicate", "low_update", "low_sample", "same_sample_conflict", "old_generation", "old_revision", "old_source"} {
		t.Run(scenario, func(t *testing.T) {
			each(t, func(t *testing.T, be string) {
				g := newG2Env(t, be, "G2-03", g2Options{proxy: true})
				p := g.paths["lab-a"]
				var oldRevision, oldDigest string
				if scenario == "old_revision" || scenario == "old_source" {
					old := g.view("lab-a/http")
					oldRevision = old.Revision
					oldDigest = old.ReadinessView.ContractDigest
					exps := g.oics["lab-a"].cfg["exposures"].([]any)
					m := exps[0].(map[string]any)
					if scenario == "old_revision" {
						m["target"].(map[string]any)["address"] = g.t3.Addr()
					} else {
						m["probe"].(map[string]any)["ready_statuses"] = []int{200}
					}
					g.rewriteOIC("lab-a", exps)
					g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
						if scenario == "old_revision" {
							return v.Revision != oldRevision
						}
						return v.ReadinessView != nil && v.ReadinessView.ContractDigest != oldDigest
					})
				}
				if scenario == "old_generation" {
					// Restart the actual OIC with identical config and the same TLS proxy;
					// generation 1 is now an observed historical generation, not an invented ID.
					o := g.oics["lab-a"]
					o.proc.kill()
					select {
					case <-o.proc.exited:
					case <-time.After(3 * time.Second):
						t.Fatal("old OIC did not exit")
					}
					o.proc = startProc(t, g.dir, "oic-lab-a-generation-restart", oicBin, nil, "run", "--config", o.path)
					l, _ := o.proc.waitLog(t, 20*time.Second, "diagnostics listening", 0)
					o.diag = l["addr"].(string)
					g.waitRegistered("lab-a")
				}
				for round := range 20 {
					g2ReadySample(g, 2)
					b := g2SnapshotBarrier(t, p, 0)
					held := g2WaitBarrier(t, b)
					priorEvent, prior := g2PreviousSnapshot(t, p, held.ConnID, held.ID)
					if prior.SampleSeq == nil || prior.RemainingMS <= 1 || prior.UpdateSeq < 2 {
						t.Fatal("sequence fixture needs a positive prior sample")
					}
					before := g.view("lab-a/http")
					actual := g2DecodeSnapshot(t, held)
					candidate := *actual
					fails := scenario == "same_sample_conflict" || scenario == "old_generation" || scenario == "old_revision" || scenario == "old_source"
					switch scenario {
					case "duplicate", "low_update", "same_sample_conflict":
						candidate = *prior
						candidate.Header = actual.Header
						candidate.QueryID = actual.QueryID
						candidate.RemainingMS = min(prior.RemainingMS-1, actual.RemainingMS)
						if candidate.RemainingMS <= 0 {
							t.Fatal("prior sample expired before instrument change")
						}
						if scenario == "low_update" {
							candidate.UpdateSeq--
						}
						if scenario == "same_sample_conflict" {
							changed := candidate.ObservedAt.Add(time.Hour)
							candidate.ObservedAt = &changed
						}
					case "low_sample":
						candidate = *prior
						candidate.Header = actual.Header
						candidate.QueryID = actual.QueryID
						candidate.UpdateSeq = prior.UpdateSeq + 1
						old := *prior.SampleSeq - 1
						if old == 0 {
							t.Fatal("low sample requires prior seq >=2")
						}
						candidate.SampleSeq = &old
					case "old_generation":
						candidate.Binding.Generation--
					case "old_revision":
						candidate.Binding.Revision = oldRevision
					case "old_source":
						candidate.Binding.ContractDigest = oldDigest
					}
					if fails {
						welcomeGate := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgWelcome})
						injected := g2InjectSnapshot(t, p, held, &candidate)
						b.Drop()
						wireError := g2WaitWire(t, p, held.ID, g2Match{ConnID: held.ConnID, Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgError})
						var rejection protocol.ControlError
						if err := json.Unmarshal(wireError.Raw, &rejection); err != nil || rejection.Code != protocol.CodeProtocolError {
							t.Fatalf("binding/content violation did not produce ProtocolError: %s %v", wireError.Raw, err)
						}
						welcome := g2WaitBarrier(t, welcomeGate)
						firstSnapshotGate := g2SnapshotBarrier(t, p, welcome.ConnID)
						welcomeGate.Release()
						firstSnapshot := g2WaitBarrier(t, firstSnapshotGate)
						fresh := g2DecodeSnapshot(t, firstSnapshot)
						unknown := g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
							return v.ReadinessView != nil && v.ReadinessView.Generation == fresh.Binding.Generation && v.Readiness == protocol.ReadinessUnknown
						})
						if fresh.Binding.Generation <= actual.Binding.Generation {
							t.Fatal("reconnect did not advance generation")
						}
						firstSnapshotGate.Release()
						g.event("binding_rejection", map[string]any{"scenario": scenario, "round": round, "before": before, "prior": priorEvent, "held": held, "injected_raw": injected, "error": wireError, "welcome": welcome, "new_first_snapshot": firstSnapshot, "new_generation_unknown": unknown})
					} else {
						// Holding the next real response gives a protocol progress barrier: the
						// injected query has finished, while no newer cache sample can hide it.
						guard := g2SnapshotBarrier(t, p, held.ConnID)
						injected := g2InjectSnapshot(t, p, held, &candidate)
						b.Drop()
						next := g2WaitBarrier(t, guard)
						after := g.view("lab-a/http")
						if after.ReadinessView == nil || after.ReadinessView.LastReported == nil || after.ReadinessView.LastReported.SampleSeq != *prior.SampleSeq || after.ReadinessView.LastAttempt != prior.Attempt || after.ReadinessView.RemainingMS > before.ReadinessView.RemainingMS {
							t.Fatalf("old/duplicate state revived or regressed cache: before=%+v after=%+v", before.ReadinessView, after.ReadinessView)
						}
						guard.Release()
						g.event("sequence_ignored_or_idempotent", map[string]any{"scenario": scenario, "round": round, "before": before, "prior": priorEvent, "held": held, "injected_raw": injected, "next_response_held": next, "after": after})
					}
				}
				g.settled()
				g2RecordWire(g)
				g.recordHealth("lab-a/http")
			})
		})
	}
}

// G2-04 uses fixture monotonic boundaries to compute an independently generous
// upper bound: OIS sent before the proxy received the query, and evaluated the
// view after its HTTP request began. Thus this bound can never over-deduct RTT.
func TestTVG2_04_QueryTTL(t *testing.T) {
	for _, scenario := range []string{"query_delay", "response_delay", "asymmetric", "cache_wait", "duplicate_reply"} {
		t.Run(scenario, func(t *testing.T) {
			each(t, func(t *testing.T, be string) {
				g := newG2Env(t, be, "G2-04", g2Options{proxy: true})
				p := g.paths["lab-a"]
				for round := range 20 {
					g2ReadySample(g, 1)
					queryGate := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgObservationQuery})
					query := g2WaitBarrier(t, queryGate)
					responseGate := g2SnapshotBarrier(t, p, query.ConnID)
					queryDelay, responseDelay := time.Duration(0), time.Duration(0)
					switch scenario {
					case "query_delay":
						queryDelay = 100 * time.Millisecond
					case "response_delay":
						responseDelay = 200 * time.Millisecond
					case "asymmetric":
						queryDelay = 75 * time.Millisecond
						responseDelay = 225 * time.Millisecond
					}
					if queryDelay > 0 {
						time.Sleep(queryDelay)
					} // explicit measured network impairment, not a race oracle
					queryGate.Release()
					response := g2WaitBarrier(t, responseGate)
					snapshot := g2DecodeSnapshot(t, response)
					if snapshot.Readiness != protocol.ReadinessReady || snapshot.SampleSeq == nil {
						t.Fatalf("ready fixture returned %s", response.Raw)
					}
					if responseDelay > 0 {
						time.Sleep(responseDelay)
					}
					guard := g2SnapshotBarrier(t, p, query.ConnID)
					responseGate.Release()
					written := g2WaitWritten(t, p, response)
					next := g2WaitBarrier(t, guard)
					if scenario == "cache_wait" {
						time.Sleep(300 * time.Millisecond)
					}
					viewStart := resourceNS()
					view := g.view("lab-a/http")
					viewEnd := resourceNS()
					maxRemaining := max(int64(0), snapshot.RemainingMS-(viewStart-query.ReceivedNS)/int64(time.Millisecond))
					if view.ReadinessView == nil || view.ReadinessView.LastReported == nil || view.ReadinessView.LastReported.SampleSeq != *snapshot.SampleSeq || view.ReadinessView.RemainingMS > maxRemaining {
						t.Fatalf("query/response/cache delay extended TTL: got=%+v upper=%d", view.ReadinessView, maxRemaining)
					}
					fields := map[string]any{"scenario": scenario, "round": round, "query": query, "response": response, "response_written": written, "next_response_held": next, "query_delay_ms": queryDelay.Milliseconds(), "response_delay_ms": responseDelay.Milliseconds(), "view_request_started_ns": viewStart, "view_request_ended_ns": viewEnd, "remaining_upper_bound_ms": maxRemaining, "view": view}
					if scenario == "duplicate_reply" {
						current := g2DecodeSnapshot(t, next)
						duplicate := *snapshot
						duplicate.Header = current.Header
						duplicate.QueryID = current.QueryID
						duplicate.RemainingMS = min(snapshot.RemainingMS, current.RemainingMS)
						if duplicate.RemainingMS <= 0 {
							t.Fatal("duplicate fixture exhausted before injection")
						}
						lastGuard := g2SnapshotBarrier(t, p, query.ConnID)
						injected := g2InjectSnapshot(t, p, next, &duplicate)
						guard.Drop()
						third := g2WaitBarrier(t, lastGuard)
						duplicateStart := resourceNS()
						after := g.view("lab-a/http")
						remainingBound := max(int64(0), view.ReadinessView.RemainingMS-(duplicateStart-viewEnd)/int64(time.Millisecond))
						if after.ReadinessView == nil || after.ReadinessView.LastReported == nil || after.ReadinessView.LastReported.SampleSeq != *snapshot.SampleSeq || after.ReadinessView.RemainingMS > remainingBound {
							t.Fatalf("duplicate restarted cached TTL: %+v upper=%d", after.ReadinessView, remainingBound)
						}
						fields["injected_raw"] = injected
						fields["third_response_held"] = third
						fields["duplicate_view"] = after
						fields["duplicate_remaining_upper_bound_ms"] = remainingBound
						fields["duplicate_view_started_ns"] = duplicateStart
						lastGuard.Release()
					} else {
						guard.Release()
					}
					g.event("query_ttl_bound", fields)
				}
				g.settled()
				g2RecordWire(g)
				g.recordHealth("lab-a/http")
			})
		})
	}
}

// The wire timestamp is diagnostic metadata. These explicitly injected values
// test OIS's independence from a source clock skew; they do not claim to change
// the host OS clock. Source deadlines retain Go time.Time's monotonic component;
// controlled-clock expiry tests and this metadata test are narrower evidence.
func TestTVG2_04_ObservedAtIsDiagnostic(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-04", g2Options{proxy: true})
		p := g.paths["lab-a"]
		for round := range 20 {
			g2ReadySample(g, 1)
			var gate *g2Barrier
			var held g2WireEvent
			var sample *protocol.ObservationSnapshot
			// Select a genuinely new sample so changing its display timestamp is not a
			// same-sample conflict. Every skipped original response remains recorded.
			for {
				gate = g2SnapshotBarrier(t, p, 0)
				held = g2WaitBarrier(t, gate)
				sample = g2DecodeSnapshot(t, held)
				before := g.view("lab-a/http")
				if sample.SampleSeq != nil && (before.ReadinessView.LastReported == nil || *sample.SampleSeq > before.ReadinessView.LastReported.SampleSeq) {
					break
				}
				gate.Release()
			}
			originalObserved := *sample.ObservedAt
			delta := 365 * 24 * time.Hour
			if round%2 == 0 {
				delta = -delta
			}
			shifted := originalObserved.Add(delta)
			sample.ObservedAt = &shifted
			guard := g2SnapshotBarrier(t, p, held.ConnID)
			injected := g2InjectSnapshot(t, p, held, sample)
			gate.Drop()
			next := g2WaitBarrier(t, guard)
			start := resourceNS()
			view := g.view("lab-a/http")
			if view.ReadinessView == nil || view.ReadinessView.LastReported == nil || view.ReadinessView.LastReported.ObservedAt == nil || !view.ReadinessView.LastReported.ObservedAt.Equal(shifted) || view.ReadinessView.RemainingMS > sample.RemainingMS {
				t.Fatalf("metadata timestamp changed validity or was not observed: %+v", view.ReadinessView)
			}
			// Keep all later snapshots outside the OIS until this sample's bound has
			// elapsed. Expiry must use the monotonic deadline even for a future date.
			g.eventually(2*time.Second, "monotonic expiry despite observed_at skew", func() bool { return g.view("lab-a/http").Readiness == protocol.ReadinessUnknown })
			expired := g.view("lab-a/http")
			expiredAt := resourceNS()
			if expired.ReadinessView.RemainingMS != 0 || expired.ReadinessView.LastReported == nil || !expired.ReadinessView.LastReported.ObservedAt.Equal(shifted) {
				t.Fatal("expiry discarded history or retained effective sample")
			}
			// The held future reply is now late. Dropping it avoids turning our
			// deliberate timestamp mutation into an unrelated immutable-sample error.
			guard.Drop()
			g.event("observed_at_skew", map[string]any{"round": round, "scope": "source_timestamp_metadata_only", "host_clock_changed": false, "original_observed_at": originalObserved, "shifted_observed_at": shifted, "held": held, "injected_raw": injected, "later_response_held": next, "view": view, "view_started_ns": start, "expired": expired, "expired_ns": expiredAt})
		}
		g.settled()
		g2RecordWire(g)
		g.recordHealth("lab-a/http")
	})
}

func TestTVG2_04_LateQueryCannotRevive(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-04", g2Options{proxy: true})
		p := g.paths["lab-a"]
		for round := range 20 {
			g2ReadySample(g, 1)
			gate := g2SnapshotBarrier(t, p, 0)
			held := g2WaitBarrier(t, gate)
			beforeStart := resourceNS()
			before := g.view("lab-a/http")
			beforeEnd := resourceNS()
			// This is an explicit > QUERY_MS link delay. Its start and actual write
			// return remain raw evidence; the product deadline is never extended.
			guard := g2SnapshotBarrier(t, p, held.ConnID)
			time.Sleep(600 * time.Millisecond)
			gate.Release()
			written := g2WaitWritten(t, p, held)
			next := g2WaitBarrier(t, guard)
			afterStart := resourceNS()
			after := g.view("lab-a/http")
			upper := max(int64(0), before.ReadinessView.RemainingMS-(afterStart-beforeEnd)/int64(time.Millisecond))
			if before.ReadinessView.LastReported == nil || after.ReadinessView.LastReported == nil || after.ReadinessView.LastReported.SampleSeq != before.ReadinessView.LastReported.SampleSeq || after.ReadinessView.RemainingMS > upper {
				t.Fatalf("late query revived/updated snapshot: before=%+v after=%+v upper=%d", before.ReadinessView, after.ReadinessView, upper)
			}
			guard.Release()
			g.event("late_query_discarded", map[string]any{"round": round, "before": before, "before_request_started_ns": beforeStart, "before_request_ended_ns": beforeEnd, "late_response": held, "late_response_written": written, "next_response_held": next, "after": after, "after_request_started_ns": afterStart, "remaining_upper_bound_ms": upper})
		}
		g.settled()
		g2RecordWire(g)
		g.recordHealth("lab-a/http")
	})
}
