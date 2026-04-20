//go:build linux

package run

import "golang.org/x/sys/unix"

// setNoDump calls prctl(PR_SET_DUMPABLE, 0) to prevent /proc/<pid>/environ
// from being read by other processes during the brief resolution window.
func setNoDump() {
	unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) //nolint:errcheck
}
