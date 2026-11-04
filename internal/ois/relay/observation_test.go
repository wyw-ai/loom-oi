package relay

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestObservationDirtyNotificationAndSessionFence(t *testing.T) {
	f := newObservedFixture(t, true)
	q := expect[*protocol.ObservationQuery](f.o)
	for _, seq := range []uint64{2, 3} {
		f.o.send(&protocol.ObservationChanged{Binding: f.binding, UpdateSeq: seq})
	}
	f.o.send(snapshot(q, 1, 1, protocol.ReadinessReady, 1000, f.clk.Now()))
	next := expect[*protocol.ObservationQuery](f.o)
	if next.QueryID == q.QueryID {
		t.Fatal("dirty work reused query identity")
	}
	if count, _ := f.h.metrics.Snapshot().Counter(metrics.DirtyCoalesced, ""); count != 1 {
		t.Fatal("dirty was not merged", count)
	}
	f.o.send(snapshot(next, 3, 1, protocol.ReadinessReady, 900, f.clk.Now()))
	waitFor(t, "new directory update", func() bool {
		f.h.mu.Lock()
		defer f.h.mu.Unlock()
		return f.h.exposures[exp1].observation.record != nil && f.h.exposures[exp1].observation.record.UpdateSeq == 3
	})
	for _, seq := range []uint64{2, 3} {
		f.o.send(&protocol.ObservationChanged{Binding: f.binding, UpdateSeq: seq})
	}
	old := f.binding
	old.ObservationRevision = "ob_99"
	f.o.send(&protocol.ObservationChanged{Binding: old, UpdateSeq: 10})
	f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{f.entry}})
	expect[*protocol.RegisterAck](f.o)
	if got := f.h.Stats().Observation; got.PendingQueries != 0 {
		t.Fatal("old notification scheduled fresh work", got)
	}
	foreign := f.binding
	foreign.AccessID = "lab-b"
	foreign.ExposureID = "lab-b/http"
	f.o.send(&protocol.ObservationChanged{Binding: foreign, UpdateSeq: 4})
	f.o.conn.waitClosed(t)
	f.o.waitDone()
	if view := f.view(); view.Readiness != protocol.ReadinessUnknown || view.ReadinessView.Reason != "source_invalid" {
		t.Fatal(view)
	}
}

func TestObservationQueryCancellationAndLateExpiry(t *testing.T) {
	for _, kind := range []string{"expired_reply", "lower_sample", "sample_time_conflict", "invalidated_binding"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservedFixture(t, true)
			q := expect[*protocol.ObservationQuery](f.o)
			at := f.clk.Now()
			if kind == "expired_reply" {
				f.clk.Add(time.Second)
				f.applySnapshot(snapshot(q, 1, 1, protocol.ReadinessReady, 1000, at))
				if view := f.view(); view.Readiness != protocol.ReadinessUnknown {
					t.Fatal(view)
				}
				return
			}
			if kind == "invalidated_binding" {
				f.h.mu.Lock()
				b := *f.h.exposures[exp1].binding
				b.ObservationRevision = "ob_2"
				f.h.exposures[exp1].binding = &b
				f.h.mu.Unlock()
				f.applySnapshot(snapshot(q, 1, 1, protocol.ReadinessReady, 1000, at))
				if f.h.Stats().Observation.PendingQueries != 0 {
					t.Fatal("invalidated query survived")
				}
				return
			}
			f.applySnapshot(snapshot(q, 3, 2, protocol.ReadinessReady, 1000, at))
			f.h.mu.Lock()
			f.h.exposures[exp1].observation.dirty = true
			f.h.pumpQueries(f.clk.Now())
			f.h.mu.Unlock()
			next := expect[*protocol.ObservationQuery](f.o)
			m := snapshot(next, 4, 2, protocol.ReadinessReady, 900, at)
			if kind == "lower_sample" {
				m.SampleSeq = ptr(uint64(1))
				f.applySnapshot(m)
				if *f.view().ReadinessView.SampleSeq != 2 {
					t.Fatal("sample rolled backwards")
				}
			} else {
				shift := at.Add(time.Nanosecond)
				m.ObservedAt = &shift
				f.applySnapshot(m)
				f.o.conn.waitClosed(t)
			}
		})
	}
}

