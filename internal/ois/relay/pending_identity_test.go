package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const pendingSample = "0123456789abcdef0123456789abcdef"

func pendingFixture(t *testing.T, queries, sources int) (*Hub, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	h := New(Config{Limits: testLimits(), Store: newFakeStore(), Epoch: 3,
		RunID: "run_" + strings.Repeat("a", 26), Now: func() time.Time { return now }})
	t.Cleanup(h.cancel)
	h.oblim.QuerySlots, h.oblim.QueryMS, h.oblim.RefreshMS = 8, 500, 500
	s := &session{h: h, registered: true, version: protocol.ControlSubprotocolV2, accessID: "lab-a", epoch: 3, gen: 7}
	h.accesses["lab-a"] = &access{sess: s, gen: 7}
	for i := 0; i < sources; i++ {
		id := fmt.Sprintf("lab-a/source-%02d", i)
		b := protocol.ObservationBinding{AccessID: "lab-a", Epoch: 3, Generation: 7, ExposureID: id,
			Revision: rv(1), ObservationRevision: "ob_1", ContractDigest: strings.Repeat("b", 64), SourceID: "local", ContractVersion: 1}
		e := &exposure{rec: store.Exposure{AccessID: "lab-a", ExposureID: id}, gen: 7, binding: &b, probeSupport: protocol.ProbeSupported,
			observation: directoryObservation{dirty: i >= queries || i%2 == 0, periodicOnly: i >= queries, refreshAt: now.Add(450 * time.Millisecond)}}
		if i < queries {
			qid := fmt.Sprintf("%032x", i+1)
			e.observation.queryID = qid
			h.queries[qid] = &directoryQuery{id: qid, sess: s, binding: b, started: now.Add(-50 * time.Millisecond), deadline: now.Add(450 * time.Millisecond)}
		}
		h.exposures[id] = e
		h.queryOrder = append(h.queryOrder, id)
	}
	h.queryCursor = 3
	return h, now
}

func TestPendingIdentitySnapshotOwnership(t *testing.T) {
	h, now := pendingFixture(t, 8, 16)
	var calls atomic.Int32
	h.now = func() time.Time {
		calls.Add(1)
		if h.mu.TryLock() {
			h.mu.Unlock()
			t.Error("diagnostic clock was read outside coordinator lock")
		}
		return now
	}
	before := h.Stats()
	queryOrder := slices.Clone(h.queryOrder)
	stats, d := h.StatsWithPendingIdentity(pendingSample)
	if calls.Load() != 1 || !d.Complete || len(d.Pending) != 8 || len(d.Sources) != 16 || d.PendingQueries != 8 || d.ObservationBacklog != 16 || d.DirtyMarked != 12 ||
		!reflect.DeepEqual(before.Observation, stats.Observation) || !reflect.DeepEqual(queryOrder, h.queryOrder) || h.queryCursor != 3 {
		t.Fatalf("not the same unchanged coordinator snapshot: %+v / %+v", stats.Observation, d)
	}
	for _, q := range d.Pending {
		if *q.AgeNS != int64(50*time.Millisecond) || *q.RemainingNS != int64(450*time.Millisecond) || !q.BindingCurrent || !q.SessionCurrent || q.Online == nil || !*q.Online {
			t.Fatalf("query %+v", q)
		}
	}
	original := append([]byte(nil), d.JSONPayload()...)
	// The real reducers may release and replace owners after Stats returns.
	// No diagnostic may retain their mutable pointers or the order backing array.
	h.mu.Lock()
	for _, q := range h.queries {
		h.dropQuery(q)
		q.binding.Generation++
		q.started = now.Add(time.Hour)
	}
	for _, e := range h.exposures {
		e.binding.Generation++
		e.observation.dirty = false
		e.observation.periodicOnly = false
		e.observation.refreshAt = time.Time{}
	}
	h.queryOrder[0] = "lab-a/replaced"
	h.mu.Unlock()
	if !bytes.Equal(original, d.JSONPayload()) {
		t.Fatal("later release/rebind mutated prior diagnostic")
	}
	if got := h.Stats().Observation; got.PendingQueries != 0 || got.ObservationBacklog != 0 {
		t.Fatalf("mutation did not actually take effect: %+v", got)
	}
}

