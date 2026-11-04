package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

var g2MetricsClients sync.Map

func g2MetricsClient(g *g2Env) *http.Client {
	if c, ok := g2MetricsClients.Load(g.env); ok {
		return c.(*http.Client)
	}
	tr := &http.Transport{Proxy: nil, MaxConnsPerHost: 1, MaxIdleConns: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: 2 * time.Second}
	c := &http.Client{Transport: tr, Timeout: time.Second}
	g2MetricsClients.Store(g.env, c)
	g.t.Cleanup(func() { tr.CloseIdleConnections(); g2MetricsClients.Delete(g.env) })
	return c
}
func g2Metric(g *g2Env, name string) uint64 {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := must(http.NewRequestWithContext(ctx, "GET", "http://"+g.oics["lab-a"].diag+"/metrics", nil))
	res, err := g2MetricsClient(g).Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || res.StatusCode != 200 || len(body) > 65536 {
		g.t.Fatalf("metrics: status=%d error=%v", res.StatusCode, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, name+" ") {
			n, err := strconv.ParseUint(strings.TrimPrefix(line, name+" "), 10, 64)
			if err != nil {
				g.t.Fatal(err)
			}
			return n
		}
	}
	g.t.Fatalf("missing bounded metric %s", name)
	return 0
}
func g2WaitMetric(g *g2Env, name string, previous uint64, budget time.Duration) int64 {
	g.t.Helper()
	deadline := time.Now().Add(budget)
	for {
		started := resourceNS()
		if n := g2Metric(g, name); n > previous {
			observed := resourceNS()
			g.event("local_metric_advanced", map[string]any{"metric": name, "before": previous, "value": n, "sample_started_ns": started, "sample_finished_ns": observed})
			return observed
		}
		if !time.Now().Before(deadline) {
			g.t.Fatalf("metric %s did not advance in %v", name, budget)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func g2HealthHeld(g *g2Env, status int) fixture.HealthRecord {
	g.t.Helper()
	h := g.health["lab-a/http"]
	s := h.Snapshot()
	var after uint64
	if len(s.Records) > 0 {
		after = s.Records[len(s.Records)-1].ID
	}
	version := h.Set(status, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		r, err := h.WaitStarted(ctx, after)
		if err != nil {
			g.t.Fatal(err)
		}
		if r.Version == version {
			return r
		}
		after = r.ID
	}
}
func g2FreshReady(g *g2Env) {
	g.t.Helper()
	before := g2Metric(g, `oi_observation_samples_total{result="ready"}`)
	g.health["lab-a/http"].Set(200, false)
	g2WaitMetric(g, `oi_observation_samples_total{result="ready"}`, before, time.Second)
	g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessReady })
}
func g2CommitOpen(g *g2Env, c *client.Client) protocol.OpenResponse {
	g.t.Helper()
	v := must(c.Lookup(bg, "lab-a/http"))
	return must(c.Open(bg, protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision, Admission: protocol.AdmissionRequireReady}))
}
func g2Leg(g *g2Env, c *client.Client, o protocol.OpenResponse) *websocket.Conn {
	g.t.Helper()
	leg, err := c.AttachLeg(bg, o)
	if err != nil {
		g.t.Fatal(err)
	}
	g.t.Cleanup(func() { leg.CloseNow() })
	return leg
}
func g2Frame(g *g2Env, leg *websocket.Conn) protocol.Frame {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	typ, raw, err := leg.Read(ctx)
	if err != nil {
		g.t.Fatal(err)
	}
	if typ != websocket.MessageBinary {
		g.t.Fatalf("unexpected text %q", raw)
	}
	return must(protocol.DecodeFrame(raw, 1<<20))
}
func g2NoPayload(g *g2Env, after int) []fixture.Record {
	g.t.Helper()
	rs := g.t1.Conns()[after:]
	for _, r := range rs {
		if len(r.Data) != 0 {
			g.t.Fatalf("zero-tolerance target received %q", r.Data)
		}
	}
	return rs
}
func g2CommitCleanup(g *g2Env, o protocol.OpenResponse, after int, zero bool, origin int64) protocol.ChannelView {
	g.t.Helper()
	g.cleanupStartNS = origin
	var v protocol.ChannelView
	var lastS protocol.OISStats
	var lastA protocol.OICStats
	deadline := origin + int64(2*time.Second)
	targetClosedNS := int64(0)
	for {
		var err error
		v, err = g.consumer("alice").Channel(bg, o.ChannelID)
		if err != nil {
			g.t.Fatal(err)
		}
		s := g.oisStats()
		a := g.oicStats("lab-a")
		lastS, lastA = s, a
		done := true
		for _, r := range g.t1.Conns()[after:] {
			done = done && r.Done
		}
		if done && targetClosedNS == 0 {
			targetClosedNS = resourceNS()
		}
		if targetClosedNS == 0 && resourceNS() > origin+int64(tClose) {
			g.t.Fatal("target socket exceeded original T_close")
		}
		if targetClosedNS > origin+int64(tClose) {
			g.t.Fatal("target closure first observed after original T_close")
		}
		if v.State == protocol.StateClosed && s.Channels.Preparing+s.Channels.Open+s.Channels.Closing == 0 && s.Tickets == 0 && s.Observation != nil && s.Observation.PendingChecks == 0 && s.Observation.PendingOpens == 0 && s.Observation.HTTPWaiters == 0 && s.Observation.PendingQueries == 0 && a.Observation != nil && a.Observation.PendingChecks == 0 && a.Observation.PendingQueries == 0 && a.Channels == 0 && done {
			break
		}
		if resourceNS() > deadline {
			g.t.Fatalf("cleanup past original 2s origin: channel=%+v ois=%+v oic=%+v", v, s, a)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if resourceNS() > deadline {
		g.t.Fatal("cleanup observation exceeded deadline")
	}
	if zero {
		g2NoPayload(g, after)
	}
	g.event("commit_cleanup", map[string]any{"channel_id": o.ChannelID, "origin_ns": origin, "target_closed_observed_ns": targetClosedNS, "finished_ns": resourceNS(), "target": g.t1.Conns()[after:], "ois": lastS, "oic": lastA})
	return v
}
func g2CompleteLeg(g *g2Env, leg *websocket.Conn, payload string) {
	g.t.Helper()
	sendFrame(g.t, leg, protocol.DataFrame([]byte(payload)))
	sendFrame(g.t, leg, protocol.FinFrame())
	var got []byte
	for {
		f := g2Frame(g, leg)
		if f.Type == protocol.FrameFin {
			break
		}
		if f.Type != protocol.FrameData {
			g.t.Fatalf("business interrupted: %+v", f)
		}
		got = append(got, f.Data...)
	}
	if string(got) != "T1:"+payload {
		g.t.Fatalf("target identity/content %q", got)
	}
	leg.CloseNow()
}
func g2FinalMatch(message string) g2Match {
	direction := g2OICToOIS
	if message == protocol.MsgAdmissionCheck {
		direction = g2OISToOIC
	}
	return g2Match{Role: g2RoleControl, Direction: direction, Type: message, Stage: protocol.CheckFinal}
}
func g2Result(g *g2Env, e g2WireEvent) protocol.AdmissionResult {
	g.t.Helper()
	var r protocol.AdmissionResult
	if err := json.Unmarshal(e.Raw, &r); err != nil {
		g.t.Fatal(err)
	}
	return r
}

// 真实进程中的两种顺序：在第二条腿到齐前归约 NotReady，以及 final
// check 已发出但尚未抵达 OIC 时归约 NotReady。原方向流不做重排。
func TestTVG2_05_FinalRecheck(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"before_second_leg_not_ready", "after_both_legs_not_ready", "ready_expired_before_second_leg"} {
			t.Run(scenario, func(t *testing.T) {
				g := newG2Env(t, be, "G2-05", g2Options{proxy: true})
				p := g.paths["lab-a"]
				c := g.consumer("alice")
				for round := range 20 {
					g2FreshReady(g)
					after := len(g.t1.Conns())
					mark := g2Mark(p)
					expired := scenario == "ready_expired_before_second_leg"
					metric := `oi_observation_samples_total{result="not_ready"}`
					if expired {
						metric = "oi_observation_sample_expired_total"
					}
					count := g2Metric(g, metric)
					var health fixture.HealthRecord
					if !expired {
						health = g2HealthHeld(g, 503)
					} else {
						g.health["lab-a/http"].Set(200, true)
					}
					o := g2CommitOpen(g, c)
					g.eventually(time.Second, "provider target connection before mutation", func() bool { return len(g.t1.Conns()) > after })
					var leg *websocket.Conn
					var b *g2Barrier
					var held g2WireEvent
					if scenario == "after_both_legs_not_ready" {
						b = g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionCheck))
						leg = g2Leg(g, c, o)
						held = g2WaitBarrier(t, b)
					}
					if !expired {
						g.health["lab-a/http"].Release()
					}
					applied := g2WaitMetric(g, metric, count, 1800*time.Millisecond)
					if b != nil {
						b.Release()
					} else {
						leg = g2Leg(g, c, o)
					}
					f := g2Frame(g, leg)
					want := protocol.CodeTargetNotReady
					if expired {
						want = protocol.CodeReadinessRequired
					}
					if f.Type != protocol.FrameAbort || f.Reason != want {
						t.Fatalf("final check %s: %+v", scenario, f)
					}
					origin := resourceNS()
					leg.CloseNow()
					cv := g2CommitCleanup(g, o, after, true, origin)
					if cv.CloseReason != want {
						t.Fatalf("GET changed first reason: %+v", cv)
					}
					final := g2WaitWire(t, p, mark, g2FinalMatch(protocol.MsgAdmissionResult))
					r := g2Result(g, final)
					if r.Code != want {
						t.Fatalf("actual final result: %+v", r)
					}
					for _, e := range p.Snapshot().Events {
						if e.ChannelID == o.ChannelID && e.Type == "READY" {
							t.Fatal("rejected final emitted READY")
						}
					}
					g.event("final_recheck_round", map[string]any{"scenario": scenario, "round": round, "health_request": health, "local_reduction_observed_ns": applied, "held_check": held, "final": final, "consumer_abort": f, "channel": cv, "target": g2NoPayload(g, after), "cleanup_origin_ns": origin, "cleanup_finished_ns": resourceNS()})
				}
				g2CommitHistory(g)
			})
		}
	})
}

