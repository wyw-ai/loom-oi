package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/ois/identity"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

const g2LegacySHA = "570b573f8151466fd0ad42b4cdf94ac3c81d323f"

type g2Legacy struct {
	OIS, OIC, OI string
	Artifact     map[string]any
}

func g2Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// 旧版从固定 Git 对象逐字节解包并实际构建，绝不以 Go fake 代替旧节点。
func g2BuildLegacy(t *testing.T) g2Legacy {
	t.Helper()
	root := moduleRoot(t)
	cmd := exec.Command("git", "archive", "--format=tar", g2LegacySHA, "cmd", "internal", "go.mod", "go.sum")
	cmd.Dir = root
	archive, err := cmd.Output()
	if err != nil {
		t.Fatal("fixed legacy archive", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	bin := filepath.Join(dir, "bin")
	if err = os.MkdirAll(src, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(archive))
	files := map[string]string{}
	archiveCommitVerified := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			if h.PAXRecords["comment"] != g2LegacySHA {
				t.Fatalf("archive commit marker mismatch: %v", h.PAXRecords)
			}
			archiveCommitVerified = true
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		clean := filepath.ToSlash(filepath.Clean(name))
		if clean != name || filepath.IsAbs(name) || strings.HasPrefix(name, "../") || !(name == "cmd" || strings.HasPrefix(name, "cmd/") || name == "internal" || strings.HasPrefix(name, "internal/") || name == "go.mod" || name == "go.sum") {
			t.Fatalf("unexpected archive path %q", h.Name)
		}
		dst := filepath.Join(src, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(dst, 0700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if h.Typeflag != tar.TypeReg {
			t.Fatalf("archive contains nonregular source %q", name)
		}
		if _, dup := files[name]; dup {
			t.Fatal("duplicate archive path", name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if int64(len(body)) != h.Size {
			t.Fatal("short archive entry")
		}
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(dst, body, 0600); err != nil {
			t.Fatal(err)
		}
		files[name] = g2Hash(body)
	}
	if !archiveCommitVerified {
		t.Fatal("git archive lacks exact commit marker")
	}
	for _, required := range []string{"go.mod", "go.sum", "cmd/ois/main.go", "cmd/oic/main.go", "cmd/oi/main.go"} {
		if files[required] == "" {
			t.Fatal("missing frozen source", required)
		}
	}
	if err = crossBuild(src, bin, runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	inventory := make([]map[string]string, 0, len(names))
	for _, name := range names {
		inventory = append(inventory, map[string]string{"path": name, "sha256": files[name]})
	}
	binaries := map[string]any{}
	paths := map[string]string{}
	for _, name := range []string{"ois", "oic", "oi"} {
		file := filepath.Join(bin, name)
		if runtime.GOOS == "windows" {
			file += ".exe"
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		info, err := exec.Command("go", "version", "-m", file).Output()
		if err != nil {
			t.Fatal(err)
		}
		paths[name] = file
		binaries[name] = map[string]any{"sha256": g2Hash(body), "bytes": len(body), "go_version_m": string(info)}
	}
	return g2Legacy{OIS: paths["ois"], OIC: paths["oic"], OI: paths["oi"], Artifact: map[string]any{"source_sha": g2LegacySHA, "archive_sha256": g2Hash(archive), "source_files": inventory, "binaries": binaries, "goos": runtime.GOOS, "goarch": runtime.GOARCH, "actual_build": true}}
}

type g2CLIResult struct {
	Stdout, Stderr        string
	ExitCode              int
	StartedNS, FinishedNS int64
	TimedOut              bool
}

func g2LegacyCLI(bin string, e *env, exposure, input string) g2CLIResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--ois", "https://"+e.api, "--ca", e.caFile, "connect", exposure, "--stdio")
	cmd.Env = append(os.Environ(), "OI_TOKEN="+e.token("alice"))
	cmd.Stdin = strings.NewReader(input)
	var out, errout bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errout
	r := g2CLIResult{StartedNS: resourceNS()}
	err := cmd.Run()
	r.FinishedNS = resourceNS()
	r.Stdout, r.Stderr = out.String(), errout.String()
	r.TimedOut = ctx.Err() != nil
	if err != nil {
		r.ExitCode = -1
		if ex, ok := err.(*exec.ExitError); ok {
			r.ExitCode = ex.ExitCode()
		}
	}
	return r
}
func g2LegacyCLIAsync(bin string, e *env, exposure, input string) <-chan g2CLIResult {
	// Ensure token lookup/admin mutation happens on the caller before spawning.
	e.token("alice")
	done := make(chan g2CLIResult, 1)
	go func() { done <- g2LegacyCLI(bin, e, exposure, input) }()
	return done
}
func g2AwaitLegacyCLI(t *testing.T, done <-chan g2CLIResult) g2CLIResult {
	t.Helper()
	select {
	case r := <-done:
		if r.TimedOut {
			t.Fatal("legacy CLI exceeded deadline; killed process is not passing evidence")
		}
		return r
	case <-time.After(6 * time.Second):
		t.Fatal("legacy CLI did not join")
		return g2CLIResult{}
	}
}

// 预期拒绝时专用：真实Access/TLS授权与配置，启动到diagnostics，不等待注册。
func g2LaunchRejectedOIC(g *g2Env, beforeStart func(*g2Proxy)) *oicProc {
	e := g.env
	leaf := must(e.ca.Client("lab-a"))
	if _, _, err := leaf.Write(e.dir, "lab-a"); err != nil {
		e.t.Fatal(err)
	}
	e.write("POST", "/v1/admin/accesses", protocol.AccessSpec{AccessID: "lab-a", SPKIPins: []string{identity.SPKIPin(leaf.Cert)}})
	p := newG2Proxy(e.t, "wss://"+e.accessAddr, e.ca, leaf)
	g.paths["lab-a"] = p
	if beforeStart != nil {
		beforeStart(p)
	}
	exp := exposure("http", e.t1.Addr(), protocol.AdmissionRequireReady, nil)
	exp["probe"] = g2ProbeConfig(g.health["lab-a/http"])
	o := &oicProc{access: "lab-a", leaf: leaf, path: filepath.Join(e.dir, "rejected-lab-a.json")}
	o.cfg = map[string]any{"access_id": "lab-a", "ois": map[string]any{"url": p.URL(), "ca_file": "ca.pem"}, "tls": map[string]any{"cert_file": "lab-a.pem", "key_file": "lab-a-key.pem"}, "exposures": []any{exp, exposure("alt", e.t3.Addr(), protocol.AdmissionBestEffort, nil)}, "limits": e.limits, "observation_limits": g2ObservationLimits(), "metrics_enabled": true, "diag_listen": "127.0.0.1:0"}
	writeJSON(e.t, o.path, o.cfg)
	o.proc = startProc(e.t, e.dir, "oic-v2-refused", e.oicBinary, nil, "run", "--config", o.path)
	line, _ := o.proc.waitLog(e.t, 5*time.Second, "diagnostics listening", 0)
	o.diag = line["addr"].(string)
	e.oics["lab-a"] = o
	return o
}
func g2CompatEvent(g *g2Env, legacy g2Legacy, combination string) {
	g.event("compatibility_binary_sources", map[string]any{"combination": combination, "legacy": legacy.Artifact})
}
func g2LegacyErrorBody(t *testing.T, body []byte) protocol.Code {
	t.Helper()
	var v protocol.ErrorBody
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("invalid HTTP error body %q: %v", body, err)
	}
	return v.Code
}
