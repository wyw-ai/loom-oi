package integration

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/client"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

type g3EnvOpts struct {
	Limits map[string]any
	// BeforeFreeze starts and warms any additional consumer (notably oi
	// connect) while the original profile is still absent.
	BeforeFreeze func(*g3Env)
	// ControlProfile selects the no-business rehearsal, if any.
	ControlProfile string
}

type g3Env struct {
	*env
	accessProxies map[string]*g3FaultProxy
	consumerProxy *g3FaultProxy
	targets       map[string]*fixture.PathTarget
	controls      map[string]uint64
	resources     *g3Resources
}

// The explicit arguments also shadow env.settled: a G3 vector cannot silently
// emit legacy online-only resource records or invent its cleanup start later.
func (g *g3Env) settled(kind, phase string, cycle *int, start int64, origin string, cleanup func(context.Context) error) {
	g.t.Helper()
	if err := g.resources.check(kind, "validation", phase, cycle, start, origin, cleanup); err != nil {
		g.t.Fatal(err)
	}
}

// newG3Env routes the very first OIC connection through its own instrument.
// No product is restarted and no earlier baseline is overwritten.
func newG3Env(t *testing.T, be, vector, scenario string, opts g3EnvOpts) *g3Env {
	t.Helper()
	g := &g3Env{accessProxies: map[string]*g3FaultProxy{}, targets: map[string]*fixture.PathTarget{}, controls: map[string]uint64{}}
	newEnv(t, be, envOpts{limits: opts.Limits,
		beforeOIC: func(e *env) {
			g.env = e
			for _, a := range []string{"lab-a", "lab-b"} {
				p := must(newG3FaultProxy(e.accessAddr, resourceNS))
				t.Cleanup(func() {
					if err := p.Close(); err != nil {
						t.Error(err)
					}
				})
				g.accessProxies[a] = p
				target := must(fixture.NewPathTarget(resourceNS))
				t.Cleanup(func() {
					if err := target.Close(); err != nil {
						t.Error(err)
					}
				})
				g.targets[a] = target
			}
			g.consumerProxy = must(newG3FaultProxy(e.api, resourceNS))
			t.Cleanup(func() {
				if err := g.consumerProxy.Close(); err != nil {
					t.Error(err)
				}
			})
			e.consumerURL = "https://" + g.consumerProxy.Addr()
			e.oicRoute = func(access string, _ []any) (string, []any) {
				p, target := g.accessProxies[access], g.targets[access]
				if p == nil || target == nil {
					t.Fatalf("unprepared G3 access %s", access)
				}
				return "wss://" + p.Addr(), []any{exposure("http", target.Addr(), protocol.AdmissionBestEffort, nil)}
			}
			g.resources = newG3Resources(e, vector, scenario, g.observeTopology)
		},
		finishSetup: func(e *env) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, a := range []string{"lab-a", "lab-b"} {
				ids, err := g.accessProxies[a].WaitAccepted(ctx, 0, 1)
				if err != nil || len(ids) != 1 {
					t.Fatalf("initial control %s: %v %v", a, ids, err)
				}
				if err = g.accessProxies[a].BindRole(ids[0], "control", a, ""); err != nil {
					t.Fatal(err)
				}
				g.controls[a] = ids[0]
			}
			g.warm("alice", "lab-a", "warm-lab-a")
			g.warm("carol", "lab-b", "warm-lab-b")
			if opts.BeforeFreeze != nil {
				opts.BeforeFreeze(g)
			}
			if err := g.resources.freeze("online", g.expectedTopology("online"), resourceNS()); err != nil {
				t.Fatal(err)
			}
			if opts.ControlProfile != "" {
				g.prepareControlProfile(opts.ControlProfile)
			}
			g.resources.preparationComplete = true
		},
	})
	return g
}

func g3ConnID(proxy string, id uint64) string { return fmt.Sprintf("%s/%d", proxy, id) }

