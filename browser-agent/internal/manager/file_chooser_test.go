package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const managerFileChooserID = "chooser-0000000000000001"
const managerOtherChooserID = "chooser-0000000000000002"

type fileUploadSlice struct {
	items []FileUpload
	index int
}

func (source *fileUploadSlice) Next() (FileUpload, error) {
	if source.index >= len(source.items) {
		return FileUpload{}, io.EOF
	}
	item := source.items[source.index]
	source.index++
	return item, nil
}

type repeatingReader struct {
	remaining int64
}

func (reader *repeatingReader) Read(p []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > reader.remaining {
		p = p[:reader.remaining]
	}
	for index := range p {
		p[index] = 'a'
	}
	reader.remaining -= int64(len(p))
	return len(p), nil
}

func startFileChooserRuntime(t *testing.T, workspaceID int64) *harness {
	t.Helper()
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	return h
}

func writeFileChooserPending(t *testing.T, h *harness, workspaceID int64, chooserID string) string {
	t.Helper()
	guardPath := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardPath, 0o700))
	now := time.Now().Unix()
	payload, err := json.Marshal(map[string]any{
		"id":              0,
		"chooser_id":      chooserID,
		"state":           fileChooserStatePending,
		"error":           "",
		"mode":            fileChooserModeSingle,
		"backend_node_id": 11,
		"session_id":      "session-1",
		"created_at":      now,
		"expires_at":      now + 300,
		"updated_at":      now,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(guardPath, fileChooserStateName), payload, 0o644))
	return guardPath
}

func respondToFileChooserCommand(t *testing.T, guardPath string, chooserID string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			data, err := os.ReadFile(filepath.Join(guardPath, fileChooserCommandName))
			if err == nil {
				var command fileChooserCommandFile
				if json.Unmarshal(data, &command) == nil && command.ChooserID == chooserID {
					status, marshalErr := json.Marshal(map[string]any{
						"id":              command.ID,
						"chooser_id":      chooserID,
						"state":           fileChooserStateDone,
						"error":           "",
						"mode":            fileChooserModeSingle,
						"backend_node_id": 11,
						"session_id":      "session-1",
						"created_at":      time.Now().Unix(),
						"expires_at":      time.Now().Add(300 * time.Second).Unix(),
						"updated_at":      time.Now().Unix(),
					})
					if marshalErr != nil {
						done <- marshalErr
						return
					}
					if writeErr := os.WriteFile(filepath.Join(guardPath, fileChooserStateName), status, 0o644); writeErr != nil {
						done <- writeErr
						return
					}
					done <- nil
					return
				}
			}
			if time.Now().After(deadline) {
				done <- errors.New("file chooser command was not written")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return done
}

func TestFileChooserUploadDerivesStagingPathInsideWorkspace(t *testing.T) {
	workspaceID := int64(101)
	h := startFileChooserRuntime(t, workspaceID)
	guardPath := writeFileChooserPending(t, h, workspaceID, managerFileChooserID)
	done := respondToFileChooserCommand(t, guardPath, managerFileChooserID)

	result, err := h.mgr.UploadFileChooserFiles(context.Background(), workspaceID, managerFileChooserID, &fileUploadSlice{
		items: []FileUpload{{Name: "../../escape.txt", Reader: bytes.NewReader([]byte("hello"))}},
	})
	require.NoError(t, err)
	require.NoError(t, <-done)
	assert.Equal(t, fileChooserStateDone, result.State)

	workspaceDir := WorkspaceDir(h.dataRoot, workspaceID)
	stagedPath := filepath.Join(workspaceDir, "uploads", ".bridge", managerFileChooserID, "escape.txt")
	content, err := os.ReadFile(stagedPath)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(content))
	relative, err := filepath.Rel(workspaceDir, stagedPath)
	require.NoError(t, err)
	assert.False(t, filepath.IsAbs(relative))
	assert.NotContains(t, relative, "..")
	assert.NoFileExists(t, filepath.Join(filepath.Dir(workspaceDir), "escape.txt"))
}

func TestFileChooserUploadRefusesSymlinkedChooserDirectory(t *testing.T) {
	workspaceID := int64(102)
	h := startFileChooserRuntime(t, workspaceID)
	_ = writeFileChooserPending(t, h, workspaceID, managerFileChooserID)
	outside := t.TempDir()
	workspaceDir := WorkspaceDir(h.dataRoot, workspaceID)
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceDir, "uploads", ".bridge"), 0o700))
	require.NoError(t, os.Symlink(outside, filepath.Join(workspaceDir, "uploads", ".bridge", managerFileChooserID)))

	_, err := h.mgr.UploadFileChooserFiles(context.Background(), workspaceID, managerFileChooserID, &fileUploadSlice{
		items: []FileUpload{{Name: "a.txt", Reader: bytes.NewReader([]byte("a"))}},
	})
	require.ErrorIs(t, err, ErrFileInjectFailed)
	assert.NoFileExists(t, filepath.Join(outside, "a.txt"))
}