func TestObservationIdentityCapacityAndQueuedExpiry(t *testing.T) {
	t.Run("deleted_identity_capacity", func(t *testing.T) {
		f := newObservedFixture(t, false, func(l *limits.Limits) { l.MaxExposures = 1 })
		f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{}})
		expect[*protocol.RegisterAck](f.o)
		x := f.entry
		b := f.binding
		b.ExposureID = "lab-a/new"
		x.ExposureID = b.ExposureID
		x.Binding = &b
		f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{x}})
		msg := expect[*protocol.ControlError](f.o)
		if msg.Code != protocol.CodeResourceExhausted {
			t.Fatal(msg)
		}
		f.o.waitDone()
	})
	t.Run("queued_query_original_deadline", func(t *testing.T) {
		f := newObservedFixture(t, true)
		_ = expect[*protocol.ObservationQuery](f.o)
		f.h.mu.Lock()
		s := f.h.accesses["lab-a"].sess
		// Stop all active queries, then hold the writer in a critical Write.
		for _, q := range f.h.queries {
			f.h.dropQuery(q)
		}
		gate := make(chan struct{})
		f.o.conn.setGate(gate)
		s.send(&protocol.Heartbeat{LicenseTTLMS: 1000})
		f.h.exposures[exp1].observation.dirty = true
		f.h.pumpQueries(f.clk.Now())
		f.clk.Add(time.Second)
		f.h.mu.Unlock()
		close(gate)
		waitFor(t, "queued query expired", func() bool { return f.h.Stats().Observation.PendingQueries == 0 })
	})
}

func TestDirectoryQuerySlotsAndRoundRobin(t *testing.T) {
	f := newObservedFixture(t, false)
	var entries []protocol.RegisterExposure
	for i := range 12 {
		x := expo(fmt.Sprintf("lab-a/e%02d", i), rv(1))
		b := f.binding
		b.ExposureID = x.ExposureID
		b.SourceID = "local"
		b.ContractVersion = 1
		b.ContractDigest = strings.Repeat("b", 64)
		x.Binding = &b
		x.ProbeSupport = protocol.ProbeSupported
		entries = append(entries, x)
	}
	f.o.send(&protocol.Register{Exposures: entries})
	expect[*protocol.RegisterAck](f.o)
	queries := make([]*protocol.ObservationQuery, 8)
	seen := map[string]bool{}
	ids := map[string]bool{}
	for i := range queries {
		queries[i] = expect[*protocol.ObservationQuery](f.o)
		seen[queries[i].Binding.ExposureID] = true
		ids[queries[i].QueryID] = true
	}
	f.h.mu.Lock()
	if len(f.h.queries) != 8 {
		t.Fatal("query capacity")
	}
	f.h.mu.Unlock()
	for i := range 4 {
		q := queries[i]
		f.applySnapshot(&protocol.ObservationSnapshot{QueryID: q.QueryID, Binding: q.Binding, UpdateSeq: 1, Attempt: protocol.ObservationAttempt{State: "idle"}, Readiness: protocol.ReadinessUnknown, Reason: "no_sample"})
		next := expect[*protocol.ObservationQuery](f.o)
		if seen[next.Binding.ExposureID] {
			t.Fatal("another exposure starved")
		}
		seen[next.Binding.ExposureID] = true
	}
	if len(seen) != 12 {
		t.Fatal(seen)
	}
	f.clk.Add(time.Second)
	f.h.sweep(f.clk.Now())
	f.h.mu.Lock()
	for id := range f.h.queries {
		if ids[id] {
			t.Fatal("query deadline renewed original ID")
		}
	}
	f.h.mu.Unlock()
	// Old query replies are harmless even with a formerly valid binding.
	f.applySnapshot(snapshot(queries[0], 100, 20, protocol.ReadinessReady, 1000, f.clk.Now()))
}

