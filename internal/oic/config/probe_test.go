package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/tests/pki"
)

func probeInput() map[string]any {
	return map[string]any{"kind": "http_status_v1", "url": "http://LOCALHOST.:80/ready", "allowed_cidrs": []string{"127.0.0.1/8", "127.0.0.0/8"}, "ready_statuses": []int{204, 200, 200}, "not_ready_statuses": []int{503}}
}

func configWithProbe(p map[string]any) map[string]any {
	m := base()
	m["exposures"].([]any)[0].(map[string]any)["probe"] = p
	return m
}

func TestProbeCompileContract(t *testing.T) {
	m := configWithProbe(probeInput())
	c, err := parse(t, m)
	if err != nil {
		t.Fatal(err)
	}
	e := c.Exposures[0]
	p := e.Probe
	if p.Plan.URL() != "http://localhost:80/ready" || p.Target.Host != "localhost" || len(p.Target.Prefixes) != 1 ||
		p.Plan.Interval() != 5*time.Second || p.Plan.Timeout() != time.Second || p.Plan.Validity() != 15*time.Second || p.Plan.Jitter() != 500*time.Millisecond {
		t.Fatalf("unexpected compiled contract: %s %+v", p.Plan.Canonical(), p.Target)
	}
	without, err := parse(t, base())
	if err != nil || without.Exposures[0].ProbePlan().Digest() != observation.NullDigest || e.Revision != without.Exposures[0].Revision {
		t.Fatal("probe changed business revision or Unsupported identity", err)
	}
	other := probeInput()
	other["url"] = "http://localhost/ready"
	other["allowed_cidrs"] = []string{"127.0.0.0/8"}
	other["ready_statuses"] = []int{200, 204}
	same, err := parse(t, configWithProbe(other))
	if err != nil || same.Exposures[0].ProbePlan().Digest() != p.Plan.Digest() {
		t.Fatal("equivalent normalized probe changed digest", err)
	}
	other["jitter_ms"] = 0
	changed, err := parse(t, configWithProbe(other))
	if err != nil || changed.Exposures[0].ProbePlan().Jitter() != 0 || changed.Exposures[0].ProbePlan().Digest() == p.Plan.Digest() || changed.Exposures[0].Revision != e.Revision {
		t.Fatal("explicit zero jitter or independent revision mishandled", err)
	}
	other["timeout_ms"], other["interval_ms"] = 1000, 100
	if _, err := parse(t, configWithProbe(other)); err != nil {
		t.Fatal("timeout < interval is advice, not a new rejection rule", err)
	}
}