func TestTVG2_06_CommitSnapshotWindow(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-06", g2Options{proxy: true})
		p := g.paths["lab-a"]
		c := g.consumer("alice")
		for round := range 20 {
			g2FreshReady(g)
			after := len(g.t1.Conns())
			n := g2Metric(g, `oi_observation_samples_total{result="not_ready"}`)
			health := g2HealthHeld(g, 503)
			b := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
			o := g2CommitOpen(g, c)
			leg := g2Leg(g, c, o)
			held := g2WaitBarrier(t, b)
			r := g2Result(g, held)
			if r.Code != "" || r.Readiness != protocol.ReadinessReady || r.DecisionID == "" {
				t.Fatalf("not a frozen Ready decision: %+v", r)
			}
			g.health["lab-a/http"].Release()
			nr := g2WaitMetric(g, `oi_observation_samples_total{result="not_ready"}`, n, 300*time.Millisecond)
			released := resourceNS()
			b.Release()
			written := g2WaitWritten(t, p, held)
			f := g2Frame(g, leg)
			if f.Type != protocol.FrameReady {
				t.Fatalf("Commit-Snapshot revoked by later sample: %+v", f)
			}
			readyObserved := resourceNS()
			g2CompleteLeg(g, leg, fmt.Sprintf("g2-snapshot-%d", round))
			origin := resourceNS()
			cv := g2CommitCleanup(g, o, after, false, origin)
			if cv.CloseReason != protocol.CodeCompleted || cv.ReadinessDecision == nil || cv.ReadinessDecision.Readiness != protocol.ReadinessReady {
				t.Fatalf("fixed decision changed: %+v", cv)
			}
			g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessNotReady })
			if _, err := c.OpenLatest(bg, "lab-a/http", "", false); protocol.CodeOf(err) != protocol.CodeTargetNotReady {
				t.Fatalf("new Open ignored new NotReady: %v", err)
			}
			check := g2WaitWire(t, p, 0, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgAdmissionCheck, CheckID: held.CheckID})
			check = g2WaitWritten(t, p, check)
			providerReady := g2WaitWire(t, p, held.ID, g2Match{Role: g2RoleProvider, Direction: g2OISToOIC, Type: "READY", ChannelID: o.ChannelID})
			// 跨进程无共享 t_check 钟；真实线性化点只能以同一代理时钟的因果
			// 区间表示。Write 返回可能晚于对端处理，因此下界取 WriteStarted。
			if check.WriteStartedNS <= 0 || check.WriteStartedNS > held.ReceivedNS || held.ReceivedNS >= nr || nr > released || written.WriteStartedNS > providerReady.ReceivedNS {
				t.Fatal("invalid causal timing bounds")
			}
			g.event("commit_snapshot_round", map[string]any{"round": round, "health_request": health, "t_check_bounds_ns": []int64{check.WriteStartedNS, held.ReceivedNS}, "ois_commit_bounds_ns": []int64{written.WriteStartedNS, providerReady.ReceivedNS}, "final_generated_before_proxy_receive_ns": held.ReceivedNS, "final": held, "not_ready_reduction_observed_ns": nr, "release_ns": released, "final_written": written, "consumer_ready_observed_ns": readyObserved, "channel": cv, "target": g.t1.Conns()[after:]})
		}
		g2CommitHistory(g)
	})
}

