//go:build darwin

package verify

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// PathVerifier for macOS: resolves the peer's executable path via proc_pidpath
// and compares it against os.Executable().
type PathVerifier struct {
	selfPath string
}

// NewPathVerifier records the current executable path at startup.
func NewPathVerifier() (*PathVerifier, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("os.Executable: %w", err)
	}
	return &PathVerifier{selfPath: exe}, nil
}

// Verify returns true if the peer executable path matches self.
func (v *PathVerifier) Verify(pid uint32) (bool, error) {
	path, err := procPidPath(pid)
	if err != nil {
		return false, err
	}
	return path == v.selfPath, nil
}

// proc_pidpath wraps the macOS proc_pidpath(3) syscall.
func procPidPath(pid uint32) (string, error) {
	const PROC_PIDPATHINFO_MAXSIZE = 4096
	buf := make([]byte, PROC_PIDPATHINFO_MAXSIZE)
	// SYS_PROC_INFO = 336 on arm64/amd64 macOS
	ret, _, errno := syscall.Syscall6(
		336,
		uintptr(pid),
		uintptr(11), // PROC_PIDPATHINFO
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		0,
	)
	if errno != 0 {
		return "", fmt.Errorf("proc_pidpath(%d): %w", pid, errno)
	}
	if ret == 0 {
		return "", fmt.Errorf("proc_pidpath(%d): empty result", pid)
	}
	return string(buf[:ret]), nil
}
