package manager

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// workspaceSubdirectories is the frozen per-workspace data layout. Only the
// agent derives these paths, and it derives them from an integer workspace id.
var workspaceSubdirectories = []string{"profile", "uploads", "downloads", "cache", "tmp"}

// WorkspaceDir returns the host directory of a workspace. The path is derived
// from the integer id only; client supplied paths are never accepted.
func WorkspaceDir(dataRoot string, workspaceID int64) string {
	return filepath.Join(dataRoot, fmt.Sprintf("workspace-%d", workspaceID))
}

// EnsureWorkspaceDirs creates the workspace directory tree if it is missing,
// keeps the profile directory across runtime restarts and hands the tree to the
// non-root identity the runtime image runs as (runtime.RuntimeUID /
// runtime.RuntimeGID). The runtime container cannot write /workspace otherwise.
//
// chown performs the ownership handover; nil selects the process default
// (os.Chown). The handover fails closed: a chown error is tolerated only when
// the path already belongs to the runtime identity, which is the normal case
// when the agent itself runs as that identity and is not allowed to chown.
// Every other failure is returned to the caller.
func EnsureWorkspaceDirs(workspaceDir string, chown func(path string, uid int, gid int) error) error {
	return ensureWorkspaceDirs(workspaceDir, chown, ownerOf)
}

func ensureWorkspaceDirs(workspaceDir string, chown func(path string, uid int, gid int) error, owner func(path string) (int, int, bool)) error {
	if chown == nil {
		chown = os.Chown
	}
	targets := workspaceTreePaths(workspaceDir)
	for _, path := range targets {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	for _, path := range targets {
		if err := chown(path, runtime.RuntimeUID, runtime.RuntimeGID); err != nil {
			uid, gid, known := owner(path)
			if !known || uid != runtime.RuntimeUID || gid != runtime.RuntimeGID {
				return fmt.Errorf("hand over %s to %d:%d: %w", path, runtime.RuntimeUID, runtime.RuntimeGID, err)
			}
		}
	}
	return nil
}

// workspaceTreePaths lists the workspace directory and its fixed subdirectories
// in creation order.
func workspaceTreePaths(workspaceDir string) []string {
	paths := make([]string, 0, len(workspaceSubdirectories)+1)
	paths = append(paths, workspaceDir)
	for _, name := range workspaceSubdirectories {
		paths = append(paths, filepath.Join(workspaceDir, name))
	}
	return paths
}

// ownerOf reports the owning uid and gid of a path. known is false when the
// path cannot be inspected or the platform has no POSIX owner model.
func ownerOf(path string) (int, int, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}
	return fileUIDGID(info)
}
