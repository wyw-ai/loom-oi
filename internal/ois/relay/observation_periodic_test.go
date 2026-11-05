package relay

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/metrics"
	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// These fixtures execute the actual reducers and bounded telemetry callbacks
// synchronously. There is no writer/background goroutine or production hook.
type periodicFixture struct {
	h       *Hub
	s       *session
	clk     *clock
	base    time.Time
	legacy  bool
	entries []protocol.RegisterExposure
	held    []ctrlOut
	output  []protocol.Message
	results []bool
}

func newPeriodicFixture(t *testing.T, legacy bool, ids protocol.ControlIDs, queryMS int64, sources int) *periodicFixture {
	t.Helper()
	base := time.Date(2026, 11, 4, 8, 0, 0, 0, time.UTC)
	clk := &clock{t: base}
	h := New(Config{Limits: testLimits(), Store: newFakeStore(), Now: clk.Now, Epoch: 1})
	h.oblim.QueryMS, h.oblim.RefreshMS, h.oblim.QuerySlots = queryMS, 500, 1
	s := &session{h: h, conn: newFakeConn(), accessID: "lab-a", epoch: 1, gen: 1,
		version: protocol.ControlSubprotocolV2, ids: &ids, out: make(chan ctrlOut, 128),
		telemetry: make(chan ctrlOut, 128), identities: map[string]protocol.ObservationBinding{}}
	h.accesses[s.accessID] = &access{rec: store.Access{AccessID: s.accessID, MaxExposures: 32}, gen: 1, sess: s}
	f := &periodicFixture{h: h, s: s, clk: clk, base: base, legacy: legacy}
	for i := 0; i < sources; i++ {
		id := fmt.Sprintf("lab-a/source%d", i)
		b := protocol.ObservationBinding{AccessID: s.accessID, Epoch: s.epoch, Generation: s.gen,
			ExposureID: id, Revision: rv(1), ObservationRevision: "ob_1", SourceID: "local",
			ContractVersion: 1, ContractDigest: strings.Repeat("b", 64)}
		x := expo(id, b.Revision)
		x.Binding, x.ProbeSupport = &b, protocol.ProbeSupported
		f.entries = append(f.entries, x)
	}
	f.register()
	t.Cleanup(func() { h.cancel(); _ = s.conn.CloseNow() })
	return f
}

func (f *periodicFixture) at(ms int64) time.Time {
	t := f.base.Add(time.Duration(ms) * time.Millisecond)
	f.clk.Add(t.Sub(f.clk.Now()))
	return t
}

func (f *periodicFixture) register() {
	m := &protocol.Register{Exposures: f.entries}
	if f.legacy {
		f.h.periodicLegacyOnRegister(f.s, m, f.clk.Now())
	} else {
		f.h.onRegister(f.s, m, f.clk.Now())
	}
}

func (f *periodicFixture) sweep(now time.Time) {
	if f.legacy {
		f.h.periodicLegacySweepObservations(now)
	} else {
		f.h.sweepObservations(now)
	}
}

func (f *periodicFixture) changed(id string, update uint64) {
	m := &protocol.ObservationChanged{Binding: *f.h.exposures[id].binding, UpdateSeq: update}
	if f.legacy {
		f.h.periodicLegacyOnObservationChanged(f.s, m, f.clk.Now())
	} else {
		f.h.onObservationChanged(f.s, m, f.clk.Now())
	}
}

func (f *periodicFixture) onlyQuery(t *testing.T) *directoryQuery {
	t.Helper()
	if len(f.h.queries) != 1 {
		t.Fatalf("want one real pending query, got %d", len(f.h.queries))
	}
	for _, q := range f.h.queries {
		return q
	}
	panic("unreachable")
}

func (f *periodicFixture) snapshot(q *directoryQuery, update uint64) {
	at := f.base
	seq := uint64(1)
	m := &protocol.ObservationSnapshot{QueryID: q.id, Binding: q.binding, UpdateSeq: update,
		SampleSeq: &seq, ObservedAt: &at, RemainingMS: 1000, Readiness: protocol.ReadinessReady,
		Reason: "matched_ready_status", Attempt: protocol.ObservationAttempt{Seq: 1, State: "idle", Outcome: "matched_ready_status"}}
	if f.legacy {
		f.h.periodicLegacyOnObservationSnapshot(f.s, m, f.clk.Now())
	} else {
		f.h.onObservationSnapshot(f.s, m, f.clk.Now())
	}
}

