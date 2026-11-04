package integration

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const g3ResourceSchema = "g3-resource-v1"

type g3OICTopology struct {
	Connected  bool `json:"connected"`
	Registered bool `json:"registered"`
}

type g3ControlTopology struct {
	ControlOpen         bool `json:"control_open"`
	DownstreamPaused    bool `json:"downstream_paused"`
	PendingControlCount int  `json:"pending_control_count"`
}

type g3ExpectedTopology struct {
	OISAccessesOnline       int                          `json:"ois_accesses_online"`
	OICs                    map[string]g3OICTopology     `json:"oics"`
	Controls                map[string]g3ControlTopology `json:"controls"`
	ActiveDataConnections   int                          `json:"active_data_connections"`
	ActiveTargetWorkers     int                          `json:"active_target_workers"`
	UnclassifiedConnections int                          `json:"unclassified_connections"`
}

// ControlOpen is the proxy's local connection lifetime, not a claim that an
// unread remote FIN has been observed. OIS online state is sampled separately.
type g3ObservedControl struct {
	ControlConnID     string   `json:"control_conn_id"`
	ControlOpen       bool     `json:"control_open"`
	DownstreamPaused  bool     `json:"downstream_paused"`
	PendingControlIDs []string `json:"pending_control_ids"`
}

type g3ObservedTopology struct {
	Controls                  map[string]g3ObservedControl `json:"controls"`
	ActiveDataConnectionIDs   []string                     `json:"active_data_connection_ids"`
	ActiveTargetWorkerIDs     []string                     `json:"active_target_worker_ids"`
	UnclassifiedConnectionIDs []string                     `json:"unclassified_connection_ids"`
}

type g3ResourceSample struct {
	SampleStartNS int64                                         `json:"sample_start_ns"`
	SampleEndNS   int64                                         `json:"sample_end_ns"`
	OIS           resourceProcess[protocol.OISStats]            `json:"ois"`
	OICs          map[string]resourceProcess[protocol.OICStats] `json:"oics"`
	Observed      g3ObservedTopology                            `json:"observed"`
}

type g3OICBinding struct {
	PID        int   `json:"pid"`
	Epoch      int64 `json:"epoch"`
	Generation int64 `json:"generation"`
}

type g3ResourceBinding struct {
	OISPID            int                     `json:"ois_pid"`
	OICs              map[string]g3OICBinding `json:"oics"`
	ControlConnIDs    map[string]string       `json:"control_conn_ids"`
	PendingControlIDs map[string][]string     `json:"pending_control_ids"`
}

type g3ResourceProfile struct {
	ID, Kind   string
	RecordedNS int64
	Sample     g3ResourceSample
	Expected   g3ExpectedTopology
}

type g3Resources struct {
	e                   *env
	vector, scenario    string
	observe             func() (g3ObservedTopology, error)
	profiles            map[string]g3ResourceProfile
	validationStarted   bool
	preparationComplete bool
	checks              map[string]bool
}

func newG3Resources(e *env, vector, scenario string, observe func() (g3ObservedTopology, error)) *g3Resources {
	return &g3Resources{e: e, vector: vector, scenario: scenario, observe: observe,
		profiles: map[string]g3ResourceProfile{}, checks: map[string]bool{}}
}

func (r *g3Resources) capture(ctx context.Context) (g3ResourceSample, error) {
	s, err := r.e.resourceSnapshot(ctx)
	if err != nil {
		return g3ResourceSample{}, err
	}
	o, err := r.observe()
	if err != nil {
		return g3ResourceSample{}, err
	}
	return g3ResourceSample{s.StartedNS, resourceNS(), s.OIS, s.OICs, o}, nil
}

func (r *g3Resources) emit(kind, stage string, p g3ResourceProfile, phase string, cycle *int, fields map[string]any) {
	fields["schema"], fields["vector"] = g3ResourceSchema, r.vector
	fields["package"] = "github.com/wyw-ai/loom-oi/tests/integration"
	fields["scenario"], fields["stage"] = r.scenario, stage
	fields["profile_id"], fields["profile_kind"] = p.ID, p.Kind
	fields["phase"], fields["cycle"] = nil, cycle
	if phase != "" {
		fields["phase"] = phase
	}
	r.e.obs("g3_resources_"+kind, fields)
}

