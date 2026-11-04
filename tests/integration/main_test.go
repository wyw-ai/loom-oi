package integration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	oisBin, oicBin, oiBin string
	runRoot               string
)

func TestMain(m *testing.M) {
	if os.Getenv("OI_IT") != "1" {
		fmt.Println("integration vectors skipped: set OI_IT=1")
		os.Exit(0)
	}
	code, err := setup(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration setup:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func setup(m *testing.M) (int, error) {
	root, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return 0, err
	}
	bin := os.Getenv("OI_IT_BIN")
	if bin == "" {
		if bin, err = os.MkdirTemp("", "oi-it-bin-"); err != nil {
			return 0, err
		}
		defer os.RemoveAll(bin)
	}
	if err := buildAll(strings.TrimSpace(string(root)), bin); err != nil {
		return 0, err
	}
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	oisBin, oicBin, oiBin = filepath.Join(bin, "ois"+exe), filepath.Join(bin, "oic"+exe), filepath.Join(bin, "oi"+exe)
	runRoot = os.Getenv("OI_IT_RUNDIR")
	if runRoot == "" {
		if runRoot, err = os.MkdirTemp("", "oi-it-run-"); err != nil {
			return 0, err
		}
		defer os.RemoveAll(runRoot)
	}
	return m.Run(), nil
}

// buildAll builds the three binaries as CI does: CGO_ENABLED=0, -trimpath.
func buildAll(root, out string) error {
	return crossBuild(root, out, runtime.GOOS, runtime.GOARCH)
}

// crossBuild builds the three binaries for goos/goarch like the release
// builds of B4 §5: -trimpath and CGO_ENABLED=0.
func crossBuild(root, out, goos, goarch string) error {
	for _, name := range []string{"ois", "oic", "oi"} {
		dst := filepath.Join(out, name)
		if goos == "windows" {
			dst += ".exe"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-o", dst, "./cmd/"+name)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s for %s/%s: %v\n%s", name, goos, goarch, err, out)
		}
	}
	return nil
}

// proc is a running binary whose JSON log lines are kept and saved.
type proc struct {
	name   string
	cmd    *exec.Cmd
	mu     sync.Mutex
	lines  []map[string]any
	raw    []string
	exited chan struct{}
	err    error
}

func startProc(t *testing.T, dir, name, bin string, env []string, args ...string) *proc {
	t.Helper()
	p := &proc{name: name, exited: make(chan struct{})}
	p.cmd = exec.Command(bin, args...)
	p.cmd.Env = append(os.Environ(), env...)
	logf, err := os.Create(filepath.Join(dir, name+".log"))
	if err != nil {
		t.Fatal(err)
	}
	// stdout and stderr share one pipe so every JSON line is seen in order.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stdout, p.cmd.Stderr = pw, pw
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()
	go func() {
		defer pr.Close()
		sc := bufio.NewScanner(io.TeeReader(pr, logf))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var m map[string]any
			_ = json.Unmarshal(sc.Bytes(), &m)
			p.mu.Lock()
			p.raw = append(p.raw, sc.Text())
			if m != nil {
				p.lines = append(p.lines, m)
			}
			p.mu.Unlock()
		}
		p.err = p.cmd.Wait()
		_ = logf.Close()
		close(p.exited)
	}()
	t.Cleanup(func() { p.kill() })
	return p
}

// waitLog returns the first log line (after index after) whose msg is msg.
func (p *proc) waitLog(t *testing.T, timeout time.Duration, msg string, after int) (map[string]any, int) {
	t.Helper()
	return p.waitLine(t, timeout, msg, after, func(m map[string]any) bool { return m["msg"] == msg })
}

// waitLine returns the first JSON line (after index after) matching match.
func (p *proc) waitLine(t *testing.T, timeout time.Duration, msg string, after int, match func(map[string]any) bool) (map[string]any, int) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		p.mu.Lock()
		for i := after; i < len(p.lines); i++ {
			if match(p.lines[i]) {
				l := p.lines[i]
				p.mu.Unlock()
				return l, i + 1
			}
		}
		n := len(p.raw)
		p.mu.Unlock()
		select {
		case <-p.exited:
			t.Fatalf("%s exited (%v) before %q:\n%s", p.name, p.err, msg, p.tail(n))
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: no %q within %v:\n%s", p.name, msg, timeout, p.tail(n))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *proc) tail(n int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	hi := min(n, len(p.raw))
	return strings.Join(p.raw[max(0, hi-20):hi], "\n")
}

// waitExit waits for the process to end and returns its exit code.
func (p *proc) waitExit(t *testing.T, timeout time.Duration) int {
	t.Helper()
	select {
	case <-p.exited:
	case <-time.After(timeout):
		t.Fatalf("%s did not exit", p.name)
	}
	if p.cmd.ProcessState == nil {
		return -1
	}
	return p.cmd.ProcessState.ExitCode()
}

func (p *proc) kill() {
	select {
	case <-p.exited:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	<-p.exited
}

// stop ends the process gracefully where the OS allows it.
func (p *proc) stop(t *testing.T) {
	t.Helper()
	select {
	case <-p.exited:
		return
	default:
	}
	if err := terminate(p.cmd.Process); err != nil {
		p.kill()
		return
	}
	select {
	case <-p.exited:
	case <-time.After(10 * time.Second):
		p.kill()
	}
}