// 先以真实目标接收 DATA 证明 OIC 已过 READY gate；再更改独立 probe 合同。
func TestTVG2_08c_RunningProbeReload(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-08c", g2Options{proxy: true})
		p := g.paths["lab-a"]
		c := g.consumer("alice")
		for round := range 20 {
			g2FreshReady(g)
			before := g.view("lab-a/http")
			after := len(g.t1.Conns())
			mark := g2Mark(p)
			o := g2CommitOpen(g, c)
			leg := g2Leg(g, c, o)
			if f := g2Frame(g, leg); f.Type != protocol.FrameReady {
				t.Fatal(f)
			}
			payload := fmt.Sprintf("g2-running-%d", round)
			sendFrame(t, leg, protocol.DataFrame([]byte(payload)))
			g.eventually(time.Second, "actual target data proves Running", func() bool { rs := g.t1.Conns(); return len(rs) > after && string(rs[after].Data) == payload })
			running := resourceNS()
			exps := g.oics["lab-a"].cfg["exposures"].([]any)
			for _, raw := range exps {
				m := raw.(map[string]any)
				if m["name"] == "http" {
					probe := m["probe"].(map[string]any)
					probe["ready_statuses"] = []int{200, 204, 210 + round}
				}
			}
			g.rewriteOIC("lab-a", exps)
			afterView := g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
				return v.ReadinessView != nil && before.ReadinessView != nil && v.ReadinessView.ObservationRevision != before.ReadinessView.ObservationRevision && v.Readiness == protocol.ReadinessReady
			})
			if afterView.Revision != before.Revision {
				t.Fatal("probe-only reload changed target revision")
			}
			sendFrame(t, leg, protocol.FinFrame())
			var got []byte
			for {
				f := g2Frame(g, leg)
				if f.Type == protocol.FrameFin {
					break
				}
				if f.Type != protocol.FrameData {
					t.Fatalf("Running channel interrupted by probe-only reload: %+v", f)
				}
				got = append(got, f.Data...)
			}
			if string(got) != "T1:"+payload {
				t.Fatalf("payload %q", got)
			}
			origin := resourceNS()
			leg.CloseNow()
			cv := g2CommitCleanup(g, o, after, false, origin)
			if cv.CloseReason != protocol.CodeCompleted {
				t.Fatal(cv)
			}
			newOpen := g2CommitOpen(g, c)
			newLeg := g2Leg(g, c, newOpen)
			if f := g2Frame(g, newLeg); f.Type != protocol.FrameReady {
				t.Fatal(f)
			}
			g2CompleteLeg(g, newLeg, "new-observation")
			final := g2WaitWire(t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckFinal, ChannelID: newOpen.ChannelID})
			if r := g2Result(g, final); r.Binding.ObservationRevision != afterView.ReadinessView.ObservationRevision {
				t.Fatalf("new channel used old binding: %+v", r)
			}
			g2CommitCleanup(g, newOpen, after, false, resourceNS())
			g.event("running_probe_reload_round", map[string]any{"round": round, "before": before, "after": afterView, "running_proved_ns": running, "old_channel": cv, "new_final": final, "target": g.t1.Conns()[after:]})
		}
		g2CommitHistory(g)
	})
}

