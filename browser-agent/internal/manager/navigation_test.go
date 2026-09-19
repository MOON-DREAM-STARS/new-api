package manager

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

func writeNavigationReceipt(t *testing.T, dir string, id int64, back bool, forward bool) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"id":             id,
		"can_go_back":    back,
		"can_go_forward": forward,
		"updated_at":     1234 + id,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, navigationReceiptFileName), payload, 0o644))
}

func serveNavigationCommand(t *testing.T, dir string) (<-chan navigationCommandFile, func()) {
	t.Helper()
	commands := make(chan navigationCommandFile, 1)
	stop := make(chan struct{})
	go func() {
		path := filepath.Join(dir, navigationCommandFileName)
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err == nil {
				var command navigationCommandFile
				if json.Unmarshal(data, &command) == nil && command.ID > 0 {
					select {
					case commands <- command:
					case <-stop:
						return
					}
					payload, _ := json.Marshal(map[string]any{
						"id":             command.ID,
						"can_go_back":    true,
						"can_go_forward": false,
						"updated_at":     1234 + command.ID,
					})
					_ = os.WriteFile(filepath.Join(dir, navigationReceiptFileName), payload, 0o644)
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return commands, func() { close(stop) }
}

func TestNavigateWritesCommandWaitsForReceiptAndAdvancesID(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(71)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	dir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeNavigationReceipt(t, dir, 7, false, true)

	commands, stop := serveNavigationCommand(t, dir)
	defer stop()
	status, err := h.mgr.Navigate(workspaceID, "back")
	require.NoError(t, err)
	assert.True(t, status.CanGoBack)
	assert.False(t, status.CanGoForward)

	select {
	case command := <-commands:
		assert.Equal(t, int64(8), command.ID)
		assert.Equal(t, "back", command.Action)
		assert.NotZero(t, command.RequestedAt)
	case <-time.After(time.Second):
		t.Fatal("guard did not observe navigation command")
	}
}

func TestNavigateReturnsTimeoutWhenGuardDoesNotReply(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(72)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	_, err = h.mgr.Navigate(workspaceID, "reload")
	require.ErrorIs(t, err, ErrNavigationTimeout)
}

func TestNavigateRequiresLiveRuntimeAndValidRequest(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(73)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	_, err = h.mgr.Stop(context.Background(), workspaceID)
	require.NoError(t, err)

	_, err = h.mgr.Navigate(workspaceID, "back")
	require.ErrorIs(t, err, runtime.ErrNotRunning)

	_, err = h.mgr.Navigate(0, "back")
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = h.mgr.Navigate(workspaceID, "bogus")
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = h.mgr.Navigate(999, "state")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNavigateReturnsUnavailableWhenCommandCannotBeWritten(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(74)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	dir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.Mkdir(filepath.Join(dir, navigationCommandFileName), 0o700))
	_, err = h.mgr.Navigate(workspaceID, "forward")
	require.ErrorIs(t, err, ErrNavigationUnavailable)
}

func TestCountStreamAccumulatesSnapshotBytes(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 9, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	h.mgr.CountStream(9, 2048, 64)
	h.mgr.CountStream(9, 1024, 0)
	h.mgr.CountStream(0, 10, 10)
	h.mgr.CountStream(404, 10, 10)

	snapshot, found := h.mgr.Get(9)
	require.True(t, found)
	assert.EqualValues(t, 3072, snapshot.StreamBytesOut)
	assert.EqualValues(t, 64, snapshot.StreamBytesIn)

	// A restart starts a new runtime, so its counters start from zero again.
	_, err = h.mgr.Restart(context.Background(), 9, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	snapshot, found = h.mgr.Get(9)
	require.True(t, found)
	assert.Zero(t, snapshot.StreamBytesOut)
	assert.Zero(t, snapshot.StreamBytesIn)
}
func TestSnapshotRefreshesNavigationReceipt(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(75)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	dir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	writeNavigationReceipt(t, dir, 3, true, false)

	snapshot, found := h.mgr.Get(workspaceID)
	require.True(t, found)
	require.NotNil(t, snapshot.Navigation)
	assert.True(t, snapshot.Navigation.CanGoBack)
	assert.False(t, snapshot.Navigation.CanGoForward)
	assert.Equal(t, int64(1237), snapshot.Navigation.UpdatedAt)

	activity, found := h.mgr.Activity(workspaceID)
	require.True(t, found)
	require.NotNil(t, activity.Navigation)
	assert.Equal(t, *snapshot.Navigation, *activity.Navigation)

	require.NoError(t, os.WriteFile(filepath.Join(dir, navigationReceiptFileName), []byte("corrupt"), 0o644))
	corrupt, found := h.mgr.Get(workspaceID)
	require.True(t, found)
	assert.Nil(t, corrupt.Navigation)
}
