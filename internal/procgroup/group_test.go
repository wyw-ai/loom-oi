package procgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestGroupHelper runs only in explicitly marked subprocesses. The leaf is
// deliberately in a separate session on Linux, as a PTY child would be.
func TestGroupHelper(t *testing.T) {
	if os.Getenv("LOOM_GROUP_TEST_HELPER") != "1" {
		return
	}
	args := os.Args
	mode := args[len(args)-2]
	path := args[len(args)-1]
	if mode == "parent" {
		_ = os.WriteFile(path+".parent", []byte(strconv.Itoa(os.Getpid())), 0600)
		leaf := exec.Command(os.Args[0], "-test.run=^TestGroupHelper$", "--", "leaf", path)
		leaf.Env = os.Environ()
		prepareDetachedLeaf(leaf)
		if err := leaf.Start(); err != nil {
			os.Exit(71)
		}
	} else {
		_ = os.WriteFile(path+".child", []byte(strconv.Itoa(os.Getpid())), 0600)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func waitPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, e := os.ReadFile(path); e == nil {
			p, e := strconv.Atoi(string(b))
			if e == nil && p > 0 {
				return p
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("PID file absent: %s", path)
	return 0
}
func assertDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("owned descendant %d still alive", pid)
}

func groupTree(t *testing.T) (*Group, *exec.Cmd, int, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "owned")
	cmd := exec.Command(os.Args[0], "-test.run=^TestGroupHelper$", "--", "parent", path)
	cmd.Env = append(os.Environ(), "LOOM_GROUP_TEST_HELPER=1")
	g, e := New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { g.Close() })
	if e = g.Start(cmd); e != nil {
		t.Fatal(e)
	}
	parent := waitPID(t, path+".parent")
	child := waitPID(t, path+".child")
	return g, cmd, parent, child
}

func TestCloseReclaimsOwnedDescendants(t *testing.T) {
	g, cmd, parent, child := groupTree(t)
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("group close timed out")
	}
	assertDead(t, parent)
	assertDead(t, child)
	if err := g.Close(); err != nil {
		t.Fatal("close not idempotent", err)
	}
}

func TestWorkerCrashReclaimsOwnedDescendants(t *testing.T) {
	g, cmd, parent, child := groupTree(t)
	p, e := os.FindProcess(parent)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Kill(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker crash did not complete")
	}
	if e = g.Close(); e != nil {
		t.Fatal(e)
	}
	assertDead(t, child)
}