// 注入重用已结束 check 的 positive 时，只借下一条真实消息的 envelope
// 槽位构造显式负例；原消息 Drop，注入与原流永不混记，也不重编号原字节。
func g2InjectOldPositive(g *g2Env, p *g2Proxy, old g2WireEvent) g2WireEvent {
	b := g2Arm(g.t, p, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgObservationSnapshot, ConnID: old.ConnID})
	slot := g2WaitBarrier(g.t, b)
	var h protocol.Header
	if err := json.Unmarshal(slot.Raw, &h); err != nil {
		g.t.Fatal(err)
	}
	r := g2Result(g, old)
	r.Seq = h.Seq
	raw := must(json.Marshal(r))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Inject(ctx, slot.ConnID, g2OICToOIS, websocket.MessageText, raw); err != nil {
		g.t.Fatal(err)
	}
	b.Drop()
	return slot
}
func TestTVG2_08a_MessageAndCleanup(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"final_timeout_late", "final_response_drop", "duplicate_and_closed_positive", "pending_slots_full", "cli_preflight_response_drop"} {
			t.Run(scenario, func(t *testing.T) {
				g := newG2Env(t, be, "G2-08a", g2Options{proxy: true})
				p := g.paths["lab-a"]
				c := g.consumer("alice")
				for round := range 20 {
					g2FreshReady(g)
					after := len(g.t1.Conns())
					if scenario == "pending_slots_full" {
						v := g.view("lab-a/http")
						b := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckPreflight})
						outs := []<-chan g2HTTPOutcome{g2AsyncOpen(c, context.Background(), protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision})}
						held := g2WaitBarrier(t, b)
						for range 15 {
							outs = append(outs, g2AsyncOpen(c, context.Background(), protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision}))
						}
						full := g2WaitSlots(g, 16, 16)
						if full.Observation.PendingChecks != 16 || full.Channels.Preparing+full.Channels.Open != 0 || full.Tickets != 0 {
							t.Fatalf("pending checks allocated channel/quota: %+v", full)
						}
						over := g2AwaitOpen(t, g2AsyncOpen(c, context.Background(), protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision}))
						if over.Status != 429 || over.Code != protocol.CodeResourceExhausted {
							t.Fatalf("slot full: %+v", over)
						}
						results := []g2HTTPOutcome{}
						for _, d := range outs {
							out := g2AwaitOpen(t, d)
							if out.Status != 503 || out.Code != protocol.CodeReadinessCheckFailed {
								t.Fatalf("timeout outcome: %+v", out)
							}
							results = append(results, out)
						}
						origin := resourceNS()
						b.Drop()
						zero := g2WaitSlots(g, 0, 0)
						if resourceNS()-origin > int64(2*time.Second) || zero.Observation.PendingChecks != 0 || len(g.t1.Conns()) != after {
							t.Fatal("pending cleanup leaked target/slot")
						}
						g.event("pending_capacity_round", map[string]any{"round": round, "held": held, "full": full, "overflow": over, "outcomes": results, "after": zero, "cleanup_origin_ns": origin, "cleanup_finished_ns": resourceNS()})
						continue
					}
					if scenario == "cli_preflight_response_drop" {
						b := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckPreflight})
						type output struct {
							stdout, stderr string
							code           int
						}
						done := make(chan output, 1)
						token := g.token("alice")
						go func() {
							out, code, errout := runIn(oiBin, []string{"OI_TOKEN=" + token}, "must-not-reach-target", "--ois", "https://"+g.api, "--ca", g.caFile, "connect", "lab-a/http", "--stdio")
							done <- output{out, errout, code}
						}()
						held := g2WaitBarrier(t, b)
						b.Drop()
						var out output
						select {
						case out = <-done:
						case <-time.After(3 * time.Second):
							t.Fatal("CLI did not terminate")
						}
						if out.code == 0 || out.stdout != "" || !strings.Contains(out.stderr, string(protocol.CodeReadinessCheckFailed)) {
							t.Fatalf("CLI response loss: %+v", out)
						}
						zero := g2WaitSlots(g, 0, 0)
						if zero.Observation.PendingChecks != 0 || len(g.t1.Conns()) != after {
							t.Fatal("preflight response loss created target")
						}
						g.event("cli_response_drop_round", map[string]any{"round": round, "held": held, "exit_code": out.code, "stdout": out.stdout, "stderr": out.stderr, "after": zero})
						continue
					}
					b := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
					o := g2CommitOpen(g, c)
					var abortGate *g2Barrier
					if scenario != "duplicate_and_closed_positive" {
						abortGate = g2Arm(t, p, g2Match{Role: g2RoleProvider, Direction: g2OICToOIS, Type: "ABORT", ChannelID: o.ChannelID})
					}
					leg := g2Leg(g, c, o)
					held := g2WaitBarrier(t, b)
					if r := g2Result(g, held); r.Code != "" || r.DecisionID == "" {
						t.Fatalf("expected positive final: %+v", r)
					}
					if scenario == "duplicate_and_closed_positive" {
						b.Release()
						if f := g2Frame(g, leg); f.Type != protocol.FrameReady {
							t.Fatal(f)
						}
						duplicateSlot := g2InjectOldPositive(g, p, held)
						g2CompleteLeg(g, leg, fmt.Sprintf("duplicate-%d", round))
						origin := resourceNS()
						cv := g2CommitCleanup(g, o, after, false, origin)
						if cv.CloseReason != protocol.CodeCompleted {
							t.Fatal(cv)
						}
						lateSlot := g2InjectOldPositive(g, p, held)
						latest := must(c.Channel(bg, o.ChannelID))
						if latest.State != protocol.StateClosed || latest.CloseReason != cv.CloseReason {
							t.Fatal("late positive revived/changed closed channel", latest)
						}
						ready := 0
						for _, e := range p.Snapshot().Events {
							if e.ChannelID == o.ChannelID && e.Type == "READY" && !e.Injected {
								ready++
							}
						}
						if ready != 1 {
							t.Fatalf("provider READY count=%d", ready)
						}
						nextOpen := g2CommitOpen(g, c)
						nextLeg := g2Leg(g, c, nextOpen)
						if nextFrame := g2Frame(g, nextLeg); nextFrame.Type != protocol.FrameReady {
							t.Fatal("late response affected next operation", nextFrame)
						}
						g2CompleteLeg(g, nextLeg, "after-late-positive")
						nextChannel := g2CommitCleanup(g, nextOpen, after, false, resourceNS())
						current := g.oicStats("lab-a")
						if !current.Registered || !current.Connected || current.Generation != g2Result(g, held).Binding.Generation || nextOpen.ChannelID == o.ChannelID || nextChannel.CloseReason != protocol.CodeCompleted {
							t.Fatalf("late response disturbed session/new operation: %+v %+v", current, nextChannel)
						}
						g.event("duplicate_closed_positive_round", map[string]any{"round": round, "final": held, "duplicate_slot": duplicateSlot, "late_slot": lateSlot, "channel": latest, "provider_ready_count": ready, "next_channel": nextChannel, "oic_after": current})
						continue
					}
					if scenario == "final_response_drop" {
						b.Drop()
					}
					f := g2Frame(g, leg)
					if f.Type != protocol.FrameAbort || f.Reason != protocol.CodeReadinessCheckFailed {
						t.Fatalf("fixed check deadline not enforced: %+v", f)
					}
					abortGate.Release()
					origin := resourceNS()
					leg.CloseNow()
					// 终态 ABORT 已观察；在同一原始清理窗口释放迟到控制消息。
					// 持有整条控制方向时，正常周期 query 不能回到在线基线。
					if scenario == "final_timeout_late" {
						b.Release()
						g2WaitWritten(t, p, held)
					}
					cv := g2CommitCleanup(g, o, after, true, origin)
					if cv.CloseReason != protocol.CodeReadinessCheckFailed || cv.ReadinessDecision == nil || cv.ReadinessDecision.Reason != "timeout" {
						t.Fatalf("GET must preserve known final-check timeout: %+v", cv)
					}
					latest := must(c.Channel(bg, o.ChannelID))
					if latest.State != protocol.StateClosed || latest.CloseReason != cv.CloseReason {
						t.Fatal("late final revived Channel", latest)
					}
					for _, e := range p.Snapshot().Events {
						if e.ChannelID == o.ChannelID && e.Type == "READY" {
							t.Fatal("late/lost final emitted READY")
						}
					}
					g.event("final_loss_round", map[string]any{"scenario": scenario, "round": round, "final": held, "consumer_abort": f, "channel": latest, "target": g2NoPayload(g, after), "cleanup_origin_ns": origin, "cleanup_finished_ns": resourceNS()})
				}
				g2CommitHistory(g)
			})
		}
	})
}

