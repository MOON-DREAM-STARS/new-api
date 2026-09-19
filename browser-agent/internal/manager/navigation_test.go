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

func writePageNavigationReceipt(t *testing.T, dir string, page map[string]any) {
	t.Helper()
	payload := map[string]any{
		"id":             9,
		"can_go_back":    true,
		"can_go_forward": false,
		"updated_at":     2222,
	}
	for key, value := range page {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, navigationReceiptFileName), data, 0o644))
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

func TestSnapshotParsesPageHealthAndRejectsInvalidValues(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(76)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	dir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	writePageNavigationReceipt(t, dir, map[string]any{
		"page_state":    "RETRYING",
		"page_error":    "ERR_TUNNEL_CONNECTION_FAILED",
		"page_attempts": 2,
	})

	snapshot, found := h.mgr.Get(workspaceID)
	require.True(t, found)
	require.NotNil(t, snapshot.Page)
	assert.Equal(t, PageStateRetrying, snapshot.Page.State)
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", snapshot.Page.Error)
	assert.Equal(t, 2, snapshot.Page.Attempts)
	assert.Equal(t, int64(2222), snapshot.Page.UpdatedAt)

	for name, page := range map[string]map[string]any{
		"missing page fields": nil,
		"invalid state":       {"page_state": "BUSY", "page_error": "", "page_attempts": 1},
		"negative attempts":   {"page_state": "RETRYING", "page_error": "", "page_attempts": -1},
		"non-number attempts": {"page_state": "RETRYING", "page_error": "", "page_attempts": "2"},
		"non-string state":    {"page_state": 1, "page_error": "", "page_attempts": 1},
		"url in error":        {"page_state": "FAILED", "page_error": "https://provider.example/private", "page_attempts": 1},
		"ready with attempts": {"page_state": "READY", "page_error": "", "page_attempts": 1},
	} {
		t.Run(name, func(t *testing.T) {
			writePageNavigationReceipt(t, dir, page)
			snapshot, found := h.mgr.Get(workspaceID)
			require.True(t, found)
			assert.Nil(t, snapshot.Page)
			require.NotNil(t, snapshot.Navigation, "page validation must not invalidate navigation state")
		})
	}
}
