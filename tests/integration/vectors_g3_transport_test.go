package integration

import (
	"context"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/tests/fixture"
)

func TestTVG3_01_HalfCloseDelayed(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"consumer_fin_first", "target_fin_first"} {
			t.Run(scenario, func(t *testing.T) {
				g := newG3Env(t, be, "TV-G3-01", scenario, g3EnvOpts{})
				ctx := g3Context(t, 30*time.Second)
				plan := fixture.PathPlan{RequestBytes: 4, ResponseBytes: g3TailBytes, Pattern: true, HoldResponse: true}
				if scenario == "target_fin_first" {
					plan = fixture.PathPlan{RequestBytes: g3TailBytes, ResponseBytes: 4, Pattern: true, ResponseFirst: true}
				}
				g3Check(t, g.targets["lab-a"].AddPlan(scenario, plan))
				p := g.dialPath(ctx)
				_, err := p.stream.Write(fixture.PathRequestHeader(scenario))
				g3Check(t, err)
				x := must(g.targets["lab-a"].WaitRequest(ctx, scenario, 1))
				if scenario == "consumer_fin_first" {
					g3Check(t, fixture.WritePathPattern(p.stream, plan.RequestBytes))
					g3Check(t, p.stream.CloseWrite())
					g3Check(t, x.Wait(ctx, fixture.PathRequestFIN))
					first := x.Snapshot()
					if first.ResponseBytes != 0 || first.ResponseFIN {
						t.Fatalf("response escaped hold: %+v", first)
					}
					start := resourceNS()
					g3Check(t, g3Delay(ctx, 250*time.Millisecond))
					released := resourceNS()
					x.ReleaseResponse()
					g.event("G3-fault", "fin_delay", map[string]any{"channel_id": p.open.ChannelID, "request_id": scenario, "direction": "server_to_client", "first_fin_ns": first.Events[fixture.PathRequestFIN], "hold_start_ns": start, "released_ns": released, "delay_ms": 250})
					g3Check(t, fixture.CheckPathPattern(p.stream, plan.ResponseBytes))
					g3Check(t, g3ReadEOF(p.stream))
				} else {
					g3Check(t, fixture.CheckPathPattern(p.stream, plan.ResponseBytes))
					g3Check(t, g3ReadEOF(p.stream))
					g3Check(t, x.Wait(ctx, fixture.PathResponseFIN))
					first := x.Snapshot()
					if first.RequestFIN || first.ReceivedBytes != 0 {
						t.Fatalf("request unexpectedly sent: %+v", first)
					}
					start := resourceNS()
					g3Check(t, g3Delay(ctx, 250*time.Millisecond))
					released := resourceNS()
					g.event("G3-fault", "fin_delay", map[string]any{"channel_id": p.open.ChannelID, "request_id": scenario, "direction": "client_to_server", "first_fin_ns": first.Events[fixture.PathResponseFIN], "hold_start_ns": start, "released_ns": released, "delay_ms": 250})
					g3Check(t, fixture.WritePathPattern(p.stream, plan.RequestBytes))
					g3Check(t, p.stream.CloseWrite())
					g3Check(t, x.Wait(ctx, fixture.PathRequestFIN))
				}
				finished := resourceNS()
				g.event("G3-transfer", "exchange_fin_observed", map[string]any{"channel_id": p.open.ChannelID, "request_id": scenario, "finished_ns": finished, "request_bytes": plan.RequestBytes, "response_bytes": plan.ResponseBytes})
				g.settled("online", "exchange_complete", nil, finished, "exchange_fin_observed", func(clean context.Context) error {
					g.finishExchange(clean, p, x, plan.RequestBytes, plan.ResponseBytes)
					return nil
				})
			})
		}
	})
}

