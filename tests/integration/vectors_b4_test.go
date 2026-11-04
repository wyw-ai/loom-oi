package integration

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wyw-ai/loom-oi/internal/procstats"
	"github.com/wyw-ai/loom-oi/internal/protocol"
	"github.com/wyw-ai/loom-oi/tests/fixture"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// buildInfo returns the `go version -m` lines that pin dependencies and
// build settings.
func buildInfo(t *testing.T, bin string) []string {
	t.Helper()
	out, err := exec.Command("go", "version", "-m", bin).Output()
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && (f[0] == "dep" || f[0] == "mod" || f[0] == "build") {
			keep = append(keep, strings.Join(f, " "))
		}
	}
	return keep
}

// TV-B4-01: the same commit built twice for each of the four B4 §5
// platforms gives identical binaries and dependency lines, none using cgo,
// and the host build is the one the vectors ran.
func TestTVB4_01_ReproducibleBuild(t *testing.T) {
	root := moduleRoot(t)
	used := map[string]string{"ois": oisBin, "oic": oicBin, "oi": oiBin}
	for _, p := range []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/arm64"} {
		goos, goarch, _ := strings.Cut(p, "/")
		rounds := [2]string{t.TempDir(), t.TempDir()}
		for _, dir := range rounds {
			if err := crossBuild(root, dir, goos, goarch); err != nil {
				t.Fatal(err)
			}
		}
		exe := ""
		if goos == "windows" {
			exe = ".exe"
		}
		for _, name := range []string{"ois", "oic", "oi"} {
			a, b := filepath.Join(rounds[0], name+exe), filepath.Join(rounds[1], name+exe)
			info := buildInfo(t, a)
			if strings.Join(info, "\n") != strings.Join(buildInfo(t, b), "\n") || fileDigest(t, a) != fileDigest(t, b) {
				t.Fatalf("%s %s: the two builds differ", p, name)
			}
			for _, want := range []string{"build CGO_ENABLED=0", "build GOOS=" + goos, "build GOARCH=" + goarch} {
				if !contains(info, want) {
					t.Fatalf("%s %s: %q missing from %v", p, name, want, info)
				}
			}
			if goos == runtime.GOOS && goarch == runtime.GOARCH && strings.Join(buildInfo(t, used[name]), "\n") != strings.Join(info, "\n") {
				t.Fatalf("%s: the vectors ran a different build", name)
			}
		}
	}
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TV-B4-03: after each failure, channels, tickets, goroutines and
// descriptors return to baseline within T_close+1s (every vector also
// calls settled; this test chains failures back to back).
func TestTVB4_03_ResourceRegression(t *testing.T) {
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		// Target reset mid-answer.
		s := must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("reset"))
		_ = s.CloseWrite()
		_, _ = io.ReadAll(s)
		_ = s.Close()
		e.settled()
		// Revocation of an open channel.
		s = must(e.consumer("alice").Dial(bg, "lab-a/http"))
		_, _ = s.Write([]byte("hold"))
		e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(false)})
		_, _ = io.ReadAll(s)
		_ = s.Close()
		e.write("PATCH", "/v1/admin/policies/pa", protocol.PolicySpec{Enabled: ptr(true)})
		e.settled()
		// A consumer that drops its leg.
		o := e.open("alice", "lab-a/http")
		leg := must2(e.rawAttach(o.Attach.URL, e.token("alice"), o.Attach.Ticket, nil, protocol.TunnelSubprotocol))
		readFrame(t, leg)
		_ = leg.CloseNow()
		e.settled()
		// Prepare timeout.
		e.acc.setMode(hold)
		o = e.open("alice", "lab-a/http")
		e.closedState("alice", o.ChannelID)
		e.acc.setMode(pass)
		e.settled()
	})
}

// allowedModules are the only non-standard modules OI binaries may link
// (B4 §2 pins and their own dependencies).
var allowedModules = []string{
	"github.com/wyw-ai/loom-oi", "github.com/coder/websocket", "github.com/jackc/", "modernc.org/",
	"golang.org/x/", "github.com/google/uuid", "github.com/dustin/go-humanize", "github.com/mattn/go-isatty",
	"github.com/ncruces/go-strftime", "github.com/remyoudompheng/bigfft",
}

