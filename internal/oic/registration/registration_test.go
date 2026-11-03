package registration

import (
	"encoding/json"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
)

func cfg(t *testing.T, exps ...map[string]any) *config.Config {
	t.Helper()
	m := map[string]any{
		"access_id": "lab-a",
		"ois":       map[string]any{"url": "wss://127.0.0.1:1", "ca_file": "ca.pem"},
		"tls":       map[string]any{"cert_file": "c.pem", "key_file": "k.pem"},
		"exposures": exps,
	}
	b, _ := json.Marshal(m)
	c, err := config.Parse(b, ".")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func exp(name, addr, desc string, enabled bool) map[string]any {
	return map[string]any{"name": name, "protocol": "http", "description": desc, "enabled": enabled,
		"target": map[string]any{"address": addr}}
}

func TestSnapshot(t *testing.T) {
	c := cfg(t, exp("b", "127.0.0.1:2", "two", true), exp("a", "127.0.0.1:1", "one", true), exp("off", "127.0.0.1:3", "", false))
	s := Snapshot(c)
	if len(s) != 2 || s[0].ExposureID != "lab-a/a" || s[1].ExposureID != "lab-a/b" {
		t.Fatalf("%+v", s)
	}
	if s[0].Revision != c.Find("lab-a/a").Revision || s[0].TransportProfile != "tcp" || s[0].AdmissionMode != "best_effort" || s[0].Description != "one" {
		t.Fatalf("%+v", s[0])
	}
	if len(Snapshot(cfg(t))) != 0 || Snapshot(cfg(t)) == nil {
		t.Fatal("empty snapshot must be an empty list")
	}
}

func TestInvalidated(t *testing.T) {
	old := cfg(t, exp("same", "127.0.0.1:1", "x", true), exp("desc", "127.0.0.1:2", "x", true),
		exp("moved", "127.0.0.1:3", "", true), exp("gone", "127.0.0.1:4", "", true), exp("disabled", "127.0.0.1:5", "", true))
	next := cfg(t, exp("same", "127.0.0.1:1", "x", true), exp("desc", "127.0.0.1:2", "y", true),
		exp("moved", "127.0.0.1:9", "", true), exp("disabled", "127.0.0.1:5", "", false), exp("new", "127.0.0.1:6", "", true))
	got := Invalidated(old, next)
	if len(got) != 3 || !got["lab-a/moved"] || !got["lab-a/gone"] || !got["lab-a/disabled"] {
		t.Fatalf("%v", got)
	}
}