func TestFileChooserUploadRejectsOversizeFile(t *testing.T) {
	workspaceID := int64(103)
	h := startFileChooserRuntime(t, workspaceID)
	_ = writeFileChooserPending(t, h, workspaceID, managerFileChooserID)

	_, err := h.mgr.UploadFileChooserFiles(context.Background(), workspaceID, managerFileChooserID, &fileUploadSlice{
		items: []FileUpload{{Name: "large.bin", Reader: &repeatingReader{remaining: fileChooserMaxFileBytes + 1}}},
	})
	require.ErrorIs(t, err, ErrFileTooLarge)
	assert.NoDirExists(t, filepath.Join(WorkspaceDir(h.dataRoot, workspaceID), "uploads", ".bridge", managerFileChooserID))
}

func TestFileChooserUploadRejectsTooManyFiles(t *testing.T) {
	workspaceID := int64(104)
	h := startFileChooserRuntime(t, workspaceID)
	_ = writeFileChooserPending(t, h, workspaceID, managerFileChooserID)
	items := make([]FileUpload, 0, fileChooserMaxFiles+1)
	for index := 0; index <= fileChooserMaxFiles; index++ {
		items = append(items, FileUpload{Name: "file.txt", Reader: bytes.NewReader([]byte("x"))})
	}

	_, err := h.mgr.UploadFileChooserFiles(context.Background(), workspaceID, managerFileChooserID, &fileUploadSlice{items: items})
	require.ErrorIs(t, err, ErrFileLimitExceeded)
	assert.NoDirExists(t, filepath.Join(WorkspaceDir(h.dataRoot, workspaceID), "uploads", ".bridge", managerFileChooserID))
}

func TestFileChooserUploadRejectsDifferentPendingChooser(t *testing.T) {
	workspaceID := int64(105)
	h := startFileChooserRuntime(t, workspaceID)
	_ = writeFileChooserPending(t, h, workspaceID, managerFileChooserID)
	source := &fileUploadSlice{items: []FileUpload{{Name: "a.txt", Reader: bytes.NewReader([]byte("a"))}}}

	_, err := h.mgr.UploadFileChooserFiles(context.Background(), workspaceID, managerOtherChooserID, source)
	require.ErrorIs(t, err, ErrFileBridgeBusy)
	assert.Equal(t, 0, source.index)
}

func TestFileChooserCancelReportsExpiredForFinishedChooser(t *testing.T) {
	workspaceID := int64(107)
	h := startFileChooserRuntime(t, workspaceID)
	guardPath := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardPath, 0o700))
	now := time.Now().Unix()
	payload, err := json.Marshal(map[string]any{
		"id":              0,
		"chooser_id":      managerFileChooserID,
		"state":           fileChooserStateDone,
		"error":           "",
		"mode":            fileChooserModeSingle,
		"backend_node_id": 11,
		"session_id":      "session-1",
		"created_at":      now,
		"expires_at":      now + 300,
		"updated_at":      now,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(guardPath, fileChooserStateName), payload, 0o644))

	_, err = h.mgr.CancelFileChooser(context.Background(), workspaceID, managerOtherChooserID)
	require.ErrorIs(t, err, ErrFileChooserExpired)
}

func TestNavigationIsNotBlockedByFileChooserLongPoll(t *testing.T) {
	workspaceID := int64(106)
	h := startFileChooserRuntime(t, workspaceID)
	dir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitDone := make(chan error, 1)
	go func() {
		_, err := h.mgr.WaitFileChooser(ctx, workspaceID, time.Hour)
		waitDone <- err
	}()
	time.Sleep(50 * time.Millisecond)

	commands, stop := serveNavigationCommand(t, dir)
	defer stop()
	navDone := make(chan error, 1)
	go func() {
		_, err := h.mgr.Navigate(workspaceID, "state")
		navDone <- err
	}()

	select {
	case command := <-commands:
		assert.Equal(t, "state", command.Action)
	case <-time.After(time.Second):
		t.Fatal("navigation command was not written during the file-chooser long poll")
	}
	select {
	case err := <-navDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("navigation waited for the file-chooser long poll")
	}

	cancel()
	require.ErrorIs(t, <-waitDone, context.Canceled)
}
