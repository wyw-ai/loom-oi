package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type g3CLI struct {
	process *proc
	addr    string
}
type g3CLIPath struct {
	local              *net.TCPConn
	channel            string
	provider, consumer uint64
	reportAfter        int
}

func g3LogMark(p *proc) int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.lines) }

func (g *g3Env) startCLI() *g3CLI {
	g.t.Helper()
	p := startProc(g.t, g.dir, "g3-oi-listener", oiBin, []string{"OI_TOKEN=" + g.token("alice")},
		"--ois", g.consumerEndpoint(), "--ca", g.caFile, "connect", "lab-a/http", "--listen", "127.0.0.1:0")
	l, _ := p.waitLine(g.t, 5*time.Second, "listening", 0, func(v map[string]any) bool { return v["listening"] != nil })
	c := &g3CLI{p, l["listening"].(string)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	g3Check(g.t, g.targets["lab-a"].AddPlan("warm-cli", fixture.PathPlan{RequestBytes: 4, ResponseBytes: 4, Pattern: true}))
	x := g.cliPath(ctx, c, "warm-cli")
	g3Check(g.t, fixture.WritePathPattern(x.local, 4))
	g3Check(g.t, x.local.CloseWrite())
	g3Check(g.t, fixture.CheckPathPattern(x.local, 4))
	g3Check(g.t, g3ReadEOF(x.local))
	_ = x.local.Close()
	target := must(g.targets["lab-a"].WaitRequest(ctx, "warm-cli", 1))
	g3Check(g.t, target.Wait(ctx, fixture.PathClosed))
	g3Check(g.t, g.waitReason(ctx, x.channel, protocol.CodeCompleted))
	c.process.waitLine(g.t, 5*time.Second, "warm Completed", x.reportAfter, func(v map[string]any) bool { return v["code"] == string(protocol.CodeCompleted) })
	g.closeIdle()
	return c
}

// One CLI request is the sole producer of connections in this interval.
// Its implementation performs discovery/Open over one HTTP connection, then
// Attach over a second connection. Require both exact counts and OIS open/READY
// plus the target envelope; no overlapping or unbound connection is accepted.
func (g *g3Env) cliPath(ctx context.Context, cli *g3CLI, requestID string) g3CLIPath {
	g.t.Helper()
	pm, cm, lm, report := g.accessProxies["lab-a"].Mark(), g.consumerProxy.Mark(), g3LogMark(g.ois), g3LogMark(cli.process)
	c := must(net.DialTCP("tcp", nil, must(net.ResolveTCPAddr("tcp", cli.addr))))
	g3Check(g.t, c.SetReadBuffer(g3FrameBytes))
	g3Check(g.t, c.SetWriteBuffer(g3FrameBytes))
	g3Check(g.t, c.SetDeadline(time.Now().Add(30*time.Second)))
	g.t.Cleanup(func() { _ = c.Close() })
	_, err := c.Write(fixture.PathRequestHeader(requestID))
	g3Check(g.t, err)
	line, _ := g.ois.waitLog(g.t, 5*time.Second, "open", lm)
	id, ok := line["channel"].(string)
	if !ok || id == "" {
		g.t.Fatalf("CLI open log has no channel: %v", line)
	}
	g.ois.waitLine(g.t, 5*time.Second, "CLI ready", lm, func(v map[string]any) bool { return v["msg"] == "ready" && v["channel"] == id })
	provider := must(g.accessProxies["lab-a"].WaitAccepted(ctx, pm, 1))
	consumer := must(g.consumerProxy.WaitAccepted(ctx, cm, 2))
	if len(provider) != 1 || len(consumer) != 2 {
		g.t.Fatalf("ambiguous CLI exclusive connections: %v %v", provider, consumer)
	}
	g3Check(g.t, g.accessProxies["lab-a"].BindRole(provider[0], "provider", "lab-a", id))
	g3Check(g.t, g.consumerProxy.BindRole(consumer[0], "api", "", ""))
	g3Check(g.t, g.consumerProxy.BindRole(consumer[1], "consumer", "", id))
	// The CLI exposes no CloseIdle hook. This completed, classified HTTP socket
	// belongs to the test proxy; close it without touching the live data leg or
	// killing the CLI listener. The same cleanup occurs during initial warmup.
	_, err = g.consumerProxy.Reset(consumer[0])
	g3Check(g.t, err)
	g3Check(g.t, g.consumerProxy.WaitClosed(ctx, consumer[0]))
	_ = must(g.targets["lab-a"].WaitRequest(ctx, requestID, 1))
	g.event("G3-transfer", "cli_ready", map[string]any{"channel_id": id, "request_id": requestID, "provider_conn_id": g3ConnID("access/lab-a", provider[0]), "consumer_conn_id": g3ConnID("consumer", consumer[1]), "idle_api_conn_id": g3ConnID("consumer", consumer[0]), "socket_buffer_requested": g3FrameBytes, "cli_pid": cli.process.cmd.Process.Pid})
	return g3CLIPath{c, id, provider[0], consumer[1], report}
}

func g3Interrupted(r io.Reader) error {
	var b [g3FrameBytes]byte
	for {
		n, err := r.Read(b[:])
		if n > 0 {
			return fmt.Errorf("unexpected response payload before interruption: %d", n)
		}
		if err == io.EOF {
			return fmt.Errorf("interruption reported natural EOF")
		}
		if err != nil {
			return nil
		}
	}
}

func (g *g3Env) noReady(id string) error {
	g.ois.mu.Lock()
	defer g.ois.mu.Unlock()
	count := 0
	for _, v := range g.ois.lines {
		if v["msg"] == "ready" && v["channel"] == id {
			count++
		}
	}
	g.event("G3-transfer", "preparing_no_ready", map[string]any{"channel_id": id, "ready_events": count})
	if count != 0 {
		return fmt.Errorf("ZERO-TOLERANCE: preparing channel emitted READY: %s", id)
	}
	return nil
}

func (g *g3Env) oldTicketRejected(o protocol.OpenResponse) error {
	mark := g.consumerProxy.Mark()
	leg, res, err := g.rawAttach(o.Attach.URL, g.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
	if leg != nil {
		_ = leg.CloseNow()
	}
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	if bindErr := g.bindAPI(mark); bindErr != nil {
		return bindErr
	}
	if err == nil || res == nil || res.StatusCode != 410 {
		return fmt.Errorf("old/late ticket accepted: %v status %+v", err, res)
	}
	g.event("G3-transfer", "ticket_rejected", map[string]any{"channel_id": o.ChannelID, "status": res.StatusCode})
	return nil
}

func TestTVG3_03_DataLegInterrupted(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"consumer_open_rst", "provider_open_rst", "consumer_preparing_rst", "provider_preparing_rst"} {
			t.Run(scenario, func(t *testing.T) {
				var cli *g3CLI
				opts := g3EnvOpts{}
				if scenario == "provider_open_rst" {
					opts.BeforeFreeze = func(g *g3Env) { cli = g.startCLI() }
				}
				g := newG3Env(t, be, "TV-G3-03", scenario, opts)
				ctx := g3Context(t, 30*time.Second)
				if scenario == "consumer_open_rst" || scenario == "provider_open_rst" {
					g3Check(t, g.targets["lab-a"].AddPlan(scenario, fixture.PathPlan{RequestBytes: -1, HoldResponse: true, Observe: []string{"before-rst"}}))
					var p g3Path
					var cp g3CLIPath
					var r io.Reader
					var w io.Writer
					var channel string
					var proxy *g3FaultProxy
					var conn uint64
					if cli != nil {
						cp = g.cliPath(ctx, cli, scenario)
						r, w, channel, proxy, conn = cp.local, cp.local, cp.channel, g.accessProxies["lab-a"], cp.provider
					} else {
						p = g.dialPath(ctx)
						_, err := p.stream.Write(fixture.PathRequestHeader(scenario))
						g3Check(t, err)
						r, w, channel, proxy, conn = p.stream, p.stream, p.open.ChannelID, g.consumerProxy, p.consumer
					}
					_, err := w.Write([]byte("before-rst"))
					g3Check(t, err)
					x := must(g.targets["lab-a"].WaitRequest(ctx, scenario, 1))
					g3Check(t, g3Poll(ctx, func() (bool, error) { return x.Snapshot().Markers["before-rst"] == 1, nil }))
					if x.Snapshot().ResponseBytes != 0 {
						t.Fatal("response escaped hold")
					}
					beforeReset := x.Snapshot()
					start := must(proxy.Reset(conn))
					g.event("G3-fault", "data_rst", map[string]any{"channel_id": channel, "request_id": scenario, "conn_id": conn, "conn_ref": g.connRef(proxy, conn), "role": scenario, "applied_ns": start, "target_before": beforeReset})
					g.settled("online", "interrupted_cleanup", nil, start, "first_reset_applied", func(clean context.Context) error {
						x.ReleaseResponse()
						if err := proxy.WaitClosed(clean, conn); err != nil {
							return err
						}
						if err := g3Interrupted(r); err != nil {
							return err
						}
						if cli != nil {
							_ = cp.local.Close()
							cli.process.waitLine(t, time.Until(resourceOrigin.Add(time.Duration(start)+2*time.Second)), "interrupted report", cp.reportAfter, func(v map[string]any) bool { return v["code"] == string(protocol.CodeChannelInterrupted) })
						} else {
							_ = p.stream.Close()
							if err := g.oldTicketRejected(p.open); err != nil {
								return err
							}
						}
						if err := x.Wait(clean, fixture.PathClosed); err != nil {
							return err
						}
						return g.waitReason(clean, channel, protocol.CodeChannelInterrupted)
					})
					if scenario == "consumer_open_rst" {
						stats := must(g.resourceOIC(ctx, "lab-a")).Stats
						before := g.resources.profiles["online"].Sample.OICs["lab-a"].Stats.Closed[string(protocol.CodeChannelInterrupted)]
						if stats.Closed[string(protocol.CodeChannelInterrupted)] != before+1 {
							t.Fatalf("surviving provider did not record ChannelInterrupted: %+v", stats.Closed)
						}
						g.event("G3-transfer", "surviving_provider_interrupted", map[string]any{"channel_id": channel, "before": before, "after": stats.Closed[string(protocol.CodeChannelInterrupted)]})
					}
					g.event("G3-transfer", "interrupted_verified", map[string]any{"channel_id": channel, "request_id": scenario, "local_end": "error_not_eof", "target": x.Snapshot()})
					return
				}
				before := g.targets["lab-a"].Snapshot()
				var last uint64
				for _, x := range before.Exchanges {
					last = max(last, x.ID)
				}
				consumerFirst := scenario == "consumer_preparing_rst"
				if consumerFirst {
					g.accessProxies["lab-a"].HoldNew(true)
				}
				prepareStart := resourceNS()
				o, provider, err := g.openPath(ctx, "alice", "lab-a/http")
				g3Check(t, err)
				proxy, conn := g.accessProxies["lab-a"], provider
				var attached io.Closer
				if consumerFirst {
					mark := g.consumerProxy.Mark()
					leg, _, err := g.rawAttach(o.Attach.URL, g.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
					g3Check(t, err)
					ids := must(g.consumerProxy.WaitAccepted(ctx, mark, 1))
					if len(ids) != 1 {
						t.Fatal("ambiguous attached consumer")
					}
					g3Check(t, g.consumerProxy.BindRole(ids[0], "consumer", "", o.ChannelID))
					// CloseNow adapter does not wait for a graceful close handshake.
					attached = g3CloseFunc(leg.CloseNow)
					proxy, conn = g.consumerProxy, ids[0]
				}
				gateCtx, cancel := context.WithDeadline(ctx, resourceOrigin.Add(time.Duration(prepareStart)+2*time.Second))
				g3Check(t, g3Poll(gateCtx, func() (bool, error) {
					v, err := g.channelState(gateCtx, o.ChannelID)
					s := must(g.resourceSnapshot(gateCtx))
					return err == nil && v.State == protocol.StatePreparing && s.OIS.Stats.Tickets == 1, err
				}))
				cancel()
				g3Check(t, g.noReady(o.ChannelID))
				start := must(proxy.Reset(conn))
				g.event("G3-fault", "preparing_rst", map[string]any{"channel_id": o.ChannelID, "role": scenario, "conn_id": conn, "conn_ref": g.connRef(proxy, conn), "applied_ns": start, "prepare_started_ns": prepareStart, "ready_sent": 0, "tickets": 1})
				g.settled("online", "interrupted_cleanup", nil, start, "first_reset_applied", func(clean context.Context) error {
					if err := proxy.WaitClosed(clean, conn); err != nil {
						return err
					}
					if attached != nil {
						_ = attached.Close()
					}
					if err := g.waitReason(clean, o.ChannelID, protocol.CodeChannelInterrupted); err != nil {
						return err
					}
					g.accessProxies["lab-a"].ReleaseNew()
					if err := g.oldTicketRejected(o); err != nil {
						return err
					}
					return g.noReady(o.ChannelID)
				})
				var wire int64
				newExchanges := []fixture.PathExchangeSnapshot{}
				for _, x := range g.targets["lab-a"].Snapshot().Exchanges {
					if x.ID > last {
						newExchanges = append(newExchanges, x)
						wire += x.WireReceivedBytes
						if !x.Closed {
							t.Fatal("target worker survived cleanup")
						}
					}
				}
				if wire != 0 {
					t.Fatalf("ZERO-TOLERANCE: preparing target received %d bytes", wire)
				}
				g.event("G3-transfer", "preparing_interruption_verified", map[string]any{"channel_id": o.ChannelID, "ready_sent": 0, "target_wire_bytes": wire, "target_exchange_after": last, "target": newExchanges})
			})
		}
	})
}