func snapshot(q *protocol.ObservationQuery, update, sample uint64, state string, remaining int64, at time.Time) *protocol.ObservationSnapshot {
	m := &protocol.ObservationSnapshot{QueryID: q.QueryID, Binding: q.Binding, UpdateSeq: update, Readiness: state, RemainingMS: remaining, Attempt: protocol.ObservationAttempt{Seq: 1, State: "idle", Outcome: "matched_ready_status", DurationMS: 10}}
	if state == protocol.ReadinessUnknown {
		m.Reason = "expired"
	} else {
		m.SampleSeq = ptr(sample)
		m.ObservedAt = &at
		m.Reason = "matched_ready_status"
		if state == protocol.ReadinessNotReady {
			m.Reason = "matched_not_ready_status"
			m.Attempt.Outcome = m.Reason
		}
	}
	return m
}
func (f *observedFixture) view() protocol.ExposureView {
	f.t.Helper()
	page, err := f.h.Discover(f.alice, 0, "")
	if err != nil || len(page.Items) != 1 {
		f.t.Fatal(page, err)
	}
	return page.Items[0]
}
func (f *observedFixture) applySnapshot(m *protocol.ObservationSnapshot) {
	f.t.Helper()
	f.h.mu.Lock()
	s := f.h.accesses["lab-a"].sess
	f.h.onObservationSnapshot(s, m, f.clk.Now())
	f.h.mu.Unlock()
}

func TestDirectoryRTTExpiryAndHistory(t *testing.T) {
	f := newObservedFixture(t, true)
	q := expect[*protocol.ObservationQuery](f.o)
	at := f.clk.Now()
	f.clk.Add(200 * time.Millisecond)
	f.applySnapshot(snapshot(q, 1, 1, protocol.ReadinessReady, 1000, at))
	view := f.view()
	if view.Readiness != protocol.ReadinessReady || view.ReadinessView.RemainingMS != 800 || view.ReadinessView.Authority != "directory_snapshot" {
		t.Fatal(view)
	}
	f.clk.Add(100 * time.Millisecond)
	f.h.mu.Lock()
	f.h.exposures[exp1].observation.dirty = true
	f.h.pumpQueries(f.clk.Now())
	f.h.mu.Unlock()
	next := expect[*protocol.ObservationQuery](f.o)
	// A fresh query of the same update/sample can only tighten the previous
	// conservative bound, even if this round trip happens to be shorter.
	f.clk.Add(10 * time.Millisecond)
	f.applySnapshot(snapshot(next, 1, 1, protocol.ReadinessReady, 900, at))
	if got := f.view().ReadinessView.RemainingMS; got != 690 {
		t.Fatalf("duplicate renewed TTL: %d", got)
	}
	f.clk.Add(690 * time.Millisecond)
	view = f.view()
	if view.Readiness != protocol.ReadinessUnknown || view.ReadinessView.Reason != "expired" || view.ReadinessView.LastReported == nil || view.ReadinessView.LastReported.SampleSeq != 1 {
		t.Fatal(view)
	}
	f.h.mu.Lock()
	f.h.exposures[exp1].observation.dirty = true
	f.h.pumpQueries(f.clk.Now())
	f.h.mu.Unlock()
	next = expect[*protocol.ObservationQuery](f.o)
	m := snapshot(next, 2, 0, protocol.ReadinessUnknown, 0, at)
	m.Attempt = protocol.ObservationAttempt{Seq: 2, State: "idle", Outcome: "timeout", DurationMS: 200}
	f.applySnapshot(m)
	view = f.view()
	if view.ReadinessView.LastAttempt.Outcome != "timeout" || view.ReadinessView.LastReported == nil {
		t.Fatal("attempt/history overwritten", view)
	}
	// No discover permission leaks the observation details.
	page, err := f.h.Discover(Principal{Subject: "unrelated"}, 0, "")
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}

func TestDirectoryOldUpdateAndConflictingSample(t *testing.T) {
	for _, kind := range []string{"lower_update", "same_update_conflict", "same_sample_conflict", "binding_mismatch", "remaining_overflow", "attempt_rollback"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservedFixture(t, true)
			q := expect[*protocol.ObservationQuery](f.o)
			at := f.clk.Now()
			first := snapshot(q, 3, 2, protocol.ReadinessReady, 1500, at)
			first.Attempt.Seq = 3
			f.applySnapshot(first)
			f.h.mu.Lock()
			f.h.exposures[exp1].observation.dirty = true
			f.h.pumpQueries(f.clk.Now())
			f.h.mu.Unlock()
			next := expect[*protocol.ObservationQuery](f.o)
			m := snapshot(next, 4, 2, protocol.ReadinessReady, 1400, at)
			m.Attempt.Seq = 4
			switch kind {
			case "lower_update":
				m.UpdateSeq = 2
				m.SampleSeq = ptr(uint64(1))
			case "same_update_conflict":
				m.UpdateSeq = 3
			case "same_sample_conflict":
				m.Readiness = protocol.ReadinessNotReady
				m.Reason = "matched_not_ready_status"
			case "binding_mismatch":
				m.Binding.ObservationRevision = "ob_2"
			case "remaining_overflow":
				m.RemainingMS = 1 << 62
			case "attempt_rollback":
				m.Attempt.Seq = 2
			}
			f.applySnapshot(m)
			if kind == "lower_update" {
				view := f.view()
				if view.Readiness != protocol.ReadinessReady || *view.ReadinessView.SampleSeq != 2 || view.ReadinessView.LastAttempt.Seq != 3 {
					t.Fatal(view)
				}
			} else {
				f.o.conn.waitClosed(t)
				f.o.waitDone()
			}
		})
	}
}

