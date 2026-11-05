package relay

import (
	"errors"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestDataOwnersCoverBothDirectionsAndCleanup(t *testing.T) {
	for _, interrupt := range []bool{false, true} {
		name := "written"
		if interrupt {
			name = "peer_closed"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			fl := f.openFlow()
			cg, pg := make(chan struct{}), make(chan struct{})
			fl.cons.conn.setGate(cg)
			fl.prov.conn.setGate(pg)
			fl.cons.data("consumer")
			fl.prov.data("provider")
			waitFor(t, "both directions own both DATA slots", func() bool {
				c, p := fl.cons.conn.control.Snapshot(), fl.prov.conn.control.Snapshot()
				return c.Forwarding && c.PendingData && p.Forwarding && p.PendingData
			})
			if got := f.h.inflight.Load(); got != int64(len("consumerprovider")) {
				t.Fatal(got)
			}
			if interrupt {
				_ = fl.cons.conn.CloseNow()
				close(pg)
				fl.prov.waitDone()
				fl.cons.waitDone()
			} else {
				close(cg)
				close(pg)
				if got := fl.cons.expectType(protocol.FrameData); string(got.Data) != "provider" {
					t.Fatal(string(got.Data))
				}
				if got := fl.prov.expectType(protocol.FrameData); string(got.Data) != "consumer" {
					t.Fatal(string(got.Data))
				}
			}
			waitFor(t, "all DATA owners released", func() bool {
				c, p := fl.cons.conn.control.Snapshot(), fl.prov.conn.control.Snapshot()
				return !c.Forwarding && !c.PendingData && !p.Forwarding && !p.PendingData && f.h.inflight.Load() == 0
			})
		})
	}
}

func TestCompletedDrainsFINBeforeStoppingBlockedControl(t *testing.T) {
	f := newFixture(t, func(l *limits.Limits) { l.PingMS = 250 })
	fl := f.openFlow()
	blocked := make(chan struct{})
	fl.cons.conn.controlGate.Store(&blocked)
	waitFor(t, "control write claimed before DATA", func() bool { return fl.cons.conn.control.Snapshot().Wire == "ping" })
	gate := make(chan struct{})
	fl.cons.conn.setGate(gate)
	fl.prov.data("last bytes")
	waitFor(t, "destination owns DATA", func() bool { return fl.cons.conn.control.Snapshot().PendingData })
	fl.cons.fin()
	fl.prov.expectType(protocol.FrameFin)
	gate <- struct{}{}
	if got := fl.cons.expectType(protocol.FrameData); string(got.Data) != "last bytes" {
		t.Fatal(string(got.Data))
	}
	fl.prov.fin()
	f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeCompleted)
	if s := fl.cons.conn.control.Snapshot(); s.Stopped {
		t.Fatal("Completed stopped transport before queued FIN", s)
	}
	if fl.cons.conn.isClosed() {
		t.Fatal("Completed closed before FIN drain")
	}
	gate <- struct{}{}
	fl.cons.expectType(protocol.FrameFin)
	fl.cons.waitDone()
	fl.prov.waitDone()
	if s := fl.cons.conn.control.Snapshot(); !s.Stopped || !s.WorkerDone || s.Forwarding || s.PendingData {
		t.Fatal(s)
	}
	if v := f.state(f.id(fl)); v.CloseReason != protocol.CodeCompleted {
		t.Fatal(v)
	}
}

func TestControlWriteFailurePreservesEarlierCompleted(t *testing.T) {
	f := newFixture(t, func(l *limits.Limits) { l.PingMS = 250 })
	fl := f.openFlow()
	blocked := make(chan struct{})
	fl.cons.conn.controlGate.Store(&blocked)
	waitFor(t, "real control write claimed", func() bool { return fl.cons.conn.control.Snapshot().Wire == "ping" })
	gate := make(chan struct{})
	fl.cons.conn.setGate(gate)
	fl.cons.fin()
	fl.prov.expectType(protocol.FrameFin)
	fl.prov.fin()
	f.waitState(f.id(fl), protocol.StateClosed, protocol.CodeCompleted)
	if s := fl.cons.conn.control.Snapshot(); s.Stopped || s.Wire != "ping" {
		t.Fatal("business completion stole control owner", s)
	}
	failed := errors.New("actual control write failure")
	fl.cons.conn.controlErr.Store(&failed)
	close(blocked)
	fl.cons.waitDone()
	fl.prov.waitDone()
	if v := f.state(f.id(fl)); v.CloseReason != protocol.CodeCompleted {
		t.Fatal("later transport error changed first reason", v)
	}
	if s := fl.cons.conn.control.Snapshot(); !s.WorkerDone || !s.Stopped {
		t.Fatal(s)
	}
}
