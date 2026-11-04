package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

// g2Env retains the real processes, storage, TLS identities and native target.
// The health server and optional protocol proxy are test instruments only.
type g2Env struct {
	*env
	vector    string
	health    map[string]*fixture.Health
	paths     map[string]*g2Proxy
	startedNS int64
	// The final channel's existing logical cleanup origin also bounds FD and
	// goroutine recovery. Waiting for slots first must not grant another 2s.
	cleanupStartNS int64
}

type g2Options struct {
	proxy bool
	// Additional probes use independently controlled health endpoints.
	probes         []string
	configureProbe func(string, map[string]any)
}

func g2ObservationLimits() map[string]any {
	return map[string]any{"pending_checks": 16, "query_slots": 8, "check_ms": 500, "query_ms": 500,
		"refresh_ms": 500, "probe_workers": 2, "probe_queue": 4}
}

func g2ProbeConfig(h *fixture.Health) map[string]any {
	return map[string]any{"kind": "http_status_v1", "url": h.URL(), "allowed_cidrs": []string{"127.0.0.0/8"},
		"ready_statuses": []int{200, 204}, "not_ready_statuses": []int{503},
		"interval_ms": 500, "timeout_ms": 200, "validity_ms": 1500, "jitter_ms": 50}
}

func newG2Env(t *testing.T, be, vector string, o g2Options) *g2Env {
	t.Helper()
	g := &g2Env{vector: vector, health: map[string]*fixture.Health{}, paths: map[string]*g2Proxy{}, startedNS: resourceNS()}
	if len(o.probes) == 0 {
		o.probes = []string{"lab-a/http"}
	}
	for _, id := range o.probes {
		h := fixture.NewHealth(t, resourceNS)
		h.Set(200, false)
		g.health[id] = h
	}
	g.env = newEnv(t, be, envOpts{limits: map[string]any{"max_exposures": 16, "idle_ms": 600000},
		configureOIS: func(cfg map[string]any) {
			cfg["observation_limits"] = g2ObservationLimits()
			cfg["diag_listen"] = "127.0.0.1:0"
			cfg["metrics_enabled"] = true
		},
		beforeOIC: func(e *env) {
			g.env = e
			e.configureOIC = func(p *oicProc) { p.cfg["observation_limits"] = g2ObservationLimits(); p.cfg["metrics_enabled"] = true }
			e.oicRoute = func(access string, exps []any) (string, []any) {
				seen := map[string]bool{}
				for _, raw := range exps {
					seen[access+"/"+raw.(map[string]any)["name"].(string)] = true
				}
				for _, id := range o.probes {
					if strings.HasPrefix(id, access+"/") && !seen[id] {
						exps = append(exps, exposure(strings.TrimPrefix(id, access+"/"), e.t1.Addr(), protocol.AdmissionBestEffort, nil))
					}
				}
				for _, raw := range exps {
					m := raw.(map[string]any)
					id := access + "/" + m["name"].(string)
					if h := g.health[id]; h != nil {
						cfg := g2ProbeConfig(h)
						if o.configureProbe != nil {
							o.configureProbe(id, cfg)
						}
						m["probe"] = cfg
					}
				}
				if !o.proxy {
					return "wss://" + e.acc.addr(), exps
				}
				p := newG2Proxy(t, "wss://"+e.accessAddr, e.ca, e.oics[access].leaf)
				g.paths[access] = p
				return p.URL(), exps
			}
		},
		finishSetup: func(e *env) {
			g.env = e
			for id := range g.health {
				g.waitView(id, func(v protocol.ExposureView) bool { return v.Readiness == protocol.ReadinessReady })
			}
			// Initialize both native paths before the one and only profile.
			for _, w := range []struct{ subject, exposure, want string }{{"alice", "lab-a/http", "T1:warm"}, {"carol", "lab-b/http", "T2:warm"}} {
				if got, _ := request(t, e.consumer(w.subject), w.exposure, "warm"); got != w.want {
					t.Fatalf("G2 resource warmup: got %q want %q", got, w.want)
				}
			}
			e.baselineWhen(g2ProbeIdle)
		}})
	g.event("environment", map[string]any{"probe_limits": g2ObservationLimits(), "probe_count": len(g.health), "proxy": o.proxy,
		"baseline": g.resourceBase, "started_ns": g.startedNS})
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for id := range g.health {
			g.recordHealth(id)
		}
		for id, p := range g.paths {
			g.event("failed_wire_history", map[string]any{"access_id": id, "proxy": p.Snapshot()})
		}
	})
	return g
}

// Healthy periodic probes remain enabled. A transient probe socket must not
// become a permanent FD allowance in the pre-fault resource profile.
func g2ProbeIdle(s resourceSnapshot) bool {
	for _, o := range s.OICs {
		if o.Stats.Observation == nil || o.Stats.Observation.ProbeActive != 0 || o.Stats.Observation.ProbeQueued != 0 {
			return false
		}
	}
	return true
}

// settled shadows env.settled so restoring instrument inputs and closing the
// metrics client's idle sockets are included in the original absolute budget.
// It does not disable probes, change their jobs/configuration or recapture FD.
func (g *g2Env) settled() {
	g.t.Helper()
	start := resourceNS()
	if g.cleanupStartNS != 0 {
		start = g.cleanupStartNS
	}
	for _, h := range g.health {
		h.Set(200, false)
	}
	if c, ok := g2MetricsClients.Load(g.env); ok {
		c.(*http.Client).CloseIdleConnections()
	}
	g.env.settledFrom(start)
}

func (g *g2Env) event(name string, fields map[string]any) {
	fields["schema"] = "g2-behavior-v1"
	fields["vector"] = g.vector
	fields["event"] = name
	fields["observed_ns"] = resourceNS()
	fields["backend"] = g.be
	fields["test"] = g.t.Name()
	g.obs("G2", fields)
}

func (g *g2Env) view(id string) protocol.ExposureView {
	g.t.Helper()
	subject := "alice"
	if strings.HasPrefix(id, "lab-b/") {
		subject = "carol"
	}
	views, err := g.consumer(subject).DiscoverAll(bg)
	if err != nil {
		g.t.Fatal(err)
	}
	for _, v := range views {
		if v.ExposureID == id {
			return v
		}
	}
	g.t.Fatalf("missing exposure %s", id)
	return protocol.ExposureView{}
}

func (g *g2Env) waitView(id string, predicate func(protocol.ExposureView) bool) protocol.ExposureView {
	g.t.Helper()
	var v protocol.ExposureView
	g.eventually(5*time.Second, id+" directory condition", func() bool { v = g.view(id); return predicate(v) })
	g.event("directory_observed", map[string]any{"exposure_id": id, "view": v})
	return v
}

func (g *g2Env) recordHealth(id string) {
	s := g.health[id].Snapshot()
	if s.Overflow {
		g.t.Fatal("health instrument record capacity exhausted")
	}
	g.event("health_history", map[string]any{"exposure_id": id, "health": s})
}

func (g *g2Env) assertNoProbeSecret(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		g.t.Fatal(err)
	}
	for _, h := range g.health {
		if strings.Contains(string(b), h.URL()) {
			g.t.Fatal("probe URL escaped into consumer response")
		}
	}
}