// Windows 无 SIGHUP。这里只把已观察 register 的周期作为装配输入，
// 成功必须另由真实 prepare_failed/revision 事件及未超期原窗口证明。
func g2BeforeReloadWindow(g *g2Env, round int) g2WireEvent {
	if runtime.GOOS != "windows" {
		return g2WireEvent{}
	}
	p := g.paths["lab-a"]
	mark := g2Mark(p)
	exps := g.oics["lab-a"].cfg["exposures"].([]any)
	for _, raw := range exps {
		m := raw.(map[string]any)
		if m["name"] == "http" {
			m["description"] = fmt.Sprintf("watch-phase-%d", round)
		}
	}
	g.rewriteOIC("lab-a", exps)
	actual := g2WaitWire(g.t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgRegister})
	g2WaitWritten(g.t, p, actual)
	wait := time.Duration(actual.ReceivedNS + int64(1800*time.Millisecond) - resourceNS())
	if wait > 0 {
		timer := time.NewTimer(wait)
		<-timer.C
	}
	return actual
}
func g2RequestReload(g *g2Env, exps []any) {
	g.rewriteOIC("lab-a", exps)
	if runtime.GOOS != "windows" {
		if err := g.oics["lab-a"].cmd.Process.Signal(syscall.SIGHUP); err != nil {
			g.t.Fatal(err)
		}
	}
}
func g2ChangeProbe(g *g2Env, round int) {
	exps := g.oics["lab-a"].cfg["exposures"].([]any)
	for _, raw := range exps {
		m := raw.(map[string]any)
		if m["name"] == "http" {
			m["probe"].(map[string]any)["ready_statuses"] = []int{200, 204, 210 + round}
		}
	}
	g2RequestReload(g, exps)
}
func TestTVG2_08b_ObservationRevisionCancelsBeforeReady(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-08b", g2Options{proxy: true})
		p := g.paths["lab-a"]
		c := g.consumer("alice")
		for round := range 20 {
			g2FreshReady(g)
			phase := g2BeforeReloadWindow(g, round)
			before := g.view("lab-a/http")
			after := len(g.t1.Conns())
			mark := g2Mark(p)
			b := g2Arm(t, p, g2Match{Role: g2RoleProvider, Direction: g2OISToOIC, Type: "READY"})
			abortGate := g2Arm(t, p, g2Match{Role: g2RoleProvider, Direction: g2OICToOIS, Type: "ABORT"})
			o := g2CommitOpen(g, c)
			leg := g2Leg(g, c, o)
			held := g2WaitBarrier(t, b)
			final := g2WaitWire(t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgAdmissionResult, Stage: protocol.CheckFinal, ChannelID: o.ChannelID})
			if r := g2Result(g, final); r.Code != "" || r.DecisionID == "" {
				t.Fatalf("no successful final before READY hold: %+v", r)
			}
			g2ChangeProbe(g, round)
			heldAbort := g2WaitBarrier(t, abortGate)
			failed := g2WaitWire(t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgPrepareFailed, ChannelID: o.ChannelID})
			var m protocol.PrepareFailed
			if err := json.Unmarshal(failed.Raw, &m); err != nil {
				t.Fatal(err)
			}
			if m.Code != protocol.CodeReadinessCheckFailed || m.Reason != "observation_changed" || failed.ReceivedNS <= held.ReceivedNS || failed.ReceivedNS-final.ReceivedNS >= int64(500*time.Millisecond) {
				t.Fatalf("reload did not win original check window: %+v final=%d failed=%d", m, final.ReceivedNS, failed.ReceivedNS)
			}
			g2WaitWritten(t, p, failed)
			g.eventually(200*time.Millisecond, "real prepare_failed applied before provider ABORT", func() bool {
				v := must(c.Channel(bg, o.ChannelID))
				return v.CloseReason == protocol.CodeReadinessCheckFailed && v.ReadinessDecision != nil && v.ReadinessDecision.Reason == "observation_changed"
			})
			b.Release()
			abortGate.Release()
			ready := false
			var abort protocol.Frame
			for {
				f := g2Frame(g, leg)
				if f.Type == protocol.FrameReady {
					if ready {
						t.Fatal("second READY")
					}
					ready = true
					continue
				}
				if f.Type != protocol.FrameAbort {
					t.Fatalf("payload/FIN before cancelled READY gate: %+v", f)
				}
				abort = f
				break
			}
			if abort.Reason != protocol.CodeReadinessCheckFailed {
				t.Fatalf("first cancellation reason changed: %+v", abort)
			}
			origin := resourceNS()
			leg.CloseNow()
			cv := g2CommitCleanup(g, o, after, true, origin)
			if cv.CloseReason != protocol.CodeReadinessCheckFailed || cv.ReadinessDecision == nil || cv.ReadinessDecision.Reason != "observation_changed" {
				t.Fatalf("first precise reason changed: %+v", cv)
			}
			next := g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
				return v.ReadinessView != nil && v.ReadinessView.ObservationRevision != before.ReadinessView.ObservationRevision && v.Readiness == protocol.ReadinessReady
			})
			if next.Revision != before.Revision {
				t.Fatal("probe-only update changed target revision")
			}
			g.event("observation_cancel_before_ready_round", map[string]any{"round": round, "watch_phase_input": phase, "before": before, "after": next, "final": final, "held_ready": held, "held_provider_abort": heldAbort, "actual_prepare_failed": failed, "consumer_saw_ready": ready, "consumer_abort": abort, "channel": cv, "target": g2NoPayload(g, after), "cleanup_origin_ns": origin, "cleanup_finished_ns": resourceNS()})
		}
		g2CommitHistory(g)
	})
}

