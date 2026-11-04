package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type g3ReadEnd struct {
	bytes int64
	err   error
	at    int64
}

func g3ReceiveEnd(s *client.Stream, pattern bool) g3ReadEnd {
	var off int64
	buf := make([]byte, g3FrameBytes)
	for {
		n, err := s.Read(buf)
		if pattern {
			for i, b := range buf[:n] {
				if b != fixture.Pattern(off+int64(i)) {
					return g3ReadEnd{off, fmt.Errorf("response differs at %d", off+int64(i)), resourceNS()}
				}
			}
		}
		off += int64(n)
		if err != nil {
			return g3ReadEnd{off, err, resourceNS()}
		}
	}
}

func (g *g3Env) nonActive(ctx context.Context, id string, c *client.Client) (int64, error) {
	var at int64
	err := g3Poll(ctx, func() (bool, error) {
		mark := g.consumerProxy.Mark()
		v, err := c.Channel(ctx, id)
		if bindErr := g.bindAPI(mark); bindErr != nil {
			return false, bindErr
		}
		if err != nil {
			return false, err
		}
		if v.State != protocol.StatePreparing && v.State != protocol.StateOpen {
			at = resourceNS()
			g.event("G3-transfer", "channel_non_active", map[string]any{"channel_id": id, "state": v.State, "reason": v.CloseReason, "terminal_observed_ns": at})
			return true, nil
		}
		return false, nil
	})
	return at, err
}

func (g *g3Env) refusedOpen(ctx context.Context, c *client.Client, revision string, wantStatus int, want protocol.Code) (int64, error) {
	mark := g.consumerProxy.Mark()
	var o protocol.OpenResponse
	status, err := c.Do(ctx, http.MethodPost, "/v1/channels", "", protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: revision}, &o)
	if bindErr := g.bindAPI(mark); bindErr != nil {
		return 0, bindErr
	}
	at := resourceNS()
	if status != wantStatus || protocol.CodeOf(err) != want {
		return at, fmt.Errorf("Open status %d %v, want %d/%s", status, err, wantStatus, want)
	}
	g.event("G3-transfer", "open_refused", map[string]any{"status": status, "code": protocol.CodeOf(err), "refused_ns": at})
	return at, nil
}

func TestTVG3_05_RevokeAndControlFailure(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		for _, scenario := range []string{"upload_backpressure_revoke", "download_backpressure_revoke", "preparing_revoke", "control_cut", "control_blackhole"} {
			t.Run(scenario, func(t *testing.T) {
				if scenario == "control_cut" || scenario == "control_blackhole" {
					g3ControlFailure(t, be, scenario)
					return
				}
				if scenario == "preparing_revoke" {
					g3PreparingRevoke(t, be, scenario)
					return
				}
				g3BackpressureRevoke(t, be, scenario)
			})
		}
	})
}

