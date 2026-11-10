//go:build windows

// Package procgroup starts and reclaims an owned process tree. It is independent
// of application protocols and shell command semantics.
package procgroup

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Group struct {
	mu     sync.Mutex
	job    windows.Handle
	closed bool
}

func New() (*Group, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &Group{job: h}, nil
}

// AddProcess attaches a suspended process before any user code can run. It is
// also used by the ConPTY adapter, which creates its own STARTUPINFOEX.
func (g *Group) AddProcess(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return errors.New("process group is closed")
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(g.job, h)
}

func (g *Group) Start(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr.HideWindow = true
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := g.AddProcess(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return err
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err == nil {
		defer windows.CloseHandle(snapshot)
		entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
		err = windows.Thread32First(snapshot, &entry)
		for err == nil {
			if entry.OwnerProcessID == uint32(cmd.Process.Pid) {
				var thread windows.Handle
				thread, err = windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
				if err == nil {
					_, err = windows.ResumeThread(thread)
					windows.CloseHandle(thread)
				}
				if err == nil {
					return nil
				}
				break
			}
			err = windows.Thread32Next(snapshot, &entry)
		}
	}
	cmd.Process.Kill()
	cmd.Wait()
	if err == nil {
		err = errors.New("suspended process thread not found")
	}
	return err
}

func (g *Group) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	err := windows.TerminateJobObject(g.job, 1)
	closeErr := windows.CloseHandle(g.job)
	if err != nil {
		return err
	}
	return closeErr
}
