//go:build windows

package integration

import (
	"errors"
	"os"
)

// Windows has no SIGTERM or SIGSTOP for child processes: stop kills and
// the pause vector (TV-B2-11) runs only on Linux CI.
const canPause = false

func terminate(p *os.Process) error { return p.Kill() }

func pause(*os.Process) error { return errors.ErrUnsupported }

func resume(*os.Process) error { return errors.ErrUnsupported }
