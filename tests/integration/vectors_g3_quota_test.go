package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type g3MarkerWriter struct {
	stop chan struct{}
	done chan error
}

func g3StartMarkers(p g3Path) *g3MarkerWriter {
	m := &g3MarkerWriter{make(chan struct{}), make(chan error, 1)}
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			select {
			case <-m.stop:
				m.done <- nil
				return
			case <-ticker.C:
			}
			if _, err := p.stream.Write([]byte(fmt.Sprintf("tick:%d\n", i))); err != nil {
				m.done <- err
				return
			}
		}
	}()
	return m
}

type g3QuotaItem struct {
	path      g3Path
	marker    *g3MarkerWriter
	request   string
	preparing bool
	openedNS  int64
}

func (g *g3Env) fillQuota(ctx context.Context, round, openCount, prepareCount int) []g3QuotaItem {
	g.t.Helper()
	var targetBeforeID uint64
	for _, x := range g.targets["lab-a"].Snapshot().Exchanges {
		targetBeforeID = max(targetBeforeID, x.ID)
	}
	items := []g3QuotaItem{}
	opens := []map[string]any{}
	for i := range openCount {
		id := fmt.Sprintf("quota-%d-%d", round, i)
		g3Check(g.t, g.targets["lab-a"].AddPlan(id, fixture.PathPlan{RequestBytes: -1, HoldResponse: true, Observe: []string{"tick:"}}))
		p := g.dialPath(ctx)
		_, err := p.stream.Write(fixture.PathRequestHeader(id))
		g3Check(g.t, err)
		x := must(g.targets["lab-a"].WaitRequest(ctx, id, 1))
		opens = append(opens, map[string]any{"channel_id": p.open.ChannelID, "request_id": id, "target_exchange_id": x.Snapshot().ID})
		items = append(items, g3QuotaItem{path: p, marker: g3StartMarkers(p), request: id})
	}
	for range prepareCount {
		started := resourceNS()
		o, provider, err := g.openPath(ctx, "alice", "lab-a/http")
		g3Check(g.t, err)
		items = append(items, g3QuotaItem{path: g3Path{open: o, provider: provider}, preparing: true, openedNS: started})
	}
	for _, item := range items {
		v, err := g.channelState(ctx, item.path.open.ChannelID)
		g3Check(g.t, err)
		want := protocol.StateOpen
		if item.preparing {
			want = protocol.StatePreparing
		}
		if v.State != want {
			g.t.Fatalf("quota setup channel %s state %s want %s", v.ChannelID, v.State, want)
		}
	}
	s := must(g.resourceSnapshot(ctx)).OIS.Stats
	if s.Channels.Open != openCount || s.Channels.Preparing != prepareCount || s.Tickets != prepareCount {
		g.t.Fatalf("quota setup: %+v tickets %d, want %d/%d", s.Channels, s.Tickets, openCount, prepareCount)
	}
	ids := []string{}
	prepares := []map[string]any{}
	for _, item := range items {
		ids = append(ids, item.path.open.ChannelID)
		if item.preparing {
			prepares = append(prepares, map[string]any{"channel_id": item.path.open.ChannelID, "opened_ns": item.openedNS, "prepare_expires_in_ms": item.path.open.PrepareExpiresInMS, "provider_conn_id": g3ConnID("access/lab-a", item.path.provider)})
		}
	}
	g.event("G3-fault", "quota_occupied", map[string]any{"cycle": round, "open": openCount, "preparing": prepareCount, "tickets": s.Tickets, "ois_stats": s, "channel_ids": ids, "prepares": prepares, "opens": opens, "target_before_id": targetBeforeID, "provider_proxy": g.accessProxies["lab-a"].Snapshot()})
	return items
}