func g3Binding(s g3ResourceSample) g3ResourceBinding {
	b := g3ResourceBinding{OISPID: s.OIS.PID, OICs: map[string]g3OICBinding{},
		ControlConnIDs: map[string]string{}, PendingControlIDs: map[string][]string{}}
	for a, x := range s.OICs {
		b.OICs[a] = g3OICBinding{x.PID, x.Stats.Epoch, x.Stats.Generation}
	}
	for a, c := range s.Observed.Controls {
		b.ControlConnIDs[a] = c.ControlConnID
		b.PendingControlIDs[a] = append([]string{}, c.PendingControlIDs...)
	}
	return b
}

func g3ValidIDs(ids []string) bool {
	if ids == nil {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

func g3ValidateCapture(s g3ResourceSample, expected g3ExpectedTopology, start, deadline int64) error {
	if s.SampleStartNS < start || s.SampleEndNS <= s.SampleStartNS || s.SampleEndNS > deadline ||
		s.OIS.PID <= 0 || s.OIS.SampledNS < s.SampleStartNS || s.OIS.SampledNS > s.SampleEndNS ||
		len(s.OICs) != len(expected.OICs) || len(s.Observed.Controls) != len(expected.Controls) {
		return fmt.Errorf("G3 resource capture identity/time/role mismatch")
	}
	if err := validOISStats(s.OIS.Stats); err != nil {
		return err
	}
	seenPIDs := map[int]bool{s.OIS.PID: true}
	for a := range expected.OICs {
		x, exists := s.OICs[a]
		if !exists || x.PID <= 0 || x.Stats.AccessID != a || x.Stats.Epoch <= 0 || x.Stats.Generation <= 0 ||
			x.SampledNS < s.SampleStartNS || x.SampledNS > s.SampleEndNS {
			return fmt.Errorf("G3 OIC %s identity/time invalid", a)
		}
		if err := validOICStats(x.Stats); err != nil {
			return err
		}
		if seenPIDs[x.PID] {
			return fmt.Errorf("G3 roles share process PID %d", x.PID)
		}
		seenPIDs[x.PID] = true
	}
	seenControlIDs := map[string]bool{}
	for a := range expected.Controls {
		c, exists := s.Observed.Controls[a]
		if !exists || c.ControlConnID == "" || !g3ValidIDs(c.PendingControlIDs) {
			return fmt.Errorf("G3 control %s identity invalid", a)
		}
		for _, id := range append([]string{c.ControlConnID}, c.PendingControlIDs...) {
			if seenControlIDs[id] {
				return fmt.Errorf("G3 control/pending identity reused: %s", id)
			}
			seenControlIDs[id] = true
		}
	}
	if !g3ValidIDs(s.Observed.ActiveDataConnectionIDs) || !g3ValidIDs(s.Observed.ActiveTargetWorkerIDs) ||
		!g3ValidIDs(s.Observed.UnclassifiedConnectionIDs) {
		return fmt.Errorf("G3 connection/worker identities invalid")
	}
	return nil
}

func g3TopologyOK(s g3ResourceSample, want g3ExpectedTopology) bool {
	ok := emptyOIS(s.OIS.Stats) && s.OIS.Stats.AccessesOnline == want.OISAccessesOnline &&
		len(s.Observed.ActiveDataConnectionIDs) == want.ActiveDataConnections &&
		len(s.Observed.ActiveTargetWorkerIDs) == want.ActiveTargetWorkers &&
		len(s.Observed.UnclassifiedConnectionIDs) == want.UnclassifiedConnections
	for a, w := range want.OICs {
		x := s.OICs[a].Stats
		ok = ok && x.Channels == 0 && x.Connected == w.Connected && x.Registered == w.Registered
	}
	for a, w := range want.Controls {
		x := s.Observed.Controls[a]
		ok = ok && x.ControlOpen == w.ControlOpen && x.DownstreamPaused == w.DownstreamPaused &&
			len(x.PendingControlIDs) == w.PendingControlCount
	}
	return ok
}

func g3StablePreparation(samples []g3ResourceSample, expected g3ExpectedTopology) bool {
	if len(samples) < 3 {
		return false
	}
	last := samples[len(samples)-3:]
	base := last[0]
	minG, maxG := base.OIS.Stats.Process.Goroutines, base.OIS.Stats.Process.Goroutines
	mins, maxs := map[string]int{}, map[string]int{}
	for a, x := range base.OICs {
		mins[a], maxs[a] = x.Stats.Process.Goroutines, x.Stats.Process.Goroutines
	}
	for _, s := range last {
		if !g3TopologyOK(s, expected) || !reflect.DeepEqual(g3Binding(base), g3Binding(s)) ||
			s.OIS.Stats.Process.OpenFDs != base.OIS.Stats.Process.OpenFDs {
			return false
		}
		minG, maxG = min(minG, s.OIS.Stats.Process.Goroutines), max(maxG, s.OIS.Stats.Process.Goroutines)
		for a, x := range s.OICs {
			if x.Stats.Process.OpenFDs != base.OICs[a].Stats.Process.OpenFDs {
				return false
			}
			mins[a], maxs[a] = min(mins[a], x.Stats.Process.Goroutines), max(maxs[a], x.Stats.Process.Goroutines)
		}
	}
	if maxG-minG > 2 {
		return false
	}
	for a := range mins {
		if maxs[a]-mins[a] > 2 {
			return false
		}
	}
	return true
}

func (r *g3Resources) allowFreeze(kind string) error {
	if r.preparationComplete || r.validationStarted || r.profiles[kind].ID != "" {
		return fmt.Errorf("G3 profile %s cannot be recaptured", kind)
	}
	if kind != "online" && kind != "control_cut" && kind != "control_blackhole" {
		return fmt.Errorf("unknown G3 profile kind %q", kind)
	}
	if kind != "online" && r.profiles["online"].ID == "" {
		return fmt.Errorf("G3 fault preparation requires the original online profile")
	}
	return nil
}

// freeze is used only during initial preparation. The caller applies a control
// rehearsal before this call, never after a validation phase has started.
func (r *g3Resources) freeze(kind string, expected g3ExpectedTopology, start int64) error {
	if err := r.allowFreeze(kind); err != nil {
		return err
	}
	deadline := start + int64(5*time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(deadline)))
	defer cancel()
	r.e.closeIdle()
	samples := []g3ResourceSample{}
	frozen := false
	defer func() {
		if !frozen {
			r.e.obs("G3-resource-preparation-failure", map[string]any{
				"vector": r.vector, "scenario": r.scenario, "profile_kind": kind,
				"prepare_start_ns": start, "prepare_deadline_ns": deadline,
				"preparation_samples": samples, "expected_topology": expected})
		}
	}()
	for {
		s, err := r.capture(ctx)
		if err != nil {
			return err
		}
		if err = g3ValidateCapture(s, expected, start, deadline); err != nil {
			return err
		}
		if len(samples) > 0 && (s.OIS.PID != samples[0].OIS.PID || !g3SamePIDs(samples[0], s)) {
			return fmt.Errorf("G3 process changed during baseline preparation")
		}
		if online := r.profiles["online"]; online.ID != "" && !g3SamePIDs(online.Sample, s) {
			return fmt.Errorf("G3 process changed between topology profiles")
		}
		samples = append(samples, s)
		if g3StablePreparation(samples, expected) {
			p := g3ResourceProfile{resourceID("g3-profile"), kind, resourceNS(), s, expected}
			if p.RecordedNS > deadline {
				return fmt.Errorf("G3 profile completed after preparation deadline")
			}
			r.profiles[kind] = p
			frozen = true
			r.emit("baseline", "preparation", p, "", nil, map[string]any{
				"prepare_start_ns": start, "prepare_deadline_ns": deadline, "preparation_samples": samples,
				"recorded_ns": p.RecordedNS, "ois": s.OIS, "oics": s.OICs, "observed": s.Observed,
				"expected_topology": expected})
			return nil
		}
		if err = g3WaitSample(ctx); err != nil {
			return fmt.Errorf("G3 baseline did not stabilize: %w", err)
		}
	}
}

func g3SamePIDs(a, b g3ResourceSample) bool {
	if a.OIS.PID != b.OIS.PID || len(a.OICs) != len(b.OICs) {
		return false
	}
	for role, x := range a.OICs {
		if x.PID != b.OICs[role].PID {
			return false
		}
	}
	return true
}

func g3SampleOK(p g3ResourceProfile, binding g3ResourceBinding, s g3ResourceSample, start, deadline int64) (bool, error) {
	if p.ID == "" || p.RecordedNS >= start || p.Sample.SampleEndNS >= start {
		return false, fmt.Errorf("G3 baseline must precede the cleanup")
	}
	if err := g3ValidateCapture(s, p.Expected, start, deadline); err != nil {
		return false, err
	}
	if !g3SamePIDs(p.Sample, s) || !reflect.DeepEqual(binding, g3Binding(s)) {
		return false, fmt.Errorf("G3 process/session/connection binding changed during cleanup")
	}
	ok := g3TopologyOK(s, p.Expected) && processAtBaseline(p.Sample.OIS.Stats.Process, s.OIS.Stats.Process)
	for a, x := range s.OICs {
		ok = ok && processAtBaseline(p.Sample.OICs[a].Stats.Process, x.Stats.Process)
	}
	return ok, nil
}

func g3WaitSample(ctx context.Context) error {
	t := time.NewTimer(100 * time.Millisecond)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func g3BindingReady(kind string, s g3ResourceSample) (bool, error) {
	if kind != "control_cut" {
		return true, nil
	}
	ids := s.Observed.Controls["lab-a"].PendingControlIDs
	if len(ids) > 1 {
		return false, fmt.Errorf("control cut has more than one pending attempt")
	}
	return len(ids) == 1, nil
}

// check never invents a fresh start after a fault or a slow cleanup callback.
// For RST, start is the first Reset's appliedNS. The initial binding fetch,
// callback, closeIdle, samples and completion all consume the same deadline.
func (r *g3Resources) check(kind, stage, phase string, cycle *int, start int64, origin string, cleanup func(context.Context) error) error {
	p := r.profiles[kind]
	if p.ID == "" || start <= p.RecordedNS || (stage != "preparation" && stage != "validation") {
		return fmt.Errorf("G3 cleanup has no prior profile or valid stage")
	}
	key := fmt.Sprintf("%s/%s/%v", stage, phase, cycleValue(cycle))
	if r.checks[key] {
		return fmt.Errorf("duplicate G3 resource phase %s", key)
	}
	r.checks[key] = true
	if stage == "validation" {
		r.validationStarted = true
	}
	deadline := start + int64(2*time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), resourceOrigin.Add(time.Duration(deadline)))
	defer cancel()
	var first g3ResourceSample
	bindingSamples := []g3ResourceSample{}
	bound := false
	defer func() {
		if !bound {
			r.e.obs("G3-resource-binding-failure", map[string]any{
				"vector": r.vector, "scenario": r.scenario, "stage": stage, "phase": phase, "cycle": cycle,
				"profile_id": p.ID, "profile_kind": kind, "start_ns": start, "deadline_ns": deadline, "binding_samples": bindingSamples})
		}
	}()
	for {
		var err error
		first, err = r.capture(ctx)
		if err != nil {
			return err
		}
		if err = g3ValidateCapture(first, p.Expected, start, deadline); err != nil {
			return err
		}
		if !g3SamePIDs(p.Sample, first) {
			return fmt.Errorf("G3 PID changed before cleanup")
		}
		bindingSamples = append(bindingSamples, first)
		ready, err := g3BindingReady(kind, first)
		if err != nil {
			return err
		}
		if ready {
			break
		}
		if err = g3WaitSample(ctx); err != nil {
			return fmt.Errorf("G3 binding not ready inside original deadline: %w", err)
		}
	}
	binding, id := g3Binding(first), resourceID("g3-check")
	bound = true
	r.emit("begin", stage, p, phase, cycle, map[string]any{"check_id": id, "start_ns": start, "deadline_ns": deadline,
		"origin_event": origin, "origin_ns": start, "binding": binding, "binding_samples": bindingSamples})
	if cleanup != nil {
		if err := cleanup(ctx); err != nil {
			return err
		}
	}
	r.e.closeIdle()
	consecutive, count, previousEnd := 0, 0, first.SampleEndNS
	for {
		s, err := r.capture(ctx)
		if err != nil {
			return err
		}
		count++
		r.emit("sample", stage, p, phase, cycle, map[string]any{"check_id": id, "start_ns": start, "deadline_ns": deadline,
			"sample_start_ns": s.SampleStartNS, "sample_end_ns": s.SampleEndNS, "ois": s.OIS, "oics": s.OICs, "observed": s.Observed})
		if s.SampleStartNS < previousEnd || (count > 1 && s.SampleStartNS == previousEnd) {
			return fmt.Errorf("G3 resource sampling did not advance")
		}
		previousEnd = s.SampleEndNS
		ok, err := g3SampleOK(p, binding, s, start, deadline)
		if err != nil {
			return err
		}
		if ok {
			consecutive++
		} else {
			consecutive = 0
		}
		if consecutive == 3 {
			completed := resourceNS()
			if completed > deadline {
				return fmt.Errorf("G3 resource completion exceeded fixed deadline")
			}
			r.emit("complete", stage, p, phase, cycle, map[string]any{"check_id": id, "start_ns": start, "deadline_ns": deadline,
				"completed_ns": completed, "settled_ms": (completed - start) / int64(time.Millisecond), "samples": count})
			return nil
		}
		if err = g3WaitSample(ctx); err != nil {
			return fmt.Errorf("G3 resources did not settle in the fixed window: %w", err)
		}
	}
}

func cycleValue(cycle *int) any {
	if cycle == nil {
		return nil
	}
	return *cycle
}

func g3ResourceTestSample() (g3ResourceProfile, g3ResourceSample) {
	b, s := resourceTestProfile()
	o := g3ObservedTopology{Controls: map[string]g3ObservedControl{"lab-a": {"access/lab-a/1", true, false, []string{}}},
		ActiveDataConnectionIDs: []string{}, ActiveTargetWorkerIDs: []string{}, UnclassifiedConnectionIDs: []string{}}
	w := g3ExpectedTopology{OISAccessesOnline: 1, OICs: map[string]g3OICTopology{"lab-a": {true, true}},
		Controls: map[string]g3ControlTopology{"lab-a": {true, false, 0}}}
	p := g3ResourceProfile{b.ID, "online", b.RecordedNS, g3ResourceSample{1, 19, b.OIS, b.OICs, o}, w}
	return p, g3ResourceSample{s.StartedNS, s.EndedNS, s.OIS, s.OICs, o}
}

func TestG3ResourceTopologyProfiles(t *testing.T) {
	for _, name := range []string{"ois_oic_pid", "oic_oic_pid", "shared_control", "pending_is_control", "shared_pending"} {
		t.Run("identity_"+name, func(t *testing.T) {
			p, s := g3ResourceTestSample()
			x := s.OICs["lab-a"]
			x.PID = 3
			x.Stats.AccessID = "lab-b"
			s.OICs["lab-b"] = x
			p.Expected.OICs["lab-b"] = g3OICTopology{true, true}
			p.Expected.Controls["lab-b"] = g3ControlTopology{true, false, 0}
			s.Observed.Controls["lab-b"] = g3ObservedControl{"access/lab-b/1", true, false, []string{}}
			switch name {
			case "ois_oic_pid":
				s.OIS.PID = 3
			case "oic_oic_pid":
				x.PID = s.OICs["lab-a"].PID
				s.OICs["lab-b"] = x
			case "shared_control":
				c := s.Observed.Controls["lab-b"]
				c.ControlConnID = s.Observed.Controls["lab-a"].ControlConnID
				s.Observed.Controls["lab-b"] = c
			case "pending_is_control":
				c := s.Observed.Controls["lab-b"]
				c.PendingControlIDs = []string{s.Observed.Controls["lab-a"].ControlConnID}
				s.Observed.Controls["lab-b"] = c
			case "shared_pending":
				for _, a := range []string{"lab-a", "lab-b"} {
					c := s.Observed.Controls[a]
					c.PendingControlIDs = []string{"duplicated-pending"}
					s.Observed.Controls[a] = c
				}
			}
			if err := g3ValidateCapture(s, p.Expected, 100, 200); err == nil {
				t.Fatal("impossible process/control identity accepted")
			}
		})
	}
	for _, kind := range []string{"online", "control_cut", "control_blackhole"} {
		t.Run("environment_"+kind, func(t *testing.T) {
			each(t, func(t *testing.T, be string) {
				opts := g3EnvOpts{}
				if kind != "online" {
					opts.ControlProfile = kind
				}
				g := newG3Env(t, be, "TV-G3-FIXTURE", "environment_"+kind, opts)
				if g.resourceBase.ID != "" {
					t.Fatal("G3 initialization also froze the legacy profile")
				}
				if g.resources.profiles["online"].ID == "" {
					t.Fatal("online profile absent")
				}
			})
		})
	}
	for _, kind := range []string{"online", "control_cut", "control_blackhole"} {
		t.Run(kind, func(t *testing.T) {
			p, s := g3ResourceTestSample()
			if kind != "online" {
				p.Kind, p.Expected.OISAccessesOnline, s.OIS.Stats.AccessesOnline = kind, 0, 0
				c := s.Observed.Controls["lab-a"]
				if kind == "control_cut" {
					p.Expected.OICs["lab-a"] = g3OICTopology{false, false}
					p.Expected.Controls["lab-a"] = g3ControlTopology{false, false, 1}
					x := s.OICs["lab-a"]
					x.Stats.Connected, x.Stats.Registered = false, false
					s.OICs["lab-a"] = x
					c.ControlOpen, c.PendingControlIDs = false, []string{"access/lab-a/2"}
				} else {
					p.Expected.Controls["lab-a"] = g3ControlTopology{true, true, 0}
					c.DownstreamPaused = true
				}
				s.Observed.Controls["lab-a"] = c
			}
			binding := g3Binding(s)
			if ok, err := g3SampleOK(p, binding, s, 100, 200); err != nil || !ok {
				t.Fatalf("valid %s topology: %v %v", kind, ok, err)
			}
			s.Observed.ActiveDataConnectionIDs = []string{"access/lab-a/leaked-data"}
			if ok, err := g3SampleOK(p, binding, s, 100, 200); err != nil || ok {
				t.Fatalf("FD-equal data leak accepted: %v %v", ok, err)
			}
		})
	}
	for _, name := range []string{"pid_changed", "missing_role", "generation_changed", "fd_less", "fd_more", "goroutine_below", "goroutine_above", "wrong_topology", "missing_observed"} {
		t.Run(name, func(t *testing.T) {
			p, s := g3ResourceTestSample()
			binding := g3Binding(s)
			switch name {
			case "pid_changed":
				s.OIS.PID++
			case "missing_role":
				delete(s.OICs, "lab-a")
			case "generation_changed":
				x := s.OICs["lab-a"]
				x.Stats.Generation++
				s.OICs["lab-a"] = x
			case "fd_less":
				s.OIS.Stats.Process.OpenFDs--
			case "fd_more":
				s.OIS.Stats.Process.OpenFDs++
			case "goroutine_below":
				s.OIS.Stats.Process.Goroutines -= 11
			case "goroutine_above":
				s.OIS.Stats.Process.Goroutines += 11
			case "wrong_topology":
				s.OIS.Stats.AccessesOnline = 0
			case "missing_observed":
				s.Observed.ActiveDataConnectionIDs = nil
			}
			if ok, err := g3SampleOK(p, binding, s, 100, 200); err == nil && ok {
				t.Fatal("invalid capture accepted")
			}
		})
	}
}

func TestG3ResourceDeadlineDoesNotReset(t *testing.T) {
	t.Run("pending_identity_wait_does_not_require_resource_settle", func(t *testing.T) {
		_, s := g3ResourceTestSample()
		if ready, err := g3BindingReady("control_cut", s); err != nil || ready {
			t.Fatalf("missing pending accepted: %v %v", ready, err)
		}
		c := s.Observed.Controls["lab-a"]
		c.PendingControlIDs = []string{"access/lab-a/next"}
		s.Observed.Controls["lab-a"] = c
		s.OIS.Stats.Channels.Open = 1
		s.OIS.Stats.Process.OpenFDs += 100
		s.Observed.ActiveDataConnectionIDs = []string{"still-cleaning-data"}
		if ready, err := g3BindingReady("control_cut", s); err != nil || !ready {
			t.Fatalf("identity wait improperly requires cleanup: %v %v", ready, err)
		}
		c.PendingControlIDs = append(c.PendingControlIDs, "another-attempt")
		s.Observed.Controls["lab-a"] = c
		if ready, err := g3BindingReady("control_cut", s); err == nil || ready {
			t.Fatal("ambiguous pending attempts accepted")
		}
	})
	for _, name := range []string{"before_start", "after_deadline", "zero_duration", "baseline_after_start"} {
		t.Run(name, func(t *testing.T) {
			p, s := g3ResourceTestSample()
			switch name {
			case "before_start":
				s.SampleStartNS = 99
			case "after_deadline":
				s.SampleEndNS = 201
			case "zero_duration":
				s.SampleStartNS = s.SampleEndNS
			case "baseline_after_start":
				p.RecordedNS = 100
			}
			if ok, err := g3SampleOK(p, g3Binding(s), s, 100, 200); err == nil || ok {
				t.Fatal("invalid fixed-deadline sample accepted")
			}
		})
	}
}

func TestG3BaselineCannotBeReplaced(t *testing.T) {
	for _, name := range []string{"stable", "goroutine_range_two", "goroutine_drift", "fd_changed", "busy_middle", "session_changed"} {
		t.Run(name, func(t *testing.T) {
			p, _ := g3ResourceTestSample()
			samples := make([]g3ResourceSample, 3)
			for i := range samples {
				_, samples[i] = g3ResourceTestSample()
			}
			want := name == "stable" || name == "goroutine_range_two"
			switch name {
			case "goroutine_range_two":
				samples[1].OIS.Stats.Process.Goroutines++
				samples[2].OIS.Stats.Process.Goroutines += 2
			case "goroutine_drift":
				samples[1].OIS.Stats.Process.Goroutines += 2
				samples[2].OIS.Stats.Process.Goroutines += 4
			case "fd_changed":
				samples[2].OIS.Stats.Process.OpenFDs++
			case "busy_middle":
				samples[1].OIS.Stats.Channels.Open = 1
			case "session_changed":
				x := samples[2].OICs["lab-a"]
				x.Stats.Generation++
				samples[2].OICs["lab-a"] = x
			}
			if got := g3StablePreparation(samples, p.Expected); got != want {
				t.Fatalf("stable=%v, want %v", got, want)
			}
		})
	}
	p, _ := g3ResourceTestSample()
	r := &g3Resources{profiles: map[string]g3ResourceProfile{"online": p}}
	if err := r.allowFreeze("online"); err == nil {
		t.Fatal("online replacement accepted")
	}
	if err := r.allowFreeze("control_cut"); err != nil {
		t.Fatal(err)
	}
	r.validationStarted = true
	if err := r.allowFreeze("control_cut"); err == nil {
		t.Fatal("post-fault profile accepted")
	}
	if err := r.allowFreeze("different-id"); err == nil {
		t.Fatal("new name bypassed profile freeze")
	}
	r.validationStarted = false
	r.preparationComplete = true
	if err := r.allowFreeze("control_blackhole"); err == nil {
		t.Fatal("new profile accepted after preparation, before first cleanup")
	}
}
