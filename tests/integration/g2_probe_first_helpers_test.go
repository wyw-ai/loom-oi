package integration

import (
	"encoding/json"
	"fmt"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

type g2ProbeQueuePredicate struct {
	Capacity16             bool `json:"capacity_16"`
	ProbeJobs16            bool `json:"probe_jobs_16"`
	ProbeActive2           bool `json:"probe_active_2"`
	ProbeQueued4           bool `json:"probe_queued_4"`
	IdentityValid          bool `json:"identity_valid"`
	RegisterInventoryValid bool `json:"register_inventory_valid"`
}

type g2RegistrationProjection struct {
	EventID        uint64 `json:"event_id"`
	ConnID         uint64 `json:"conn_id"`
	ReceivedNS     int64  `json:"received_ns"`
	WriteStartedNS int64  `json:"write_started_ns"`
	WrittenNS      int64  `json:"written_ns"`
}

type g2ProbeQueueWitness struct {
	OISPID                 int                                    `json:"ois_pid"`
	OICPID                 int                                    `json:"oic_pid"`
	Sample                 g2CompetitionStats                     `json:"sample"`
	SampleReturnedNS       int64                                  `json:"sample_returned_ns"`
	Attempt                int                                    `json:"attempt"`
	CallbackReturnedNS     *int64                                 `json:"callback_returned_ns"`
	WireEventCount         int                                    `json:"wire_event_count"`
	WireLastEventID        uint64                                 `json:"wire_last_event_id"`
	WireSnapshotReturnedNS int64                                  `json:"wire_snapshot_returned_ns"`
	EvaluatedNS            int64                                  `json:"evaluated_ns"`
	ReturnedNS             int64                                  `json:"returned_ns"`
	Result                 bool                                   `json:"result"`
	ControlConnID          uint64                                 `json:"control_conn_id"`
	Register               *g2RegistrationProjection              `json:"register"`
	RegisterAck            *g2RegistrationProjection              `json:"register_ack"`
	Bindings               map[string]protocol.ObservationBinding `json:"bindings"`
}

func g2RegistrationProject(e g2WireEvent) *g2RegistrationProjection {
	return &g2RegistrationProjection{e.ID, e.ConnID, e.ReceivedNS, e.WriteStartedNS, e.WrittenNS}
}

// Decode only the original Snapshot. A later write completion or replacement
// registration cannot supply a field that was absent when this predicate ran.
func g2ProbeRegistration(snapshot g2ProxySnapshot, snapshotNS int64, sample g2CompetitionStats, w *g2ProbeQueueWitness) bool {
	if snapshot.Overflow || snapshot.Failure != "" {
		return false
	}
	var reg, ack *g2WireEvent
	for i := range snapshot.Events {
		e := &snapshot.Events[i]
		if e.ID != uint64(i+1) || e.Dropped {
			return false // no Drop is armed during this preparation phase
		}
		if e.Role == g2RoleControl && e.Type == protocol.MsgRegister {
			reg, ack = e, nil
		} else if reg != nil && e.Role == g2RoleControl && e.Type == protocol.MsgRegisterAck && e.ConnID == reg.ConnID {
			ack = e
		}
	}
	valid := func(e *g2WireEvent, direction string) bool {
		return e != nil && e.ConnID > 0 && e.Direction == direction && !e.Injected && !e.Dropped && !e.WriteFailed &&
			e.ReceivedNS > 0 && e.WriteStartedNS >= e.ReceivedNS && e.WrittenNS >= e.WriteStartedNS && e.WrittenNS <= snapshotNS
	}
	if !valid(reg, g2OICToOIS) || !valid(ack, g2OISToOIC) {
		return false
	}
	w.ControlConnID, w.Register, w.RegisterAck = reg.ConnID, g2RegistrationProject(*reg), g2RegistrationProject(*ack)
	var r protocol.Register
	var a protocol.RegisterAck
	if json.Unmarshal(reg.Raw, &r) != nil || json.Unmarshal(ack.Raw, &a) != nil ||
		r.Type != protocol.MsgRegister || a.Type != protocol.MsgRegisterAck || len(r.Exposures) != 16 || len(a.Accepted) != 16 || len(a.Rejected) != 0 {
		return false
	}
	want := make(map[string]bool, 16)
	for _, id := range g2CompetitionSourceIDs() {
		want[id] = true
	}
	bindings := make(map[string]protocol.ObservationBinding, 16)
	for _, e := range r.Exposures {
		if !want[e.ExposureID] || e.Binding == nil || e.ProbeSupport != protocol.ProbeSupported {
			return false
		}
		b := *e.Binding
		if _, duplicate := bindings[e.ExposureID]; duplicate || !protocol.ValidObservationBinding(b, false) ||
			b.AccessID != "lab-a" || b.ExposureID != e.ExposureID || b.Revision != e.Revision ||
			b.SourceID != "local" || b.ContractVersion != 1 || b.Epoch != sample.OIS.Epoch ||
			b.Epoch != sample.OIC.Epoch || b.Generation != sample.OIC.Generation {
			return false
		}
		bindings[e.ExposureID] = b
	}
	for _, id := range a.Accepted {
		if !want[id] {
			return false
		}
		delete(want, id)
	}
	w.Bindings = bindings
	return len(want) == 0
}

// Preload establishes only the real probe queue. It does not call young or
// consume the later final/critical full+query predicates.
func (d *g2PreparationFailure) probeQueue(snapshot g2ProxySnapshot, snapshotNS int64, base resourceProfile) bool {
	x := d.lastSample
	p := g2ProbeQueuePredicate{}
	if o := x.OIS.Observation; o != nil {
		p.Capacity16 = o.ObservationCapacity == 16
	}
	if o := x.OIC.Observation; o != nil {
		p.ProbeJobs16, p.ProbeActive2, p.ProbeQueued4 = o.ProbeJobs == 16, o.ProbeActive == 2, o.ProbeQueued == 4
	}
	b, exists := base.OICs["lab-a"]
	p.IdentityValid = exists && d.lastOISPID > 0 && d.lastOICPID > 0 && d.lastOISPID == base.OIS.PID && d.lastOICPID == b.PID &&
		x.OIS.Epoch > 0 && x.OIS.Epoch == base.OIS.Stats.Epoch && x.OIC.Epoch == x.OIS.Epoch && x.OIC.Epoch == b.Stats.Epoch &&
		x.OIC.Generation > 0 && x.OIC.Generation == b.Stats.Generation && x.OIC.AccessID == "lab-a" &&
		x.OIS.AccessesOnline == 2 && x.OIC.Connected && x.OIC.Registered &&
		x.StartedNS >= d.StartedNS && x.CompletedNS >= x.StartedNS && d.lastSampleReturnedNS >= x.CompletedNS && snapshotNS >= d.lastSampleReturnedNS
	w := &g2ProbeQueueWitness{OISPID: d.lastOISPID, OICPID: d.lastOICPID, Sample: g2CopyCompetitionSample(x),
		SampleReturnedNS: d.lastSampleReturnedNS, Attempt: d.Attempts, WireEventCount: len(snapshot.Events),
		WireSnapshotReturnedNS: snapshotNS, EvaluatedNS: resourceNS()}
	if len(snapshot.Events) > 0 {
		w.WireLastEventID = snapshot.Events[len(snapshot.Events)-1].ID
	}
	p.RegisterInventoryValid = g2ProbeRegistration(snapshot, snapshotNS, x, w)
	w.Result = p.Capacity16 && p.ProbeJobs16 && p.ProbeActive2 && p.ProbeQueued4 && p.IdentityValid && p.RegisterInventoryValid
	w.ReturnedNS = resourceNS()
	d.lastQueueWitness, d.ProbeQueue = w, &p
	if d.current != nil {
		d.current.ProbeQueue = &p
	}
	return w.Result
}

func (d *g2PreparationFailure) armSnapshotDrops(arm func() error) error {
	if d.preloadQueueWitness == nil || !d.preloadQueueWitness.Result || d.preloadAcceptedNS == nil ||
		d.dropArmStartedNS != nil || d.totalDropped != 0 || d.dropCount != 0 {
		return fmt.Errorf("snapshot Drop requires one accepted probe queue before its sole arm batch")
	}
	if resourceNS() >= d.DeadlineNS {
		return fmt.Errorf("snapshot Drop arm has no original preparation budget")
	}
	d.dropArmStartedNS = g2DiagnosticPtr(resourceNS())
	for range 32 {
		if resourceNS() >= d.DeadlineNS {
			return fmt.Errorf("snapshot Drop arm exceeded original preparation deadline")
		}
		if err := arm(); err != nil {
			return err
		}
		d.dropArmCount++
	}
	d.dropArmCompletedNS = g2DiagnosticPtr(resourceNS())
	if *d.dropArmCompletedNS >= d.DeadlineNS {
		return fmt.Errorf("snapshot Drop completion exceeded original preparation deadline")
	}
	return nil
}

func g2ProbeQueueMatchesQueries(preload *g2ProbeQueueWitness, query *g2QueryWitness) bool {
	if preload == nil || !preload.Result || query == nil || !query.Result || len(query.Queries) != 8 ||
		preload.OISPID != query.OISPID || preload.OICPID != query.OICPID {
		return false
	}
	seen := make(map[string]bool, 8)
	for _, q := range query.Queries {
		b, exists := preload.Bindings[q.Binding.ExposureID]
		if !exists || q.ConnID != preload.ControlConnID || b != q.Binding || seen[b.ExposureID] {
			return false
		}
		seen[b.ExposureID] = true
	}
	return true
}
