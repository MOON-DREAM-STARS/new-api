package manager

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

func TestEnsureWorkspaceDirsHandsTreeToRuntimeIdentity(t *testing.T) {
	workspaceDir := WorkspaceDir(t.TempDir(), 5)
	expected := workspaceTreeNames()

	type chownCall struct {
		root string
		name string
		uid  int
		gid  int
	}
	var calls []chownCall
	err := ensureWorkspaceDirs(workspaceDir, func(root *os.Root, name string, uid int, gid int) error {
		calls = append(calls, chownCall{root: root.Name(), name: name, uid: uid, gid: gid})
		return nil
	}, ownerOfRoot)
	require.NoError(t, err)

	require.Len(t, calls, len(expected))
	for index, call := range calls {
		assert.Equal(t, expected[index], call.name)
		assert.Equal(t, workspaceDir, filepath.Clean(call.root))
		assert.Equal(t, runtime.RuntimeUID, call.uid)
		assert.Equal(t, runtime.RuntimeGID, call.gid)
	}
	for _, name := range expected {
		assert.DirExists(t, filepath.Join(workspaceDir, name))
	}
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "profile", "probe"), []byte("ok"), 0o600))
}

func TestEnsureWorkspaceDirsToleratesChownFailureWhenAlreadyOwned(t *testing.T) {
	workspaceDir := WorkspaceDir(t.TempDir(), 6)

	// A non-root agent that already runs as the runtime identity cannot always
	// chown, but the desired ownership already holds, so the start must proceed.
	err := ensureWorkspaceDirs(workspaceDir, func(root *os.Root, name string, uid int, gid int) error {
		return &os.PathError{Op: "chown", Path: name, Err: syscall.EPERM}
	}, func(*os.Root, string) (int, int, bool) {
		return runtime.RuntimeUID, runtime.RuntimeGID, true
	})
	require.NoError(t, err)
	assert.DirExists(t, workspaceDir)
}

func TestEnsureWorkspaceDirsFailsClosedWhenChownFails(t *testing.T) {
	owners := map[string]func(*os.Root, string) (int, int, bool){
		"owned by another user": func(*os.Root, string) (int, int, bool) { return 0, 0, true },
		"owner cannot be read":  func(*os.Root, string) (int, int, bool) { return 0, 0, false },
	}
	for name, owner := range owners {
		t.Run(name, func(t *testing.T) {
			workspaceDir := WorkspaceDir(t.TempDir(), 7)
			err := ensureWorkspaceDirs(workspaceDir, func(root *os.Root, name string, uid int, gid int) error {
				return &os.PathError{Op: "chown", Path: name, Err: syscall.EPERM}
			}, owner)
			require.Error(t, err)
			assert.ErrorIs(t, err, syscall.EPERM)
		})
	}
}
