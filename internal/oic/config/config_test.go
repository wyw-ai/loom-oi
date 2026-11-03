package config

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func base() map[string]any {
	return map[string]any{
		"access_id": "lab-a",
		"ois":       map[string]any{"url": "wss://127.0.0.1:7444", "ca_file": "ca.pem"},
		"tls":       map[string]any{"cert_file": "lab-a.pem", "key_file": "lab-a-key.pem"},
		"exposures": []any{
			map[string]any{"name": "http", "protocol": "http", "description": "T1",
				"target": map[string]any{"address": "127.0.0.1:8081"}},
			map[string]any{"name": "alt", "protocol": "http", "admission_mode": "require_ready", "enabled": false,
				"target": map[string]any{"address": "Localhost.:8083", "allowed_cidrs": []string{"127.0.0.1/8", "::1/128", "127.0.0.0/8"}}},
		},
		"limits":      map[string]any{"desc_max": 16},
		"diag_listen": "127.0.0.1:0",
	}
}

func parse(t *testing.T, m map[string]any) (*Config, error) {
	t.Helper()
	b, _ := json.Marshal(m)
	return Parse(b, "/etc/oic")
}

func TestParse(t *testing.T) {
	c, err := parse(t, base())
	if err != nil {
		t.Fatal(err)
	}
	if c.OIS.CAFile != filepath.Join("/etc/oic", "ca.pem") || c.Limits.DescMax != 16 {
		t.Fatalf("%+v", c)
	}
	h, alt := c.Exposures[0], c.Exposures[1]
	if h.ID != "lab-a/http" || h.AdmissionMode != protocol.AdmissionBestEffort || !h.IsEnabled() || !protocol.ValidRevision(h.Revision) {
		t.Fatalf("%+v", h)
	}
	if h.Target.Host != "127.0.0.1" || h.Target.Port != 8081 || !h.Target.Addr.IsValid() {
		t.Fatalf("%+v", h.Target)
	}
	if alt.IsEnabled() || alt.Target.Host != "localhost" || alt.Target.Addr.IsValid() || len(alt.Target.Prefixes) != 2 {
		t.Fatalf("%+v", alt.Target)
	}
	if c.Find("lab-a/alt") != alt || c.Find("lab-a/none") != nil {
		t.Fatal("find")
	}
	if !alt.Target.Allowed(netip.MustParseAddr("127.9.9.9")) || alt.Target.Allowed(netip.MustParseAddr("10.0.0.1")) {
		t.Fatal("allowed")
	}
	if !h.Target.Allowed(netip.MustParseAddr("127.0.0.1")) || h.Target.Allowed(netip.MustParseAddr("127.0.0.2")) {
		t.Fatal("literal without networks")
	}
}

// B2 §4: the revision covers target and transport, not description or
// admission, and is stable across equivalent spellings.
func TestRevision(t *testing.T) {
	c1, _ := parse(t, base())
	m := base()
	exps := m["exposures"].([]any)
	alt := exps[1].(map[string]any)
	alt["description"] = "changed"
	alt["admission_mode"] = "best_effort"
	alt["target"] = map[string]any{"address": "localhost:8083", "allowed_cidrs": []string{"::1/128", "127.0.0.0/8"}}
	c2, err := parse(t, m)
	if err != nil {
		t.Fatal(err)
	}
	if c1.Exposures[1].Revision != c2.Exposures[1].Revision {
		t.Fatal("equivalent target changed the revision")
	}
	alt["target"] = map[string]any{"address": "localhost:8084", "allowed_cidrs": []string{"127.0.0.0/8"}}
	c3, _ := parse(t, m)
	if c3.Exposures[1].Revision == c1.Exposures[1].Revision {
		t.Fatal("target change kept the revision")
	}
	exps[0].(map[string]any)["protocol"] = "https"
	c4, _ := parse(t, m)
	if c4.Exposures[0].Revision == c1.Exposures[0].Revision {
		t.Fatal("protocol change kept the revision")
	}
	tg := c1.Exposures[0].Target
	if got := Revision(tg, protocol.TransportTCP, "http"); got != c1.Exposures[0].Revision || Revision(tg, "udp", "http") == got {
		t.Fatal("transport profile ignored")
	}
}