func g3BackpressureRevoke(t *testing.T, be, scenario string) {
	upload := scenario == "upload_backpressure_revoke"
	var observer, revoked *client.Client
	var tokenID string
	limits := testLimits()
	limits["idle_ms"] = 600000
	opts := g3EnvOpts{Limits: limits}
	if !upload {
		opts.BeforeFreeze = func(g *g3Env) {
			observer = g.consumer("alice")
			g.clients["observer-alice"] = observer
			tok := g.newToken("alice", 0)
			tokenID = tok.TokenID
			revoked = g.clientFor(tok.Token)
			g.clients["alice"] = revoked
			g.warm("alice", "lab-a", "warm-revocable")
		}
	}
	g := newG3Env(t, be, "TV-G3-05", scenario, opts)
	ctx := g3Context(t, 30*time.Second)
	if upload {
		observer = g.consumer("alice")
		revoked = observer
	}
	plan := fixture.PathPlan{RequestBytes: g3LargeBytes, ResponseBytes: 4, Pattern: true, HoldRead: true}
	if !upload {
		plan = fixture.PathPlan{RequestBytes: 4, ResponseBytes: g3LargeBytes, Pattern: true}
	}
	g3Check(t, g.targets["lab-a"].AddPlan(scenario, plan))
	p := g.dialPath(ctx)
	_, err := p.stream.Write(fixture.PathRequestHeader(scenario))
	g3Check(t, err)
	x := must(g.targets["lab-a"].WaitRequest(ctx, scenario, 1))
	var done chan error
	var cw *g3CountingWriter
	if upload {
		cw = &g3CountingWriter{w: p.stream, entered: make(chan struct{})}
		done = make(chan error, 1)
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
	} else {
		g3Check(t, fixture.WritePathPattern(p.stream, 4))
		g3Check(t, p.stream.CloseWrite())
		g3Check(t, x.Wait(ctx, fixture.PathWriterEntered))
	}
	holdStart := resourceNS()
	g3Check(t, g3Delay(ctx, 250*time.Millisecond))
	snapshot := x.Snapshot()
	fields := g.boundedBuffers(ctx)
	fields["channel_id"], fields["request_id"], fields["hold_start_ns"], fields["held_until_ns"], fields["target"] = p.open.ChannelID, scenario, holdStart, resourceNS(), snapshot
	fields["writer_entered"], fields["writer_returned"] = true, false
	if upload {
		select {
		case err := <-done:
			t.Fatalf("upload not blocked: %v", err)
		default:
		}
		if snapshot.ReceivedBytes != 0 || cw.bytes.Load() >= g3LargeBytes {
			t.Fatal("upload pause ineffective")
		}
		fields["direction"], fields["written_bytes"] = "client_to_server", cw.bytes.Load()
	} else {
		if snapshot.WriterReturned || snapshot.ResponseBytes >= g3LargeBytes {
			t.Fatal("download pause ineffective")
		}
		fields["direction"], fields["written_bytes"] = "server_to_client", snapshot.ResponseBytes
	}
	g.event("G3-fault", "backpressure_held", fields)
	trigger := resourceNS()
	if upload {
		g.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Actions: []string{protocol.ActionDiscover}})
	} else {
		g.write("DELETE", "/v1/admin/tokens/"+tokenID, nil)
	}
	returned := resourceNS()
	nonActive := must(g.nonActive(ctx, p.open.ChannelID, observer))
	wantStatus, wantCode := 403, protocol.CodeDenied
	if !upload {
		wantStatus, wantCode = 401, protocol.CodeUnauthenticated
	}
	refused := must(g.refusedOpen(ctx, revoked, p.open.Revision, wantStatus, wantCode))
	start := max(nonActive, refused)
	g.event("G3-fault", "revoked", map[string]any{"channel_id": p.open.ChannelID, "request_id": scenario, "trigger_ns": trigger, "returned_ns": returned, "terminal_observed_ns": nonActive, "new_open_refused_ns": refused})
	g.settled("online", "revoked_cleanup", nil, start, "revocation_observed", func(clean context.Context) error {
		x.ReleaseRead()
		x.ReleaseResponse()
		end := g3ReceiveEnd(p.stream, !upload)
		if end.err == nil || end.err == io.EOF || abortCode(end.err) != protocol.CodeRevoked {
			return fmt.Errorf("revoked consumer result: %d %v", end.bytes, end.err)
		}
		if end.at-trigger > int64(tClose) {
			return fmt.Errorf("revocation socket result exceeded T_close: %s", time.Duration(end.at-trigger))
		}
		if upload {
			if err := g3WaitResult(clean, done); err == nil {
				return fmt.Errorf("revoked 64MiB writer succeeded")
			}
		}
		_ = p.stream.Close()
		if err := x.Wait(clean, fixture.PathClosed); err != nil {
			return err
		}
		if !upload {
			g.clients["alice"] = observer
		}
		if err := g.waitReason(clean, p.open.ChannelID, protocol.CodeRevoked); err != nil {
			return err
		}
		g.event("G3-transfer", "revoke_verified", map[string]any{"channel_id": p.open.ChannelID, "trigger_ns": trigger, "read_end_ns": end.at, "received_bytes": end.bytes, "consumer_code": abortCode(end.err), "target": x.Snapshot()})
		return nil
	})
}

