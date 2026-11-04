package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// recoveryStore can delay delivery of successful I/O results, modeling a
// caller descheduled after the store completed its work.
type recoveryStore struct {
	store.Store
	health       func(context.Context) error
	probeWrite   func(context.Context) error
	loadSnapshot func(context.Context) (store.Snapshot, error)
}

func (s *recoveryStore) Health(ctx context.Context) error {
	if s.health != nil {
		return s.health(ctx)
	}
	return s.Store.Health(ctx)
}

func (s *recoveryStore) ProbeWrite(ctx context.Context) error {
	if s.probeWrite != nil {
		return s.probeWrite(ctx)
	}
	return s.Store.ProbeWrite(ctx)
}

func (s *recoveryStore) LoadSnapshot(ctx context.Context) (store.Snapshot, error) {
	if s.loadSnapshot != nil {
		return s.loadSnapshot(ctx)
	}
	return s.Store.LoadSnapshot(ctx)
}

func waitRecoverySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for recovery I/O")
	}
}

func TestProbePreservesConcurrentWriteFailures(t *testing.T) {
	for _, kind := range []string{store.FailureDiskFull, store.FailureReadOnly} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			e.h.writeFailed(store.NewUnavailable(store.FailureDiskFull, "first failure"))
			checked, gate, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(gate) })
			defer release()
			e.h.st = &recoveryStore{Store: e.st, probeWrite: func(ctx context.Context) error {
				err := e.st.ProbeWrite(ctx)
				close(checked)
				<-gate
				return err
			}}
			go func() { e.h.probe(); close(done) }()
			waitRecoverySignal(t, checked)
			// A later write fails before the successful probe result reaches
			// the hub. Even an identical error must remain latched.
			e.h.writeFailed(store.NewUnavailable(kind, "later failure"))
			release()
			waitRecoverySignal(t, done)
			if v := e.h.StoreView(bg); v.Healthy || v.WriteFailure != kind {
				t.Fatalf("old probe erased the later failure: %+v", v)
			}
			e.h.st = e.st
			e.h.probe()
			if v := e.h.StoreView(bg); !v.Healthy || v.WriteFailure != "" || e.st.probes != 2 {
				t.Fatalf("later write probe did not recover: %+v, probes=%d", v, e.st.probes)
			}
		})
	}
}

func TestReadProbePreservesConcurrentFailure(t *testing.T) {
	e := newEnv(t)
	e.h.st = &recoveryStore{Store: e.st, health: func(context.Context) error {
		// The query succeeded before another request observed a failure.
		e.h.storeFailed(store.ErrUnavailable)
		return nil
	}}
	e.h.probe()
	if e.h.ConfigView().StoreHealthy {
		t.Fatal("an old read probe erased a newer failure")
	}
	e.h.st = e.st
	e.h.probe()
	if !e.h.ConfigView().StoreHealthy || e.st.probes != 0 {
		t.Fatal("read-only recovery should not require a write probe")
	}
}

func TestProbeReloadsMaintenanceBeforeRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"unavailable", store.ErrUnavailable},
		{"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.st.set(func(s *fakeStore) { s.commitErr = store.ErrUnavailable })
			if _, err := f.h.SetMaintenance(bg, f.admin, f.v(), true); codeOf(err) != protocol.CodeStoreUnavailable {
				t.Fatalf("unknown commit result: %v", err)
			}
			f.st.set(func(s *fakeStore) { s.commitErr = nil })
			loading, gate, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(gate) })
			defer release()
			f.h.st = &recoveryStore{Store: f.st, loadSnapshot: func(ctx context.Context) (store.Snapshot, error) {
				close(loading)
				<-gate
				if tc.err != nil {
					return store.Snapshot{}, tc.err
				}
				return f.st.LoadSnapshot(ctx)
			}}
			go func() { f.h.probe(); close(done) }()
			waitRecoverySignal(t, loading)
			if cv := f.h.ConfigView(); cv.StoreHealthy || cv.Maintenance {
				t.Fatalf("recovery published before snapshot reload: %+v", cv)
			}
			req := protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev}
			if _, err := f.h.Open(f.alice, req); codeOf(err) != protocol.CodeStoreUnavailable {
				t.Fatalf("Open used the stale pre-maintenance snapshot: %v", err)
			}
			release()
			waitRecoverySignal(t, done)
			f.h.st = f.st
			if tc.err != nil {
				if f.h.ConfigView().StoreHealthy {
					t.Fatal("a failed snapshot reload restored availability")
				}
				f.h.probe()
			}
			if cv := f.h.ConfigView(); !cv.StoreHealthy || !cv.Maintenance {
				t.Fatalf("snapshot not reconciled on recovery: %+v", cv)
			}
			if _, err := f.h.Open(f.alice, req); codeOf(err) != protocol.CodeMaintenance {
				t.Fatalf("recovered maintenance state permits Open: %v", err)
			}
		})
	}
}