func TestPendingIdentitySnapshotHoldsOriginalLock(t *testing.T) {
	h, now := pendingFixture(t, 8, 16)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() {
		unblock()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("snapshot worker did not join")
		}
	}()
	h.now = func() time.Time { close(entered); <-release; return now }
	var stats protocol.OISStats
	var d *PendingIdentitySnapshot
	go func() { defer close(done); stats, d = h.StatsWithPendingIdentity(pendingSample) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not enter")
	}
	if h.mu.TryLock() {
		h.mu.Unlock()
		t.Error("another coordinator transition could enter during snapshot")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("capture did not return")
	}
	if stats.Observation.PendingQueries != len(d.Pending) || stats.Observation.ObservationCapacity != len(d.Sources) || !d.Complete {
		t.Fatalf("mismatched original snapshot: %+v", d)
	}
}

func TestPendingIdentityOrdinaryPathIsNoop(t *testing.T) {
	h, _ := pendingFixture(t, 8, 16)
	h.now = func() time.Time { t.Error("ordinary Stats read the diagnostic clock"); return time.Time{} }
	before := h.Stats().Observation
	for _, id := range []string{"", strings.ToUpper(pendingSample), pendingSample + ",", strings.Repeat("a", 33), strings.Repeat("x", 100000)} {
		stats, d := h.StatsWithPendingIdentity(id)
		if d != nil || !reflect.DeepEqual(before, stats.Observation) {
			t.Fatalf("invalid correlation altered Stats: len=%d", len(id))
		}
	}
	if len(h.queries) != 8 || len(h.queryOrder) != 16 || h.queryCursor != 3 {
		t.Fatal("no-op path changed live query state")
	}
}

func TestPendingIdentityCapsAndRelationships(t *testing.T) {
	for _, mode := range []string{"exact", "query_overflow", "source_order_overflow", "expired", "unset_refresh", "changed_binding", "replaced_session", "offline", "absent_access", "invalid_identity", "invalid_order", "invalid_time", "invalid_run", "nil_query"} {
		t.Run(mode, func(t *testing.T) {
			queries, sources := 8, 16
			if mode == "query_overflow" {
				queries, sources = 9, 17
			}
			if mode == "source_order_overflow" {
				sources = 17
			}
			h, now := pendingFixture(t, queries, sources)
			q := h.queries[fmt.Sprintf("%032x", 1)]
			e := h.exposures["lab-a/source-00"]
			secret := "secret-not-an-identity-" + strings.Repeat("x", 100000)
			switch mode {
			case "expired":
				q.deadline = now.Add(-time.Millisecond)
			case "unset_refresh":
				e.observation.refreshAt = time.Time{}
			case "changed_binding":
				e.binding.Generation++
			case "replaced_session":
				h.accesses["lab-a"].sess = &session{registered: true, version: protocol.ControlSubprotocolV2}
				h.accesses["lab-a"].gen++
			case "offline":
				h.accesses["lab-a"].sess.registered = false
			case "absent_access":
				delete(h.accesses, "lab-a")
			case "invalid_identity":
				q.binding.ExposureID = secret
				e.binding.ContractDigest = secret
				e.observation.queryID = secret
			case "invalid_order":
				h.queryOrder[0] = secret
			case "invalid_time":
				q.started = time.Time{}
			case "invalid_run":
				h.runID = secret
			case "nil_query":
				h.queries[q.id] = nil
			}
			stats, d := h.StatsWithPendingIdentity(pendingSample)
			if d.PendingQueries != stats.Observation.PendingQueries || d.ObservationCapacity != stats.Observation.ObservationCapacity || len(d.Pending) > 8 || len(d.Sources) > 16 || len(d.QueryOrder) > 16 {
				t.Fatal("caps changed real counts")
			}
			bad := strings.HasPrefix(mode, "invalid_") || strings.HasSuffix(mode, "overflow") || mode == "nil_query"
			if d.Complete == bad {
				t.Fatalf("complete=%v reasons=%v", d.Complete, d.Reasons)
			}
			raw := d.JSONPayload()
			if len(raw) > PendingIdentityMaxBytes || bytes.Contains(raw, []byte("secret-not-an-identity")) {
				t.Fatal("unbounded/private identity leaked")
			}
			x := d.Pending[0]
			y := d.Sources[0]
			switch mode {
			case "query_overflow":
				if d.Omitted.Pending != 1 || d.Omitted.Sources != 1 || d.Omitted.QueryOrder != 1 {
					t.Fatal(d.Omitted)
				}
			case "source_order_overflow":
				if d.Omitted.Pending != 0 || d.Omitted.Sources != 1 || d.Omitted.QueryOrder != 1 {
					t.Fatal(d.Omitted)
				}
			case "expired":
				if *x.RemainingNS != -int64(time.Millisecond) {
					t.Fatal("negative remaining was clamped")
				}
			case "unset_refresh":
				if y.RefreshAtSet || y.RefreshRemainingNS != nil {
					t.Fatal("unset refresh became a timestamp")
				}
			case "changed_binding":
				if x.BindingCurrent || x.Binding.Generation != 7 || y.Binding.Generation != 8 {
					t.Fatal("old query was rebound")
				}
			case "replaced_session":
				if x.SessionCurrent || !x.BindingCurrent || *y.CurrentGeneration != 8 {
					t.Fatal("replaced session was hidden")
				}
			case "offline":
				if *x.Online || *y.Online {
					t.Fatal("offline source called online")
				}
			case "absent_access":
				if x.Online != nil || y.Online != nil || y.ControlVersion != nil || y.CurrentGeneration != nil {
					t.Fatal("absent access was invented")
				}
			case "invalid_order":
				if d.QueryOrder[0] != nil {
					t.Fatal("bad order ID was not omitted")
				}
			case "invalid_run":
				if d.RunID != nil {
					t.Fatal("bad run ID was not omitted")
				}
			}
		})
	}
}

