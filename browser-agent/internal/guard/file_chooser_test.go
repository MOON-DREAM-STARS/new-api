package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const testFileChooserID = "chooser-0000000000000001"

func awaitFileChooserState(t *testing.T, dir string, state string) fileChooserState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(dir, fileChooserStateFileName))
		if err == nil {
			var file fileChooserStateFile
			if json.Unmarshal(data, &file) == nil && file.valid() {
				current := file.value()
				if current.State == state {
					return current
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for file chooser state %s: %v", state, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFileChooserEventCreatesPendingState(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")
	f.awaitIn("session-1", "Page.setInterceptFileChooserDialog")

	f.send("Page.fileChooserOpened", "session-1", map[string]any{
		"mode":          "selectMultiple",
		"backendNodeId": 42,
	})

	state := awaitFileChooserState(t, dir, fileChooserStatePending)
	assert.NotEmpty(t, state.ChooserID)
	assert.Equal(t, "selectMultiple", state.Mode)
	assert.Equal(t, int64(42), state.BackendNodeID)
	assert.Equal(t, "session-1", state.SessionID)
	assert.Greater(t, state.ExpiresAt, state.CreatedAt)
	run.assertRunning(100 * time.Millisecond)
}

func TestValidFileChooserCommandFilePathRules(t *testing.T) {
	prefix := runtime.WorkspaceMountTarget + "/uploads/.bridge/" + testFileChooserID + "/"
	tests := []struct {
		name  string
		files []string
		want  bool
	}{
		{name: "plain name", files: []string{prefix + "report.txt"}, want: true},
		{name: "double dot inside name", files: []string{prefix + "report..v2.txt"}, want: true},
		{name: "traversal", files: []string{prefix + "../report.txt"}, want: false},
		{name: "nested path", files: []string{prefix + "nested/report.txt"}, want: false},
		{name: "empty name", files: []string{prefix}, want: false},
		{name: "other chooser directory", files: []string{runtime.WorkspaceMountTarget + "/uploads/.bridge/chooser-0000000000000002/report.txt"}, want: false},
		{name: "outside the workspace", files: []string{"/workspace/profile/Default/Cookies"}, want: false},
		{name: "no files", files: nil, want: false},
		{name: "too many files", files: []string{prefix + "a", prefix + "b", prefix + "c", prefix + "d", prefix + "e", prefix + "f"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := fileChooserCommand{
				ID:        1,
				ChooserID: testFileChooserID,
				Action:    fileChooserActionAttach,
				Files:     test.files,
			}
			assert.Equal(t, test.want, validFileChooserCommand(command))
		})
	}
}

func TestFileChooserAttachCommandSetsFilesAndWritesDone(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	writeStateFile(t, dir, fileChooserStateFileName, map[string]any{
		"id":              0,
		"chooser_id":      testFileChooserID,
		"state":           fileChooserStatePending,
		"error":           "",
		"mode":            fileChooserModeSingle,
		"backend_node_id": 77,
		"session_id":      "session-1",
		"created_at":      time.Now().Unix(),
		"expires_at":      time.Now().Add(time.Minute).Unix(),
		"updated_at":      time.Now().Unix(),
	})
	writeStateFile(t, dir, fileChooserCommandFileName, map[string]any{
		"id":           1,
		"chooser_id":   testFileChooserID,
		"action":       fileChooserActionAttach,
		"files":        []string{"/workspace/uploads/.bridge/" + testFileChooserID + "/report.txt"},
		"requested_at": time.Now().Unix(),
	})

	cmd := f.awaitIn("session-1", "DOM.setFileInputFiles")
	var params struct {
		Files         []string `json:"files"`
		BackendNodeID int64    `json:"backendNodeId"`
	}
	decodeParams(t, cmd, &params)
	assert.Equal(t, []string{"/workspace/uploads/.bridge/" + testFileChooserID + "/report.txt"}, params.Files)
	assert.Equal(t, int64(77), params.BackendNodeID)

	state := awaitFileChooserState(t, dir, fileChooserStateDone)
	assert.Equal(t, int64(1), state.ID)
	assert.Equal(t, testFileChooserID, state.ChooserID)
	assert.Empty(t, state.Error)
	run.assertRunning(100 * time.Millisecond)
}

func TestFileChooserCancelCommandWritesCancelled(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	writeStateFile(t, dir, fileChooserStateFileName, map[string]any{
		"id":              0,
		"chooser_id":      testFileChooserID,
		"state":           fileChooserStatePending,
		"error":           "",
		"mode":            fileChooserModeSingle,
		"backend_node_id": 77,
		"session_id":      "session-1",
		"created_at":      time.Now().Unix(),
		"expires_at":      time.Now().Add(time.Minute).Unix(),
		"updated_at":      time.Now().Unix(),
	})
	writeStateFile(t, dir, fileChooserCommandFileName, map[string]any{
		"id":           1,
		"chooser_id":   testFileChooserID,
		"action":       fileChooserActionCancel,
		"requested_at": time.Now().Unix(),
	})

	state := awaitFileChooserState(t, dir, fileChooserStateCancelled)
	assert.Equal(t, int64(1), state.ID)
	assert.Equal(t, testFileChooserID, state.ChooserID)
	assert.Empty(t, state.Error)
	run.assertRunning(100 * time.Millisecond)
}

func TestFileChooserStalePendingExpires(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	now := time.Now().Unix()
	writeStateFile(t, dir, fileChooserStateFileName, map[string]any{
		"id":              0,
		"chooser_id":      testFileChooserID,
		"state":           fileChooserStatePending,
		"error":           "",
		"mode":            fileChooserModeSingle,
		"backend_node_id": 77,
		"session_id":      "session-1",
		"created_at":      now - 120,
		"expires_at":      now - 1,
		"updated_at":      now - 120,
	})
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)

	state := awaitFileChooserState(t, dir, fileChooserStateExpired)
	assert.Equal(t, fileChooserErrorExpired, state.Error)
	run.assertRunning(100 * time.Millisecond)
}
