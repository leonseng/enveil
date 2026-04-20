//go:build linux

package agent

import (
	"fmt"
	"net"
	"syscall"
)

// peerPID extracts the PID of the process on the other end of a Unix socket.
func peerPID(conn *net.UnixConn) (uint32, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var cred *syscall.Ucred
	var credErr error
	err = rawConn.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return 0, fmt.Errorf("rawConn.Control: %w", err)
	}
	if credErr != nil {
		return 0, fmt.Errorf("SO_PEERCRED: %w", credErr)
	}
	return uint32(cred.Pid), nil
}