func TestTVG2_07_FinalSecurityRecheck(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"credential_revoked", "policy_revoked", "target_revision", "generation_replaced", "license_expired"} {
			t.Run(scenario, func(t *testing.T) {
				g := newG2Env(t, be, "G2-07", g2Options{proxy: true})
				p := g.paths["lab-a"]
				for round := range 20 {
					g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessReady })
					c := g.consumer("alice")
					var token protocol.TokenCreated
					var phase g2WireEvent
					var beats []g2WireEvent
					var license protocol.OICStats
					if scenario == "credential_revoked" {
						token = g.newToken("alice", 0)
						c = g.clientFor(token.Token)
					}
					if scenario == "target_revision" {
						phase = g2BeforeReloadWindow(g, round)
					}
					if scenario == "license_expired" {
						for range 2 {
							b := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgHeartbeat})
							beat := g2WaitBarrier(t, b)
							b.Drop()
							beats = append(beats, beat)
						}
						g.eventually(time.Second, "actual license remaining <=200ms", func() bool { license = g.oicStats("lab-a"); return license.LicenseRemainingMS <= 200 })
						if license.LicenseRemainingMS <= 50 {
							t.Fatalf("insufficient license gate input: %+v", license)
						}
					}
					before := g.view("lab-a/http")
					after := len(g.t1.Conns())
					mark := g2Mark(p)
					b := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
					o := g2CommitOpen(g, c)
					leg := g2Leg(g, c, o)
					held := g2WaitBarrier(t, b)
					if r := g2Result(g, held); r.Code != "" || r.DecisionID == "" {
						t.Fatalf("security gate lacks positive decision: %+v", r)
					}
					applied := resourceNS()
					localBefore := g.oicStats("lab-a")
					var adminAction map[string]any
					adminWrite := func(method, resource, path string, body any) {
						version := g.version()
						applied = resourceNS()
						status, err := g.admin.Do(bg, method, path, version, body, nil)
						completed := resourceNS()
						if err != nil {
							t.Fatal(err)
						}
						adminAction = map[string]any{"method": method, "resource": resource, "status": status, "started_ns": applied, "completed_ns": completed}
					}
					want := protocol.CodeRevoked
					switch scenario {
					case "credential_revoked":
						adminWrite("DELETE", "token", "/v1/admin/tokens/"+token.TokenID, nil)
					case "policy_revoked":
						adminWrite("PATCH", "policy", "/v1/admin/policies/pa", protocol.PolicySpec{Actions: []string{protocol.ActionDiscover}})
					case "target_revision":
						want = protocol.CodeRevisionChanged
						exps := g.oics["lab-a"].cfg["exposures"].([]any)
						for _, raw := range exps {
							m := raw.(map[string]any)
							if m["name"] == "http" {
								tg := m["target"].(map[string]any)
								if round%2 == 0 {
									tg["allowed_cidrs"] = []string{"127.0.0.0/8"}
								} else {
									delete(tg, "allowed_cidrs")
								}
							}
						}
						g2RequestReload(g, exps)
						// register is behind the held final on the same ordered control lane.
						// The independent provider ABORT is the real cancellation path here.
						g.eventually(400*time.Millisecond, "actual local target revision cancellation", func() bool { return g.oicStats("lab-a").Closed[string(want)] > localBefore.Closed[string(want)] })
					case "generation_replaced":
						want = protocol.CodeChannelInterrupted
						old := g.oics["lab-a"].proc
						replacement := startProc(t, g.dir, fmt.Sprintf("oic-replace-%d", round), oicBin, nil, "run", "--config", g.oics["lab-a"].path)
						welcome := g2WaitWire(t, p, mark, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgWelcome})
						var w protocol.Welcome
						if err := json.Unmarshal(welcome.Raw, &w); err != nil {
							t.Fatal(err)
						}
						if w.Generation <= g2Result(g, held).Binding.Generation {
							t.Fatal("replacement did not raise generation")
						}
						old.kill()
						g.oics["lab-a"].proc = replacement
						line, _ := replacement.waitLog(t, 5*time.Second, "diagnostics listening", 0)
						g.oics["lab-a"].diag = line["addr"].(string)
					case "license_expired":
						want = protocol.CodeLicenseExpired
						g.eventually(400*time.Millisecond, "actual local lease expired", func() bool { s := g.oicStats("lab-a"); return s.Closed[string(want)] > license.Closed[string(want)] })
					}
					if scenario != "generation_replaced" {
						b.Release()
					}
					f := g2Frame(g, leg)
					if f.Type != protocol.FrameAbort || f.Reason != want {
						t.Fatalf("security change allowed/changed outcome: %+v want %s", f, want)
					}
					origin := resourceNS()
					leg.CloseNow()
					cv := g2CommitCleanup(g, o, after, true, origin)
					if cv.CloseReason != want {
						t.Fatalf("GET first reason changed: %+v", cv)
					}
					for _, e := range p.Snapshot().Events {
						if e.ChannelID == o.ChannelID && e.Type == "READY" {
							t.Fatal("security-invalid final emitted READY")
						}
					}
					g.event("security_final_round", map[string]any{"scenario": scenario, "round": round, "watch_phase_input": phase, "before": before, "final": held, "security_action_started_ns": applied, "admin_action": adminAction, "dropped_heartbeats": beats, "license_before": license, "consumer_abort": f, "channel": cv, "target": g2NoPayload(g, after), "cleanup_origin_ns": origin, "cleanup_finished_ns": resourceNS()})
					if scenario == "policy_revoked" {
						g.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Actions: []string{protocol.ActionDiscover, protocol.ActionOpen}})
					}
					if scenario == "generation_replaced" || scenario == "license_expired" {
						if scenario == "license_expired" {
							g.eventually(3*time.Second, "actual renewed license before next fault round", func() bool {
								s := g.oicStats("lab-a")
								// B2 allows a new heartbeat to renew the same session after its old
								// channels were ended. A generation change is not required here.
								return s.Registered && s.Connected && s.LicenseRemainingMS >= 2000
							})
							restored := g.oicStats("lab-a")
							old := must(g.consumer("alice").Channel(bg, o.ChannelID))
							if old.State != protocol.StateClosed || old.CloseReason != protocol.CodeLicenseExpired {
								t.Fatal("renewal revived old channel", old)
							}
							g.event("license_restored_between_rounds", map[string]any{"round": round, "restored": restored, "old_channel": old})
						}
						g.waitRegistered("lab-a")
						g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessReady })
					}
				}
				g2CommitHistory(g)
			})
		}
	})
}

