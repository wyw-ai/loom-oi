//go:build linux

package procgroup

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Each group has its own subreaper supervisor. Unlike a process-group-only
// kill, this also reclaims ordinary PTY children that create a new session.
// The supervisor is a re-exec of the importing binary and runs before main.
const supervisorEnv = "LOOM_OI_PROCESS_SUPERVISOR_V1"

type launchSpec struct {
	Path       string
	Args       []string
	ExtraFiles int
}
type Group struct {
	mu      sync.Mutex
	process *os.Process
	closed  bool
}

func New() (*Group, error) { return &Group{}, nil }

func (g *Group) Start(cmd *exec.Cmd) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.process != nil {
		return errors.New("process group is not startable")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ackRead, ackWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	defer ackRead.Close()
	defer ackWrite.Close()
	spec, _ := json.Marshal(launchSpec{Path: cmd.Path, Args: cmd.Args, ExtraFiles: len(cmd.ExtraFiles)})
	cmd.ExtraFiles = append(cmd.ExtraFiles, ackWrite)
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = withoutSupervisor(env)
	cmd.Env = append(env, supervisorEnv+"="+base64.RawURLEncoding.EncodeToString(spec))
	cmd.Path = exe
	cmd.Args = []string{exe, "--loom-process-supervisor"}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !cmd.SysProcAttr.Setsid {
		cmd.SysProcAttr.Setpgid = true
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
	if err = cmd.Start(); err != nil {
		return err
	}
	g.process = cmd.Process
	_ = ackWrite.Close()
	_ = ackRead.SetReadDeadline(time.Now().Add(10 * time.Second))
	var status [1]byte
	if _, err = io.ReadFull(ackRead, status[:]); err != nil || status[0] != 0 {
		message, _ := io.ReadAll(ackRead)
		_ = g.process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
		if len(message) > 0 {
			return fmt.Errorf("start target: %s", message)
		}
		return fmt.Errorf("target startup handshake failed: %w", err)
	}
	return nil
}

func (g *Group) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	if g.process == nil {
		return nil
	}
	err := g.process.Signal(syscall.SIGTERM)
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func withoutSupervisor(env []string) []string {
	out := make([]string, 0, len(env))
	for _, v := range env {
		if !strings.HasPrefix(v, supervisorEnv+"=") {
			out = append(out, v)
		}
	}
	return out
}

func init() {
	raw := os.Getenv(supervisorEnv)
	if raw == "" {
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "--loom-process-supervisor" {
		return
	}
	encoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		os.Exit(125)
	}
	var spec launchSpec
	if json.Unmarshal(encoded, &spec) != nil || spec.Path == "" {
		os.Exit(125)
	}
	if unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) != nil {
		os.Exit(125)
	}
	// The parent-death signal may arrive before signal.Notify; check the parent
	// again after installing the handler to cover this startup window.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM)
	// A terminal sends Ctrl+C/Ctrl+\ to the foreground process group, which
	// includes this supervisor. Catch (rather than SIG_IGN) these signals so
	// exec still gives the target its normal signal dispositions. The target
	// decides whether to stop; a terminal interrupt is not a management kill.
	terminalSignals := make(chan os.Signal, 1)
	signal.Notify(terminalSignals, syscall.SIGINT, syscall.SIGQUIT)
	if os.Getppid() == 1 {
		os.Exit(125)
	}
	child := exec.Command(spec.Path, spec.Args[1:]...)
	child.Args = spec.Args
	child.Env = withoutSupervisor(os.Environ())
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	ack := os.NewFile(uintptr(3+spec.ExtraFiles), "startup-status")
	syscall.CloseOnExec(int(ack.Fd()))
	for i := 0; i < spec.ExtraFiles; i++ {
		child.ExtraFiles = append(child.ExtraFiles, os.NewFile(uintptr(3+i), "inherited-file"))
	}
	if err := child.Start(); err != nil {
		_, _ = ack.Write(append([]byte{1}, []byte(err.Error())...))
		_ = ack.Close()
		os.Exit(126)
	}
	_, _ = ack.Write([]byte{0})
	_ = ack.Close()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-signals:
		terminateDescendants(syscall.SIGTERM)
		select {
		case waitErr = <-done:
		case <-time.After(2 * time.Second):
			terminateDescendants(syscall.SIGKILL)
			waitErr = <-done
		}
	}
	// Once the direct child exits, orphaned descendants are adopted by this
	// private subreaper. Repeated enumeration closes fork/exit races.
	deadline := time.Now().Add(2 * time.Second)
	for {
		n := terminateDescendants(syscall.SIGKILL)
		reapChildren()
		if n == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	code := 0
	var exitSignal syscall.Signal
	if waitErr != nil {
		code = 1
		if e, ok := waitErr.(*exec.ExitError); ok {
			code = e.ExitCode()
			if st, ok := e.Sys().(syscall.WaitStatus); ok && st.Signaled() {
				code = 128 + int(st.Signal())
				exitSignal = st.Signal()
			}
		}
	}
	if exitSignal != 0 {
		// Preserve signal termination for the owner's WaitStatus after all
		// descendants have been reaped, rather than masquerading as exit 143.
		signal.Reset(exitSignal)
		_ = syscall.Kill(os.Getpid(), exitSignal)
		time.Sleep(100 * time.Millisecond)
	}
	os.Exit(code)
}

func terminateDescendants(sig syscall.Signal) int {
	type entry struct{ pid, parent int }
	var all []entry
	dirs, _ := os.ReadDir("/proc")
	for _, d := range dirs {
		pid, e := strconv.Atoi(d.Name())
		if e != nil {
			continue
		}
		b, e := os.ReadFile("/proc/" + d.Name() + "/stat")
		if e != nil {
			continue
		}
		i := strings.LastIndexByte(string(b), ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(string(b)[i+1:])
		if len(f) < 2 {
			continue
		}
		pp, _ := strconv.Atoi(f[1])
		all = append(all, entry{pid, pp})
	}
	owned := map[int]bool{os.Getpid(): true}
	changed := true
	for changed {
		changed = false
		for _, p := range all {
			if owned[p.parent] && !owned[p.pid] {
				owned[p.pid] = true
				changed = true
			}
		}
	}
	n := 0
	for _, p := range all {
		if p.pid != os.Getpid() && owned[p.pid] {
			_ = syscall.Kill(p.pid, sig)
			n++
		}
	}
	return n
}
func reapChildren() {
	for {
		var st syscall.WaitStatus
		pid, _ := syscall.Wait4(-1, &st, syscall.WNOHANG, nil)
		if pid <= 0 {
			return
		}
	}
}