func (g *g3Env) stopMarkers(ctx context.Context, items []g3QuotaItem) error {
	for _, item := range items {
		if item.marker != nil {
			close(item.marker.stop)
		}
	}
	for _, item := range items {
		if item.marker != nil {
			select {
			case <-item.marker.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func (g *g3Env) cleanupQuota(ctx context.Context, items []g3QuotaItem, reason protocol.Code, closeChannels bool) error {
	if err := g.stopMarkers(ctx, items); err != nil {
		return err
	}
	for _, item := range items {
		if closeChannels {
			mark := g.consumerProxy.Mark()
			_, err := g.consumer("alice").Close(ctx, item.path.open.ChannelID)
			if bindErr := g.bindAPI(mark); bindErr != nil {
				return bindErr
			}
			if err != nil {
				return err
			}
		}
		if !item.preparing {
			if err := g3Interrupted(item.path.stream); err != nil {
				return err
			}
			_ = item.path.stream.Close()
		}
		if err := g.waitReason(ctx, item.path.open.ChannelID, reason); err != nil {
			return err
		}
		if item.preparing {
			if err := g.noReady(item.path.open.ChannelID); err != nil {
				return err
			}
		}
		if err := g.oldTicketRejected(item.path.open); err != nil {
			return err
		}
	}
	for _, x := range g.targets["lab-a"].Snapshot().Exchanges {
		if x.RequestID == "" && x.WireReceivedBytes != 0 {
			return fmt.Errorf("ZERO-TOLERANCE: preparing target received %d bytes", x.WireReceivedBytes)
		}
	}
	return nil
}

func TestTVG3_06_QuotaAndCleanup(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"mixed_limit_reuse", "repeated_failures"} {
			t.Run(scenario, func(t *testing.T) {
				g := newG3Env(t, be, "TV-G3-06", scenario, g3EnvOpts{Limits: map[string]any{"n_ois": 16, "n_access": 32, "n_subject": 16, "max_local_channels": 32}})
				ctx := g3Context(t, 60*time.Second)
				if scenario == "mixed_limit_reuse" {
					for round := range 2 {
						items := g.fillQuota(ctx, round, 8, 8)
						mark := g.consumerProxy.Mark()
						var refused protocol.OpenResponse
						status, err := g.consumer("alice").Do(ctx, http.MethodPost, "/v1/channels", "", protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: items[0].path.open.Revision}, &refused)
						g3Check(t, g.bindAPI(mark))
						if status != 429 || protocol.CodeOf(err) != protocol.CodeResourceExhausted || refused.ChannelID != "" {
							t.Fatalf("17th Open: %d %v %+v", status, err, refused)
						}
						g.event("G3-fault", "quota_rejected", map[string]any{"cycle": round, "status": status, "code": protocol.CodeOf(err), "open": 8, "preparing": 8})
						start := resourceNS()
						g.settled("online", "quota_round_cleanup", &round, start, "quota_round_complete", func(clean context.Context) error {
							return g.cleanupQuota(clean, items, protocol.CodeClosedByClient, true)
						})
						g.event("G3-transfer", "quota_round_verified", map[string]any{"cycle": round, "target": g.targets["lab-a"].Snapshot()})
					}
					return
				}
				for cycle := range 10 {
					openCount, prepareCount := 8, 0
					if cycle%3 == 1 {
						openCount, prepareCount = 0, 8
					}
					if cycle%3 == 2 {
						openCount, prepareCount = 4, 4
					}
					items := g.fillQuota(ctx, cycle, openCount, prepareCount)
					reason := protocol.CodeChannelInterrupted
					var start int64
					origin := "first_reset_applied"
					switch cycle % 3 {
					case 0:
						start = must(g.consumerProxy.Reset(items[0].path.consumer))
						g.event("G3-fault", "cycle_first_rst", map[string]any{"cycle": cycle, "channel_id": items[0].path.open.ChannelID, "applied_ns": start, "conn_ref": g3ConnID("consumer", items[0].path.consumer)})
					case 1:
						reason, origin = protocol.CodePrepareTimeout, "all_prepare_timeouts_observed"
						for _, item := range items {
							at, err := g.waitReasonAt(ctx, item.path.open.ChannelID, reason)
							g3Check(t, err)
							elapsed := time.Duration(at - item.openedNS)
							// Preserve the existing B1-14 observable bound: original
							// 2s prepare deadline plus its T_close shutdown grace.
							if elapsed < 2*time.Second || elapsed > 2*time.Second+tClose {
								t.Fatalf("prepare timeout outside original B1-14 bound: %s", elapsed)
							}
							start = max(start, at)
						}
					case 2:
						reason, origin = protocol.CodeRevoked, "revocation_observed"
						// Observe ABORT immediately and close its transport, as an
						// active consumer does. Waiting for OIS Closed first would
						// artificially retain these sockets for the whole grace.
						readers := make([]chan error, 0, openCount)
						for _, item := range items {
							if item.preparing {
								continue
							}
							done := make(chan error, 1)
							readers = append(readers, done)
							go func() { err := g3Interrupted(item.path.stream); _ = item.path.stream.Close(); done <- err }()
						}
						trigger := resourceNS()
						g.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(false)})
						returned := resourceNS()
						for _, item := range items {
							at, err := g.waitReasonAt(ctx, item.path.open.ChannelID, reason)
							g3Check(t, err)
							start = max(start, at)
						}
						for _, done := range readers {
							g3Check(t, g3WaitResult(ctx, done))
						}
						if start-trigger > int64(tClose) {
							t.Fatalf("revoke exceeded T_close: %s", time.Duration(start-trigger))
						}
						g.event("G3-fault", "cycle_revoked", map[string]any{"cycle": cycle, "trigger_ns": trigger, "returned_ns": returned, "terminal_observed_ns": start, "reason": reason})
					}
					g.settled("online", "failure_cycle_cleanup", &cycle, start, origin, func(clean context.Context) error {
						if cycle%3 == 0 {
							for _, item := range items[1:] {
								at, err := g.consumerProxy.Reset(item.path.consumer)
								if err != nil {
									return err
								}
								g.event("G3-fault", "cycle_rst", map[string]any{"cycle": cycle, "channel_id": item.path.open.ChannelID, "applied_ns": at, "conn_ref": g3ConnID("consumer", item.path.consumer)})
							}
						}
						return g.cleanupQuota(clean, items, reason, false)
					})
					if cycle%3 == 2 {
						g.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(true)})
					}
					g.event("G3-transfer", "cycle_complete", map[string]any{"cycle": cycle, "reason": reason, "open": openCount, "preparing": prepareCount, "target": g.targets["lab-a"].Snapshot()})
				}
			})
		}
	})
}
