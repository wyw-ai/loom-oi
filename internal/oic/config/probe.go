package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyw-ai/loom-oi/internal/limits"
	"github.com/wyw-ai/loom-oi/internal/observation"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func parseObservationLimits(data []byte, out *limits.Observation) error {
	var fields map[string]json.RawMessage
	if err := protocol.DecodeStrictBytes(data, out); err != nil {
		return fmt.Errorf("observation_limits: %w", err)
	}
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("observation_limits must be an object")
	}
	for name, raw := range fields {
		switch name {
		case "message_max", "pending_checks", "query_slots", "check_ms", "query_ms", "write_ms", "refresh_ms", "probe_workers", "probe_queue":
		default:
			return fmt.Errorf("unknown observation_limits field %q", name)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("observation_limits.%s cannot be null", name)
		}
	}
	return nil
}

// Probe is local administrator input. The compiled plan and separately
// authorized dial target never enter registration or directory responses.
type Probe struct {
	Kind             string   `json:"kind"`
	URL              string   `json:"url"`
	AllowedCIDRs     []string `json:"allowed_cidrs"`
	TLSCAFile        string   `json:"tls_ca_file,omitempty"`
	ReadyStatuses    []int    `json:"ready_statuses"`
	NotReadyStatuses []int    `json:"not_ready_statuses"`
	IntervalMS       int64    `json:"interval_ms"`
	TimeoutMS        int64    `json:"timeout_ms"`
	ValidityMS       int64    `json:"validity_ms"`
	JitterMS         int64    `json:"jitter_ms"`

	Plan   *observation.Plan `json:"-"`
	Target Target            `json:"-"`
}

func (p *Probe) UnmarshalJSON(data []byte) error {
	type wire Probe
	v := wire{IntervalMS: 5000, TimeoutMS: 1000, ValidityMS: 15000, JitterMS: 500}
	if err := protocol.DecodeStrictBytes(data, &v); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for name, raw := range fields {
		switch name {
		case "kind", "url", "allowed_cidrs", "tls_ca_file", "ready_statuses", "not_ready_statuses", "interval_ms", "timeout_ms", "validity_ms", "jitter_ms":
		default:
			return fmt.Errorf("unknown probe field %q", name)
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("probe %s cannot be null", name)
		}
	}
	*p = Probe(v)
	return nil
}

// compile performs all filesystem work before a candidate config can be
// installed under the runtime state lock. A failed CA read keeps the old plan.
func (p *Probe) compile(dir string) error {
	if p.Kind != observation.ProbeKind {
		return errors.New("kind must be http_status_v1")
	}
	// Check in integer milliseconds before converting, preventing duration
	// overflow from turning an invalid large input into a valid small duration.
	if p.IntervalMS < 100 || p.IntervalMS > 86400000 || p.ValidityMS < 100 || p.ValidityMS > 86400000 ||
		p.TimeoutMS < 50 || p.TimeoutMS > 30000 || p.JitterMS < 0 || p.JitterMS > p.IntervalMS/2 {
		return errors.New("probe duration outside finite bounds")
	}
	var ca []byte
	if p.TLSCAFile != "" {
		if !filepath.IsAbs(p.TLSCAFile) {
			p.TLSCAFile = filepath.Join(dir, p.TLSCAFile)
		}
		var err error
		ca, err = os.ReadFile(p.TLSCAFile)
		if err != nil {
			return fmt.Errorf("read probe CA: %w", err)
		}
	}
	plan, err := observation.NewPlan(observation.PlanOptions{
		URL: p.URL, AllowedCIDRs: p.AllowedCIDRs, CA: ca,
		ReadyStatuses: p.ReadyStatuses, NotReadyStatuses: p.NotReadyStatuses,
		Interval: time.Duration(p.IntervalMS) * time.Millisecond, Timeout: time.Duration(p.TimeoutMS) * time.Millisecond,
		Validity: time.Duration(p.ValidityMS) * time.Millisecond, Jitter: time.Duration(p.JitterMS) * time.Millisecond,
	})
	if err != nil {
		return err
	}
	target := Target{Address: plan.Address(), AllowedCIDRs: append([]string(nil), p.AllowedCIDRs...)}
	if err := target.normalize(); err != nil {
		return err
	}
	p.Plan, p.Target = plan, target
	return nil
}

// ProbePlan returns nil for Unsupported. Its nil-safe Digest is the adopted
// SHA-256 of JSON null; no source identity is manufactured for that case.
func (e *Exposure) ProbePlan() *observation.Plan {
	if e.Probe == nil {
		return nil
	}
	return e.Probe.Plan
}

// ContentDigest includes successfully compiled CA content. Polling only the
// configuration JSON misses a trust-file replacement at the unchanged path.
func (c *Config) ContentDigest(data []byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write(data)
	for _, e := range c.Exposures {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(e.ProbePlan().Digest()))
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum
}
