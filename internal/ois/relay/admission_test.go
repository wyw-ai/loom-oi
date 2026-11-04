package relay

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type observedFixture struct {
	*fixture
	binding protocol.ObservationBinding
	entry   protocol.RegisterExposure
	ids     *protocol.ControlIDs
}

func newObservedFixture(t *testing.T, supported bool, mods ...func(*limits.Limits)) *observedFixture {
	t.Helper()
	e := newEnv(t, mods...)
	e.addAccess("lab-a", pin(1))
	e.addPolicy("p1", "alice", "lab-a/*", protocol.ActionDiscover, protocol.ActionOpen)
	alice, _ := e.token("alice", protocol.ScopeConsumer, 0)
	o := &oic{t: t, access: "lab-a", conn: newFakeConn(), done: make(chan struct{}), version: protocol.ControlSubprotocolV2}
	go func() { e.h.ServeControlVersion(o.conn, "lab-a", pin(1), protocol.ControlSubprotocolV2); close(o.done) }()
	o.send(&protocol.Hello{OICVersion: "observation-test"})
	o.welcome = expect[*protocol.Welcome](o)
	b := protocol.ObservationBinding{AccessID: "lab-a", Epoch: o.welcome.Epoch, Generation: o.welcome.Generation, ExposureID: exp1, Revision: rv(1), ObservationRevision: "ob_1", ContractDigest: protocol.NullObservationDigest}
	x := expo(exp1, rv(1))
	x.ProbeSupport = protocol.ProbeUnsupported
	if supported {
		b.SourceID = "local"
		b.ContractVersion = 1
		b.ContractDigest = strings.Repeat("b", 64)
		x.ProbeSupport = protocol.ProbeSupported
	}
	x.Binding = &b
	o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{x}})
	ack := expect[*protocol.RegisterAck](o)
	if len(ack.Accepted) != 1 || len(ack.Rejected) != 0 {
		t.Fatal(ack)
	}
	ids, err := protocol.NewControlIDs()
	if err != nil {
		t.Fatal(err)
	}
	return &observedFixture{fixture: &fixture{env: e, o: o, alice: alice, rev: rv(1)}, binding: b, entry: x, ids: ids}
}

type openOutcome struct {
	result OpenResult
	err    error
}

func asyncOpen(h *Hub, ctx context.Context, p Principal, request protocol.OpenRequest) <-chan openOutcome {
	done := make(chan openOutcome, 1)
	go func() { r, err := h.OpenContext(ctx, p, request); done <- openOutcome{r, err} }()
	return done
}
func opened(t *testing.T, done <-chan openOutcome) openOutcome {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(waitTimeout):
		t.Fatal("Open did not complete")
		return openOutcome{}
	}
}
func (f *observedFixture) request(key string) protocol.OpenRequest {
	return protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev, ClientRequestID: key}
}
func (f *observedFixture) reply(c *protocol.AdmissionCheck, readiness string, ttl int64) *protocol.AdmissionResult {
	m := &protocol.AdmissionResult{CheckID: c.CheckID, Binding: c.Binding, ChannelID: c.ChannelID, EffectiveAdmission: c.EffectiveAdmission, Readiness: readiness, RemainingMS: ttl}
	switch readiness {
	case protocol.ReadinessReady:
		m.Reason = "matched_ready_status"
		m.SampleSeq = ptr(uint64(1))
	case protocol.ReadinessNotReady:
		m.Reason = "matched_not_ready_status"
		m.SampleSeq = ptr(uint64(1))
		m.Code = protocol.CodeTargetNotReady
	default:
		m.Reason = "unsupported"
		if c.Binding.SourceID != "" {
			m.Reason = "no_sample"
		}
		if c.EffectiveAdmission == protocol.AdmissionRequireReady {
			m.Code = protocol.CodeReadinessRequired
		}
	}
	if c.Stage == protocol.CheckFinal && m.Code == "" {
		m.DecisionID, _ = f.ids.Next()
	}
	return m
}
func (f *observedFixture) preflight(key string) (<-chan openOutcome, *protocol.AdmissionCheck) {
	f.t.Helper()
	done := asyncOpen(f.h, context.Background(), f.alice, f.request(key))
	c := expect[*protocol.AdmissionCheck](f.o)
	if c.Stage != protocol.CheckPreflight {
		f.t.Fatal(c)
	}
	return done, c
}
func (f *observedFixture) prepared(t *testing.T, key string, readiness string) (*flow, *protocol.AdmissionCheck) {
	t.Helper()
	done, c := f.preflight(key)
	ttl := int64(1500)
	if readiness == protocol.ReadinessUnknown {
		ttl = 0
	}
	f.o.send(f.reply(c, readiness, ttl))
	out := opened(t, done)
	if out.err != nil {
		t.Fatal(out.err)
	}
	fl := &flow{open: out.result, prep: f.o.prepareMsg()}
	fl.prov = f.attach(f.reserve(fl, Provider))
	fl.cons = f.attach(f.reserve(fl, Consumer))
	final := expect[*protocol.AdmissionCheck](f.o)
	if final.Stage != protocol.CheckFinal || final.ChannelID != f.id(fl) {
		t.Fatal(final)
	}
	return fl, final
}

