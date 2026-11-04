package observation

import (
	"bytes"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/tests/pki"
)

func TestPlanContractAndImmutability(t *testing.T) {
	o := testOptions()
	o.URL = "http://LOCALHOST.:08080/Ready%2fNow"
	o.AllowedCIDRs = []string{"127.0.0.1/8", "127.0.0.0/8"}
	o.ReadyStatuses = []int{201, 200, 201}
	p, err := NewPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	if p.URL() != "http://localhost:8080/Ready%2fNow" || p.Address() != "localhost:8080" {
		t.Fatalf("wrong URL normalization %s", p.URL())
	}
	o.URL = "http://localhost:8080/Ready%2fNow"
	o.AllowedCIDRs = []string{"127.0.0.0/8"}
	o.ReadyStatuses = []int{200, 201}
	other, err := NewPlan(o)
	if err != nil || p.Digest() != other.Digest() {
		t.Fatalf("equivalent configuration changed digest: %v", err)
	}
	canonical := p.Canonical()
	canonical[0] = '!'
	o.ReadyStatuses[0] = 502
	o.AllowedCIDRs[0] = "0.0.0.0/0"
	if bytes.Equal(canonical, p.Canonical()) || !p.ready[200] || p.ready[502] {
		t.Fatal("caller mutated compiled plan")
	}
	if p.Interval() != o.Interval || p.Timeout() != o.Timeout || p.Validity() != o.Validity || p.Jitter() != o.Jitter {
		t.Fatal("duration getters disagree")
	}
	var absent *Plan
	if absent.Digest() != digest([]byte("null")) || string(absent.Canonical()) != "null" {
		t.Fatal("unsupported contract is not canonical null")
	}
	ca, err := pki.NewCA("probe")
	if err != nil {
		t.Fatal(err)
	}
	o = testOptions()
	o.URL = "https://127.0.0.1/"
	o.CA = append([]byte(nil), ca.PEM...)
	secure, err := NewPlan(o)
	if err != nil {
		t.Fatal(err)
	}
	o.CA[0] = '!'
	if secure.Digest() == "" || secure.roots == nil {
		t.Fatal("TLS trust not compiled")
	}
	o.CA = ca.PEM
	o.URL = "http://127.0.0.1/"
	if _, err := NewPlan(o); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PlanOptions)
	}{
		{"scheme", func(o *PlanOptions) { o.URL = "ftp://localhost/" }},
		{"empty", func(o *PlanOptions) { o.URL = "" }},
		{"userinfo", func(o *PlanOptions) { o.URL = "http://user:secret@localhost/" }},
		{"query", func(o *PlanOptions) { o.URL = "http://localhost/?secret=x" }},
		{"empty_query", func(o *PlanOptions) { o.URL = "http://localhost/?" }},
		{"fragment", func(o *PlanOptions) { o.URL = "http://localhost/#x" }},
		{"empty_fragment", func(o *PlanOptions) { o.URL = "http://localhost/#" }},
		{"opaque", func(o *PlanOptions) { o.URL = "http:foo" }},
		{"no_host", func(o *PlanOptions) { o.URL = "http:///foo" }},
		{"dot_host", func(o *PlanOptions) { o.URL = "http://./" }},
		{"bad_port", func(o *PlanOptions) { o.URL = "http://localhost:65536/" }},
		{"zero_port", func(o *PlanOptions) { o.URL = "http://localhost:0/" }},
		{"no_network", func(o *PlanOptions) { o.AllowedCIDRs = nil }},
		{"bad_network", func(o *PlanOptions) { o.AllowedCIDRs = []string{"invalid"} }},
		{"outside_network", func(o *PlanOptions) { o.AllowedCIDRs = []string{"192.0.2.0/24"} }},
		{"overlap", func(o *PlanOptions) { o.NotReadyStatuses = []int{200} }},
		{"empty_ready", func(o *PlanOptions) { o.ReadyStatuses = nil }},
		{"empty_not_ready", func(o *PlanOptions) { o.NotReadyStatuses = nil }},
		{"informational_status", func(o *PlanOptions) { o.ReadyStatuses = []int{199} }},
		{"status_high", func(o *PlanOptions) { o.NotReadyStatuses = []int{600} }},
		{"interval_zero", func(o *PlanOptions) { o.Interval = 0 }},
		{"interval_high", func(o *PlanOptions) { o.Interval = 24*time.Hour + time.Millisecond }},
		{"timeout_low", func(o *PlanOptions) { o.Timeout = 49 * time.Millisecond }},
		{"timeout_high", func(o *PlanOptions) { o.Timeout = 30*time.Second + time.Millisecond }},
		{"validity_low", func(o *PlanOptions) { o.Validity = 99 * time.Millisecond }},
		{"fraction_ms", func(o *PlanOptions) { o.Interval += time.Nanosecond }},
		{"jitter_negative", func(o *PlanOptions) { o.Jitter = -time.Millisecond }},
		{"jitter_high", func(o *PlanOptions) { o.Jitter = o.Interval/2 + time.Millisecond }},
		{"https_no_ca", func(o *PlanOptions) { o.URL = "https://localhost/" }},
		{"https_invalid_ca", func(o *PlanOptions) { o.URL = "https://localhost/"; o.CA = []byte("bad PEM") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := testOptions()
			tc.change(&o)
			if _, err := NewPlan(o); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
	// No invented timeout/interval ratio: the documented independent bounds apply.
	o := testOptions()
	o.Timeout = 30 * time.Second
	o.Interval = 100 * time.Millisecond
	o.Validity = 100 * time.Millisecond
	o.Jitter = 0
	if _, err := NewPlan(o); err != nil {
		t.Fatal("undocumented ratio rejected:", err)
	}
}
