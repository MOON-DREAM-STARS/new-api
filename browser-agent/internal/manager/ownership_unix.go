//go:build unix

package manager

import (
	"os"
	"syscall"
)

// fileUIDGID reads the owning uid and gid of a stat result. The agent itself is
// Linux only because DOCKER_HOST accepts unix sockets only.
func fileUIDGID(info os.FileInfo) (int, int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}
