//go:build !windows

package integration

import (
	"os"
	"syscall"
)

const canPause = true

func terminate(p *os.Process) error { return p.Signal(syscall.SIGTERM) }

func pause(p *os.Process) error { return p.Signal(syscall.SIGSTOP) }

func resume(p *os.Process) error { return p.Signal(syscall.SIGCONT) }