func TestPendingIdentityEncodingBound(t *testing.T) {
	h, now := pendingFixture(t, 8, 16)
	h.exposures = map[string]*exposure{}
	h.queries = map[string]*directoryQuery{}
	h.queryOrder = nil
	accessID := strings.Repeat("a", 63)
	s := &session{registered: true, version: protocol.ControlSubprotocolV2}
	h.accesses[accessID] = &access{sess: s, gen: math.MaxInt64}
	for i := 0; i < 16; i++ {
		id := accessID + "/" + strings.Repeat("n", 60) + fmt.Sprintf("%03d", i)
		b := protocol.ObservationBinding{AccessID: accessID, ExposureID: id, Epoch: math.MaxInt64, Generation: math.MaxInt64,
			Revision: rv(1), ObservationRevision: "ob_18446744073709551615", ContractDigest: strings.Repeat("f", 64), SourceID: "local", ContractVersion: 1}
		e := &exposure{rec: store.Exposure{AccessID: accessID, ExposureID: id}, binding: &b, probeSupport: protocol.ProbeSupported, observation: directoryObservation{dirty: true, refreshAt: now.Add(500 * time.Millisecond)}}
		if i < 8 {
			qid := fmt.Sprintf("%032x", i+1)
			e.observation.queryID = qid
			h.queries[qid] = &directoryQuery{id: qid, binding: b, sess: s, started: now.Add(-time.Millisecond), deadline: now.Add(499 * time.Millisecond)}
		}
		h.exposures[id] = e
		h.queryOrder = append(h.queryOrder, id)
	}
	_, d := h.StatsWithPendingIdentity(pendingSample)
	raw := d.JSONPayload()
	if !d.Complete || !json.Valid(raw) || len(raw) > PendingIdentityMaxBytes {
		t.Fatalf("maximum legal snapshot failed: %d %+v", len(raw), d.Reasons)
	}
	// A defensive serialization limit must never emit an oversized payload,
	// even if a future caller accidentally grows the owned value after capture.
	d.Sources[0].Binding.ExposureID = strings.Repeat("x", PendingIdentityMaxBytes)
	raw = d.JSONPayload()
	var summary PendingIdentitySnapshot
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if len(raw) > PendingIdentityMaxBytes || summary.Complete || !slices.Equal(summary.Reasons, []string{"payload"}) || len(summary.Sources) != 0 || summary.Omitted.Sources != 16 {
		t.Fatal("oversized value was not replaced by finite incomplete summary")
	}
	d.RunID = pendingValue(strings.Repeat("private", PendingIdentityMaxBytes))
	d.SampleID = strings.Repeat("private", PendingIdentityMaxBytes)
	raw = d.JSONPayload()
	if len(raw) > PendingIdentityMaxBytes || bytes.Contains(raw, []byte("private")) {
		t.Fatal("fallback copied unbounded top-level identity")
	}
}
