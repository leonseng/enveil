//go:build darwin

package agent

import (
	"fmt"
	"net"
	"syscall"
	"unsafe"
)

// peerPID extracts the PID of the process on the other end of a Unix socket
// using LOCAL_PEERCRED on macOS.
func peerPID(conn *net.UnixConn) (uint32, error) {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}

	var pid uint32
	var ctrlErr error
	err = rawConn.Control(func(fd uintptr) {
		// xucred is the macOS LOCAL_PEERCRED structure.
		type xucred struct {
			Version uint32
			UID     uint32
			NGroups int16
			Groups  [16]uint32
		}
		var xc xucred
		size := uint32(unsafe.Sizeof(xc))
		_, _, errno := syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			0, // SOL_LOCAL = 0
			2, // LOCAL_PEERCRED = 2
			uintptr(unsafe.Pointer(&xc)),
			uintptr(unsafe.Pointer(&size)),
			0,
		)
		if errno != 0 {
			ctrlErr = fmt.Errorf("LOCAL_PEERCRED: %w", errno)
			return
		}
		// On newer macOS, use LOCAL_PEEREPID (6) instead for the PID.
		// As a fallback we obtain the PID separately via LOCAL_PEEREPID.
		var epid int32
		epsize := uint32(unsafe.Sizeof(epid))
		_, _, errno = syscall.Syscall6(
			syscall.SYS_GETSOCKOPT,
			fd,
			0, // SOL_LOCAL
			6, // LOCAL_PEEREPID
			uintptr(unsafe.Pointer(&epid)),
			uintptr(unsafe.Pointer(&epsize)),
			0,
		)
		if errno != 0 {
			ctrlErr = fmt.Errorf("LOCAL_PEEREPID: %w", errno)
			return
		}
		pid = uint32(epid)
	})
	if err != nil {
		return 0, err
	}
	return pid, ctrlErr
}