type g3CloseFunc func() error

func (f g3CloseFunc) Close() error { return f() }

func TestTVG3_04_ResponseLostNoReplay(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"consumer_leg_loss", "provider_leg_loss"} {
			t.Run(scenario, func(t *testing.T) {
				var cli *g3CLI
				opts := g3EnvOpts{}
				if scenario == "provider_leg_loss" {
					opts.BeforeFreeze = func(g *g3Env) { cli = g.startCLI() }
				}
				g := newG3Env(t, be, "TV-G3-04", scenario, opts)
				ctx := g3Context(t, 30*time.Second)
				target := g.targets["lab-a"]
				oldID, newID := "old-"+scenario, "new-"+scenario
				g3Check(t, target.AddPlan(oldID, fixture.PathPlan{RequestBytes: 4, ResponseBytes: 4, Pattern: true, HoldResponse: true}))
				g3Check(t, target.AddPlan(newID, fixture.PathPlan{RequestBytes: 4, ResponseBytes: 4, Pattern: true}))
				var p g3Path
				var cp g3CLIPath
				var r io.Reader
				var channel string
				var proxy *g3FaultProxy
				var conn uint64
				if cli != nil {
					cp = g.cliPath(ctx, cli, oldID)
					r, channel, proxy, conn = cp.local, cp.channel, g.accessProxies["lab-a"], cp.provider
					g3Check(t, fixture.WritePathPattern(cp.local, 4))
					g3Check(t, cp.local.CloseWrite())
				} else {
					p = g.dialPath(ctx)
					_, err := p.stream.Write(fixture.PathRequestHeader(oldID))
					g3Check(t, err)
					g3Check(t, fixture.WritePathPattern(p.stream, 4))
					g3Check(t, p.stream.CloseWrite())
					r, channel, proxy, conn = p.stream, p.open.ChannelID, g.consumerProxy, p.consumer
				}
				x := must(target.WaitRequest(ctx, oldID, 1))
				g3Check(t, x.Wait(ctx, fixture.PathEffectCommitted))
				if target.Snapshot().Effects[oldID] != 1 || x.Snapshot().ResponseBytes != 0 {
					t.Fatal("lost response precondition absent")
				}
				g.event("G3-effect", "committed_before_loss", map[string]any{"channel_id": channel, "request_id": oldID, "effects": target.Snapshot().Effects, "target": x.Snapshot()})
				start := must(proxy.Reset(conn))
				g.event("G3-fault", "response_lost", map[string]any{"channel_id": channel, "request_id": oldID, "applied_ns": start, "conn_id": conn, "conn_ref": g.connRef(proxy, conn), "role": scenario, "response_bytes": 0})
				g.settled("online", "fault_cleanup", nil, start, "first_reset_applied", func(clean context.Context) error {
					x.ReleaseResponse()
					if err := proxy.WaitClosed(clean, conn); err != nil {
						return err
					}
					if err := g3Interrupted(r); err != nil {
						return err
					}
					if cli != nil {
						_ = cp.local.Close()
						cli.process.waitLine(t, time.Until(resourceOrigin.Add(time.Duration(start)+2*time.Second)), "interrupted report", cp.reportAfter, func(v map[string]any) bool { return v["code"] == string(protocol.CodeChannelInterrupted) })
					} else {
						_ = p.stream.Close()
					}
					if err := x.Wait(clean, fixture.PathClosed); err != nil {
						return err
					}
					if err := g.waitReason(clean, channel, protocol.CodeChannelInterrupted); err != nil {
						return err
					}
					g.event("G3-transfer", "response_loss_verified", map[string]any{"channel_id": channel, "request_id": oldID, "local_end": "error_not_eof", "target": x.Snapshot()})
					return nil
				})
				oldGen := must(g.resourceOIC(ctx, "lab-a")).Stats.Generation
				oldControl := g.controls["lab-a"]
				_, err := g.cutControl(ctx)
				g3Check(t, err)
				restoreCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				g3Check(t, g.restoreControl(restoreCtx, "control_cut", oldGen))
				cancel()
				observeStart := resourceNS()
				newGen := must(g.resourceOIC(ctx, "lab-a")).Stats.Generation
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if target.Snapshot().Effects[oldID] != 1 {
						t.Fatal("ZERO-TOLERANCE: implicit replay after new generation")
					}
					g3Check(t, g3Delay(ctx, 100*time.Millisecond))
				}
				g.event("G3-effect", "no_replay_window", map[string]any{"old_request_id": oldID, "old_generation": oldGen, "new_generation": newGen, "old_control_conn_id": g3ConnID("access/lab-a", oldControl), "new_control_conn_id": g3ConnID("access/lab-a", g.controls["lab-a"]), "start_ns": observeStart, "end_ns": resourceNS(), "effects": target.Snapshot().Effects})
				var finished int64
				var freshChannel string
				var finalize func(context.Context)
				if cli == nil {
					fresh := g.dialPath(ctx)
					_, err = fresh.stream.Write(fixture.PathRequestHeader(newID))
					g3Check(t, err)
					g3Check(t, fixture.WritePathPattern(fresh.stream, 4))
					g3Check(t, fresh.stream.CloseWrite())
					g3Check(t, fixture.CheckPathPattern(fresh.stream, 4))
					g3Check(t, g3ReadEOF(fresh.stream))
					finished, freshChannel = resourceNS(), fresh.open.ChannelID
					finalize = func(clean context.Context) {
						freshTarget := must(target.WaitRequest(clean, newID, 1))
						g.finishExchange(clean, fresh, freshTarget, 4, 4)
					}
				} else {
					fresh := g.cliPath(ctx, cli, newID)
					g3Check(t, fixture.WritePathPattern(fresh.local, 4))
					g3Check(t, fresh.local.CloseWrite())
					g3Check(t, fixture.CheckPathPattern(fresh.local, 4))
					g3Check(t, g3ReadEOF(fresh.local))
					finished, freshChannel = resourceNS(), fresh.channel
					finalize = func(clean context.Context) {
						_ = fresh.local.Close()
						freshTarget := must(target.WaitRequest(clean, newID, 1))
						g3Check(t, freshTarget.Wait(clean, fixture.PathClosed))
						snap := freshTarget.Snapshot()
						if !snap.RequestFIN || !snap.ResponseFIN || snap.Error != "" || snap.ReceivedBytes != 4 || snap.ResponseBytes != 4 {
							t.Fatalf("new CLI request incomplete: %+v", snap)
						}
						g3Check(t, g.waitReason(clean, fresh.channel, protocol.CodeCompleted))
						deadline, _ := clean.Deadline()
						cli.process.waitLine(t, time.Until(deadline), "new CLI Completed", fresh.reportAfter, func(v map[string]any) bool { return v["code"] == string(protocol.CodeCompleted) })
						g.event("G3-transfer", "exchange_complete", map[string]any{"channel_id": fresh.channel, "request_id": newID, "request_bytes": 4, "response_bytes": 4, "target": snap, "effects": target.Snapshot().Effects})
					}
				}
				g.event("G3-transfer", "exchange_fin_observed", map[string]any{"channel_id": freshChannel, "request_id": newID, "finished_ns": finished, "request_bytes": 4, "response_bytes": 4})
				g.settled("online", "new_request_cleanup", nil, finished, "exchange_fin_observed", func(clean context.Context) error {
					finalize(clean)
					counts := target.Snapshot().Effects
					if counts[oldID] != 1 || counts[newID] != 1 {
						return fmt.Errorf("ZERO-TOLERANCE: effects after explicit request: %v", counts)
					}
					g.event("G3-effect", "explicit_request_complete", map[string]any{"old_request_id": oldID, "new_request_id": newID, "effects": counts})
					return nil
				})
				counts := target.Snapshot().Effects
				if counts[oldID] != 1 || counts[newID] != 1 {
					t.Fatalf("ZERO-TOLERANCE: effects after cleanup: %v", counts)
				}
				g.event("G3-effect", "final_counts", map[string]any{"old_request_id": oldID, "new_request_id": newID, "effects": counts, "target": target.Snapshot()})
			})
		}
	})
}
