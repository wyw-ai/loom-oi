package integration

import (
	"net"
	"syscall"
)

func g2CompetitionSocketBuffer(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 1024)
}

func g2CompetitionPeekSocket(c *net.TCPConn, buf []byte) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var n uint32
	var recvErr error
	ready := false
	err = raw.Read(func(fd uintptr) bool {
		if !ready {
			// RawRead performs a zero-byte overlapped read with the original
			// read deadline. It does not consume data. No other reader exists.
			ready = true
			return false
		}
		// MSG_PEEK is valid only for a nonoverlapped receive. Readiness above
		// ensures this synchronous call has data or EOF, without changing the
		// socket mode used by the concurrent request writer.
		flags := uint32(2) // Winsock MSG_PEEK.
		b := syscall.WSABuf{Len: uint32(len(buf)), Buf: &buf[0]}
		recvErr = syscall.WSARecv(syscall.Handle(fd), &b, 1, &n, &flags, nil, nil)
		return true
	})
	if err != nil {
		return int(n), err
	}
	return int(n), recvErr
}
