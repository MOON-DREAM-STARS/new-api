//go:build unix

package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The runtime container runs as the identity that owns the workspace mount, so
// a runtime process can replace entries below /workspace with symbolic links.
// These tests pin the fail-closed behaviour of the agent: workspace and guard
// state names are resolved below an open workspace root, so a link that leaves
// the workspace is refused instead of followed. (checklist §31 symlink/path
// traversal)

func TestEnsureWorkspaceDirsRejectsSymlinkedWorkspaceEntries(t *testing.T) {
	targets := map[string]func(dataRoot string) string{
		"absolute link": func(dataRoot string) string { return filepath.Join(dataRoot, "outside") },
		"relative link": func(dataRoot string) string { return filepath.Join("..", "outside") },
		"in workspace link": func(dataRoot string) string {
			return "cache"
		},
	}
	for _, name := range workspaceSubdirectories {
		for targetName, target := range targets {
			t.Run(name+" with "+targetName, func(t *testing.T) {
				dataRoot := t.TempDir()
				workspaceDir := WorkspaceDir(dataRoot, 5)
				require.NoError(t, os.MkdirAll(workspaceDir, 0o700))
				outside := filepath.Join(dataRoot, "outside")
				require.NoError(t, os.MkdirAll(outside, 0o700))
				link := filepath.Join(workspaceDir, name)
				require.NoError(t, os.Symlink(target(dataRoot), link))

				handed := make([]string, 0)
				err := EnsureWorkspaceDirs(workspaceDir, func(root *os.Root, name string, uid int, gid int) error {
					handed = append(handed, name)
					return nil
				})
				require.Error(t, err)
				assert.Empty(t, handed, "no directory may be handed over once a link is found")

				info, err := os.Lstat(link)
				require.NoError(t, err)
				assert.NotZero(t, info.Mode()&os.ModeSymlink, "the planted link must not be replaced")
				entries, err := os.ReadDir(outside)
				require.NoError(t, err)
				assert.Empty(t, entries, "the link target must stay untouched")
			})
		}
	}
}

func TestGuardStateRejectsSymlinkedGuardDirectory(t *testing.T) {
	targets := map[string]func(dataRoot string) string{
		"absolute link": func(dataRoot string) string { return filepath.Join(dataRoot, "outside") },
		"cross workspace link": func(dataRoot string) string {
			return filepath.Join("..", "workspace-6", guardStateDirName)
		},
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			workspaceID := int64(5)
			workspaceDir := WorkspaceDir(h.dataRoot, workspaceID)
			require.NoError(t, os.MkdirAll(workspaceDir, 0o700))
			outside := filepath.Join(h.dataRoot, "outside")
			require.NoError(t, os.MkdirAll(outside, 0o700))
			sibling := filepath.Join(h.dataRoot, "workspace-6", guardStateDirName)
			require.NoError(t, os.MkdirAll(sibling, 0o700))
			require.NoError(t, os.Symlink(target(h.dataRoot), guardStateDir(workspaceDir)))

			_, err := h.mgr.PutOwnership(workspaceID, Ownership{Generation: 3})
			require.Error(t, err)
			assert.NoFileExists(t, filepath.Join(outside, ownershipFileName))
			assert.NoFileExists(t, filepath.Join(sibling, ownershipFileName))

			_, err = h.mgr.Observations(workspaceID)
			require.Error(t, err)

			_, err = h.mgr.AckObservations(workspaceID, 0)
			require.Error(t, err)
			assert.NoFileExists(t, filepath.Join(outside, observationsOffsetName))
			assert.NoFileExists(t, filepath.Join(sibling, observationsOffsetName))
		})
	}
}

func TestObservationsRejectsSymlinkedObservationFile(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(7)
	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardDir, 0o700))
	secret := filepath.Join(h.dataRoot, "secret.jsonl")
	secretLine := `{"event":"secret"}` + "\n"
	require.NoError(t, os.WriteFile(secret, []byte(secretLine), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(guardDir, observationsFileName)))

	page, err := h.mgr.Observations(workspaceID)
	require.Error(t, err)
	assert.Empty(t, page.Observations)

	_, err = h.mgr.AckObservations(workspaceID, 0)
	require.Error(t, err)

	data, err := os.ReadFile(secret)
	require.NoError(t, err)
	assert.Equal(t, secretLine, string(data))
}

func TestPutOwnershipReplacesSymlinkedStateFile(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(8)
	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardDir, 0o700))
	victim := filepath.Join(h.dataRoot, "victim.json")
	require.NoError(t, os.WriteFile(victim, []byte("keep"), 0o600))
	require.NoError(t, os.Symlink(victim, filepath.Join(guardDir, ownershipFileName)))

	counts, err := h.mgr.PutOwnership(workspaceID, Ownership{Generation: 4})
	require.NoError(t, err)
	assert.Equal(t, int64(4), counts.Generation)

	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(data), "the state write must not follow the planted link")

	stored, err := os.ReadFile(filepath.Join(guardDir, ownershipFileName))
	require.NoError(t, err)
	var ownership guardOwnership
	require.NoError(t, json.Unmarshal(stored, &ownership))
	assert.Equal(t, int64(4), ownership.Generation)

	info, err := os.Lstat(filepath.Join(guardDir, ownershipFileName))
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSymlink, "the planted link must be replaced by a regular file")
}
