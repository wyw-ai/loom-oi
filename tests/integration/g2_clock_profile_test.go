package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

const g2LocalClockProfile = "local-isolated-vm-v1"

var g2ClockUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var g2ClockDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var g2ClockSourcePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var g2LocalClockIdentityKeys = []string{
	"schema", "run_id", "job", "source_sha", "nonce_sha256", "boot_id", "execution_profile", "attestation_sha256", "vm_uuid", "vm_serial",
	"base_image_sha256", "qemu_binary_sha256", "launch_sha256", "host_nonce_sha256",
}

type g2ClockExecution struct {
	local    bool
	expected map[string]string
	pinned   map[string]string
}

func g2ClockEnvironment() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		k, v, ok := strings.Cut(item, "=")
		if ok {
			env[k] = v
		}
	}
	return env
}

func g2ParseClockExecution(env map[string]string) (*g2ClockExecution, error) {
	profile, explicit := env["OI_EXECUTION_PROFILE"]
	if !explicit {
		for key := range env {
			if strings.HasPrefix(key, "OI_LOCAL_") {
				return nil, fmt.Errorf("local clock identity without explicit execution profile")
			}
		}
		for key, want := range map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted", "GITHUB_JOB": "clock", "GITHUB_REPOSITORY": "wyw-ai/loom-oi"} {
			if env[key] != want {
				return nil, fmt.Errorf("clock workflow guard %s", key)
			}
		}
		return &g2ClockExecution{}, nil
	}
	if profile != g2LocalClockProfile {
		return nil, fmt.Errorf("unknown explicit clock execution profile")
	}
	for key := range env {
		if strings.HasPrefix(key, "GITHUB_") || key == "RUNNER_ENVIRONMENT" {
			return nil, fmt.Errorf("local clock environment contains hosted identity key %s", key)
		}
	}
	run, source, attestation, vm, boot := env["OI_LOCAL_RUN_ID"], env["OI_LOCAL_SOURCE_SHA"], env["OI_LOCAL_ATTESTATION_SHA256"], env["OI_LOCAL_VM_UUID"], env["OI_LOCAL_BOOT_ID"]
	if !strings.HasPrefix(run, "local-") || !g2ClockUUIDPattern.MatchString(strings.TrimPrefix(run, "local-")) ||
		!g2ClockSourcePattern.MatchString(source) || !g2ClockDigestPattern.MatchString(attestation) ||
		!g2ClockUUIDPattern.MatchString(vm) || !g2ClockUUIDPattern.MatchString(boot) || !g2ClockDigestPattern.MatchString(env["OI_CLOCK_NONCE"]) {
		return nil, fmt.Errorf("local clock environment has incomplete or malformed identity")
	}
	nonce := sha256.Sum256([]byte(env["OI_CLOCK_NONCE"]))
	return &g2ClockExecution{local: true, expected: map[string]string{
		"schema": "g2-clock-guard-v2", "execution_profile": g2LocalClockProfile, "run_id": run, "job": "clock", "source_sha": source,
		"attestation_sha256": attestation, "vm_uuid": vm, "boot_id": boot, "vm_serial": "loom-oi-clock-" + vm, "nonce_sha256": hex.EncodeToString(nonce[:]),
	}}, nil
}

func (p *g2ClockExecution) validateIdentity(response map[string]any) error {
	if p == nil {
		return fmt.Errorf("clock execution profile missing")
	}
	if !p.local {
		if _, ok := response["execution_profile"]; ok || response["schema"] == "g2-clock-guard-v2" {
			return fmt.Errorf("local guard identity in hosted execution")
		}
		return nil // Keep the historical hosted response contract.
	}
	if _, ok := response["workflow_ref"]; ok {
		return fmt.Errorf("hosted workflow identity in local guard response")
	}
	for key := range response {
		if strings.HasPrefix(key, "GITHUB_") || key == "RUNNER_ENVIRONMENT" {
			return fmt.Errorf("hosted key in local guard response")
		}
	}
	identity := make(map[string]string, len(g2LocalClockIdentityKeys))
	for _, key := range g2LocalClockIdentityKeys {
		value, ok := response[key].(string)
		if !ok || value == "" {
			return fmt.Errorf("local guard identity %s missing or not a string", key)
		}
		if want, ok := p.expected[key]; ok && value != want {
			return fmt.Errorf("local guard identity %s differs from execution", key)
		}
		if strings.HasSuffix(key, "_sha256") && !g2ClockDigestPattern.MatchString(value) {
			return fmt.Errorf("invalid local guard digest %s", key)
		}
		if p.pinned != nil && p.pinned[key] != value {
			return fmt.Errorf("local guard identity %s changed", key)
		}
		identity[key] = value
	}
	// Commit the first complete identity only after every field was validated.
	if p.pinned == nil {
		p.pinned = identity
	}
	return nil
}