// 保留原 1500ms TTL / 500ms check：在有效样本尾部发起，实际样本先到期。
// 这与只等 check timeout 不同，不能用后者替代 TTL 拒绝的证据。
func TestTVG2_06_ExpiredFrozenReady(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		g := newG2Env(t, be, "G2-06", g2Options{proxy: true})
		p := g.paths["lab-a"]
		c := g.consumer("alice")
		for round := range 20 {
			g2FreshReady(g)
			count := g2Metric(g, "oi_observation_sample_expired_total")
			g.health["lab-a/http"].Set(200, true)
			var near protocol.ExposureView
			g.eventually(1600*time.Millisecond, "actual Ready remaining 120..220ms", func() bool {
				near = g.view("lab-a/http")
				return near.Readiness == protocol.ReadinessReady && near.ReadinessView != nil && near.ReadinessView.RemainingMS >= 120 && near.ReadinessView.RemainingMS <= 220
			})
			after := len(g.t1.Conns())
			b := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
			o := g2CommitOpen(g, c)
			abortGate := g2Arm(t, p, g2Match{Role: g2RoleProvider, Direction: g2OICToOIS, Type: "ABORT", ChannelID: o.ChannelID})
			leg := g2Leg(g, c, o)
			held := g2WaitBarrier(t, b)
			r := g2Result(g, held)
			if r.Code != "" || r.Readiness != protocol.ReadinessReady || r.RemainingMS <= 0 || r.RemainingMS > 350 {
				t.Fatalf("not a short remaining frozen Ready: %+v", r)
			}
			expiry := g2WaitMetric(g, "oi_observation_sample_expired_total", count, 400*time.Millisecond)
			b.Release()
			f := g2Frame(g, leg)
			if f.Type != protocol.FrameAbort || f.Reason != protocol.CodeReadinessRequired {
				t.Fatalf("expired frozen Ready accepted/misclassified: %+v", f)
			}
			origin := resourceNS()
			abortGate.Release()
			leg.CloseNow()
			cv := g2CommitCleanup(g, o, after, true, origin)
			if cv.CloseReason != protocol.CodeReadinessRequired || cv.ReadinessDecision == nil || cv.ReadinessDecision.Reason != "expired" {
				t.Fatalf("OIS must retain its exact conservative TTL rejection: %+v", cv)
			}
			for _, e := range p.Snapshot().Events {
				if e.ChannelID == o.ChannelID && e.Type == "READY" {
					t.Fatal("expired Ready emitted READY")
				}
			}
			g.event("frozen_ready_expiry_round", map[string]any{"round": round, "near_expiry_directory": near, "final": held, "sample_expiry_observed_ns": expiry, "consumer_abort": f, "channel": cv, "target": g2NoPayload(g, after)})
		}
		g2CommitHistory(g)
	})
}

func g2CommitHistory(g *g2Env) {
	g.settled()
	g2RecordWire(g)
	for id := range g.health {
		g.recordHealth(id)
	}
}
