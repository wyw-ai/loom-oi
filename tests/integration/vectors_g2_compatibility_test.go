package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

func g2LegacyEnv(t *testing.T, be, ois, oic string, proxy bool) *g2Env {
	g := &g2Env{vector: "G2-13", health: map[string]*fixture.Health{}, paths: map[string]*g2Proxy{}, startedNS: resourceNS()}
	opts := envOpts{oisBinary: ois, oicBinary: oic, limits: map[string]any{"idle_ms": 600000}}
	if proxy {
		opts.beforeOIC = func(e *env) {
			g.env = e
			e.oicRoute = func(access string, exps []any) (string, []any) {
				p := newG2Proxy(t, "wss://"+e.accessAddr, e.ca, e.oics[access].leaf)
				g.paths[access] = p
				return p.URL(), exps
			}
		}
	}
	g.env = newEnv(t, be, opts)
	return g
}
func g2LegacyStrict(g *g2Env, bin string) {
	t := g.t
	c := g.consumer("alice")
	v := must(c.Lookup(bg, "lab-a/http"))
	var o protocol.OpenResponse
	status, err := c.Do(bg, "POST", "/v1/channels", "", protocol.OpenRequest{ExposureID: v.ExposureID, ExpectedRevision: v.Revision, Admission: protocol.AdmissionRequireReady}, &o)
	if status != 412 || protocol.CodeOf(err) != protocol.CodeReadinessRequired || o.ChannelID != "" {
		t.Fatalf("unknown strict must be 412: status=%d open=%+v err=%v", status, o, err)
	}
	old := g2LegacyCLI(bin, g.env, "lab-a/alt", "must-not-reach-strict")
	if old.TimedOut || old.ExitCode == 0 || old.Stdout != "" || !strings.Contains(old.Stderr, string(protocol.CodeReadinessRequired)) {
		t.Fatalf("CLI strict: %+v", old)
	}
	g.event("compatibility_unknown_strict", map[string]any{"status": status, "code": protocol.CodeOf(err), "cli": old})
}