func (f *periodicFixture) drain() {
	for len(f.s.out) > 0 {
		m := <-f.s.out
		f.output = append(f.output, m.message)
	}
	for len(f.s.telemetry) > 0 {
		m := <-f.s.telemetry
		f.held = append(f.held, m)
	}
}

func (f *periodicFixture) callback(t *testing.T, id string, at time.Time) {
	t.Helper()
	f.drain()
	for _, m := range f.held {
		if q, ok := m.message.(*protocol.ObservationQuery); ok && q.QueryID == id {
			f.results = append(f.results, m.before(at))
			return
		}
	}
	t.Fatalf("no actual queued callback for %s", id)
}

// The old/new pair is two alternative executions of one virtual session. The
// AES block is immutable; copying the initial value gives independent counters.
func periodicPair(t *testing.T, queryMS int64, sources int) (*periodicFixture, *periodicFixture) {
	t.Helper()
	ids, err := protocol.NewControlIDs()
	if err != nil {
		t.Fatal(err)
	}
	return newPeriodicFixture(t, true, *ids, queryMS, sources), newPeriodicFixture(t, false, *ids, queryMS, sources)
}

func periodicState(t *testing.T, f *periodicFixture) []byte {
	t.Helper()
	f.drain()
	queries := map[string]any{}
	for id, q := range f.h.queries {
		queries[id] = []any{q.binding, q.started, q.deadline, q.sess.accessID, q.sess.epoch, q.sess.gen}
	}
	exposures := map[string]any{}
	for id, e := range f.h.exposures {
		d := e.observation
		if d.periodicOnly && !d.dirty {
			t.Fatalf("%s retained periodic provenance after allocation", id)
		}
		ready, view := e.directoryView(f.clk.Now())
		exposures[id] = []any{e.rec, e.gen, e.binding, e.probeSupport, d.record, d.lastSample, d.history,
			d.deadline, d.queryID, d.dirty && !d.periodicOnly, d.refreshAt, ready, view}
	}
	var queued []any
	for _, m := range f.held {
		queued = append(queued, []any{m.message, m.queued})
	}
	b, err := json.Marshal([]any{queries, exposures, f.h.queryOrder, f.h.queryCursor, queued, f.output, f.results,
		f.s.identities, f.s.ended, f.s.registered, f.h.regSeq, f.h.regDirty, f.h.accesses[f.s.accessID].online()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func periodicEqual(t *testing.T, old, current *periodicFixture) {
	t.Helper()
	a, b := periodicState(t, old), periodicState(t, current)
	if string(a) != string(b) {
		t.Fatalf("old/new actual state or wire differs\nold: %s\nnew: %s", a, b)
	}
	if !reflect.DeepEqual(old.h.metrics.Snapshot(), current.h.metrics.Snapshot()) {
		t.Fatalf("old/new original counters or RTT histogram differ: %+v / %+v", old.h.metrics.Snapshot(), current.h.metrics.Snapshot())
	}
}

func periodicPrime(t *testing.T, f *periodicFixture) {
	t.Helper()
	for i := range f.entries {
		q := f.onlyQuery(t)
		f.at(int64((i + 1) * 10))
		f.callback(t, q.id, f.clk.Now())
		f.snapshot(q, 2)
	}
	if len(f.h.queries) != 0 {
		t.Fatal("priming failed to complete all initial queries")
	}
}

func TestObservationPeriodicWorkMaterialization(t *testing.T) {
	for _, name := range []string{"due_full", "not_due", "pending", "normal_dirty", "offline", "unregistered", "v1", "unsupported", "missing_binding", "missing_access", "shutting"} {
		t.Run(name, func(t *testing.T) {
			old, f := periodicPair(t, 750, 3)
			periodicPrime(t, f)
			f.sweep(f.at(500)) // source0 now occupies the sole slot until 1250.
			e := f.h.exposures[f.entries[1].ExposureID]
			f.at(520)
			wantDirty, wantPeriodic := false, false
			switch name {
			case "due_full":
				wantDirty, wantPeriodic = true, true
			case "not_due":
				e.observation.refreshAt = f.base.Add(521 * time.Millisecond)
			case "pending":
				e = f.h.exposures[f.entries[0].ExposureID]
			case "normal_dirty":
				e.observation.dirty, wantDirty = true, true
			case "offline":
				f.h.accesses[f.s.accessID].sess = nil
			case "unregistered":
				f.s.registered = false
			case "v1":
				f.s.version = protocol.ControlSubprotocol
			case "unsupported":
				e.probeSupport = protocol.ProbeUnsupported
			case "missing_binding":
				e.binding = nil
			case "missing_access":
				delete(f.h.accesses, f.s.accessID)
			case "shutting":
				f.h.shutting = true
			}
			q := f.onlyQuery(t)
			ids := *f.s.ids
			f.sweep(f.clk.Now())
			f.sweep(f.clk.Now())
			if e.observation.dirty != wantDirty || e.observation.periodicOnly != wantPeriodic || f.h.queries[q.id] != q {
				t.Fatalf("materialized ineligible work or replaced pending: %+v", e.observation)
			}
			a, _ := ids.Next()
			b, _ := f.s.ids.Next()
			if a != b {
				t.Fatal("slot-full materialization consumed an ID")
			}
			if name == "due_full" {
				periodicPrime(t, old)
				old.sweep(old.at(500))
				old.sweep(old.at(520))
				oldStats := old.h.Stats().Observation
				if oldStats.DirtyMarked != 0 || oldStats.ObservationBacklog != 1 || oldStats.PendingQueries != 1 {
					t.Fatalf("fixed old oracle unexpectedly materialized periodic backlog: %+v", oldStats)
				}
				s := f.h.Stats().Observation
				if s.DirtyMarked != 2 || s.PendingQueries != 1 || s.ObservationBacklog != 3 {
					t.Fatalf("actual source-slot union: %+v", s)
				}
			}
		})
	}
}

func TestObservationPeriodicWorkTraceEquivalence(t *testing.T) {
	t.Run("fixed_cc653e_oracle", periodicOracleProvenance)
	for _, qms := range []int64{250, 500, 750} {
		t.Run(fmt.Sprintf("query_%d_refresh_500", qms), func(t *testing.T) {
			for _, name := range []string{"early_snapshot", "deadline_snapshot", "late_snapshot", "old_update", "same_binding", "new_binding", "new_generation", "telemetry_refusal", "callback_before", "callback_exact", "callback_after", "query_refresh_relation"} {
				t.Run(name, func(t *testing.T) {
					old, current := periodicPair(t, qms, 3)
					for _, f := range []*periodicFixture{old, current} {
						periodicPrime(t, f)
						f.sweep(f.at(500))
						f.sweep(f.at(520))
					}
					periodicEqual(t, old, current)
					for _, f := range []*periodicFixture{old, current} {
						q := f.onlyQuery(t)
						switch name {
						case "early_snapshot":
							f.at(550)
							f.snapshot(q, 3)
						case "deadline_snapshot":
							f.at(500 + qms)
							f.snapshot(q, 3)
						case "late_snapshot":
							f.at(501 + qms)
							f.snapshot(q, 3)
						case "old_update":
							f.at(550)
							f.snapshot(q, 1)
						case "same_binding":
							f.register()
						case "new_binding":
							for i := range f.entries {
								b := *f.entries[i].Binding
								b.ObservationRevision = "ob_2"
								f.entries[i].Binding = &b
							}
							f.register()
							f.snapshot(q, 3) // cancelled old-binding reply cannot revive it.
						case "new_generation":
							f.h.endSession(f.s, causeLost, "", "")
							f.sweep(f.at(600))
							f.drain()
							prior := f.s
							f.s = &session{h: f.h, conn: newFakeConn(), accessID: prior.accessID, epoch: prior.epoch, gen: 2,
								version: prior.version, ids: prior.ids, out: make(chan ctrlOut, 128), telemetry: make(chan ctrlOut, 128), identities: map[string]protocol.ObservationBinding{}}
							f.h.accesses[f.s.accessID].sess, f.h.accesses[f.s.accessID].gen = f.s, 2
							for i := range f.entries {
								b := *f.entries[i].Binding
								b.Generation = 2
								f.entries[i].Binding = &b
							}
							f.register()
							f.snapshot(q, 3)
						case "telemetry_refusal":
							f.drain()
							f.s.telemetry = make(chan ctrlOut)
							f.at(550)
							f.snapshot(q, 3)
							f.changed(f.entries[1].ExposureID, 4)
						case "callback_before":
							f.at(500 + qms - 1)
							f.callback(t, q.id, f.clk.Now())
						case "callback_exact":
							f.at(500 + qms)
							f.callback(t, q.id, f.clk.Now())
						case "callback_after":
							f.at(501 + qms)
							f.callback(t, q.id, f.clk.Now())
						case "query_refresh_relation":
							// Remove other sources through the actual Register path; only
							// source0 remains, with refresh=1000 and D=500+QueryMS.
							f.entries = f.entries[:1]
							f.register()
							f.sweep(f.at(min(1000, 500+qms)))
							if qms < 500 && len(f.h.queries) != 0 {
								t.Fatal("timeout retried before refresh")
							}
							if qms > 500 && f.h.queries[q.id] != q {
								t.Fatal("refresh replaced active query")
							}
							f.sweep(f.at(max(1000, 500+qms)))
						}
					}
					periodicEqual(t, old, current)
					// Includes rejected sends and cancelled callbacks: no hidden IDs.
					a, ea := old.s.ids.Next()
					b, eb := current.s.ids.Next()
					if a != b || ea != eb {
						t.Fatalf("ID consumption diverged: %s/%v %s/%v", a, ea, b, eb)
					}
				})
			}
		})
	}
}

func TestObservationPeriodicWorkDirtyProvenance(t *testing.T) {
	for _, name := range []string{"periodic_then_changed", "registered_dirty", "refused_send", "ignored_old_update", "same_binding", "new_binding", "allocation_clears", "offline_and_new_generation"} {
		t.Run(name, func(t *testing.T) {
			old, current := periodicPair(t, 750, 3)
			for _, f := range []*periodicFixture{old, current} {
				if name != "registered_dirty" {
					periodicPrime(t, f)
					f.sweep(f.at(500))
					f.sweep(f.at(520))
				}
			}
			periodicEqual(t, old, current)
			id := current.entries[1].ExposureID
			for _, f := range []*periodicFixture{old, current} {
				switch name {
				case "same_binding":
					f.register()
				case "new_binding":
					b := *f.entries[1].Binding
					b.ObservationRevision = "ob_2"
					f.entries[1].Binding = &b
					f.register()
				case "refused_send":
					f.drain()
					f.s.telemetry = make(chan ctrlOut)
					f.at(550)
					f.snapshot(f.onlyQuery(t), 3)
				case "ignored_old_update":
					f.changed(id, 2)
				case "allocation_clears":
					f.at(550)
					f.snapshot(f.onlyQuery(t), 3)
				case "offline_and_new_generation":
					f.h.endSession(f.s, causeLost, "", "")
					f.sweep(f.at(600))
				}
			}
			periodicEqual(t, old, current)
			if name == "offline_and_new_generation" {
				return
			} // new session exercised in trace root.
			for i := uint64(3); i <= 4; i++ {
				for _, f := range []*periodicFixture{old, current} {
					f.changed(id, i)
				}
				periodicEqual(t, old, current)
			}
			want := uint64(1)
			if name == "registered_dirty" || name == "new_binding" || name == "refused_send" {
				want = 2
			}
			n, _ := current.h.metrics.Snapshot().Counter(metrics.DirtyCoalesced, "")
			if n != want {
				t.Fatalf("real Changed coalescence=%d, want %d", n, want)
			}
		})
	}
}

func TestObservationPeriodicWorkUsesSweepNow(t *testing.T) {
	for _, name := range []string{"clock_advanced_while_waiting_for_hub", "allocation_after_capture"} {
		t.Run(name, func(t *testing.T) {
			old, current := periodicPair(t, 500, 1)
			for _, f := range []*periodicFixture{old, current} {
				periodicPrime(t, f)
				if name == "clock_advanced_while_waiting_for_hub" {
					// A real Changed allocation at150 sets refreshAt650; its
					// real reply completes at160 before janitor captures500.
					f.at(150)
					f.changed(f.entries[0].ExposureID, 3)
					q := f.onlyQuery(t)
					f.at(160)
					f.snapshot(q, 3)
				}
				captured := f.at(500)
				if name == "clock_advanced_while_waiting_for_hub" {
					f.at(800)
				} else {
					f.at(800)
					f.changed(f.entries[0].ExposureID, 3) // real allocation before stale sweep takes the lock.
				}
				f.sweep(captured)
				if name == "clock_advanced_while_waiting_for_hub" && len(f.h.queries) != 0 {
					t.Fatal("fresh clock illegally made stale sweep allocate")
				}
				if name == "allocation_after_capture" {
					q := f.onlyQuery(t)
					if !q.started.Equal(f.base.Add(800*time.Millisecond)) || f.h.exposures[q.binding.ExposureID].observation.dirty {
						t.Fatal("stale sweep changed the newer allocation")
					}
				}
			}
			periodicEqual(t, old, current)
		})
	}
}

func periodicOracleProvenance(t *testing.T) {
	t.Helper()
	sources := map[string]string{}
	for file, hash := range map[string]string{
		"observation.go": "15dabd7bd65b36ef026a4b03103050d9639af31fcd8c6cef28954e6af949ab77",
		"control.go":     "b37c7e1ef08054539ea1e8e66a79096241767398e4ebc0458e8bbcf2cc1d2f79",
	} {
		b, err := os.ReadFile("testdata/periodic-work-cc653e/" + file + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(b)) != hash {
			t.Fatalf("fixed cc653e source changed: %s", file)
		}
		sources[file] = strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	functions := [][2]string{{"observation.go", "pumpQueries"}, {"observation.go", "onObservationChanged"}, {"observation.go", "onObservationSnapshot"}, {"observation.go", "sweepObservations"}, {"control.go", "onRegister"}}
	text := "// The methods below are mechanically extracted from cc653e0554c57fd13857e74e431e011c4e219dfd.\n// TestObservationPeriodicWorkTraceEquivalence verifies their exact derivation.\npackage relay\n\nimport (\n\"time\"\n\"github.com/wyw-ai/loom-oi/internal/limits\"\n\"github.com/wyw-ai/loom-oi/internal/metrics\"\n\"github.com/wyw-ai/loom-oi/internal/ois/store\"\n\"github.com/wyw-ai/loom-oi/internal/protocol\"\n)\n\n"
	var parts []string
	for _, fn := range functions {
		s := sources[fn[0]]
		start := strings.Index(s, "func (h *Hub) "+fn[1]+"(")
		if start < 0 {
			t.Fatalf("missing old function %s", fn[1])
		}
		part := s[start:]
		if end := strings.Index(part[1:], "\nfunc "); end >= 0 {
			part = part[:end+1]
		}
		part = part[:strings.LastIndex(part, "\n}")+2]
		for _, rename := range functions {
			n := rename[1]
			replacement := "periodicLegacy" + strings.ToUpper(n[:1]) + n[1:]
			part = strings.ReplaceAll(part, "h."+n+"(", "h."+replacement+"(")
			part = strings.ReplaceAll(part, "func (h *Hub) "+n+"(", "func (h *Hub) "+replacement+"(")
		}
		parts = append(parts, part)
	}
	want, err := format.Source([]byte(text + strings.Join(parts, "\n\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile("observation_periodic_oracle_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(actual) {
		t.Fatal("compiled old oracle is not the mechanical fixed-source extraction")
	}
}