func TestParseRejects(t *testing.T) {
	mod := func(f func(m map[string]any)) map[string]any {
		m := base()
		f(m)
		return m
	}
	exp := func(f func(e map[string]any)) map[string]any {
		return mod(func(m map[string]any) { f(m["exposures"].([]any)[0].(map[string]any)) })
	}
	cases := map[string]map[string]any{
		"unknown field":  mod(func(m map[string]any) { m["x"] = 1 }),
		"bad limits":     mod(func(m map[string]any) { m["limits"] = map[string]any{"nope": 1} }),
		"invalid limits": mod(func(m map[string]any) { m["limits"] = map[string]any{"data_max": -1} }),
		"access id":      mod(func(m map[string]any) { m["access_id"] = "Lab" }),
		"url scheme":     mod(func(m map[string]any) { m["ois"] = map[string]any{"url": "https://x:1", "ca_file": "c"} }),
		"url path":       mod(func(m map[string]any) { m["ois"] = map[string]any{"url": "wss://x:1/v1", "ca_file": "c"} }),
		"url bad":        mod(func(m map[string]any) { m["ois"] = map[string]any{"url": "wss://[::1", "ca_file": "c"} }),
		"missing ca":     mod(func(m map[string]any) { m["ois"] = map[string]any{"url": "wss://x:1"} }),
		"diag public":    mod(func(m map[string]any) { m["diag_listen"] = "0.0.0.0:1" }),
		"diag name":      mod(func(m map[string]any) { m["diag_listen"] = "example.com:1" }),
		"diag no port":   mod(func(m map[string]any) { m["diag_listen"] = "127.0.0.1" }),
		"null exposure":  mod(func(m map[string]any) { m["exposures"] = []any{nil} }),
		"duplicate":      mod(func(m map[string]any) { m["exposures"] = []any{m["exposures"].([]any)[0], m["exposures"].([]any)[0]} }),
		"name":           exp(func(e map[string]any) { e["name"] = "Bad Name" }),
		"protocol":       exp(func(e map[string]any) { e["protocol"] = "" }),
		"description":    exp(func(e map[string]any) { e["description"] = strings.Repeat("d", 17) }),
		"admission":      exp(func(e map[string]any) { e["admission_mode"] = "never" }),
		"address":        exp(func(e map[string]any) { e["target"] = map[string]any{"address": "nohost"} }),
		"port":           exp(func(e map[string]any) { e["target"] = map[string]any{"address": "127.0.0.1:0"} }),
		"port range":     exp(func(e map[string]any) { e["target"] = map[string]any{"address": "127.0.0.1:70000"} }),
		"empty host":     exp(func(e map[string]any) { e["target"] = map[string]any{"address": ":80"} }),
		"cidr": exp(func(e map[string]any) {
			e["target"] = map[string]any{"address": "127.0.0.1:80", "allowed_cidrs": []string{"x"}}
		}),
		"outside cidr": exp(func(e map[string]any) {
			e["target"] = map[string]any{"address": "10.0.0.1:80", "allowed_cidrs": []string{"127.0.0.0/8"}}
		}),
		"name needs cidr": exp(func(e map[string]any) { e["target"] = map[string]any{"address": "db.internal:5432"} }),
	}
	for name, m := range cases {
		if _, err := parse(t, m); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := Parse([]byte("{"), "."); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	b, _ := json.Marshal(base())
	path := filepath.Join(dir, "oic.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c, data, err := Load(path)
	if err != nil || string(data) != string(b) || c.TLS.CertFile != filepath.Join(dir, "lab-a.pem") {
		t.Fatal(c, err)
	}
	if _, _, err := Load(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing file")
	}
	if err := os.WriteFile(path, []byte(`{"access_id":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("invalid file accepted")
	}
	if err := os.WriteFile(path, make([]byte, MaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("oversize file accepted")
	}
}