func TestAdmissionFixedPreparationAndFinal(t *testing.T) {
	f := newObservedFixture(t, false)
	done, c := f.preflight("one")
	if s := f.h.Stats(); s.Channels.Preparing != 0 || s.Tickets != 0 {
		t.Fatal("preflight created side effects", s)
	}
	start := f.clk.Now()
	f.clk.Add(200 * time.Millisecond)
	f.o.send(f.reply(c, protocol.ReadinessUnknown, 0))
	out := opened(t, done)
	if out.err != nil || out.result.Replay || out.result.Response.ReadinessStage != protocol.CheckPreflight || out.result.Response.PrepareExpiresInMS != 1800 {
		t.Fatal(out)
	}
	fl := &flow{open: out.result, prep: f.o.prepareMsg()}
	if fl.prep.Binding == nil || *fl.prep.Binding != f.binding || fl.prep.PrepareTTLMS != 1800 || fl.prep.EffectiveAdmission != protocol.AdmissionBestEffort {
		t.Fatal(fl.prep)
	}
	f.h.mu.Lock()
	ch := f.h.channels[f.id(fl)]
	if ch.created != start || ch.deadline != start.Add(2*time.Second) {
		t.Fatal("preflight renewed D")
	}
	f.h.storeOK = false
	f.h.meta.Maintenance = true
	f.h.mu.Unlock()
	fl.prov = f.attach(f.reserve(fl, Provider))
	fl.cons = f.attach(f.reserve(fl, Consumer))
	final := expect[*protocol.AdmissionCheck](f.o)
	for _, leg := range []*legEnd{fl.prov, fl.cons} {
		select {
		case <-leg.conn.out:
			t.Fatal("READY before final check")
		default:
		}
	}
	f.clk.Add(100 * time.Millisecond)
	f.o.send(f.reply(final, protocol.ReadinessUnknown, 0))
	provider, consumer := fl.prov.conn.next(t), fl.cons.conn.next(t)
	if provider.data[0] != protocol.FrameReady || consumer.data[0] != protocol.FrameReady || provider.seq > consumer.seq {
		t.Fatal("READY order changed")
	}
	view := f.state(f.id(fl))
	if view.State != protocol.StateOpen || view.ReadinessDecision == nil || view.ReadinessDecision.Readiness != protocol.ReadinessUnknown {
		t.Fatal(view)
	}
	// Existing channel replay survives maintenance and store failure.
	replay, err := f.h.Open(f.alice, f.request("one"))
	if err != nil || !replay.Replay || replay.Response.ChannelID != f.id(fl) {
		t.Fatal(replay, err)
	}
}