func g3PreparingRevoke(t *testing.T, be, scenario string) {
	g := newG3Env(t, be, "TV-G3-05", scenario, g3EnvOpts{})
	ctx := g3Context(t, 20*time.Second)
	var last uint64
	for _, x := range g.targets["lab-a"].Snapshot().Exchanges {
		last = max(last, x.ID)
	}
	g.accessProxies["lab-a"].HoldNew(true)
	opened := resourceNS()
	o, _, err := g.openPath(ctx, "alice", "lab-a/http")
	g3Check(t, err)
	mark := g.consumerProxy.Mark()
	leg, _, err := g.rawAttach(o.Attach.URL, g.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol)
	g3Check(t, err)
	ids := must(g.consumerProxy.WaitAccepted(ctx, mark, 1))
	if len(ids) != 1 {
		t.Fatal("ambiguous preparing consumer")
	}
	g3Check(t, g.consumerProxy.BindRole(ids[0], "consumer", "", o.ChannelID))
	prepareCtx, cancel := context.WithDeadline(ctx, resourceOrigin.Add(time.Duration(opened)+2*time.Second))
	defer cancel()
	g3Check(t, g3Poll(prepareCtx, func() (bool, error) {
		v, err := g.channelState(prepareCtx, o.ChannelID)
		s := must(g.resourceSnapshot(prepareCtx))
		return err == nil && v.State == protocol.StatePreparing && s.OIS.Stats.Tickets == 1, err
	}))
	g3Check(t, g.noReady(o.ChannelID))
	trigger := resourceNS()
	g.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(false)})
	returned := resourceNS()
	start := must(g.nonActive(ctx, o.ChannelID, g.consumer("alice")))
	g.event("G3-fault", "preparing_revoked", map[string]any{"channel_id": o.ChannelID, "trigger_ns": trigger, "returned_ns": returned, "terminal_observed_ns": start, "prepare_started_ns": opened, "ready_sent": 0, "tickets": 1})
	var abortAt int64
	var receivedCode protocol.Code
	g.settled("online", "revoked_cleanup", nil, start, "revocation_observed", func(clean context.Context) error {
		g.accessProxies["lab-a"].ReleaseNew()
		typ, msg, err := leg.Read(clean)
		if err != nil {
			return err
		}
		f, err := protocol.DecodeFrame(msg, 1<<20)
		if err != nil || typ != 2 || f.Type != protocol.FrameAbort || f.Reason != protocol.CodeRevoked {
			return fmt.Errorf("preparing revoke frame %d %+v %v", typ, f, err)
		}
		at := resourceNS()
		abortAt, receivedCode = at, f.Reason
		if at-trigger > int64(tClose) {
			return fmt.Errorf("preparing ABORT exceeded T_close")
		}
		_ = leg.CloseNow()
		if err := g.oldTicketRejected(o); err != nil {
			return err
		}
		if err := g.waitReason(clean, o.ChannelID, protocol.CodeRevoked); err != nil {
			return err
		}
		return g.noReady(o.ChannelID)
	})
	var wire int64
	exchanges := []fixture.PathExchangeSnapshot{}
	for _, x := range g.targets["lab-a"].Snapshot().Exchanges {
		if x.ID > last {
			wire += x.WireReceivedBytes
			exchanges = append(exchanges, x)
		}
	}
	if wire != 0 {
		t.Fatalf("ZERO-TOLERANCE: preparing revoke target bytes %d", wire)
	}
	g.event("G3-transfer", "preparing_revoke_verified", map[string]any{"channel_id": o.ChannelID, "ready_sent": 0, "abort_received_ns": abortAt, "consumer_code": receivedCode, "target_exchange_after": last, "target_wire_bytes": wire, "target": exchanges})
}

