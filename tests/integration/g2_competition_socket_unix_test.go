//go:build !windows

package integration

import (
	"net"
	"syscall"
)

func g2CompetitionSocketBuffer(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 1024)
}

func g2CompetitionPeekSocket(c *net.TCPConn, buf []byte) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var n int
	var recvErr error
	err = raw.Read(func(fd uintptr) bool {
		n, _, recvErr = syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK)
		return recvErr != syscall.EAGAIN && recvErr != syscall.EWOULDBLOCK && recvErr != syscall.EINTR
	})
	if err != nil {
		return max(0, n), err
	}
	return max(0, n), recvErr
}