func TestTVG2_13_Compatibility(t *testing.T) {
	legacy := g2BuildLegacy(t)
	for _, combination := range []string{"new_ois_old_oic_new_and_old_client", "old_ois_old_oic_new_client", "old_ois_new_oic_rejected", "new_new_old_cli_check_timeout", "new_new_old_cli_ready_expired"} {
		t.Run(combination, func(t *testing.T) {
			each(t, func(t *testing.T, be string) {
				switch combination {
				case "new_ois_old_oic_new_and_old_client", "old_ois_old_oic_new_client":
					ois := ""
					if combination == "old_ois_old_oic_new_client" {
						ois = legacy.OIS
					}
					g := g2LegacyEnv(t, be, ois, legacy.OIC, true)
					g2CompatEvent(g, legacy, combination)
					v := g.view("lab-a/http")
					if v.Readiness != protocol.ReadinessUnknown {
						t.Fatalf("old OIC fabricated Ready: %+v", v)
					}
					payload := "new-client-" + protocol.NewID("")
					got, id := request(t, g.consumer("alice"), "lab-a/http", payload)
					if got != "T1:"+payload {
						t.Fatal(got)
					}
					cv := g.closedState("alice", id)
					if cv.CloseReason != protocol.CodeCompleted {
						t.Fatal(cv)
					}
					old := g2LegacyCLI(legacy.OI, g.env, "lab-a/http", "old-cli-native")
					if old.TimedOut || old.ExitCode != 0 || old.Stdout != "T1:old-cli-native" {
						t.Fatalf("old CLI native exchange: %+v", old)
					}
					g2LegacyStrict(g, legacy.OI)
					for _, p := range g.paths {
						for _, conn := range p.Snapshot().Connections {
							if conn.Role == g2RoleControl && conn.Subprotocol != protocol.ControlSubprotocol {
								t.Fatal("old OIC did not negotiate v1", conn)
							}
						}
						for _, ev := range p.Snapshot().Events {
							if ev.Type == protocol.MsgAdmissionCheck || ev.Type == protocol.MsgObservationQuery {
								t.Fatal("v2 messages sent to old OIC")
							}
						}
					}
					g.event("compatibility_native_result", map[string]any{"combination": combination, "directory": v, "new_client_channel": cv, "new_request": payload, "new_response": got, "old_cli": old})
					g.settled()
					g2RecordWire(g)
				case "old_ois_new_oic_rejected":
					g := &g2Env{vector: "G2-13", health: map[string]*fixture.Health{"lab-a/http": fixture.NewHealth(t, resourceNS)}, paths: map[string]*g2Proxy{}}
					g.env = newEnv(t, be, envOpts{oisBinary: legacy.OIS, noOICs: true, finishSetup: func(*env) {}})
					g2CompatEvent(g, legacy, combination)
					o := g2LaunchRejectedOIC(g, nil)
					p := g.paths["lab-a"]
					g.eventually(4*time.Second, "real v2 handshakes rejected by frozen old OIS", func() bool {
						n := 0
						for _, c := range p.Snapshot().Connections {
							if c.Closed && c.UpstreamStatus == 400 {
								n++
							}
						}
						return n >= 2
					})
					snap := p.Snapshot()
					for _, c := range snap.Connections {
						if c.UpstreamStatus != 400 || g2LegacyErrorBody(t, c.HandshakeBody) != protocol.CodeVersionUnsupported || c.Subprotocol != "" {
							t.Fatalf("unexpected downgrade/handshake: %+v", c)
						}
					}
					stats := g.oicStats("lab-a")
					server := g.oisStats()
					if stats.Registered || stats.Connected || stats.Generation != 0 || server.AccessesOnline != 0 || server.Channels.Preparing+server.Channels.Open+server.Channels.Closing != 0 || server.Tickets != 0 || len(g.t1.Conns()) != 0 {
						t.Fatalf("rejected v2 created state: %+v %+v", stats, server)
					}
					lines := o.tail(1000)
					g.event("compatibility_version_rejected", map[string]any{"proxy": snap, "oic": stats, "ois": server, "oic_log": lines})
					if !strings.Contains(lines, string(protocol.CodeVersionUnsupported)) {
						t.Fatalf("new OIC lost explicit VersionUnsupported diagnostic: %s", lines)
					}
				default:
					g := newG2Env(t, be, "G2-13", g2Options{proxy: true})
					g2CompatEvent(g, legacy, combination)
					p := g.paths["lab-a"]
					exps := g.oics["lab-a"].cfg["exposures"].([]any)
					for _, raw := range exps {
						m := raw.(map[string]any)
						if m["name"] == "http" {
							m["admission_mode"] = protocol.AdmissionRequireReady
						}
					}
					g.rewriteOIC("lab-a", exps)
					g.waitView("lab-a/http", func(v protocol.ExposureView) bool {
						return v.AdmissionMode == protocol.AdmissionRequireReady && v.Readiness == protocol.ReadinessReady
					})
					native := g2LegacyCLI(legacy.OI, g.env, "lab-a/http", "old-cli-new-v2-native")
					if native.TimedOut || native.ExitCode != 0 || native.Stdout != "T1:old-cli-new-v2-native" {
						t.Fatalf("old CLI native with new v2 nodes: %+v", native)
					}
					g.event("compatibility_old_cli_native", map[string]any{"cli": native})
					for round := range 20 {
						g2FreshReady(g)
						after := len(g.t1.Conns())
						expired := combination == "new_new_old_cli_ready_expired"
						count := g2Metric(g, "oi_observation_sample_expired_total")
						if expired {
							g.health["lab-a/http"].Set(200, true)
							g.eventually(1600*time.Millisecond, "actual old CLI Ready tail", func() bool {
								v := g.view("lab-a/http")
								return v.Readiness == protocol.ReadinessReady && v.ReadinessView != nil && v.ReadinessView.RemainingMS >= 120 && v.ReadinessView.RemainingMS <= 220
							})
						}
						barrier := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
						done := g2LegacyCLIAsync(legacy.OI, g.env, "lab-a/http", fmt.Sprintf("never-replay-%d", round))
						held := g2WaitBarrier(t, barrier)
						r := g2Result(g, held)
						if r.Code != "" || r.DecisionID == "" {
							t.Fatalf("old CLI missing real positive final: %+v", r)
						}
						abortGate := g2Arm(t, p, g2Match{Role: g2RoleProvider, Direction: g2OICToOIS, Type: "ABORT", ChannelID: held.ChannelID})
						want, value, reason := protocol.CodeReadinessCheckFailed, uint16(0x0205), "timeout"
						if expired {
							want, value, reason = protocol.CodeReadinessRequired, 0x0107, "expired"
							g2WaitMetric(g, "oi_observation_sample_expired_total", count, 400*time.Millisecond)
							barrier.Release()
						}
						out := g2AwaitLegacyCLI(t, done)
						origin := resourceNS()
						barrier.Release()
						abortGate.Release()
						if out.ExitCode == 0 || out.Stdout != "" || out.Stderr == "" {
							t.Fatalf("old CLI unsafe unknown ABORT: %+v", out)
						}
						cv := g2CommitCleanup(g, protocol.OpenResponse{ChannelID: held.ChannelID}, after, true, origin)
						if cv.CloseReason != want || cv.ReadinessDecision == nil || cv.ReadinessDecision.Reason != reason {
							t.Fatalf("GET lost actual cause behind old CLI generic error: %+v", cv)
						}
						abort := g2WaitWire(t, p, held.ID, g2Match{Role: g2RoleProvider, Direction: g2OISToOIC, Type: "ABORT", ChannelID: held.ChannelID})
						frame := must(protocol.DecodeFrame(abort.Raw, 1<<20))
						if frame.Value != value || frame.Reason != want {
							t.Fatalf("not the real new wire ABORT: %+v", frame)
						}
						prepare := 0
						for _, ev := range p.Snapshot().Events {
							if ev.Type == protocol.MsgPrepareChannel && ev.ChannelID == held.ChannelID {
								prepare++
							}
						}
						if prepare != 1 || len(g.t1.Conns()) != after+1 {
							t.Fatal("old CLI replayed/duplicated request")
						}
						g.event("compatibility_old_cli_abort_round", map[string]any{"round": round, "combination": combination, "final": held, "wire_abort": abort, "raw_reason_value": value, "cli": out, "channel": cv, "target": g2NoPayload(g, after), "prepare_count": prepare})
					}
					g2CommitHistory(g)
				}
			})
		})
	}
}

