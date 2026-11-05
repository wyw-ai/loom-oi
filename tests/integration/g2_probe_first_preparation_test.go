package integration

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func g2ProbeQueueInput(t *testing.T) (*g2PreparationFailure, g2ProxySnapshot, resourceProfile) {
	t.Helper()
	x, read := preparationHistoryInput()
	x.OIS.AccessesOnline = 2
	x.StartedNS, x.CompletedNS = resourceNS(), resourceNS()
	d := &g2PreparationFailure{StartedNS: x.StartedNS - 1, DeadlineNS: x.StartedNS + int64(3*time.Second), Attempts: 1, Phase: "preload_probe"}
	d.lastSample, d.lastSampleReturnedNS = x, resourceNS()
	d.beginHistory(101, 102)
	read.StartedNS, read.CompletedNS, read.ReturnedNS = x.StartedNS, &x.CompletedNS, d.lastSampleReturnedNS
	read.OIS, read.OIC = &x.OIS, &x.OIC
	d.historyRead(read)
	d.full(x)
	base := resourceProfile{OIS: resourceProcess[protocol.OISStats]{PID: 101, Stats: x.OIS},
		OICs: map[string]resourceProcess[protocol.OICStats]{"lab-a": {PID: 102, Stats: x.OIC}}}
	r := protocol.Register{Header: protocol.Header{Type: protocol.MsgRegister, Seq: 2}}
	a := protocol.RegisterAck{Header: protocol.Header{Type: protocol.MsgRegisterAck, Seq: 2}}
	for _, id := range g2CompetitionSourceIDs() {
		b := protocol.ObservationBinding{AccessID: "lab-a", Epoch: x.OIC.Epoch, Generation: x.OIC.Generation,
			ExposureID: id, Revision: "rv_" + strings.Repeat("a", 26), ObservationRevision: "ob_1", ContractDigest: strings.Repeat("a", 64), SourceID: "local", ContractVersion: 1}
		r.Exposures = append(r.Exposures, protocol.RegisterExposure{ExposureID: id, Revision: b.Revision, ProbeSupport: protocol.ProbeSupported, Binding: &b})
		a.Accepted = append(a.Accepted, id)
	}
	rawR, _ := json.Marshal(r)
	rawA, _ := json.Marshal(a)
	now := resourceNS()
	s := g2ProxySnapshot{Events: []g2WireEvent{
		{ID: 1, ConnID: 9, Role: g2RoleControl, Type: protocol.MsgRegister, Direction: g2OICToOIS, ReceivedNS: now - 3, WriteStartedNS: now - 2, WrittenNS: now - 1, Raw: rawR},
		{ID: 2, ConnID: 9, Role: g2RoleControl, Type: protocol.MsgRegisterAck, Direction: g2OISToOIC, ReceivedNS: now - 2, WriteStartedNS: now - 1, WrittenNS: now, Raw: rawA},
	}}
	return d, s, base
}

func g2EditProbeRegister(t *testing.T, s *g2ProxySnapshot, edit func(*protocol.Register)) {
	t.Helper()
	var r protocol.Register
	if err := json.Unmarshal(s.Events[0].Raw, &r); err != nil {
		t.Fatal(err)
	}
	edit(&r)
	s.Events[0].Raw, _ = json.Marshal(r)
}

func g2EditProbeAck(t *testing.T, s *g2ProxySnapshot, edit func(*protocol.RegisterAck)) {
	t.Helper()
	var a protocol.RegisterAck
	if err := json.Unmarshal(s.Events[1].Raw, &a); err != nil {
		t.Fatal(err)
	}
	edit(&a)
	s.Events[1].Raw, _ = json.Marshal(a)
}

