package relay

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func isolatedSession(t *testing.T) (*env, *session, *fakeConn) {
	t.Helper()
	e := newEnv(t)
	conn := newFakeConn()
	ids, err := protocol.NewControlIDs()
	if err != nil {
		t.Fatal(err)
	}
	s := &session{h: e.h, conn: conn, accessID: "lab-a", epoch: 1, gen: 1, version: protocol.ControlSubprotocolV2, ids: ids, out: make(chan ctrlOut, ctrlQueue), telemetry: make(chan ctrlOut, 2)}
	return e, s, conn
}

func TestControlWriterBoundedLanesAndWireSequence(t *testing.T) {
	e, s, conn := isolatedSession(t)
	for range 20 {
		if s.send(&protocol.Heartbeat{LicenseTTLMS: 1000}) == 0 {
			t.Fatal("critical unexpectedly full")
		}
	}
	for range 2 {
		if !s.sendTelemetry(&protocol.ControlError{Code: protocol.CodeInternal}, nil) {
			t.Fatal("telemetry unexpectedly full")
		}
	}
	if s.sendTelemetry(&protocol.ControlError{}, nil) || s.ended {
		t.Fatal("telemetry overflow damaged critical path")
	}
	e.h.loops.Add(1)
	go s.writeLoop()
	for i := 1; i <= 22; i++ {
		out := conn.next(t)
		m, err := protocol.DecodeControl(out.data)
		if err != nil {
			t.Fatal(err)
		}
		if protocol.ControlHeader(m).Seq != uint64(i) {
			t.Fatalf("wire seq reordered at %d: %s", i, out.data)
		}
		_, telemetry := m.(*protocol.ControlError)
		if telemetry != (i == 9 || i == 18) {
			t.Fatalf("not 8:1 at %d: %s", i, out.data)
		}
	}
}

func TestControlWriterCancellationDeadlineAndOverflow(t *testing.T) {
	t.Run("cancelled_critical_still_yields", func(t *testing.T) {
		e, s, conn := isolatedSession(t)
		discarded := 0
		atTelemetry := make(chan int, 1)
		for range 20 {
			s.sendChecked(&protocol.Heartbeat{LicenseTTLMS: 1}, func(time.Time) bool { discarded++; return false })
		}
		s.sendTelemetry(&protocol.ControlError{Code: protocol.CodeInternal}, func(time.Time) bool { atTelemetry <- discarded; return true })
		e.h.loops.Add(1)
		go s.writeLoop()
		_ = conn.next(t)
		if n := <-atTelemetry; n != 8 {
			t.Fatalf("cancelled critical starved telemetry for %d turns", n)
		}
	})
	t.Run("cancel_before_numbering", func(t *testing.T) {
		e, s, conn := isolatedSession(t)
		called := false
		s.sendChecked(&protocol.Heartbeat{LicenseTTLMS: 1000}, func(time.Time) bool {
			called = true
			if e.h.mu.TryLock() {
				e.h.mu.Unlock()
				t.Error("callback outside coordinator")
			}
			return false
		})
		s.send(&protocol.ControlError{Code: protocol.CodeInternal})
		e.h.loops.Add(1)
		go s.writeLoop()
		m, err := protocol.DecodeControl(conn.next(t).data)
		if err != nil || protocol.ControlHeader(m).Seq != 1 || !called {
			t.Fatal(m, err, called)
		}
	})
	t.Run("counter_exhausted", func(t *testing.T) {
		e, s, conn := isolatedSession(t)
		s.seq = math.MaxUint64
		s.send(&protocol.Heartbeat{LicenseTTLMS: 1})
		e.h.loops.Add(1)
		go s.writeLoop()
		conn.waitClosed(t)
		e.h.mu.Lock()
		if s.seq != math.MaxUint64 || !s.ended {
			t.Fatal("seq wrapped")
		}
		e.h.mu.Unlock()
	})
	t.Run("telemetry_write_timeout", func(t *testing.T) {
		e, s, conn := isolatedSession(t)
		block := make(chan struct{})
		defer close(block)
		conn.setGate(block)
		e.h.oblim.WriteMS = 20
		s.sendTelemetry(&protocol.Heartbeat{LicenseTTLMS: 1}, nil)
		e.h.loops.Add(1)
		go s.writeLoop()
		conn.waitClosed(t)
	})
	t.Run("message_bound_is_lane_specific", func(t *testing.T) {
		e, s, conn := isolatedSession(t)
		s.send(&protocol.ControlError{Code: protocol.CodeInternal, Detail: strings.Repeat("x", 5000)})
		s.sendTelemetry(&protocol.Heartbeat{LicenseTTLMS: 1}, nil)
		e.h.oblim.MessageMax = 1
		e.h.loops.Add(1)
		go s.writeLoop()
		m, err := protocol.DecodeControl(conn.next(t).data)
		if err != nil || len(m.(*protocol.ControlError).Detail) != 5000 {
			t.Fatal("critical inherited telemetry limit")
		}
		conn.waitClosed(t)
	})
	t.Run("critical_full_cancels", func(t *testing.T) {
		e, s, _ := isolatedSession(t)
		e.h.mu.Lock()
		for len(s.out) < cap(s.out) {
			s.out <- ctrlOut{message: &protocol.Heartbeat{LicenseTTLMS: 1}}
		}
		if s.sendChecked(&protocol.Heartbeat{LicenseTTLMS: 1}, nil) || !s.ended {
			t.Fatal("full critical queue accepted")
		}
		if s.sendChecked(&protocol.Heartbeat{}, nil) || s.sendTelemetry(&protocol.Heartbeat{}, nil) {
			t.Fatal("ended session accepted work")
		}
		e.h.mu.Unlock()
	})
}

func TestQueuedHeartbeatIsNotAcknowledged(t *testing.T) {
	f := newObservedFixture(t, false)
	f.h.mu.Lock()
	s := f.h.accesses["lab-a"].sess
	// Keep the writer in a first slow Write, then queue a heartbeat behind it.
	gate := make(chan struct{})
	f.o.conn.setGate(gate)
	s.send(&protocol.ControlError{Code: protocol.CodeInternal})
	f.h.mu.Unlock()
	f.h.heartbeatTick()
	f.h.mu.Lock()
	s.hbQueued = true
	s.hbSeq = 0
	f.h.mu.Unlock()
	f.h.heartbeatTick()
	f.h.mu.Lock()
	missed := f.h.accesses["lab-a"].missed
	f.h.mu.Unlock()
	if missed != 1 {
		t.Fatalf("queued heartbeat reset misses: %d", missed)
	}
	close(gate)
}

func TestOpenPendingCapacityAndCallerTimeout(t *testing.T) {
	f := newObservedFixture(t, false)
	f.h.oblim.PendingChecks = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := asyncOpen(f.h, ctx, f.alice, f.request("occupied"))
	_ = expect[*protocol.AdmissionCheck](f.o)
	if _, err := f.h.Open(f.alice, f.request("another")); codeOf(err) != protocol.CodeResourceExhausted {
		t.Fatal(err)
	}
	cancel()
	_ = opened(t, done)
	f.h.oblim.CheckMS = 10
	// No Start loop is running in this unit fixture: OpenContext still enforces
	// its own wait bound when a peer never responds.
	if _, err := f.h.Open(f.alice, f.request("no_response")); codeOf(err) != protocol.CodeReadinessCheckFailed {
		t.Fatal(err)
	}
}
