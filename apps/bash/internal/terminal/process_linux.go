//go:build linux

package terminal

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/wyw-ai/loom-oi/apps/bash/internal/procgroup"
	"golang.org/x/sys/unix"
)

func launch(c Config, script, cwd string, tty bool) (*process, error) {
	cmd := c.command(script, tty)
	cmd.Dir = cwd
	if !tty {
		return launchPipe(cmd)
	}
	master, slave, e := pty.Open()
	if e != nil {
		return nil, e
	}
	if e = pty.Setsize(master, &pty.Winsize{Cols: uint16(c.Terminal.Columns), Rows: uint16(c.Terminal.Rows)}); e != nil {
		master.Close()
		slave.Close()
		return nil, e
	}
	// Go's poller provides bounded/cancellable writes on the PTY master.
	if e = unix.SetNonblock(int(master.Fd()), true); e != nil {
		master.Close()
		slave.Close()
		return nil, e
	}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	group, e := procgroup.New()
	if e != nil {
		master.Close()
		slave.Close()
		return nil, e
	}
	if e = group.Start(cmd); e != nil {
		group.Close()
		master.Close()
		slave.Close()
		return nil, e
	}
	slave.Close()
	p := &process{input: master, output: master, terminate: group.Close}
	p.wait = func() (int, int, error) {
		err := cmd.Wait()
		code, sig := exitStatus(cmd, err)
		group.Close()
		return code, sig, err
	}
	return p, nil
}
func exitStatus(cmd *exec.Cmd, err error) (int, int) {
	if cmd.ProcessState == nil {
		return -1, 0
	}
	if st, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && st.Signaled() {
		return 128 + int(st.Signal()), int(st.Signal())
	}
	return cmd.ProcessState.ExitCode(), 0
}
func writeTerminal(ctx context.Context, f *os.File, b []byte) (int, error) {
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		deadline = time.Now().Add(time.Second)
	}
	if e := f.SetWriteDeadline(deadline); e != nil {
		return 0, e
	}
	done := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			_ = f.SetWriteDeadline(time.Now())
		case <-done:
		}
	}()
	n, e := f.Write(b)
	close(done)
	<-watchDone
	_ = f.SetWriteDeadline(time.Time{})
	return n, e
}