func TestG2ProbeQueueWitness(t *testing.T) {
	t.Run("queue_without_joint_or_young", func(t *testing.T) {
		d, s, base := g2ProbeQueueInput(t)
		d.lastSample.OIS.Observation.ObservationBacklog, d.lastSample.OIS.Observation.PendingQueries = 3, 1
		d.full(d.lastSample)
		snapshotNS := resourceNS()
		if !d.probeQueue(s, snapshotNS, base) || !d.returned(true, s, snapshotNS) || d.acceptedPreload() != nil {
			t.Fatal("real probe queue should not require the later joint gate")
		}
		if g2CompetitionFull(d.lastSample) || d.Young != nil || d.Queries != nil || d.lastQueryWitness != nil ||
			d.current.Young != nil || d.current.Queries != nil || d.current.ProbeQueue == nil || d.current.Full.Backlog16 ||
			len(d.preloadQueueWitness.Bindings) != 16 || d.preloadQueueWitness.ControlConnID != 9 {
			t.Fatal("preload changed full/young or lost original identity")
		}
	})
	for _, tc := range []struct {
		name string
		edit func(*g2PreparationFailure, *g2ProxySnapshot, *resourceProfile)
	}{
		{"no_active", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) {
			d.lastSample.OIC.Observation.ProbeActive = 0
		}},
		{"no_queue", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) {
			d.lastSample.OIC.Observation.ProbeQueued = 0
		}},
		{"wrong_jobs", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) {
			d.lastSample.OIC.Observation.ProbeJobs = 15
		}},
		{"wrong_capacity", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) {
			d.lastSample.OIS.Observation.ObservationCapacity = 15
		}},
		{"wrong_ois_pid", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) { d.lastOISPID++ }},
		{"wrong_oic_pid", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) { d.lastOICPID++ }},
		{"offline", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) {
			d.lastSample.OIS.AccessesOnline = 1
		}},
		{"unregistered", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) {
			d.lastSample.OIC.Registered = false
		}},
		{"generation_changed", func(d *g2PreparationFailure, _ *g2ProxySnapshot, _ *resourceProfile) { d.lastSample.OIC.Generation++ }},
		{"missing_binding", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[0].Binding = nil })
		}},
		{"missing_source", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures = r.Exposures[:15] })
		}},
		{"duplicate_source", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[15] = r.Exposures[0] })
		}},
		{"extra_source", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures = append(r.Exposures, r.Exposures[0]) })
		}},
		{"wrong_access", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[0].Binding.AccessID = "lab-b" })
		}},
		{"wrong_epoch", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[0].Binding.Epoch++ })
		}},
		{"wrong_revision", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[0].Binding.Revision = "rv_" + strings.Repeat("b", 26) })
		}},
		{"wrong_digest", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[0].Binding.ContractDigest = "bad" })
		}},
		{"unsupported", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeRegister(t, s, func(r *protocol.Register) { r.Exposures[0].ProbeSupport = protocol.ProbeUnsupported })
		}},
		{"ack_rejected", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeAck(t, s, func(a *protocol.RegisterAck) { a.Rejected = []protocol.Rejection{{ExposureID: "lab-a/http"}} })
		}},
		{"ack_duplicate", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			g2EditProbeAck(t, s, func(a *protocol.RegisterAck) { a.Accepted[15] = a.Accepted[0] })
		}},
		{"other_control", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) { s.Events[1].ConnID++ }},
		{"unwritten_register", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) { s.Events[0].WrittenNS = 0 }},
		{"unwritten_ack", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) { s.Events[1].WrittenNS = 0 }},
		{"future_written", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			s.Events[1].WrittenNS = resourceNS() + int64(time.Second)
		}},
		{"injected", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) { s.Events[0].Injected = true }},
		{"write_failed", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) { s.Events[1].WriteFailed = true }},
		{"preload_drop", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) {
			s.Events = append(s.Events, g2WireEvent{ID: 3, Dropped: true})
		}},
		{"overflow", func(_ *g2PreparationFailure, s *g2ProxySnapshot, _ *resourceProfile) { s.Overflow = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s, base := g2ProbeQueueInput(t)
			tc.edit(d, &s, &base)
			if d.probeQueue(s, resourceNS(), base) {
				t.Fatal("invalid queue identity or original wire accepted")
			}
		})
	}
	t.Run("owned_snapshot_and_sample", func(t *testing.T) {
		d, s, base := g2ProbeQueueInput(t)
		now := resourceNS()
		if !d.probeQueue(s, now, base) || !d.returned(true, s, now) || d.acceptedPreload() != nil {
			t.Fatal("valid queue")
		}
		w := d.preloadQueueWitness
		frozen, _ := json.Marshal(w)
		d.lastSample.OIC.Observation.ProbeActive = 0
		d.lastSample.OIC.Closed["changed"] = 1
		s.Events[0].WrittenNS = 0
		s.Events[0].Raw[0] = 'x'
		if b, _ := json.Marshal(w); !reflect.DeepEqual(b, frozen) {
			t.Fatal("accepted witness aliases a later sample or Snapshot")
		}
	})
}

