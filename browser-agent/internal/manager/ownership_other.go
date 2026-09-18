//go:build !unix

package manager

import "os"

// fileUIDGID reports that no POSIX owner is available on this platform. The
// ownership handover then fails closed because it cannot be verified.
func fileUIDGID(os.FileInfo) (int, int, bool) {
	return 0, 0, false
}
