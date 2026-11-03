package relay

import (
	"context"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestStartLoopsAndShutdown(t *testing.T) {
	f := newFixture(t, func(l *limits.Limits) { l.HeartbeatMS, l.StoreProbeMS, l.OfflineMisses = 20, 20, 1000 })
	f.h.Start()
	hb := heartbeat(t, f.o)
	f.o.send(&protocol.HeartbeatAck{AckSeq: hb.Seq})
	prep := f.prepare()
	f.clk.Add(time.Second)
	fl := f.openFlow()
	f.clk.Add(time.Second) // the janitor sees the prepare deadline, not idleness
	waitFor(t, "janitor", func() bool { return f.state(f.id(prep)).CloseReason == protocol.CodePrepareTimeout })
	f.st.set(func(s *fakeStore) { s.healthErr = errUnavailable })
	waitFor(t, "probe down", func() bool { return !f.h.ConfigView().StoreHealthy })
	f.st.set(func(s *fakeStore) { s.healthErr = nil })
	waitFor(t, "probe up", func() bool { return f.h.ConfigView().StoreHealthy })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		f.h.Shutdown(ctx)
		close(done)
	}()
	fl.cons.expectAbort(protocol.CodeShutdown)
	fl.prov.expectAbort(protocol.CodeShutdown)
	f.o.conn.waitClosed(t)
	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("shutdown hung")
	}
	if _, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}); codeOf(err) != protocol.CodeShutdown {
		t.Fatal(err)
	}
}

func TestShutdownDropsStuckConnections(t *testing.T) {
	f := newFixture(t)
	fl := f.openFlow()
	block := make(chan struct{})
	defer close(block)
	fl.cons.conn.setGate(block)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	f.h.Shutdown(ctx)
	fl.cons.waitDone()
	fl.prov.waitDone()
	f.o.waitDone()
}

func TestPersistRegistry(t *testing.T) {
	f := newFixture(t)
	f.st.set(func(s *fakeStore) { s.upsertErr = errUnavailable })
	if f.h.persist() {
		t.Fatal("persist ignored the failure")
	}
	if f.h.ConfigView().StoreHealthy {
		t.Fatal("store still healthy")
	}
	// Registration is accepted in memory while the store is down.
	f.o.send(&protocol.Register{Exposures: []protocol.RegisterExposure{expo(exp1, f.rev), expo("lab-a/two", rv(2))}})
	expect[*protocol.RegisterAck](f.o)
	f.st.set(func(s *fakeStore) { s.upsertErr = nil })
	f.h.probe()
	if !f.h.persist() {
		t.Fatal("persist after recovery")
	}
	f.st.mu.Lock()
	stored := len(f.st.exposures["lab-a"])
	f.st.mu.Unlock()
	if stored != 2 {
		t.Fatalf("stored %d", stored)
	}
	f.h.mu.Lock()
	n := len(f.h.regDirty)
	f.h.mu.Unlock()
	if n != 0 {
		t.Fatal("still dirty")
	}
}

// changingStore re-registers while UpsertRegistry runs, so the snapshot
// written is already outdated when the write returns.
type changingStore struct {
	*fakeStore
	during func()
}

func (c *changingStore) UpsertRegistry(ctx context.Context, id string, e []store.Exposure, a []store.AuditEntry) error {
	if c.during != nil {
		c.during()
		c.during = nil
	}
	return c.fakeStore.UpsertRegistry(ctx, id, e, a)
}

func TestPersistRaces(t *testing.T) {
	f := newFixture(t)
	cs := &changingStore{fakeStore: f.st}
	f.h.st = cs
	cs.during = func() {
		f.h.mu.Lock()
		f.h.regSeq++
		p := f.h.regDirty["lab-a"]
		p.seq = f.h.regSeq
		p.audits = append(p.audits, store.AuditEntry{Action: "later"})
		f.h.mu.Unlock()
	}
	if !f.h.persist() {
		t.Fatal("persist")
	}
	f.h.mu.Lock()
	p := f.h.regDirty["lab-a"]
	f.h.mu.Unlock()
	if p == nil || len(p.audits) != 1 || p.audits[0].Action != "later" {
		t.Fatalf("pending %+v", p)
	}
	// The dirty entry disappears (Access deleted) while the write runs.
	cs.during = func() {
		f.h.mu.Lock()
		delete(f.h.regDirty, "lab-a")
		f.h.mu.Unlock()
	}
	if !f.h.persist() {
		t.Fatal("persist")
	}
	// A rejected snapshot (Access deleted in the store) is dropped.
	f.h.mu.Lock()
	f.h.regDirty["lab-a"] = &regPending{seq: 99}
	f.h.mu.Unlock()
	f.st.set(func(s *fakeStore) { s.upsertErr = store.ErrInvalid })
	if !f.h.persist() {
		t.Fatal("an invalid snapshot is dropped, not retried")
	}
	f.h.mu.Lock()
	n := len(f.h.regDirty)
	f.h.mu.Unlock()
	if n != 0 {
		t.Fatal("invalid snapshot kept")
	}
}

func TestPersistLoopRetries(t *testing.T) {
	f := newFixture(t, func(l *limits.Limits) { l.StoreProbeMS = 20; l.HeartbeatMS = 3_600_000 })
	f.st.set(func(s *fakeStore) { s.upsertErr = errBoom })
	f.h.Start()
	f.h.kickPersist()
	time.Sleep(50 * time.Millisecond)
	f.st.set(func(s *fakeStore) { s.upsertErr = nil })
	waitFor(t, "retried", func() bool {
		f.st.mu.Lock()
		defer f.st.mu.Unlock()
		return f.st.upserts > 0
	})
}
