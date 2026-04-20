//go:build linux

package verify

import (
	"fmt"
	"os"
	"syscall"
)

// Inode-based verifier for Linux: checks that the peer's /proc/<pid>/exe
// points to the same inode as /proc/self/exe.
type InodeVerifier struct {
	selfInode uint64
}

// NewVerifier returns the platform-appropriate Verifier.
func NewVerifier() (Verifier, error) { return NewInodeVerifier() }

// NewInodeVerifier reads the inode of the current executable at startup.
func NewInodeVerifier() (*InodeVerifier, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat("/proc/self/exe", &stat); err != nil {
		return nil, fmt.Errorf("stat /proc/self/exe: %w", err)
	}
	return &InodeVerifier{selfInode: stat.Ino}, nil
}

// Verify returns true if the peer executable has the same inode as self.
func (v *InodeVerifier) Verify(pid uint32) (bool, error) {
	path := fmt.Sprintf("/proc/%d/exe", pid)
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Errorf("process %d not found", pid)
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	return stat.Ino == v.selfInode, nil
}