func TestTVG2_13_V2WireRejection(t *testing.T) {
	for _, scenario := range []string{"missing_binding", "unknown_enum", "foreign_namespace", "unsupported_contract_entry", "late_check"} {
		t.Run(scenario, func(t *testing.T) {
			each(t, func(t *testing.T, be string) {
				if scenario == "unsupported_contract_entry" {
					g := &g2Env{vector: "G2-13", health: map[string]*fixture.Health{"lab-a/http": fixture.NewHealth(t, resourceNS)}, paths: map[string]*g2Proxy{}}
					g.env = newEnv(t, be, envOpts{noOICs: true, finishSetup: func(*env) {}, configureOIS: func(cfg map[string]any) { cfg["observation_limits"] = g2ObservationLimits() }})
					oic := g2LaunchRejectedOIC(g, nil)
					g.waitRegistered("lab-a")
					g.waitView("lab-a/http", func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessReady })
					if got, _ := request(t, g.consumer("alice"), "lab-a/alt", "warm"); got != "T3:warm" {
						t.Fatal("single-access native warmup", got)
					}
					g.baselineWhen(g2ProbeIdle)
					p := g.paths["lab-a"]
					barrier := g2Arm(t, p, g2Match{Role: g2RoleControl, Direction: g2OICToOIS, Type: protocol.MsgRegister})
					// Only the next real registration is changed. The one-access
					// baseline is frozen before this protocol fault, not afterwards.
					exps := oic.cfg["exposures"].([]any)
					exps[0].(map[string]any)["description"] = "trigger-independent-entry-registration"
					writeJSON(t, oic.path, oic.cfg)
					held := g2WaitBarrier(t, barrier)
					var reg protocol.Register
					if err := json.Unmarshal(held.Raw, &reg); err != nil {
						t.Fatal(err)
					}
					changed := false
					for i := range reg.Exposures {
						if reg.Exposures[i].ExposureID == "lab-a/http" {
							if reg.Exposures[i].Binding == nil {
								t.Fatal("missing actual v2 binding")
							}
							reg.Exposures[i].Binding.ContractVersion = 2
							changed = true
						}
					}
					if !changed || len(reg.Exposures) != 2 {
						t.Fatal("single-entry rejection needs one bad and one independent good entry")
					}
					injected := protocol.EncodeControl(&reg, reg.Seq)
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					if err := p.Inject(ctx, held.ConnID, g2OICToOIS, websocket.MessageText, injected); err != nil {
						t.Fatal(err)
					}
					barrier.Drop()
					ackEvent := g2WaitWire(t, p, held.ID, g2Match{Role: g2RoleControl, Direction: g2OISToOIC, Type: protocol.MsgRegisterAck})
					var ack protocol.RegisterAck
					if err := json.Unmarshal(ackEvent.Raw, &ack); err != nil {
						t.Fatal(err)
					}
					if len(ack.Accepted) != 1 || ack.Accepted[0] != "lab-a/alt" || len(ack.Rejected) != 1 || ack.Rejected[0].ExposureID != "lab-a/http" || ack.Rejected[0].Code != protocol.CodeVersionUnsupported {
						t.Fatalf("unsupported contract was accepted or killed independent entry: %+v", ack)
					}
					g.waitRegistered("lab-a")
					if _, err := g.consumer("alice").Lookup(bg, "lab-a/http"); protocol.CodeOf(err) != protocol.CodeNotFound {
						t.Fatal("unsupported entry visible", err)
					}
					got, id := request(t, g.consumer("alice"), "lab-a/alt", "supported-independent-entry")
					if got != "T3:supported-independent-entry" {
						t.Fatal(got)
					}
					if cv := g.closedState("alice", id); cv.CloseReason != protocol.CodeCompleted {
						t.Fatal(cv)
					}
					if len(g.t1.Conns()) != 0 || g.oicStats("lab-a").Generation != 1 {
						t.Fatal("bad entry caused payload/session replacement")
					}
					g.event("compatibility_contract_entry_rejected", map[string]any{"original": held, "injected": injected, "ack": ackEvent, "target_bad": g.t1.Conns(), "target_good": g.t3.Conns()})
					g2CommitHistory(g)
					return
				}
				g := newG2Env(t, be, "G2-13", g2Options{proxy: true})
				p := g.paths["lab-a"]
				c := g.consumer("alice")
				after := len(g.t1.Conns())
				barrier := g2Arm(t, p, g2FinalMatch(protocol.MsgAdmissionResult))
				o := g2CommitOpen(g, c)
				leg := g2Leg(g, c, o)
				held := g2WaitBarrier(t, barrier)
				if scenario == "late_check" {
					barrier.Release()
					if f := g2Frame(g, leg); f.Type != protocol.FrameReady {
						t.Fatal(f)
					}
					g2CompleteLeg(g, leg, "closed-before-late-check")
					cv := g2CommitCleanup(g, o, after, false, resourceNS())
					if cv.CloseReason != protocol.CodeCompleted {
						t.Fatal(cv)
					}
					slot := g2InjectOldPositive(g, p, held)
					got, id := request(t, c, "lab-a/http", "independent-after-late-check")
					if got != "T1:independent-after-late-check" {
						t.Fatal(got)
					}
					next := g.closedState("alice", id)
					g.cleanupStartNS = resourceNS()
					old := must(c.Channel(bg, o.ChannelID))
					current := g.oicStats("lab-a")
					if next.CloseReason != protocol.CodeCompleted || old.State != protocol.StateClosed || old.CloseReason != protocol.CodeCompleted || current.Generation != g2Result(g, held).Binding.Generation {
						t.Fatal("old check disturbed a later operation")
					}
					g.event("compatibility_late_check", map[string]any{"old_result": held, "injection_slot": slot, "old_channel": old, "new_channel": next, "oic": current})
					g2CommitHistory(g)
					return
				}
				var payload map[string]any
				if err := json.Unmarshal(held.Raw, &payload); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "missing_binding":
					delete(payload, "binding")
				case "unknown_enum":
					payload["readiness"] = "fabricated_ready"
				case "foreign_namespace":
					b := payload["binding"].(map[string]any)
					b["access_id"] = "lab-b"
					b["exposure_id"] = "lab-b/http"
				}
				injected := must(json.Marshal(payload))
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := p.Inject(ctx, held.ConnID, g2OICToOIS, websocket.MessageText, injected); err != nil {
					t.Fatal(err)
				}
				barrier.Drop()
				f := g2Frame(g, leg)
				if f.Type != protocol.FrameAbort || (f.Reason != protocol.CodeProtocolError && f.Reason != protocol.CodeAccessOffline && f.Reason != protocol.CodeChannelInterrupted) {
					t.Fatalf("invalid v2 reply was accepted: %+v", f)
				}
				origin := resourceNS()
				leg.CloseNow()
				cv := g2CommitCleanup(g, o, after, true, origin)
				g.eventually(2*time.Second, "violating control session closed", func() bool {
					for _, v := range p.Snapshot().Connections {
						if v.ID == held.ConnID {
							return v.Closed
						}
					}
					return false
				})
				for _, ev := range p.Snapshot().Events {
					if ev.ChannelID == o.ChannelID && ev.Type == "READY" {
						t.Fatal("invalid v2 message committed READY")
					}
				}
				if len(g.t1.Conns()) != after+1 {
					t.Fatalf("invalid v2 reply caused another target dial: before=%d after=%d", after, len(g.t1.Conns()))
				}
				g.event("compatibility_v2_invalid_reply", map[string]any{"scenario": scenario, "original": held, "injected": injected, "consumer_abort": f, "channel": cv, "target": g2NoPayload(g, after)})
				g2CommitHistory(g)
			})
		})
	}
}
