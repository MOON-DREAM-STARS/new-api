//go:build unix

package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// TestEnsureWorkspaceDirsRealOwnership proves with real system calls that the
// workspace tree is handed to the runtime image identity. It needs permission to
// chown another uid, so it is skipped for unprivileged test runs.
func TestEnsureWorkspaceDirsRealOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skipf("chown to uid %d requires root (running as uid %d)", runtime.RuntimeUID, os.Geteuid())
	}

	workspaceDir := WorkspaceDir(t.TempDir(), 8)
	require.NoError(t, EnsureWorkspaceDirs(workspaceDir, nil))

	for _, path := range workspaceTreePaths(workspaceDir) {
		uid, gid, known := ownerOf(path)
		require.True(t, known, path)
		assert.Equal(t, runtime.RuntimeUID, uid, path)
		assert.Equal(t, runtime.RuntimeGID, gid, path)
	}
	require.NoError(t, os.WriteFile(filepath.Join(workspaceDir, "profile", "probe"), []byte("ok"), 0o600))
}