func TestFinalDecisionDoesNotUseNewDirectoryHealth(t *testing.T) {
	f := newObservedFixture(t, true)
	q := expect[*protocol.ObservationQuery](f.o)
	fl, final := f.prepared(t, "snapshot", protocol.ReadinessReady)
	// OIS receives a new negative directory snapshot before the independently
	// formed final reply. Commit-Snapshot keeps the final decision authoritative.
	f.applySnapshot(snapshot(q, 2, 2, protocol.ReadinessNotReady, 1500, f.clk.Now()))
	if f.view().Readiness != protocol.ReadinessNotReady {
		t.Fatal("negative update not applied")
	}
	f.o.send(f.reply(final, protocol.ReadinessReady, 1000))
	fl.prov.expectType(protocol.FrameReady)
	fl.cons.expectType(protocol.FrameReady)
	if view := f.state(f.id(fl)); view.State != protocol.StateOpen || view.ReadinessDecision.Readiness != protocol.ReadinessReady {
		t.Fatal(view)
	}
}

func TestFinalTTLDeadlineAndPendingInvalidation(t *testing.T) {
	for _, kind := range []string{"strict_ttl", "check_deadline", "total_deadline", "mode_loosen", "wrong_binding", "wrong_channel", "late_local_cancel", "lost_session"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservedFixture(t, true)
			_ = expect[*protocol.ObservationQuery](f.o)
			if kind == "strict_ttl" {
				f.entry.AdmissionMode = protocol.AdmissionRequireReady
				f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{f.entry}})
				expect[*protocol.RegisterAck](f.o)
			}
			fl, final := f.prepared(t, "final", protocol.ReadinessReady)
			reply := f.reply(final, protocol.ReadinessReady, 1000)
			want := protocol.CodeReadinessCheckFailed
			switch kind {
			case "strict_ttl":
				f.clk.Add(200 * time.Millisecond)
				reply.RemainingMS = 200
				want = protocol.CodeReadinessRequired
			case "check_deadline":
				f.clk.Add(time.Second)
			case "total_deadline":
				f.clk.Add(2 * time.Second)
				want = protocol.CodePrepareTimeout
			case "mode_loosen":
				f.h.mu.Lock()
				f.h.channels[f.id(fl)].check.mode = protocol.AdmissionRequireReady
				f.h.mu.Unlock()
				want = protocol.CodeAccessOffline
			case "wrong_binding":
				reply.Binding.ObservationRevision = "ob_2"
				want = protocol.CodeAccessOffline
			case "wrong_channel":
				reply.ChannelID = protocol.NewID("ch_")
				want = protocol.CodeAccessOffline
			case "lost_session":
				_ = f.o.conn.CloseNow()
				want = protocol.CodeAccessOffline
			}
			if kind != "lost_session" {
				f.o.send(reply)
			}
			if kind == "late_local_cancel" {
				fl.prov.expectType(protocol.FrameReady)
				fl.cons.expectType(protocol.FrameReady)
				f.o.send(&protocol.PrepareFailed{ChannelID: f.id(fl), Code: protocol.CodeReadinessCheckFailed, Reason: "observation_changed"})
			}
			fl.cons.expectAbort(want)
			if kind == "strict_ttl" {
				if decision := f.state(f.id(fl)).ReadinessDecision; decision == nil || decision.Reason != "expired" {
					t.Fatal("lost precise strict TTL cause", decision)
				}
			}
			f.h.mu.Lock()
			if len(f.h.admissions) != 0 {
				t.Fatal("final slot leaked")
			}
			f.h.mu.Unlock()
		})
	}
}