func g2ValidateClockResponse(execution *g2ClockExecution, guardPID, peerPID int, response map[string]any) error {
	if response["ok"] != true {
		return fmt.Errorf("clock guard rejected request")
	}
	n, ok := response["guard_pid"].(json.Number)
	if !ok {
		return fmt.Errorf("clock guard PID is not an integer")
	}
	pid, err := n.Int64()
	if err != nil || pid <= 0 || (guardPID != 0 && pid != int64(guardPID)) {
		return fmt.Errorf("clock guard PID changed or invalid")
	}
	if response["peer_pid"] != json.Number(fmt.Sprint(peerPID)) {
		return fmt.Errorf("clock guard saw another test process")
	}
	return execution.validateIdentity(response)
}

func g2TestClockLocalEnvironment() map[string]string {
	return map[string]string{"OI_EXECUTION_PROFILE": g2LocalClockProfile, "OI_LOCAL_RUN_ID": "local-12345678-1234-1234-1234-123456789abc",
		"OI_LOCAL_SOURCE_SHA": strings.Repeat("a", 40), "OI_LOCAL_ATTESTATION_SHA256": strings.Repeat("b", 64),
		"OI_LOCAL_VM_UUID": "23456789-1234-1234-1234-123456789abc", "OI_LOCAL_BOOT_ID": "34567890-1234-1234-1234-123456789abc", "OI_CLOCK_NONCE": strings.Repeat("c", 64)}
}

func g2TestClockResponse(p *g2ClockExecution) map[string]any {
	r := map[string]any{"ok": true, "guard_pid": json.Number("123"), "peer_pid": json.Number("456")}
	for k, v := range p.expected {
		r[k] = v
	}
	for _, k := range []string{"base_image_sha256", "qemu_binary_sha256", "launch_sha256", "host_nonce_sha256"} {
		r[k] = strings.Repeat("d", 64)
	}
	return r
}

func TestG2ClockExecutionProfile(t *testing.T) {
	t.Run("hosted_default", func(t *testing.T) {
		env := map[string]string{"GITHUB_ACTIONS": "true", "RUNNER_ENVIRONMENT": "github-hosted", "GITHUB_JOB": "clock", "GITHUB_REPOSITORY": "wyw-ai/loom-oi"}
		p, err := g2ParseClockExecution(env)
		if err != nil || p.local {
			t.Fatalf("old hosted profile rejected: %v", err)
		}
		for _, key := range []string{"GITHUB_ACTIONS", "RUNNER_ENVIRONMENT", "GITHUB_JOB", "GITHUB_REPOSITORY"} {
			saved := env[key]
			env[key] = "wrong"
			if _, err := g2ParseClockExecution(env); err == nil {
				t.Fatalf("hosted guard %s omitted", key)
			}
			env[key] = saved
		}
	})
	t.Run("local_explicit", func(t *testing.T) {
		p, err := g2ParseClockExecution(g2TestClockLocalEnvironment())
		if err != nil || !p.local {
			t.Fatalf("local profile rejected: %v", err)
		}
	})
	for _, scenario := range []string{"unknown_profile", "empty_profile", "implicit_local", "mixed_github", "mixed_runner", "missing_run", "bad_run", "bad_source", "bad_attestation", "bad_vm", "bad_boot", "bad_nonce"} {
		t.Run(scenario, func(t *testing.T) {
			env := g2TestClockLocalEnvironment()
			switch scenario {
			case "unknown_profile":
				env["OI_EXECUTION_PROFILE"] = "local"
			case "empty_profile":
				env["OI_EXECUTION_PROFILE"] = ""
			case "implicit_local":
				delete(env, "OI_EXECUTION_PROFILE")
			case "mixed_github":
				env["GITHUB_ACTIONS"] = ""
			case "mixed_runner":
				env["RUNNER_ENVIRONMENT"] = ""
			case "missing_run":
				delete(env, "OI_LOCAL_RUN_ID")
			case "bad_run":
				env["OI_LOCAL_RUN_ID"] = "local-no-uuid"
			case "bad_source":
				env["OI_LOCAL_SOURCE_SHA"] = strings.Repeat("a", 39)
			case "bad_attestation":
				env["OI_LOCAL_ATTESTATION_SHA256"] = strings.Repeat("A", 64)
			case "bad_vm":
				env["OI_LOCAL_VM_UUID"] = "invalid"
			case "bad_boot":
				env["OI_LOCAL_BOOT_ID"] = "invalid"
			case "bad_nonce":
				env["OI_CLOCK_NONCE"] = strings.Repeat("x", 64)
			}
			if _, err := g2ParseClockExecution(env); err == nil {
				t.Fatal("invalid local environment accepted")
			}
		})
	}
}