func TestG2ProbeFirstArmOrder(t *testing.T) {
	for _, mode := range []string{"complete", "not_accepted", "expired_before_arm", "partial_error", "partial_deadline", "duplicate_arm", "late_accept", "late_final", "unarmed_final"} {
		t.Run(mode, func(t *testing.T) {
			d, s, base := g2ProbeQueueInput(t)
			var actions []string
			set := func(id string, _ bool) uint64 { actions = append(actions, id); return uint64(len(actions)) }
			if err := d.preloadSources(set); err != nil {
				t.Fatal(err)
			}
			// Queue identity validation is tested independently; this uses its
			// actual returned witness and tests all subsequent owner boundaries.
			now := resourceNS()
			if !d.probeQueue(s, now, base) || !d.returned(true, s, now) {
				t.Fatal("valid queue")
			}
			if mode == "late_accept" {
				d.DeadlineNS = resourceNS() - 1
			}
			acceptedErr := d.acceptedPreload()
			if mode == "late_accept" {
				if acceptedErr == nil || d.preloadQueueWitness != nil || d.preloadAcceptedNS != nil {
					t.Fatal("late callback promoted to accepted queue")
				}
				return
			}
			if acceptedErr != nil {
				t.Fatal(acceptedErr)
			}
			if mode == "not_accepted" {
				d.preloadQueueWitness = nil
			}
			if mode == "expired_before_arm" {
				d.DeadlineNS = resourceNS() - 1
			}
			if mode == "unarmed_final" {
				if d.finishSlowSources(set, resourceOrigin.Add(time.Duration(d.DeadlineNS))) == nil || len(actions) != 8 {
					t.Fatal("final Set ran before arm")
				}
				return
			}
			calls := 0
			arm := func() error {
				calls++
				actions = append(actions, "arm")
				if calls == 4 && mode == "partial_error" {
					return errors.New("arm failed")
				}
				if calls == 4 && mode == "partial_deadline" {
					d.DeadlineNS = resourceNS() - 1
				}
				return nil
			}
			err := d.armSnapshotDrops(arm)
			if mode == "not_accepted" || mode == "expired_before_arm" || mode == "partial_error" || mode == "partial_deadline" {
				if err == nil || d.dropArmCompletedNS != nil || d.finishSlowSources(set, resourceOrigin.Add(time.Duration(d.DeadlineNS))) == nil || d.finalSetStartedNS != nil {
					t.Fatal("invalid or incomplete arm led to final input")
				}
				if (mode == "not_accepted" || mode == "expired_before_arm") && calls != 0 {
					t.Fatal("arm called without accepted original budget")
				}
				if mode == "partial_error" && d.dropArmCount != 3 || mode == "partial_deadline" && d.dropArmCount != 4 {
					t.Fatal("partial arm fabricated completed count")
				}
				return
			}
			if err != nil || calls != 32 || d.dropArmCount != 32 {
				t.Fatal("one complete 32 Drop batch", err, calls)
			}
			if mode == "duplicate_arm" {
				if d.armSnapshotDrops(arm) == nil || calls != 32 {
					t.Fatal("second Drop quota accepted")
				}
			}
			if mode == "late_final" {
				d.DeadlineNS = resourceNS() - 1
				if d.finishSlowSources(set, resourceOrigin.Add(time.Duration(d.DeadlineNS))) == nil || len(actions) != 40 {
					t.Fatal("final input renewed original deadline")
				}
				return
			}
			if err := d.finishSlowSources(set, resourceOrigin.Add(time.Duration(d.DeadlineNS))); err != nil || len(actions) != 48 || len(d.inputVersions) != 16 ||
				*d.preloadAcceptedNS > *d.dropArmStartedNS || *d.dropArmStartedNS > *d.dropArmCompletedNS || *d.dropArmCompletedNS > *d.finalSetStartedNS {
				t.Fatal("first8/accepted/arm32/final8 order changed", err, actions)
			}
		})
	}
}