func (g *g3Env) bindAPI(after uint64) error {
	for _, c := range g.consumerProxy.Snapshot().Conns {
		if c.ID > after && c.Role == "" {
			if err := g.consumerProxy.BindRole(c.ID, "api", "", ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// open and attach provide exclusive identification windows. Concurrent quota
// vectors can use the proxies directly and bind an entire verified set instead.
func (g *g3Env) openPath(ctx context.Context, subject, exposureID string) (protocol.OpenResponse, uint64, error) {
	a, _, ok := strings.Cut(exposureID, "/")
	if !ok || g.accessProxies[a] == nil {
		return protocol.OpenResponse{}, 0, fmt.Errorf("unknown G3 exposure %s", exposureID)
	}
	p := g.accessProxies[a]
	mark, apiMark := p.Mark(), g.consumerProxy.Mark()
	o, err := g.consumer(subject).OpenLatest(ctx, exposureID, "", false)
	if bindErr := g.bindAPI(apiMark); bindErr != nil {
		return o, 0, bindErr
	}
	g.closeIdle()
	if err != nil {
		return o, 0, err
	}
	ids, err := p.WaitAccepted(ctx, mark, 1)
	if err != nil || len(ids) != 1 {
		return o, 0, fmt.Errorf("provider identity: %v (%v)", ids, err)
	}
	if err = p.BindRole(ids[0], "provider", a, o.ChannelID); err != nil {
		return o, 0, err
	}
	return o, ids[0], nil
}

func (g *g3Env) attachPath(ctx context.Context, subject string, o protocol.OpenResponse) (*client.Stream, uint64, error) {
	mark := g.consumerProxy.Mark()
	s, err := g.consumer(subject).Attach(ctx, o)
	ids, idErr := g.consumerProxy.WaitAccepted(ctx, mark, 1)
	if idErr != nil || len(ids) != 1 {
		return s, 0, fmt.Errorf("consumer identity: %v (%v); attach: %v", ids, idErr, err)
	}
	if bindErr := g.consumerProxy.BindRole(ids[0], "consumer", "", o.ChannelID); bindErr != nil {
		return s, 0, bindErr
	}
	return s, ids[0], err
}

func (g *g3Env) warm(subject, access, requestID string) {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := g.targets[access]
	if err := target.AddPlan(requestID, fixture.PathPlan{RequestBytes: 4, ResponseBytes: 4, Pattern: true}); err != nil {
		g.t.Fatal(err)
	}
	o, _, err := g.openPath(ctx, subject, access+"/http")
	if err != nil {
		g.t.Fatal(err)
	}
	s, _, err := g.attachPath(ctx, subject, o)
	if err != nil {
		g.t.Fatal(err)
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { s.Abort(protocol.CodeChannelInterrupted) })
	defer stop()
	if _, err = s.Write(fixture.PathRequestHeader(requestID)); err != nil {
		g.t.Fatal(err)
	}
	if err = fixture.WritePathPattern(s, 4); err != nil {
		g.t.Fatal(err)
	}
	if err = s.CloseWrite(); err != nil {
		g.t.Fatal(err)
	}
	if err = fixture.CheckPathPattern(s, 4); err != nil {
		g.t.Fatal(err)
	}
	var extra [1]byte
	if n, err := s.Read(extra[:]); n != 0 || err != io.EOF {
		g.t.Fatalf("G3 warm response not a complete FIN: %d %v", n, err)
	}
	_ = s.Close()
	x, err := target.WaitRequest(ctx, requestID, 1)
	if err != nil {
		g.t.Fatal(err)
	}
	if err = g3Poll(ctx, func() (bool, error) {
		s := x.Snapshot()
		return s.Closed && s.RequestFIN && s.ResponseFIN, nil
	}); err != nil {
		g.t.Fatalf("G3 warm target: %v", err)
	}
	apiMark := g.consumerProxy.Mark()
	if err = g3Poll(ctx, func() (bool, error) {
		v, err := g.consumer(subject).Channel(ctx, o.ChannelID)
		return err == nil && v.State == protocol.StateClosed && v.CloseReason == protocol.CodeCompleted, err
	}); err != nil {
		g.t.Fatalf("G3 warm channel: %v", err)
	}
	if err = g.bindAPI(apiMark); err != nil {
		g.t.Fatal(err)
	}
	g.closeIdle()
}

func (g *g3Env) expectedTopology(kind string) g3ExpectedTopology {
	w := g3ExpectedTopology{OISAccessesOnline: 2,
		OICs:     map[string]g3OICTopology{"lab-a": {true, true}, "lab-b": {true, true}},
		Controls: map[string]g3ControlTopology{"lab-a": {true, false, 0}, "lab-b": {true, false, 0}}}
	switch kind {
	case "control_cut":
		w.OISAccessesOnline = 1
		w.OICs["lab-a"] = g3OICTopology{false, false}
		w.Controls["lab-a"] = g3ControlTopology{false, false, 1}
	case "control_blackhole":
		w.OISAccessesOnline = 1
		w.Controls["lab-a"] = g3ControlTopology{true, true, 0}
	}
	return w
}

func (g *g3Env) observeTopology() (g3ObservedTopology, error) {
	o := g3ObservedTopology{Controls: map[string]g3ObservedControl{}, ActiveDataConnectionIDs: []string{},
		ActiveTargetWorkerIDs: []string{}, UnclassifiedConnectionIDs: []string{}}
	for _, a := range []string{"lab-a", "lab-b"} {
		p := g.accessProxies[a].Snapshot()
		if p.Closed {
			return o, fmt.Errorf("G3 access proxy %s stopped", a)
		}
		control := g3ObservedControl{ControlConnID: g3ConnID("access/"+a, g.controls[a]), PendingControlIDs: []string{}}
		found := false
		for _, c := range p.Conns {
			live := !(c.ClientClosed && c.ServerClosed)
			id := g3ConnID("access/"+a, c.ID)
			if c.ID == g.controls[a] {
				found = true
				if c.Role != "control" || c.AccessID != a {
					return o, fmt.Errorf("G3 control %s lost role binding", id)
				}
				control.ControlOpen, control.DownstreamPaused = live, c.ServerToClient.Paused
			} else if live && c.Role == "control" && c.Pending {
				control.PendingControlIDs = append(control.PendingControlIDs, id)
			} else if live && c.Role == "provider" {
				o.ActiveDataConnectionIDs = append(o.ActiveDataConnectionIDs, id)
			} else if live {
				o.UnclassifiedConnectionIDs = append(o.UnclassifiedConnectionIDs, id)
			}
		}
		if !found {
			return o, fmt.Errorf("G3 control %s missing", a)
		}
		o.Controls[a] = control
		target := g.targets[a].Snapshot()
		if target.Closed {
			return o, fmt.Errorf("G3 target %s stopped", a)
		}
		active := 0
		for _, x := range target.Exchanges {
			if !x.Closed {
				active++
				o.ActiveTargetWorkerIDs = append(o.ActiveTargetWorkerIDs, fmt.Sprintf("target/%s/%d", a, x.ID))
			}
		}
		if active != target.Workers {
			return o, fmt.Errorf("G3 target %s worker identities disagree: %d/%d", a, active, target.Workers)
		}
	}
	p := g.consumerProxy.Snapshot()
	if p.Closed {
		return o, fmt.Errorf("G3 consumer proxy stopped")
	}
	for _, c := range p.Conns {
		if c.ClientClosed && c.ServerClosed {
			continue
		}
		id := g3ConnID("consumer", c.ID)
		if c.Role == "consumer" {
			o.ActiveDataConnectionIDs = append(o.ActiveDataConnectionIDs, id)
		} else {
			// Idle HTTP must also close; it cannot compensate for a leaked data FD.
			o.UnclassifiedConnectionIDs = append(o.UnclassifiedConnectionIDs, id)
		}
	}
	sort.Strings(o.ActiveDataConnectionIDs)
	sort.Strings(o.ActiveTargetWorkerIDs)
	sort.Strings(o.UnclassifiedConnectionIDs)
	return o, nil
}

func g3Poll(ctx context.Context, predicate func() (bool, error)) error {
	for {
		ok, err := predicate()
		if err != nil || ok {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// cutControl keeps exactly one reconnect TCP pending before its upstream dial.
// The first reset timestamp remains available to the vector independently of
// waiting for the reconnect identity; no cleanup window is started here.
func (g *g3Env) cutControl(ctx context.Context) (int64, error) {
	applied, done, err := g.cutControlAsync(ctx)
	if err != nil {
		return applied, err
	}
	return applied, <-done
}

// cutControlAsync returns at the actual reset. The vector observes its business
// terminal conditions concurrently and preserves that original cleanup origin;
// waiting for this identity result must never postpone the cleanup deadline.
func (g *g3Env) cutControlAsync(ctx context.Context) (int64, <-chan error, error) {
	p := g.accessProxies["lab-a"]
	mark := p.Mark()
	p.HoldNew(true)
	applied, err := p.Reset(g.controls["lab-a"])
	if err != nil {
		return applied, nil, err
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		ids, err := p.WaitAccepted(ctx, mark, 1)
		if err != nil || len(ids) != 1 {
			done <- fmt.Errorf("control pending identity: %v (%v)", ids, err)
			return
		}
		done <- p.BindRole(ids[0], "control", "lab-a", "")
	}()
	return applied, done, nil
}

func (g *g3Env) restoreControl(ctx context.Context, kind string, oldGeneration int64) error {
	p := g.accessProxies["lab-a"]
	old := g.controls["lab-a"]
	if kind == "control_cut" {
		p.ReleaseNew()
	} else if kind == "control_blackhole" {
		if err := p.ResumeRead(old, "server_to_client"); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unknown control profile %s", kind)
	}
	if err := g3Poll(ctx, func() (bool, error) {
		x, err := g.resourceOIC(ctx, "lab-a")
		return err == nil && x.Stats.Registered && x.Stats.Connected && x.Stats.Generation > oldGeneration, err
	}); err != nil {
		return err
	}
	var current uint64
	for _, c := range p.Snapshot().Conns {
		if c.ID == old || c.Pending || (c.ClientClosed && c.ServerClosed) {
			continue
		}
		if current != 0 {
			return fmt.Errorf("ambiguous restored control connection")
		}
		current = c.ID
		if c.Role == "" {
			if err := p.BindRole(c.ID, "control", "lab-a", ""); err != nil {
				return err
			}
		} else if c.Role != "control" {
			return fmt.Errorf("restored control reused data identity")
		}
	}
	if current == 0 {
		return fmt.Errorf("restored control connection missing")
	}
	g.controls["lab-a"] = current
	return nil
}

func (g *g3Env) prepareControlProfile(kind string) {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	initial := must(g.resourceOIC(ctx, "lab-a"))
	var applied int64
	if kind == "control_cut" {
		applied = must(g.cutControl(ctx))
	} else if kind == "control_blackhole" {
		// This no-business rehearsal may choose a normal heartbeat phase.
		// Formal fault injection retains the separate >=2000ms precondition.
		var phase resourceProcess[protocol.OICStats]
		if err := g3Poll(ctx, func() (bool, error) {
			x, err := g.resourceOIC(ctx, "lab-a")
			phase = x
			return err == nil && x.Stats.Generation == initial.Stats.Generation &&
				x.Stats.LicenseRemainingMS >= 1600 && x.Stats.LicenseRemainingMS <= 1800, err
		}); err != nil {
			g.t.Fatal(err)
		}
		pause := must(g.accessProxies["lab-a"].PauseRead(ctx, g.controls["lab-a"], "server_to_client"))
		applied = pause.AppliedNS
		g.obs("G3-preparation", map[string]any{"vector": g.resources.vector, "scenario": g.resources.scenario,
			"stage": "preparation", "event": "blackhole_phase", "applied_ns": applied,
			"phase_sampled_ns": phase.SampledNS, "license_remaining_ms": phase.Stats.LicenseRemainingMS})
	} else {
		g.t.Fatalf("unknown G3 control profile %s", kind)
	}
	if err := g.resources.freeze(kind, g.expectedTopology(kind), applied); err != nil {
		g.t.Fatal(err)
	}
	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer restoreCancel()
	if err := g.restoreControl(restoreCtx, kind, initial.Stats.Generation); err != nil {
		g.t.Fatal(err)
	}
	start := resourceNS()
	if err := g.resources.check("online", "preparation", "online_restored_preparation", nil, start, "control_restored", nil); err != nil {
		g.t.Fatal(err)
	}
}