// TV-B4-04: OI links no Loom, LLM or business SDK, and a different TCP
// service is exposed by changing only the OIC configuration.
func TestTVB4_04_NoBusinessDependency(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./cmd/...")
	cmd.Dir = moduleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	for _, p := range strings.Fields(string(out)) {
		ok := false
		for _, a := range allowedModules {
			if strings.HasPrefix(p, a) {
				ok = true
			}
		}
		if !ok {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		t.Fatalf("unexpected dependencies: %v", bad)
	}
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{})
		echo := fixture.New(t, "ECHO", fixture.Echo)
		exps := append([]any{}, e.oics["lab-a"].cfg["exposures"].([]any)...)
		exps = append(exps, map[string]any{"name": "echo", "protocol": "tcp-echo", "description": "a plain TCP echo service",
			"target": map[string]any{"address": echo.Addr()}})
		e.rewriteOIC("lab-a", exps)
		e.eventually(10*time.Second, "echo registered", func() bool {
			_, err := e.consumer("alice").Lookup(bg, "lab-a/echo")
			return err == nil
		})
		got, _ := request(t, e.consumer("alice"), "lab-a/echo", "line one\nline two\n")
		if got != "line one\nline two\n" {
			t.Fatalf("%q", got)
		}
		e.settled()
	})
}

// TV-B4-06: fixed load, recorded only (O3): 1, 8 and 32 concurrent
// channels moving 1 MiB and 64 MiB each. Set OI_IT_LOAD=1 to run.
func TestTVB4_06_Load(t *testing.T) {
	if os.Getenv("OI_IT_LOAD") != "1" {
		t.Skip("set OI_IT_LOAD=1 to record the O3 load profile")
	}
	each(t, func(t *testing.T, be string) {
		e := newEnv(t, be, envOpts{limits: map[string]any{"n_subject": 64}})
		for _, size := range []int64{1 << 20, 64 << 20} {
			for _, n := range []int{1, 8, 32} {
				oisBefore, oicBefore := e.oisStats().Process, e.oicStats("lab-a").Process
				relayBefore, clientBefore := e.oisStats().RelayBytes, procstats.Read()
				var wg sync.WaitGroup
				var mu sync.Mutex
				var opens []int64
				start := time.Now()
				for range n {
					wg.Add(1)
					go func() {
						defer wg.Done()
						t0 := time.Now()
						s, err := e.consumer("alice").Dial(bg, "lab-a/http")
						if err != nil {
							t.Error(err)
							return
						}
						open := time.Since(t0).Milliseconds()
						_, _ = s.Write([]byte(fmt.Sprintf("stream %d", size)))
						_ = s.CloseWrite()
						got, err := io.Copy(io.Discard, s)
						_ = s.Close()
						if err != nil || got != size {
							t.Errorf("received %d: %v", got, err)
						}
						mu.Lock()
						opens = append(opens, open)
						mu.Unlock()
					}()
				}
				wg.Wait()
				el := time.Since(start)
				sort.Slice(opens, func(i, j int) bool { return opens[i] < opens[j] })
				oisAfter, oicAfter := e.oisStats(), e.oicStats("lab-a")
				clientAfter := procstats.Read()
				e.obs("O3", map[string]any{"vector": "TV-B4-06", "channels": n, "bytes_each": size, "ms": el.Milliseconds(),
					"mib_per_s": float64(int64(n)*size) / (1 << 20) / el.Seconds(), "open_ms_p50": opens[len(opens)/2], "open_ms_max": opens[len(opens)-1],
					"ois_cpu_ms": oisAfter.Process.CPUMillis - oisBefore.CPUMillis, "oic_cpu_ms": oicAfter.Process.CPUMillis - oicBefore.CPUMillis,
					"ois_rss": oisAfter.Process.RSSBytes, "oic_rss": oicAfter.Process.RSSBytes, "ois_fds": oisAfter.Process.OpenFDs,
					"ois_goroutines": oisAfter.Process.Goroutines, "ois_max_buffered": oisAfter.MaxBufferedBytes, "oic_max_buffered": oicAfter.MaxBufferedBytes,
					"oic_fds": oicAfter.Process.OpenFDs, "oic_goroutines": oicAfter.Process.Goroutines,
					"relay_bytes": oisAfter.RelayBytes - relayBefore,
					// The Go client shares the test process with the fixtures and proxies.
					"test_process_cpu_ms": clientAfter.CPUMillis - clientBefore.CPUMillis, "test_process_rss": clientAfter.RSSBytes,
					"path": "R", "h_path": "not applicable / not measured"})
				e.settled()
			}
		}
	})
}