func TestAdmissionPendingMergeCancellationAndCredential(t *testing.T) {
	f := newObservedFixture(t, false)
	creatorCtx, cancelCreator := context.WithCancel(context.Background())
	defer cancelCreator()
	first := asyncOpen(f.h, creatorCtx, f.alice, f.request("merge"))
	check := expect[*protocol.AdmissionCheck](f.o)
	other, _ := f.token("alice", protocol.ScopeConsumer, 0)
	waiters := []<-chan openOutcome{}
	for range limits.ObservationMaxWaiters - 1 {
		waiters = append(waiters, asyncOpen(f.h, context.Background(), other, f.request("merge")))
	}
	waitFor(t, "16 waiters", func() bool {
		f.h.mu.Lock()
		defer f.h.mu.Unlock()
		return len(f.h.pending[idemKey{"alice", "merge"}].waiters) == 16
	})
	if _, err := f.h.Open(other, f.request("merge")); codeOf(err) != protocol.CodeResourceExhausted {
		t.Fatal(err)
	}
	different := f.request("merge")
	different.Admission = protocol.AdmissionRequireReady
	if _, err := f.h.Open(other, different); codeOf(err) != protocol.CodeIdempotencyConflict {
		t.Fatal(err)
	}
	cancelCreator()
	if r := opened(t, first); !errors.Is(r.err, context.Canceled) {
		t.Fatal(r)
	}
	f.o.send(f.reply(check, protocol.ReadinessUnknown, 0))
	var channelID string
	for _, done := range waiters {
		out := opened(t, done)
		if out.err != nil || !out.result.Replay {
			t.Fatal(out)
		}
		if channelID != "" && channelID != out.result.Response.ChannelID {
			t.Fatal("two channels")
		}
		channelID = out.result.Response.ChannelID
		if _, err := f.h.ReserveAttach(AttachRequest{ChannelID: channelID, Ticket: out.result.Ticket, Role: Consumer, Principal: other}); codeOf(err) != protocol.CodeTicketInvalid {
			t.Fatal("waiter credential replaced creator", err)
		}
	}
	f.h.mu.Lock()
	defer f.h.mu.Unlock()
	if len(f.h.operations) != 0 || len(f.h.pending) != 0 || len(f.h.admissions) != 0 || f.h.channels[channelID].tokenID != f.alice.TokenID {
		t.Fatal("operation leaked or owner changed")
	}
}

func TestAdmissionLastWaiterCancellationAndLateResult(t *testing.T) {
	f := newObservedFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := asyncOpen(f.h, ctx, f.alice, f.request("retry"))
	old := expect[*protocol.AdmissionCheck](f.o)
	cancel()
	if r := opened(t, done); !errors.Is(r.err, context.Canceled) {
		t.Fatal(r)
	}
	newDone, next := f.preflight("retry")
	if next.CheckID == old.CheckID {
		t.Fatal("check ID reused")
	}
	f.o.send(f.reply(old, protocol.ReadinessUnknown, 0))
	f.o.send(f.reply(next, protocol.ReadinessUnknown, 0))
	out := opened(t, newDone)
	if out.err != nil || f.h.Stats().Channels.Preparing != 1 {
		t.Fatal(out)
	}
	if stats := f.h.metrics.Snapshot(); true {
		n, _ := stats.Counter(metrics.Checks, "cancelled")
		if n != 1 {
			t.Fatal("cancelled check not counted once", n)
		}
	}
}

func TestAdmissionCancellationAlreadyVisibleUnderCoordinator(t *testing.T) {
	f := newObservedFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := asyncOpen(f.h, ctx, f.alice, f.request("cancel-before-create"))
	check := expect[*protocol.AdmissionCheck](f.o)
	f.h.mu.Lock()
	cancel()
	s := f.h.accesses["lab-a"].sess
	f.h.onAdmissionResult(s, f.reply(check, protocol.ReadinessUnknown, 0), f.clk.Now())
	if len(f.h.channels) != 0 || len(f.h.operations) != 0 || len(f.h.admissions) != 0 {
		t.Fatal("cancelled waiter created a Channel while awaiting coordinator")
	}
	f.h.mu.Unlock()
	if out := opened(t, done); !errors.Is(out.err, context.Canceled) {
		t.Fatal(out)
	}
}

