package relay

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestMaintenanceModeRefusesOpens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	v := f.v()
	f.ok(f.h.SetMaintenance(ctx, f.admin, v, true))
	if !f.h.ConfigView().Maintenance || !f.st.maintenance || !slices.Contains(f.st.auditActions(), "maintenance_entered") {
		t.Fatal("maintenance not recorded")
	}
	_, err := f.h.Open(f.alice, protocol.OpenRequest{ExposureID: exp1, ExpectedRevision: f.rev})
	if codeOf(err) != protocol.CodeMaintenance || protocol.CodeMaintenance.HTTPStatus() != 503 {
		t.Fatalf("open in maintenance: %v", err)
	}
	if _, err := f.h.Discover(f.alice, 0, ""); err != nil {
		t.Fatalf("discovery stays available: %v", err)
	}
	if _, err := f.h.SetMaintenance(ctx, f.admin, v, false); codeOf(err) != protocol.CodeVersionConflict {
		t.Fatalf("stale If-Match: %v", err)
	}
	f.ok(f.h.SetMaintenance(ctx, f.admin, f.v(), false))
	if f.h.ConfigView().Maintenance || !slices.Contains(f.st.auditActions(), "maintenance_resumed") {
		t.Fatal("still in maintenance")
	}
	f.openFlow()
}

// TestRestoredSnapshotStartsInMaintenance: a database marked by ois restore
// refuses Opens from the first request and says so at startup (B3 §8).
func TestRestoredSnapshotStartsInMaintenance(t *testing.T) {
	var logs bytes.Buffer
	st := newFakeStore()
	h := New(Config{Limits: testLimits(), Store: st, Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Snapshot: store.Snapshot{Meta: store.Meta{StoreID: "st_fake", Maintenance: true}}, Epoch: 1, RunID: "r"})
	h.Start()
	defer func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h.Shutdown(ctx)
	}()
	if !h.ConfigView().Maintenance || !strings.Contains(logs.String(), "maintenance mode") {
		t.Fatalf("logs %s", logs.String())
	}
}

func TestWriteFailureNeedsWriteProbe(t *testing.T) {
	e := newEnv(t)
	var logs bytes.Buffer
	e.h.log = slog.New(slog.NewTextHandler(&logs, nil))
	full := store.NewUnavailable(store.FailureDiskFull, "database or disk is full")
	e.st.set(func(f *fakeStore) { f.applyErr = full })
	if _, err := e.h.CreateAccess(context.Background(), e.admin, e.v(), protocol.AccessSpec{AccessID: "x"}); codeOf(err) != protocol.CodeStoreUnavailable {
		t.Fatal(err)
	}
	v := e.h.StoreView(context.Background())
	if v.Healthy || v.WriteFailure != store.FailureDiskFull || !strings.Contains(logs.String(), "failure=disk_full") {
		t.Fatalf("%+v %s", v, logs.String())
	}
	// Reads work again but writes do not: the store stays unavailable.
	e.st.set(func(f *fakeStore) {
		f.applyErr, f.probeErr = nil, store.NewUnavailable(store.FailureReadOnly, "readonly")
	})
	e.h.probe()
	if e.h.StoreView(context.Background()).Healthy || e.st.probes != 1 || e.h.StoreView(context.Background()).WriteFailure != store.FailureReadOnly {
		t.Fatalf("recovered on a read probe (probes %d)", e.st.probes)
	}
	e.st.set(func(f *fakeStore) { f.probeErr = nil })
	e.h.probe()
	if v := e.h.StoreView(context.Background()); !v.Healthy || v.WriteFailure != "" || e.st.probes != 2 {
		t.Fatalf("%+v", v)
	}
	// Without a latched write failure the probe only reads.
	e.h.probe()
	if e.st.probes != 2 {
		t.Fatal("probe wrote without a write failure")
	}
	// A failing read probe keeps an unlatched store down without a write.
	e.st.set(func(f *fakeStore) { f.healthErr = store.ErrUnavailable })
	e.h.probe()
	if e.h.StoreView(context.Background()).Healthy || e.st.probes != 2 {
		t.Fatal("read failure")
	}
}