func TestTVG3_02_Backpressure(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"upload_target_paused", "download_consumer_paused"} {
			t.Run(scenario, func(t *testing.T) {
				limits := testLimits()
				limits["idle_ms"] = 600000
				g := newG3Env(t, be, "TV-G3-02", scenario, g3EnvOpts{Limits: limits})
				ctx := g3Context(t, 30*time.Second)
				plan := fixture.PathPlan{RequestBytes: g3LargeBytes, ResponseBytes: 4, Pattern: true, HoldRead: true}
				if scenario == "download_consumer_paused" {
					plan = fixture.PathPlan{RequestBytes: 4, ResponseBytes: g3LargeBytes, Pattern: true}
				}
				g3Check(t, g.targets["lab-a"].AddPlan(scenario, plan))
				p := g.dialPath(ctx)
				_, err := p.stream.Write(fixture.PathRequestHeader(scenario))
				g3Check(t, err)
				x := must(g.targets["lab-a"].WaitRequest(ctx, scenario, 1))
				if scenario == "upload_target_paused" {
					cw := &g3CountingWriter{w: p.stream, entered: make(chan struct{})}
					done := make(chan error, 1)
					go func() {
						err := fixture.WritePathPattern(cw, g3LargeBytes)
						if err == nil {
							err = p.stream.CloseWrite()
						}
						done <- err
					}()
					select {
					case <-cw.entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					g3Check(t, g3Poll(ctx, func() (bool, error) { return cw.bytes.Load() >= g3FrameBytes, nil }))
					start := resourceNS()
					g3Check(t, g3Delay(ctx, 250*time.Millisecond))
					select {
					case err := <-done:
						t.Fatalf("64MiB writer not blocked: %v", err)
					default:
					}
					if x.Snapshot().ReceivedBytes != 0 || cw.bytes.Load() >= g3LargeBytes {
						t.Fatal("target hold ineffective")
					}
					fields := g.boundedBuffers(ctx)
					fields["channel_id"], fields["request_id"] = p.open.ChannelID, scenario
					fields["direction"], fields["hold_start_ns"], fields["released_ns"] = "client_to_server", start, resourceNS()
					fields["writer_entered"], fields["writer_returned"], fields["written_bytes"] = true, false, cw.bytes.Load()
					fields["target"] = x.Snapshot()
					g.event("G3-fault", "backpressure_held", fields)
					x.ReleaseRead()
					g3Check(t, g3WaitResult(ctx, done))
				} else {
					g3Check(t, fixture.WritePathPattern(p.stream, 4))
					g3Check(t, p.stream.CloseWrite())
					g3Check(t, x.Wait(ctx, fixture.PathWriterEntered))
					start := resourceNS()
					g3Check(t, g3Delay(ctx, 250*time.Millisecond))
					s := x.Snapshot()
					if s.WriterReturned || s.ResponseBytes >= g3LargeBytes {
						t.Fatalf("64MiB target writer not blocked: %+v", s)
					}
					fields := g.boundedBuffers(ctx)
					fields["channel_id"], fields["request_id"] = p.open.ChannelID, scenario
					fields["direction"], fields["hold_start_ns"], fields["released_ns"] = "server_to_client", start, resourceNS()
					fields["writer_entered"], fields["writer_returned"], fields["written_bytes"], fields["target"] = s.WriterEntered, s.WriterReturned, s.ResponseBytes, s
					g.event("G3-fault", "backpressure_held", fields)
				}
				g3Check(t, fixture.CheckPathPattern(p.stream, plan.ResponseBytes))
				g3Check(t, g3ReadEOF(p.stream))
				finished := resourceNS()
				g.event("G3-transfer", "exchange_fin_observed", map[string]any{"channel_id": p.open.ChannelID, "request_id": scenario, "finished_ns": finished, "request_bytes": plan.RequestBytes, "response_bytes": plan.ResponseBytes})
				g.settled("online", "transfer_complete", nil, finished, "exchange_fin_observed", func(clean context.Context) error {
					g.finishExchange(clean, p, x, plan.RequestBytes, plan.ResponseBytes)
					g.event("G3-transfer", "buffer_bounds", g.boundedBuffers(clean))
					return nil
				})
			})
		}
	})
}