func TestG2ProbeFirstHistoryAcrossArm(t *testing.T) {
	d, s, base := g2ProbeQueueInput(t)
	now := resourceNS()
	if !d.probeQueue(s, now, base) || !d.returned(true, s, now) || d.acceptedPreload() != nil || d.armSnapshotDrops(func() error { return nil }) != nil {
		t.Fatal("valid original preload")
	}
	old := d.history[0]
	// Received occurs before capture's lock. A legitimate future capture can
	// have this earlier timestamp without changing the already-seen prefix.
	s.Events = append(s.Events, g2WireEvent{ID: 3, ConnID: 9, Type: protocol.MsgObservationSnapshot,
		Role: g2RoleControl, Direction: g2OICToOIS, Dropped: true, QueryID: "query", ReceivedNS: *d.dropArmStartedNS - 1})
	d.Phase, d.Attempts = "slow_full", 2
	d.beginHistory(101, 102)
	d.lastQueueWitness = nil
	d.returned(false, s, resourceNS())
	d.returned(false, s, resourceNS())
	if d.historyIncomplete || d.dropCount != 1 || d.totalDropped != 1 || *old.DroppedCount != 0 ||
		*old.WireEventCount != 2 || old.Young != nil || old.Queries != nil || d.drops[0].EventID != 3 {
		t.Fatal("cross-arm history lost/recounted a Drop or backfilled preload")
	}
}

func TestG2ProbeFirstQueryBinding(t *testing.T) {
	for _, mode := range []string{"same", "other_control", "other_ois_pid", "other_oic_pid", "other_binding", "duplicate_source", "missing_query", "not_young"} {
		t.Run(mode, func(t *testing.T) {
			d, s, base := g2ProbeQueueInput(t)
			if !d.probeQueue(s, resourceNS(), base) {
				t.Fatal("valid queue")
			}
			w := d.lastQueueWitness
			q := &g2QueryWitness{OISPID: w.OISPID, OICPID: w.OICPID, Result: true}
			ids := g2CompetitionSourceIDs()
			for _, id := range ids[:8] {
				q.Queries = append(q.Queries, g2PreparationQuery{ConnID: w.ControlConnID, Binding: w.Bindings[id]})
			}
			switch mode {
			case "other_control":
				q.Queries[0].ConnID++
			case "other_ois_pid":
				q.OISPID++
			case "other_oic_pid":
				q.OICPID++
			case "other_binding":
				q.Queries[0].Binding.ObservationRevision = "ob_2"
			case "duplicate_source":
				q.Queries[0] = q.Queries[1]
			case "missing_query":
				q.Queries = q.Queries[:7]
			case "not_young":
				q.Result = false
			}
			if g2ProbeQueueMatchesQueries(w, q) != (mode == "same") {
				t.Fatal("later query must keep original control, binding and product identity")
			}
		})
	}
}
