//go:build linux

package agent

import "golang.org/x/sys/unix"

// setNoDump calls prctl(PR_SET_DUMPABLE, 0) so that the agent process cannot
// be memory-dumped and its /proc/<pid>/environ is not readable by co-resident
// processes, even those running as the same user.
func setNoDump() {
	unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) //nolint:errcheck
}
