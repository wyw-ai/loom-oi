package relay

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyw-ai/loom-oi/tests/pki"
)

func TestReloadProbeCAWithoutJSONChange(t *testing.T) {
	ca := must(pki.NewCA("control"))
	dir := t.TempDir()
	cfg := baseConfig(t, dir, ca, newFakeOIS(t, ca), newTarget(t))
	probeCA := must(pki.NewCA("probe"))
	probePath := must(probeCA.Write(dir, "probe"))
	cfg["exposures"].([]any)[0].(map[string]any)["probe"] = map[string]any{
		"kind": "http_status_v1", "url": "https://localhost/ready", "allowed_cidrs": []string{"127.0.0.0/8"},
		"tls_ca_file": "probe.pem", "ready_statuses": []int{200}, "not_ready_statuses": []int{503},
	}
	data, _ := json.Marshal(cfg)
	path := filepath.Join(dir, "oic.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	a := must(New(Options{ConfigPath: path}))
	defer a.hardCancel()
	old, oldSum := a.cfg, a.cfgSum
	replacement := must(pki.NewCA("replacement"))
	if err := os.WriteFile(probePath, replacement.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	a.reload(context.Background())
	if a.cfg == old || a.cfgSum == oldSum || a.cfg.Exposures[0].ProbePlan().Digest() == old.Exposures[0].ProbePlan().Digest() || a.cfg.Exposures[0].Revision != old.Exposures[0].Revision {
		t.Fatal("unchanged JSON hid replaced trust content")
	}
	valid, validSum := a.cfg, a.cfgSum
	if err := os.WriteFile(probePath, []byte("bad CA"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.reload(context.Background())
	if a.cfg != valid || a.cfgSum != validSum || a.cfgErr == "" {
		t.Fatal("failed candidate changed live configuration")
	}
	if err := os.WriteFile(probePath, replacement.PEM, 0o600); err != nil {
		t.Fatal(err)
	}
	a.reload(context.Background())
	if a.cfg != valid || a.cfgErr != "" {
		t.Fatal("restored identical trust did not clear error without replacement")
	}
}
