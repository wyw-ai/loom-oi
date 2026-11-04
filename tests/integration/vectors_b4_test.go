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
				targetBefore := len(e.t1.Conns())
				oisBefore, oicBefore := e.oisStats().Process, e.oicStats("lab-a").Process
				relayBefore, clientBefore := e.oisStats().RelayBytes, procstats.Read()
				var wg sync.WaitGroup
				var opens []int64
				workers := make([]loadWorker, n)
				start := time.Now()
				for i := range n {
					wg.Add(1)
					go func() {
						defer wg.Done()
						w := &workers[i]
						defer func() {
							if w.FinishedNS == 0 {
								w.FinishedNS = resourceNS()
							}
						}()
						w.Worker = i
						w.StartedNS = resourceNS()
						t0 := time.Now()
						s, err := e.consumer("alice").Dial(bg, "lab-a/http")
						w.DialReturnedNS = resourceNS()
						if err != nil {
							w.FirstError = "dial: " + err.Error()
							t.Error(err)
							return
						}
						w.ChannelID, w.OpenMS = s.ChannelID(), time.Since(t0).Milliseconds()
						req := []byte(fmt.Sprintf("stream %d", size))
						w.WriteStartedNS = resourceNS()
						w.Written, err = s.Write(req)
						w.WriteReturnedNS, w.WriteError = resourceNS(), loadError(err)
						if err != nil || w.Written != len(req) {
							w.FirstError = fmt.Sprintf("write %d/%d: %v", w.Written, len(req), err)
							t.Errorf("channel %s %s", w.ChannelID, w.FirstError)
						}
						w.FINStartedNS = resourceNS()
						err = s.CloseWrite()
						w.FINReturnedNS, w.FINError = resourceNS(), loadError(err)
						if err != nil {
							if w.FirstError == "" {
								w.FirstError = "close_write: " + err.Error()
							}
							t.Errorf("channel %s close_write: %v", w.ChannelID, err)
						}
						w.ReadStartedNS = resourceNS()
						reader := &loadReader{Reader: s}
						got, err := io.Copy(io.Discard, reader)
						w.FirstReadNS, w.ReadReturnedNS, w.Received, w.ReadError = reader.firstNS, resourceNS(), got, loadError(err)
						w.CloseError = loadError(s.Close())
						w.FinishedNS = resourceNS()
						if err != nil || got != size {
							if w.FirstError == "" {
								w.FirstError = fmt.Sprintf("received %d: %v", got, err)
							}
							t.Errorf("received %d: %v", got, err)
						}
						w.Success = w.FirstError == ""
					}()
				}
				wg.Wait()
				el := time.Since(start)
				var received, successfulBytes int64
				succeeded := 0
				for _, w := range workers {
					received += w.Received
					if w.Success {
						succeeded++
						successfulBytes += w.Received
					}
					if w.ChannelID != "" {
						opens = append(opens, w.OpenMS)
					}
				}
				sort.Slice(opens, func(i, j int) bool { return opens[i] < opens[j] })
				var p50, maximum int64
				if len(opens) > 0 {
					p50, maximum = opens[len(opens)/2], opens[len(opens)-1]
				}
				oisAfter, oicAfter := e.oisStats(), e.oicStats("lab-a")
				clientAfter := procstats.Read()
				e.obs("O3", map[string]any{"vector": "TV-B4-06", "channels": n, "bytes_each": size, "ms": el.Milliseconds(), "elapsed_ns": el.Nanoseconds(),
					"mib_per_s": float64(received) / (1 << 20) / el.Seconds(), "requested_bytes": int64(n) * size, "received_bytes": received,
					"throughput_basis": "received_bytes_including_partial", "successful_bytes": successfulBytes,
					"successful_mib_per_s": float64(successfulBytes) / (1 << 20) / el.Seconds(),
					"successful_channels":  succeeded, "failed_channels": n - succeeded, "open_ms_p50": p50, "open_ms_max": maximum,
					"ois_cpu_ms": oisAfter.Process.CPUMillis - oisBefore.CPUMillis, "oic_cpu_ms": oicAfter.Process.CPUMillis - oicBefore.CPUMillis,
					"ois_rss": oisAfter.Process.RSSBytes, "oic_rss": oicAfter.Process.RSSBytes, "ois_fds": oisAfter.Process.OpenFDs,
					"ois_goroutines": oisAfter.Process.Goroutines, "ois_max_buffered": oisAfter.MaxBufferedBytes, "oic_max_buffered": oicAfter.MaxBufferedBytes,
					"oic_fds": oicAfter.Process.OpenFDs, "oic_goroutines": oicAfter.Process.Goroutines,
					"relay_bytes": oisAfter.RelayBytes - relayBefore,
					// The Go client shares the test process with the fixtures and proxies.
					"test_process_cpu_ms": clientAfter.CPUMillis - clientBefore.CPUMillis, "test_process_rss": clientAfter.RSSBytes,
					"path": "R", "h_path": "not applicable / not measured"})
				e.obs("O3-load-workers", map[string]any{"vector": "TV-B4-06", "channels": n, "bytes_each": size, "workers": workers})
				targets := e.t1.Conns()
				for i := targetBefore; i < len(targets); i++ {
					r := targets[i]
					e.obs("O3-target-boundaries", map[string]any{"vector": "TV-B4-06", "channels": n, "bytes_each": size,
						"local_address": r.LocalAddress, "remote_address": r.RemoteAddress, "request_bytes": len(r.Data),
						"accepted_ns": loadNS(r.Accepted), "request_first_ns": loadNS(r.First), "request_fin_ns": loadNS(r.EOFAt),
						"response_write_started_ns": loadNS(r.ResponseWriteStarted), "response_first_write_returned_ns": loadNS(r.ResponseFirstWriteReturned),
						"response_first_write_bytes": r.ResponseFirstWriteBytes, "response_first_write_result": r.ResponseFirstWriteResult,
						"response_write_finished_ns": loadNS(r.ResponseWriteFinished), "response_bytes": r.ResponseBytes, "response_write_result": r.ResponseWriteResult,
						"done_ns": loadNS(r.DoneAt), "eof": r.EOF, "reset": r.Reset, "done": r.Done})
				}
				e.settled()
			}
		}
	})
}

type loadWorker struct {
	Worker          int    `json:"worker"`
	ChannelID       string `json:"channel_id"`
	StartedNS       int64  `json:"started_ns"`
	DialReturnedNS  int64  `json:"dial_returned_ns"`
	OpenMS          int64  `json:"open_ms"`
	WriteStartedNS  int64  `json:"write_started_ns"`
	WriteReturnedNS int64  `json:"write_returned_ns"`
	Written         int    `json:"written"`
	WriteError      string `json:"write_error"`
	FINStartedNS    int64  `json:"fin_started_ns"`
	FINReturnedNS   int64  `json:"fin_returned_ns"`
	FINError        string `json:"fin_error"`
	ReadStartedNS   int64  `json:"read_started_ns"`
	FirstReadNS     int64  `json:"first_read_ns"`
	ReadReturnedNS  int64  `json:"read_returned_ns"`
	FinishedNS      int64  `json:"finished_ns"`
	Received        int64  `json:"received"`
	ReadError       string `json:"read_error"`
	CloseError      string `json:"close_error"`
	FirstError      string `json:"first_error"`
	Success         bool   `json:"success"`
}

type loadReader struct {
	io.Reader
	firstNS int64
}

func (r *loadReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 && r.firstNS == 0 {
		r.firstNS = resourceNS()
	}
	return n, err
}
func loadError(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
func loadNS(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Sub(resourceOrigin).Nanoseconds()
}