func TestObservationRevisionChangesCancelOnlyPreparing(t *testing.T) {
	f := newObservedFixture(t, false)
	running, final := f.prepared(t, "running", protocol.ReadinessUnknown)
	f.o.send(f.reply(final, protocol.ReadinessUnknown, 0))
	running.prov.expectType(protocol.FrameReady)
	running.cons.expectType(protocol.FrameReady)
	pending, check := f.preflight("pending")
	changed := f.entry
	b := f.binding
	b.ObservationRevision = "ob_2"
	b.SourceID = "local"
	b.ContractVersion = 1
	b.ContractDigest = strings.Repeat("c", 64)
	changed.Binding = &b
	changed.ProbeSupport = protocol.ProbeSupported
	f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{changed}})
	expect[*protocol.RegisterAck](f.o)
	if out := opened(t, pending); codeOf(out.err) != protocol.CodeReadinessCheckFailed {
		t.Fatal(out)
	}
	if f.state(f.id(running)).State != protocol.StateOpen {
		t.Fatal("probe-only change killed Running channel")
	}
	f.o.send(f.reply(check, protocol.ReadinessUnknown, 0)) // obsolete result cannot resurrect old operation
	_ = expect[*protocol.ObservationQuery](f.o)
	f.h.mu.Lock()
	if len(f.h.pending) != 0 || len(f.h.operations) != 0 {
		t.Fatal("cancelled operation survived")
	}
	f.h.mu.Unlock()
}

func TestFinalResultAfterObservationRegisterCannotCommit(t *testing.T) {
	f := newObservedFixture(t, false)
	fl, final := f.prepared(t, "register-before-final", protocol.ReadinessUnknown)
	positive := f.reply(final, protocol.ReadinessUnknown, 0)
	changed := f.entry
	b := f.binding
	b.ObservationRevision = "ob_2"
	b.SourceID, b.ContractVersion = "local", 1
	b.ContractDigest = strings.Repeat("c", 64)
	changed.Binding, changed.ProbeSupport = &b, protocol.ProbeSupported
	// This is the coordinator ordering, independent of ordered wire delivery:
	// apply the registration, then consume a previously formed final positive.
	f.h.mu.Lock()
	s := f.h.accesses["lab-a"].sess
	f.h.onRegister(s, &protocol.Register{Exposures: []protocol.RegisterExposure{changed}}, f.clk.Now())
	f.h.onAdmissionResult(s, positive, f.clk.Now())
	if ch := f.h.channels[f.id(fl)]; ch.state != stateClosed || ch.reason != protocol.CodeReadinessCheckFailed || len(f.h.admissions) != 0 {
		t.Fatalf("late final revived cancelled preparation: %+v", ch)
	}
	f.h.mu.Unlock()
	for _, leg := range []*legEnd{fl.prov, fl.cons} {
		if frame := leg.expectType(protocol.FrameAbort); frame.Reason != protocol.CodeReadinessCheckFailed {
			t.Fatal(frame)
		}
	}
}

func TestObservationRegistrationVersionsAndIdentityBounds(t *testing.T) {
	t.Run("unknown_contract_rejects_only_entry", func(t *testing.T) {
		f := newObservedFixture(t, false)
		bad := f.entry
		bb := f.binding
		bb.SourceID = "local"
		bb.ContractVersion = 2
		bb.ContractDigest = strings.Repeat("c", 64)
		bb.ObservationRevision = "ob_2"
		bad.Binding = &bb
		bad.ProbeSupport = protocol.ProbeSupported
		good := expo("lab-a/other", rv(2))
		gb := f.binding
		gb.ExposureID = good.ExposureID
		gb.Revision = good.Revision
		good.Binding = &gb
		good.ProbeSupport = protocol.ProbeUnsupported
		f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{bad, good}})
		ack := expect[*protocol.RegisterAck](f.o)
		if len(ack.Accepted) != 1 || ack.Accepted[0] != good.ExposureID || len(ack.Rejected) != 1 || ack.Rejected[0].Code != protocol.CodeVersionUnsupported {
			t.Fatal(ack)
		}
	})
	for _, kind := range []string{"digest_without_revision", "wrong_access", "deleted_identity_reused", "backwards_revision"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservedFixture(t, false)
			x := f.entry
			b := f.binding
			switch kind {
			case "digest_without_revision":
				b.SourceID = "local"
				b.ContractVersion = 1
				b.ContractDigest = strings.Repeat("c", 64)
				x.ProbeSupport = protocol.ProbeSupported
			case "wrong_access":
				b.Epoch++
			case "deleted_identity_reused":
				f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{}})
				expect[*protocol.RegisterAck](f.o)
			case "backwards_revision":
				next := f.entry
				nb := f.binding
				nb.ObservationRevision = "ob_2"
				next.Binding = &nb
				f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{next}})
				expect[*protocol.RegisterAck](f.o)
			}
			x.Binding = &b
			f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{x}})
			f.o.conn.waitClosed(t)
			f.o.waitDone()
		})
	}
}