// A control-v2 preflight cannot treat a missing reply as known Unknown.
// Keep the public HTTP failure and its finite reason separate from the old
// stream's license/cleanup clocks. Never record attachment secrets.
func g3BlackholePreflightResult(status int, openErr error, opened protocol.OpenResponse) (protocol.Code, string, error) {
	var pe *protocol.Error
	if !errors.As(openErr, &pe) || pe == nil || status != http.StatusServiceUnavailable || opened.ChannelID != "" || opened.Attach != nil {
		return "", "", fmt.Errorf("blackhole preflight returned channel/attachment or invalid HTTP result: %d %v", status, openErr)
	}
	if (pe.Code == protocol.CodeReadinessCheckFailed && pe.Reason == "timeout") || (pe.Code == protocol.CodeAccessOffline && pe.Reason == "") {
		return pe.Code, pe.Reason, nil
	}
	return "", "", fmt.Errorf("blackhole preflight returned unexpected code/reason: %s/%s", pe.Code, pe.Reason)
}

func g3NoPendingBlackholeOpen(s resourceProcess[protocol.OISStats]) error {
	o := s.Stats.Observation
	if s.Stats.Tickets != 0 || s.Stats.Channels.Preparing != 0 || o == nil || o.PendingChecks != 0 || o.PendingOpens != 0 || o.HTTPWaiters != 0 {
		return fmt.Errorf("blackhole preflight retained preparing channel, tickets, or pending operation: %+v", s.Stats)
	}
	return nil
}

// The mark follows an observed READY log for the existing stream. Ordered OIS
// logging then lets both the immediate and final slices expose any new channel
// creation or READY, including a late attempt released when control resumes.
func g3BlackholeChannelEvents(p *proc, after int) ([]map[string]any, []map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	opens, ready := []map[string]any{}, []map[string]any{}
	for _, row := range p.lines[after:] {
		switch row["msg"] {
		case "open":
			opens = append(opens, row)
		case "ready":
			ready = append(ready, row)
		}
	}
	return opens, ready
}