func TestMaintainAndPruneThroughHub(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.st.set(func(f *fakeStore) { f.usage = protocol.StoreUsage{DBBytes: 10, WALBytes: 5} })
	if v := e.h.StoreView(ctx); !v.Healthy || v.Usage.WALBytes != 5 || v.Backend != "fake" || v.Engine != "fake-1" {
		t.Fatalf("%+v", v)
	}
	out, err := e.h.MaintainStore(ctx, e.admin, protocol.StoreMaintain{Checkpoint: "truncate"})
	if err != nil || out.After.DBBytes != 10 || len(e.st.maintained) != 1 || !slices.Contains(e.st.auditActions(), "store_maintained") {
		t.Fatalf("%+v %v", out, err)
	}
	for err, want := range map[error]protocol.Code{
		store.ErrBusy:                  protocol.CodeStoreBusy,
		store.ErrInvalid:               protocol.CodeInvalidRequest,
		store.NewUnavailable("x", "y"): protocol.CodeStoreUnavailable,
	} {
		e.st.set(func(f *fakeStore) { f.maintErr, f.pruneErr = err, err })
		if _, got := e.h.MaintainStore(ctx, e.admin, protocol.StoreMaintain{}); codeOf(got) != want {
			t.Fatalf("maintain %v: %v", err, got)
		}
		if _, got := e.h.PruneAudit(ctx, e.admin, protocol.AuditPrune{ThroughSeq: 1, SHA256: strings.Repeat("a", 64)}); codeOf(got) != want {
			t.Fatalf("prune %v: %v", err, got)
		}
	}
	e.st.set(func(f *fakeStore) { f.maintErr, f.pruneErr, f.usageErr = nil, nil, store.ErrUnavailable })
	if v := e.h.StoreView(ctx); v.Healthy {
		t.Fatal("usage failure must mark the store down")
	}
	e.st.set(func(f *fakeStore) { f.usageErr = nil })
	e.h.probe()
	for _, bad := range []protocol.AuditPrune{{ThroughSeq: 0, SHA256: strings.Repeat("a", 64)}, {ThroughSeq: 1, SHA256: "zz"}, {ThroughSeq: 1, SHA256: "ab"}} {
		if _, err := e.h.PruneAudit(ctx, e.admin, bad); codeOf(err) != protocol.CodeInvalidRequest {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	e.st.set(func(f *fakeStore) {
		f.audits = append(f.audits, store.AuditEntry{Action: "a"}, store.AuditEntry{Action: "b"})
	})
	before := len(e.st.auditActions())
	res, err := e.h.PruneAudit(ctx, e.admin, protocol.AuditPrune{ThroughSeq: 2, SHA256: strings.Repeat("A", 64)})
	if err != nil || res.Deleted != 2 || res.ThroughSeq != 2 || res.SHA256 != strings.Repeat("a", 64) || len(e.st.auditActions()) != before-1 {
		t.Fatalf("%+v %v", res, err)
	}
	// A revoked admin credential cannot maintain or prune.
	p, _ := e.token("ops", protocol.ScopeAdmin, 0)
	e.ok(e.h.RevokeToken(ctx, e.admin, e.v(), p.TokenID))
	if _, err := e.h.MaintainStore(ctx, p, protocol.StoreMaintain{}); codeOf(err) != protocol.CodeUnauthenticated {
		t.Fatal(err)
	}
	if _, err := e.h.PruneAudit(ctx, p, protocol.AuditPrune{ThroughSeq: 1, SHA256: strings.Repeat("a", 64)}); codeOf(err) != protocol.CodeUnauthenticated {
		t.Fatal(err)
	}
}

// TestAdminWritesBoundedByWriteQueue: configuration writes beyond
// WRITE_QUEUE are refused at once with StoreBusy (B3 §3, TV-B3-11g).
func TestAdminWritesBoundedByWriteQueue(t *testing.T) {
	e := newEnv(t, func(l *limits.Limits) { l.WriteQueue = 1 })
	e.h.adminSlots <- struct{}{} // one write holds the only slot
	if _, err := e.h.CreateAccess(context.Background(), e.admin, e.v(), protocol.AccessSpec{AccessID: "x"}); codeOf(err) != protocol.CodeStoreBusy {
		t.Fatalf("over the bound: %v", err)
	}
	<-e.h.adminSlots
	e.addAccess("x")
}