func TestAdmissionFailuresAndNoFailedIdempotency(t *testing.T) {
	for _, tc := range []struct {
		name, readiness string
		strict          bool
		want            protocol.Code
	}{
		{"best_not_ready", protocol.ReadinessNotReady, false, protocol.CodeTargetNotReady},
		{"strict_not_ready", protocol.ReadinessNotReady, true, protocol.CodeTargetNotReady},
		{"strict_unknown", protocol.ReadinessUnknown, true, protocol.CodeReadinessRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newObservedFixture(t, true)
			_ = expect[*protocol.ObservationQuery](f.o)
			req := f.request("failed")
			if tc.strict {
				req.Admission = protocol.AdmissionRequireReady
			}
			done := asyncOpen(f.h, context.Background(), f.alice, req)
			check := expect[*protocol.AdmissionCheck](f.o)
			ttl := int64(1000)
			if tc.readiness == protocol.ReadinessUnknown {
				ttl = 0
			}
			f.o.send(f.reply(check, tc.readiness, ttl))
			if out := opened(t, done); codeOf(out.err) != tc.want {
				t.Fatal(out)
			}
			f.h.mu.Lock()
			if len(f.h.pending) != 0 || len(f.h.channels) != 0 {
				t.Fatal("failed preflight retained key/channel")
			}
			f.h.mu.Unlock()
		})
	}
	for _, total := range []bool{false, true} {
		t.Run(map[bool]string{false: "check_timeout", true: "total_timeout"}[total], func(t *testing.T) {
			f := newObservedFixture(t, false)
			if total {
				f.h.oblim.CheckMS = 2000
			}
			done, _ := f.preflight("timeout")
			f.clk.Add(limits.D(f.h.oblim.CheckMS))
			f.h.sweep(f.clk.Now())
			want := protocol.CodeReadinessCheckFailed
			if total {
				want = protocol.CodePrepareTimeout
			}
			if out := opened(t, done); codeOf(out.err) != want {
				t.Fatal(out)
			}
		})
	}
}

func TestAdmissionCreatorAndWaiterRecheck(t *testing.T) {
	t.Run("creator_revoked", func(t *testing.T) {
		f := newObservedFixture(t, false)
		done, _ := f.preflight("revoke")
		f.ok(f.h.RevokeToken(context.Background(), f.admin, f.v(), f.alice.TokenID))
		if out := opened(t, done); codeOf(out.err) != protocol.CodeUnauthenticated {
			t.Fatal(out)
		}
	})
	t.Run("waiter_revoked_after_creation", func(t *testing.T) {
		f := newObservedFixture(t, false)
		first, check := f.preflight("recheck")
		other, _ := f.token("alice", protocol.ScopeConsumer, 0)
		second := asyncOpen(f.h, context.Background(), other, f.request("recheck"))
		waitFor(t, "joined waiter", func() bool {
			f.h.mu.Lock()
			defer f.h.mu.Unlock()
			return len(f.h.pending[idemKey{"alice", "recheck"}].waiters) == 2
		})
		f.h.mu.Lock()
		s := f.h.accesses["lab-a"].sess
		f.h.onAdmissionResult(s, f.reply(check, protocol.ReadinessUnknown, 0), f.clk.Now())
		tok := f.h.tokens[other.TokenID]
		now := f.clk.Now()
		tok.RevokedAt = &now
		f.h.tokens[other.TokenID] = tok
		f.h.mu.Unlock()
		if out := opened(t, first); out.err != nil || out.result.Replay {
			t.Fatal(out)
		}
		if out := opened(t, second); codeOf(out.err) != protocol.CodeUnauthenticated {
			t.Fatal(out)
		}
	})
}

func TestAdmissionTimerUsesCurrentCoordinatorTime(t *testing.T) {
	for _, creatorExpired := range []bool{false, true} {
		t.Run(map[bool]string{false: "total_deadline", true: "creator_expiry_with_live_waiter"}[creatorExpired], func(t *testing.T) {
			f := newObservedFixture(t, false)
			other, _ := f.token("alice", protocol.ScopeConsumer, 0)
			first, _ := f.preflight("late-timer")
			second := asyncOpen(f.h, context.Background(), other, f.request("late-timer"))
			waitFor(t, "joined before clock advance", func() bool {
				f.h.mu.Lock()
				defer f.h.mu.Unlock()
				return len(f.h.pending[idemKey{"alice", "late-timer"}].waiters) == 2
			})
			f.h.mu.Lock()
			want := protocol.CodePrepareTimeout
			if creatorExpired {
				token := f.h.tokens[f.alice.TokenID]
				token.ExpiresAt = f.clk.Now().Add(1500 * time.Millisecond)
				f.h.tokens[f.alice.TokenID] = token
				want = protocol.CodeUnauthenticated
			}
			// Advance only the authoritative clock under the coordinator. The
			// already-created real timer must re-read it, without a sweep/result.
			f.clk.Add(3 * time.Second)
			f.h.mu.Unlock()
			for _, done := range []<-chan openOutcome{first, second} {
				if out := opened(t, done); codeOf(out.err) != want {
					t.Fatalf("late timer: got %v, want %s", out.err, want)
				}
			}
		})
	}
}