func g3ControlFailure(t *testing.T, be, scenario string) {
	g := newG3Env(t, be, "TV-G3-05", scenario, g3EnvOpts{ControlProfile: scenario})
	ctx := g3Context(t, 30*time.Second)
	marker := "after-expiry:" + scenario
	g3Check(t, g.targets["lab-a"].AddPlan(scenario, fixture.PathPlan{RequestBytes: -1, HoldResponse: true, Observe: []string{"tick:", marker}}))
	p := g.dialPath(ctx)
	_, err := p.stream.Write(fixture.PathRequestHeader(scenario))
	g3Check(t, err)
	x := must(g.targets["lab-a"].WaitRequest(ctx, scenario, 1))
	writer := g3StartMarkers(p)
	targetCount := len(g.targets["lab-a"].Snapshot().Exchanges)
	g3Check(t, g3Poll(ctx, func() (bool, error) { return x.Snapshot().Markers["tick:"] >= 1, nil }))
	existingReady, _ := g.ois.waitLine(t, 5*time.Second, "existing control-fault stream READY", 0, func(row map[string]any) bool {
		return row["msg"] == "ready" && row["channel"] == p.open.ChannelID
	})
	channelLogMark := g3LogMark(g.ois)
	var license resourceProcess[protocol.OICStats]
	g3Check(t, g3Poll(ctx, func() (bool, error) {
		v, err := g.resourceOIC(ctx, "lab-a")
		license = v
		// Select a heartbeat phase with margin for the instrument call, then
		// check the actual applied timestamp against the required 2000ms.
		return err == nil && v.Stats.LicenseRemainingMS >= 2250, err
	}))
	oldGen, oldControl := license.Stats.Generation, g.controls["lab-a"]
	readDone := make(chan g3ReadEnd, 1)
	go func() { end := g3ReceiveEnd(p.stream, false); _ = p.stream.Close(); readDone <- end }()
	var applied int64
	var identity <-chan error
	if scenario == "control_cut" {
		applied, identity, err = g.cutControlAsync(ctx)
		g3Check(t, err)
	} else {
		pause := must(g.accessProxies["lab-a"].PauseRead(ctx, oldControl, g3ServerToClient))
		applied = pause.AppliedNS
	}
	remainingAtApply := license.Stats.LicenseRemainingMS - (applied-license.SampledNS+int64(time.Millisecond)-1)/int64(time.Millisecond)
	if remainingAtApply < 2000 {
		t.Fatalf("control fault phase slipped below 2000ms: %d", remainingAtApply)
	}
	direction := "server_to_client"
	if scenario == "control_cut" {
		direction = "both"
	}
	g.event("G3-fault", "control_applied", map[string]any{"channel_id": p.open.ChannelID, "request_id": scenario, "applied_ns": applied, "control_conn_id": g3ConnID("access/lab-a", oldControl), "direction": direction, "generation": oldGen, "license_sampled_ns": license.SampledNS, "license_remaining_ms": license.Stats.LicenseRemainingMS, "license_remaining_at_apply_ms": remainingAtApply, "target_exchanges_before": targetCount, "oic_closed_before": license.Stats.Closed})
	var blocked protocol.OpenResponse
	if scenario == "control_blackhole" {
		before := g.targets["lab-a"].Snapshot()
		oisBefore := must(g.resourceSnapshot(ctx)).OIS
		g3Check(t, g3NoPendingBlackholeOpen(oisBefore))
		mark := g.consumerProxy.Mark()
		requestStarted := resourceNS()
		status, openErr := g.consumer("alice").Do(ctx, http.MethodPost, "/v1/channels", "", protocol.OpenRequest{ExposureID: "lab-a/http", ExpectedRevision: p.open.Revision}, &blocked)
		returned := resourceNS()
		g3Check(t, g.bindAPI(mark))
		openCode, reason, err := g3BlackholePreflightResult(status, openErr, blocked)
		g3Check(t, err)
		oisAfter := must(g.resourceSnapshot(ctx)).OIS
		g3Check(t, g3NoPendingBlackholeOpen(oisAfter))
		newOpens, newReady := g3BlackholeChannelEvents(g.ois, channelLogMark)
		after := g.targets["lab-a"].Snapshot()
		g.event("G3-fault", "blackhole_new_open", map[string]any{"admission_path": "v2_preflight", "channel_id": blocked.ChannelID, "attach_present": blocked.Attach != nil, "status": status, "code": openCode, "reason": reason, "request_started_ns": requestStarted, "returned_ns": returned, "ois_before": oisBefore, "ois_after": oisAfter, "channel_log_after": channelLogMark, "existing_ready_event": existingReady, "new_open_events": newOpens, "new_ready_events": newReady, "target_exchanges_before": len(before.Exchanges), "target_exchanges_after": len(after.Exchanges), "target": after})
		if len(after.Exchanges) != len(before.Exchanges) || len(newOpens) != 0 || len(newReady) != 0 {
			t.Fatal("ZERO-TOLERANCE: blackhole preflight created channel, READY, or target prepare")
		}
	}
	var end *g3ReadEnd
	var offlineAt int64
	var origin int64
	g3Check(t, g3Poll(ctx, func() (bool, error) {
		if end == nil {
			select {
			case e := <-readDone:
				end = &e
			default:
			}
		}
		if offlineAt == 0 {
			s, err := g.resourceSnapshot(ctx)
			if err != nil {
				return false, err
			}
			if s.OIS.Stats.AccessesOnline == 1 {
				offlineAt, err = g.refusedOpen(ctx, g.consumer("alice"), p.open.Revision, 503, protocol.CodeAccessOffline)
				if err != nil {
					return false, err
				}
			}
		}
		if end != nil && offlineAt != 0 {
			origin = max(end.at, offlineAt)
			return true, nil
		}
		return false, nil
	}))
	if end.err == nil || end.err == io.EOF || end.bytes != 0 {
		t.Fatalf("control failure did not interrupt: %+v", *end)
	}
	code := abortCode(end.err)
	if code != protocol.CodeLicenseExpired && code != protocol.CodeAccessOffline {
		t.Fatalf("control failure: %v", end.err)
	}
	duration := end.at - applied
	if duration < int64(2*time.Second) || duration > int64(4*time.Second) {
		t.Fatalf("control close outside L±1s: %s", time.Duration(duration))
	}
	g.event("G3-fault", "control_terminated", map[string]any{"channel_id": p.open.ChannelID, "applied_ns": applied, "stream_end_ns": end.at, "offline_refused_ns": offlineAt, "origin_ns": origin, "reason": code, "generation": oldGen})
	g.settled(scenario, "control_fault_cleanup", nil, origin, "control_termination_and_offline_observed", func(clean context.Context) error {
		close(writer.stop)
		select {
		case <-writer.done:
		case <-clean.Done():
			return clean.Err()
		}
		if identity != nil {
			if err := g3WaitResult(clean, identity); err != nil {
				return err
			}
		}
		x.ReleaseResponse()
		if err := x.Wait(clean, fixture.PathClosed); err != nil {
			return err
		}
		if blocked.ChannelID != "" {
			if err := g.noReady(blocked.ChannelID); err != nil {
				return err
			}
		}
		stats, err := g.resourceOIC(clean, "lab-a")
		if err != nil {
			return err
		}
		if stats.Stats.Generation != oldGen {
			return fmt.Errorf("control generation changed under held fault")
		}
		_, writeErr := p.stream.Write([]byte(marker))
		if writeErr == nil {
			return fmt.Errorf("expired old stream accepted new marker")
		}
		if x.Snapshot().Markers[marker] != 0 {
			return fmt.Errorf("ZERO-TOLERANCE: after-expiry marker forwarded")
		}
		if code == protocol.CodeLicenseExpired && stats.Stats.Closed[string(protocol.CodeLicenseExpired)] <= license.Stats.Closed[string(protocol.CodeLicenseExpired)] {
			return fmt.Errorf("no same-generation LicenseExpired counter")
		}
		g.event("G3-transfer", "expiry_marker_rejected", map[string]any{"channel_id": p.open.ChannelID, "marker": marker, "target_count": x.Snapshot().Markers[marker], "generation": oldGen, "oic_stats": stats.Stats, "write_error": writeErr.Error()})
		return nil
	})
	restoreCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	g3Check(t, g.restoreControl(restoreCtx, scenario, oldGen))
	cancel()
	restored := resourceNS()
	_, err = p.stream.Write([]byte(marker))
	if err == nil {
		t.Fatal("old stream revived after restore")
	}
	if blocked.ChannelID != "" {
		g3Check(t, g.noReady(blocked.ChannelID))
	}
	g.event("G3-fault", "control_restored", map[string]any{"old_generation": oldGen, "new_generation": must(g.resourceOIC(ctx, "lab-a")).Stats.Generation, "old_control_conn_id": g3ConnID("access/lab-a", oldControl), "new_control_conn_id": g3ConnID("access/lab-a", g.controls["lab-a"]), "restored_ns": restored})
	g.settled("online", "online_restored_cleanup", nil, restored, "control_restored", nil)
	if x.Snapshot().Markers[marker] != 0 {
		t.Fatal("ZERO-TOLERANCE: marker forwarded after restore")
	}
	if len(g.targets["lab-a"].Snapshot().Exchanges) != targetCount {
		t.Fatal("ZERO-TOLERANCE: held control delivered a new target prepare after restoration")
	}
	verified := map[string]any{"channel_id": p.open.ChannelID, "blocked_channel_id": blocked.ChannelID, "after_expiry_marker": marker, "target_count": x.Snapshot().Markers[marker], "target": g.targets["lab-a"].Snapshot()}
	if scenario == "control_blackhole" {
		newOpens, newReady := g3BlackholeChannelEvents(g.ois, channelLogMark)
		oisAfter := must(g.resourceSnapshot(ctx)).OIS
		g3Check(t, g3NoPendingBlackholeOpen(oisAfter))
		verified["admission_path"], verified["new_open_events"], verified["new_ready_events"], verified["ois_after"] = "v2_preflight", newOpens, newReady, oisAfter
		verified["channel_log_after"], verified["existing_ready_event"] = channelLogMark, existingReady
		if len(newOpens) != 0 || len(newReady) != 0 {
			t.Fatal("ZERO-TOLERANCE: restored control replayed a rejected preflight")
		}
	}
	g.event("G3-transfer", "control_failure_verified", verified)
}