func TestPreflightAppliedChangesPrecedeLatePositive(t *testing.T) {
	for _, kind := range []string{"policy", "revision", "offline", "maintenance", "store", "shutdown"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservedFixture(t, false)
			done, check := f.preflight("priority")
			want := protocol.CodeReadinessCheckFailed
			f.h.mu.Lock()
			s := f.h.accesses["lab-a"].sess
			switch kind {
			case "policy":
				f.h.policyList = nil
				want = protocol.CodeNotFound
			case "revision":
				f.h.exposures[exp1].rec.Revision = rv(2)
				want = protocol.CodeRevisionMismatch
			case "offline":
				f.h.accesses["lab-a"].sess = nil
				want = protocol.CodeAccessOffline
			case "maintenance":
				f.h.meta.Maintenance = true
				want = protocol.CodeMaintenance
			case "store":
				f.h.storeOK = false
				want = protocol.CodeStoreUnavailable
			case "shutdown":
				f.h.shutting = true
				want = protocol.CodeShutdown
			}
			f.h.onAdmissionResult(s, f.reply(check, protocol.ReadinessUnknown, 0), f.clk.Now())
			if kind == "offline" {
				f.h.accesses["lab-a"].sess = s
			}
			f.h.mu.Unlock()
			if out := opened(t, done); codeOf(out.err) != want {
				t.Fatal(out)
			}
		})
	}
	// Already cancelled callers never allocate a pending operation.
	f := newObservedFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.h.OpenContext(ctx, f.alice, f.request("never")); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestReadinessTerminalReasonIsPreciseAndFirstWins(t *testing.T) {
	for _, kind := range []string{"timeout", "prepare_failed", "late_local_cancel"} {
		t.Run(kind, func(t *testing.T) {
			f := newObservedFixture(t, false)
			fl, final := f.prepared(t, "reason", protocol.ReadinessUnknown)
			want := "observation_changed"
			if kind == "late_local_cancel" {
				f.o.send(f.reply(final, protocol.ReadinessUnknown, 0))
				fl.prov.expectType(protocol.FrameReady)
				fl.cons.expectType(protocol.FrameReady)
			}
			if kind == "timeout" {
				want = "timeout"
				f.clk.Add(time.Second)
				f.h.sweep(f.clk.Now())
			} else {
				f.o.send(&protocol.PrepareFailed{ChannelID: f.id(fl), Code: protocol.CodeReadinessCheckFailed, Reason: want})
			}
			fl.cons.expectAbort(protocol.CodeReadinessCheckFailed)
			got := f.state(f.id(fl))
			if got.ReadinessDecision == nil || got.ReadinessDecision.Reason != want {
				t.Fatalf("lost precise terminal reason: %+v", got.ReadinessDecision)
			}
			f.h.mu.Lock()
			s := f.h.accesses["lab-a"].sess
			f.h.onPrepareFailed(s, &protocol.PrepareFailed{ChannelID: f.id(fl), Code: protocol.CodeReadinessCheckFailed, Reason: "unavailable"}, f.clk.Now())
			if ch := f.h.channels[f.id(fl)]; ch.decision.Reason != want || len(f.h.admissions) != 0 {
				t.Fatal("late terminal response rewrote first reason")
			}
			f.h.mu.Unlock()
			if kind == "timeout" {
				n, _ := f.h.metrics.Snapshot().Counter(metrics.Checks, "timeout")
				if n != 1 {
					t.Fatal("known timeout was counted as unavailable", n)
				}
			}
		})
	}
}
