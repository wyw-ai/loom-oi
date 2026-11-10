//go:build windows

package terminal

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/wyw-ai/loom-oi/internal/procgroup"
	"golang.org/x/sys/windows"
)

func launch(c Config, script, cwd string, tty bool) (*process, error) {
	cmd := c.command(script, tty)
	cmd.Dir = cwd
	if !tty {
		return launchPipe(cmd)
	}
	return launchConPTY(cmd, c.Terminal)
}
func exitStatus(cmd *exec.Cmd, err error) (int, int) {
	if cmd.ProcessState == nil {
		return -1, 0
	}
	return cmd.ProcessState.ExitCode(), 0
}

// ConPTY is a real Windows pseudoconsole, not redirected shell pipes. The
// client is created suspended and assigned to its Job before being resumed.
// https://learn.microsoft.com/windows/console/creating-a-pseudoconsole-session
func launchConPTY(cmd *exec.Cmd, size Dimensions) (result *process, err error) {
	inRead, inWrite, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	outRead, outWrite, e := os.Pipe()
	if e != nil {
		inRead.Close()
		inWrite.Close()
		return nil, e
	}
	var console windows.Handle
	defer func() {
		inRead.Close()
		outWrite.Close()
		if err != nil {
			inWrite.Close()
			outRead.Close()
			if console != 0 {
				windows.ClosePseudoConsole(console)
			}
		}
	}()
	if err = windows.CreatePseudoConsole(windows.Coord{X: int16(size.Columns), Y: int16(size.Rows)}, windows.Handle(inRead.Fd()), windows.Handle(outWrite.Fd()), 0, &console); err != nil {
		return nil, err
	}
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	// This attribute takes the opaque HPCON value, not a pointer to Go memory.
	// Passing it through AttributeList.Update would retain an invalid Go
	// pointer; call the Win32 function with the handle as uintptr instead.
	update := windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")
	if ok, _, callErr := update.Call(uintptr(unsafe.Pointer(attrs.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(console), unsafe.Sizeof(console), 0, 0); ok == 0 {
		return nil, callErr
	}
	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	// Explicit zero standard handles let ConPTY supply its console handles;
	// otherwise Windows can carry the hosting process's redirected handles.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()
	path, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return nil, err
	}
	argv := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		argv[i] = syscall.EscapeArg(a)
	}
	line, err := windows.UTF16PtrFromString(strings.Join(argv, " "))
	if err != nil {
		return nil, err
	}
	cwd, err := windows.UTF16PtrFromString(cmd.Dir)
	if err != nil {
		return nil, err
	}
	env := append([]string(nil), cmd.Env...)
	sort.Slice(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	var envBlock []uint16
	for _, s := range env {
		u, e := windows.UTF16FromString(s)
		if e != nil {
			return nil, e
		}
		envBlock = append(envBlock, u...)
	}
	envBlock = append(envBlock, 0)
	if len(envBlock) == 1 {
		envBlock = append(envBlock, 0)
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err = windows.CreateProcess(path, line, nil, nil, false, flags, &envBlock[0], cwd, &si.StartupInfo, &pi); err != nil {
		return nil, err
	}
	defer windows.CloseHandle(pi.Thread)
	group, err := procgroup.New()
	if err != nil {
		windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		return nil, err
	}
	if err = group.AddProcess(int(pi.ProcessId)); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		group.Close()
		return nil, err
	}
	if _, err = windows.ResumeThread(pi.Thread); err != nil {
		group.Close()
		windows.CloseHandle(pi.Process)
		return nil, err
	}
	var closeOnce sync.Once
	closeConsole := func() { closeOnce.Do(func() { inWrite.Close(); go windows.ClosePseudoConsole(console) }) }
	p := &process{input: inWrite, output: outRead, terminate: group.Close, closeExtra: closeConsole}
	p.wait = func() (int, int, error) {
		_, e := windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		var code uint32
		if e == nil {
			e = windows.GetExitCodeProcess(pi.Process, &code)
		}
		group.Close()
		windows.CloseHandle(pi.Process)
		closeConsole()
		if e != nil {
			return -1, 0, e
		}
		return int(code), 0, e
	}
	runtime.KeepAlive(attrs)
	runtime.KeepAlive(envBlock)
	return p, nil
}

var cancelSynchronousIO = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

// Anonymous ConPTY pipes are synchronous. Pin the writer to a native thread
// and cancel that thread's pending IO, then join it before allowing another
// write. A timeout must never leave an invisible write running in the back.
func writeTerminal(ctx context.Context, f *os.File, b []byte) (int, error) {
	type answer struct {
		n int
		e error
	}
	done := make(chan answer, 1)
	ready := make(chan windows.Handle, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		h, e := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
		if e != nil {
			ready <- 0
			done <- answer{0, e}
			return
		}
		ready <- h
		n, e := f.Write(b)
		done <- answer{n, e}
	}()
	h := <-ready
	if h != 0 {
		defer windows.CloseHandle(h)
	}
	select {
	case a := <-done:
		return a.n, a.e
	case <-ctx.Done():
		var a answer
		// The writer may not have entered WriteFile at the instant the first
		// cancellation is attempted. Retry until it has actually joined.
		for {
			if h != 0 {
				_, _, _ = cancelSynchronousIO.Call(uintptr(h))
			}
			select {
			case a = <-done:
				goto joined
			case <-time.After(5 * time.Millisecond):
			}
		}
	joined:
		if a.e == nil && a.n == len(b) {
			return a.n, nil
		}
		if a.e == nil {
			a.e = ctx.Err()
		}
		return a.n, a.e
	}
}
