//go:build windows

package store

import (
	"os"
	"syscall"
	"unsafe"
)

var procLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

const (
	lockfileFailImmediately = 0x1
	lockfileExclusiveLock   = 0x2
	errorLockViolation      = syscall.Errno(33)
)

// lockFile takes an exclusive, non-blocking lock on path. Windows releases
// the lock when the handle closes or the process exits.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	var ov syscall.Overlapped
	r, _, callErr := procLockFileEx.Call(f.Fd(), lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if r == 0 {
		_ = f.Close()
		if callErr == errorLockViolation {
			return nil, ErrAnotherInstance
		}
		return nil, callErr
	}
	return f, nil
}
