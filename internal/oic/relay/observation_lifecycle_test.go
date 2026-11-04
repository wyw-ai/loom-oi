package relay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestExpiryFoundDuringReductionPublishesOutsideLock(t *testing.T) {
	e := probeEnv(t)
	a, s := e.agent, currentSession(e)
	a.mu.Lock()
	b := a.bindings["lab-a/http"]
	at := time.Now().Add(-16 * time.Second)
	token, _, err := a.observations.Begin(b, at)
	if err == nil {
		_, err = a.observations.Finish(token, observation.Outcome{Readiness: observation.Ready, Reason: observation.MatchedReady}, at)
	}
	if err == nil {
		a.scheduleExpiryLocked(s)
	}
	snap, _, _ := a.observations.Snapshot(b, time.Now())
	a.mu.Unlock()
	if err != nil || snap.Reason != observation.Expired {
		t.Fatalf("%+v %v", snap, err)
	}
	for {
		notice := expectMsg[*protocol.ObservationChanged](e.ctl)
		if notice.Binding == wireBinding(b) && notice.UpdateSeq == snap.UpdateSeq {
			break
		}
	}
	waitFor(t, "queued dirty publication consumed", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return len(s.pendingDirty) == 0 })
	controlBarrier(t, e)
}

func TestPrepareNeedsAcceptedBindingAndOriginalDeadline(t *testing.T) {
	for _, kind := range []string{"rejected_binding", "elapsed_prepare"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t, nil)
			if kind == "rejected_binding" {
				e.connect(1, 30000, false)
				e.ctl.send(&protocol.RegisterAck{Accepted: []string{"lab-a/closed"}})
				waitFor(t, "partial registration", func() bool { return e.agent.Stats().Registered })
			} else {
				e.connect(1, 30000, true)
			}
			p := e.prepareMsg("lab-a/http", e.rev, 1)
			if kind == "elapsed_prepare" {
				p.PrepareTTLMS = 1000
				e.agent.prepareReceived(context.Background(), currentSession(e), p, time.Now().Add(-2*time.Second))
			} else {
				e.ctl.send(p)
			}
			r := expectMsg[*protocol.PrepareFailed](e.ctl)
			want := protocol.CodeReadinessCheckFailed
			if kind == "elapsed_prepare" {
				want = protocol.CodePrepareTimeout
			}
			if r.ChannelID != p.ChannelID || r.Code != want {
				t.Fatal(r)
			}
			select {
			case <-e.tgt.conns:
				t.Fatal("invalid prepare dialed target")
			default:
			}
			if e.agent.Stats().Channels != 0 {
				t.Fatal("invalid prepare kept reservation")
			}
		})
	}
}

func TestUnacknowledgedRegisterBoundAndOldSession(t *testing.T) {
	e := newEnv(t, func(m map[string]any) { m["limits"].(map[string]any)["write_queue"] = 1 })
	e.connect(1, 30000, false)
	old := currentSession(e)
	if _, err := e.agent.sendRegister(context.Background(), old); protocol.CodeOf(err) != protocol.CodeResourceExhausted {
		t.Fatalf("unbounded pending register: %v", err)
	}
	e.connect(2, 30000, true)
	if _, err := e.agent.sendRegister(context.Background(), old); !errors.Is(err, context.Canceled) {
		t.Fatalf("old session register: %v", err)
	}
	if err := e.agent.reconcileProbes(old); err != nil {
		t.Fatal(err)
	}
	e.agent.mu.Lock()
	e.agent.scheduleExpiryLocked(old)
	e.agent.mu.Unlock()
	controlBarrier(t, e)
	if d := e.agent.Stats().Observation; d.ProbeJobs+d.ProbeActive+d.ProbeQueued != 0 {
		t.Fatalf("old session recreated probe jobs %+v", d)
	}
}

func TestControlInputLimitsAndSequence(t *testing.T) {
	for _, kind := range []string{"duplicate_sequence", "invalid_v2", "oversize_query", "extra_register_ack"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t, func(m map[string]any) {
				if kind == "oversize_query" {
					m["observation_limits"] = map[string]any{"message_max": 128}
				}
			})
			e.connect(1, 30000, kind != "oversize_query")
			old := currentSession(e)
			switch kind {
			case "duplicate_sequence":
				if err := e.ctl.c.Write(context.Background(), websocket.MessageText, protocol.EncodeControl(&protocol.Heartbeat{LicenseTTLMS: 30000}, e.ctl.seq)); err != nil {
					t.Fatal(err)
				}
			case "invalid_v2":
				e.ctl.send(&protocol.ObservationQuery{QueryID: "invalid", Binding: currentBinding(e), DeadlineMS: 1000})
			case "oversize_query":
				e.ctl.send(&protocol.ObservationQuery{QueryID: newCheckID(), Binding: currentBinding(e), DeadlineMS: 1000})
			case "extra_register_ack":
				e.ctl.send(&protocol.RegisterAck{})
			}
			if kind == "duplicate_sequence" || kind == "invalid_v2" {
				if r := expectMsg[*protocol.ControlError](e.ctl); r.Code != protocol.CodeProtocolError {
					t.Fatal(r)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), wait)
			defer cancel()
			for {
				_, _, err := e.ctl.c.Read(ctx)
				if err != nil {
					if ctx.Err() != nil {
						t.Fatal("invalid control did not close")
					}
					break
				}
			}
			waitFor(t, "old session retired", func() bool { return currentSession(e) != old })
		})
	}
}

func TestEarlyLicenseTimerDoesNotMoveDeadline(t *testing.T) {
	e := newEnv(t, nil)
	e.connect(1, 30000, true)
	before := e.agent.license.Load().deadline
	e.agent.expire()
	if !e.agent.license.Load().deadline.Equal(before) || e.agent.allowFor(1)() != "" {
		t.Fatal("early/stale timer changed license")
	}
	controlBarrier(t, e)
}

func TestControlWriterBoundAndCancelledCriticalSlot(t *testing.T) {
	w, p := writerPair(t, 2, 1, 1, 4096)
	release, first := blockWriter(t, w)
	build := func(time.Time) (protocol.Message, error) { return &protocol.HeartbeatAck{AckSeq: 8}, nil }
	if _, err := w.offer("dirty:first", context.Background(), build); err != nil {
		t.Fatal(err)
	}
	if _, err := w.offer("dirty:excess", context.Background(), build); protocol.CodeOf(err) != protocol.CodeResourceExhausted {
		t.Fatalf("unbounded dirty lane: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.send(ctx, &protocol.Hello{OICVersion: "must not be sent"}) }()
	waitFor(t, "critical queued", func() bool { return len(w.critical) == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	expectMsg[*protocol.Hello](p)
	expectMsg[*protocol.HeartbeatAck](p)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	w.stop()
	if err := w.send(context.Background(), &protocol.Hello{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed writer accepted critical: %v", err)
	}
}
