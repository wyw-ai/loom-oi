//go:build linux || darwin || freebsd || netbsd || openbsd

package store

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive, non-blocking lock on path. The lock lives
// as long as the returned file is open; the OS releases it if the process
// dies, so a crash never leaves a stale lock.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAnotherInstance
		}
		return nil, err
	}
	return f, nil
}