func TestProbeCompileRejectsUnsafeInput(t *testing.T) {
	changes := map[string]map[string]any{
		"case_alias":   {"Kind": "http_status_v1"},
		"unknown_kind": {"kind": "tcp"}, "unknown_field": {"headers": map[string]string{"Authorization": "secret"}},
		"no_networks": {"allowed_cidrs": []string{}}, "literal_without_networks": {"url": "http://127.0.0.1/", "allowed_cidrs": []string{}},
		"literal_denied": {"url": "http://10.0.0.1/"}, "bad_network": {"allowed_cidrs": []string{"invalid"}},
		"query": {"url": "http://localhost/ready?token=secret"}, "empty_query": {"url": "http://localhost/?"},
		"fragment": {"url": "http://localhost/#"}, "userinfo": {"url": "http://user:secret@localhost/"},
		"scheme": {"url": "file:///etc/passwd"}, "invalid_url": {"url": "http://[::1"},
		"status_overlap": {"not_ready_statuses": []int{200}}, "status_empty": {"ready_statuses": []int{}},
		"status_low": {"ready_statuses": []int{199}}, "status_high": {"ready_statuses": []int{600}},
		"duration_zero": {"timeout_ms": 0}, "duration_negative": {"validity_ms": -1},
		"duration_overflow": {"interval_ms": int64(9223372036854775807)}, "duration_float": {"timeout_ms": 50.5},
		"duration_null": {"timeout_ms": nil}, "jitter_null": {"jitter_ms": nil},
		"jitter_negative": {"jitter_ms": -1}, "jitter_large": {"jitter_ms": 2501},
		"tls_without_ca": {"url": "https://localhost/ready"}, "missing_ca": {"tls_ca_file": "not-present.pem"},
	}
	for name, fields := range changes {
		t.Run(name, func(t *testing.T) {
			p := probeInput()
			for k, v := range fields {
				p[k] = v
			}
			if _, err := parse(t, configWithProbe(p)); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}

func TestObservationConfigBoundsAndExporterOptIn(t *testing.T) {
	m := base()
	m["limits"] = map[string]any{"max_exposures": 2, "n_ois": 2, "n_access": 2, "n_subject": 2, "max_local_channels": 2}
	c, err := parse(t, m)
	if err != nil || c.ObservationLimits.ProbeWorkers != 2 || c.ObservationLimits.ProbeQueue != 2 || c.ObservationLimits.QuerySlots != 2 {
		t.Fatal(c, err)
	}
	m = base()
	m["observation_limits"] = map[string]any{"probe_workers": 2, "probe_queue": 4, "check_ms": 500}
	m["metrics_enabled"] = true
	c, err = parse(t, m)
	if err != nil || !c.MetricsEnabled || c.ObservationLimits.ProbeWorkers != 2 || c.ObservationLimits.ProbeQueue != 4 || c.ObservationLimits.CheckMS != 500 {
		t.Fatal(c, err)
	}
	for name, value := range map[string]any{"null": nil, "unknown": map[string]any{"extra": 1}, "case": map[string]any{"CHECK_MS": 500}, "field_null": map[string]any{"check_ms": nil}, "zero": map[string]any{"probe_workers": 0}, "unbounded": map[string]any{"probe_queue": 1000000}, "negative": map[string]any{"query_ms": -1}, "too_long": map[string]any{"write_ms": 101}} {
		t.Run(name, func(t *testing.T) {
			m := base()
			m["observation_limits"] = value
			if _, err := parse(t, m); err == nil {
				t.Fatal("unsafe observation config accepted")
			}
		})
	}
	m = base()
	m["metrics_enabled"] = true
	delete(m, "diag_listen")
	if _, err := parse(t, m); err == nil {
		t.Fatal("metrics without explicit loopback listener")
	}
	m = base()
	m["diag_listen"] = "localhost:0"
	if _, err := parse(t, m); err != nil {
		t.Fatal(err)
	}
}

func TestProbeCAContentAndFailedCandidate(t *testing.T) {
	dir := t.TempDir()
	ca, err := pki.NewCA("probe")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "probe.pem")
	if err := os.WriteFile(path, ca.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	p := probeInput()
	p["url"], p["tls_ca_file"] = "https://localhost/ready", "probe.pem"
	data, _ := json.Marshal(configWithProbe(p))
	old, err := Parse(data, dir)
	if err != nil || old.Exposures[0].Probe.TLSCAFile != path {
		t.Fatal(err)
	}
	initial := old.Exposures[0].ProbePlan().Digest()
	// Trust identity uses content, never the local filesystem path.
	if err := os.WriteFile(filepath.Join(dir, "copy.pem"), ca.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	copyData := []byte(strings.ReplaceAll(string(data), "probe.pem", "copy.pem"))
	copyConfig, err := Parse(copyData, dir)
	if err != nil || copyConfig.Exposures[0].ProbePlan().Digest() != initial {
		t.Fatal("path entered observation contract", err)
	}
	nextCA, err := pki.NewCA("replacement")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nextCA.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	next, err := Parse(data, dir)
	if err != nil || next.ContentDigest(data) == old.ContentDigest(data) || next.Exposures[0].ProbePlan().Digest() == initial || next.Exposures[0].Revision != old.Exposures[0].Revision {
		t.Fatal("same-path CA update was missed or changed business revision", err)
	}
	if old.Exposures[0].ProbePlan().Digest() != initial {
		t.Fatal("old immutable plan changed")
	}
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data, dir); err == nil {
		t.Fatal("bad replacement accepted")
	}
}
