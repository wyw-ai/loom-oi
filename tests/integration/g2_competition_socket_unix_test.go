//go:build !windows

package integration

import "syscall"

func g2CompetitionSocketBuffer(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 1024)
}