func TestG2ClockLocalResponseIdentity(t *testing.T) {
	for _, key := range g2LocalClockIdentityKeys {
		t.Run("changed_"+key, func(t *testing.T) {
			p, err := g2ParseClockExecution(g2TestClockLocalEnvironment())
			if err != nil {
				t.Fatal(err)
			}
			r := g2TestClockResponse(p)
			if err := g2ValidateClockResponse(p, 0, 456, r); err != nil {
				t.Fatal(err)
			}
			if err := g2ValidateClockResponse(p, 123, 456, r); err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(key, "_sha256") {
				r[key] = strings.Repeat("e", 64)
			} else {
				r[key] = r[key].(string) + "changed"
			}
			if err := g2ValidateClockResponse(p, 123, 456, r); err == nil {
				t.Fatal("changed local identity accepted")
			}
		})
	}
	for _, scenario := range []string{"missing_key", "numeric_key", "malformed_hash", "hosted_workflow", "github_key", "runner_key", "bad_first_then_valid"} {
		t.Run(scenario, func(t *testing.T) {
			p, err := g2ParseClockExecution(g2TestClockLocalEnvironment())
			if err != nil {
				t.Fatal(err)
			}
			r := g2TestClockResponse(p)
			switch scenario {
			case "missing_key":
				delete(r, "launch_sha256")
			case "numeric_key":
				r["launch_sha256"] = json.Number("1")
			case "malformed_hash":
				r["launch_sha256"] = "invalid"
			case "hosted_workflow":
				r["workflow_ref"] = "workflow"
			case "github_key":
				r["GITHUB_JOB"] = "clock"
			case "runner_key":
				r["RUNNER_ENVIRONMENT"] = "github-hosted"
			case "bad_first_then_valid":
				r["source_sha"] = strings.Repeat("f", 40)
			}
			if err := g2ValidateClockResponse(p, 0, 456, r); err == nil || p.pinned != nil {
				t.Fatal("bad initial response accepted or partially pinned")
			}
			if err := g2ValidateClockResponse(p, 0, 456, g2TestClockResponse(p)); err != nil {
				t.Fatalf("bad response poisoned clean initial identity: %v", err)
			}
		})
	}
}

func TestG2ClockResponseProcessBinding(t *testing.T) {
	for _, scenario := range []string{"wrong_guard", "wrong_peer", "zero_guard", "noninteger_guard", "rejected", "hosted_legacy", "hosted_local_mix"} {
		t.Run(scenario, func(t *testing.T) {
			p := &g2ClockExecution{}
			r := map[string]any{"ok": true, "guard_pid": json.Number("123"), "peer_pid": json.Number("456"), "schema": "g2-clock-guard-v1"}
			switch scenario {
			case "wrong_guard":
				r["guard_pid"] = json.Number("124")
			case "wrong_peer":
				r["peer_pid"] = json.Number("457")
			case "zero_guard":
				r["guard_pid"] = json.Number("0")
			case "noninteger_guard":
				r["guard_pid"] = "123"
			case "rejected":
				r["ok"] = false
			case "hosted_local_mix":
				r["execution_profile"] = g2LocalClockProfile
			}
			err := g2ValidateClockResponse(p, 123, 456, r)
			if (err == nil) != (scenario == "hosted_legacy") {
				t.Fatalf("wrong process/profile verdict: %v", err)
			}
		})
	}
}
