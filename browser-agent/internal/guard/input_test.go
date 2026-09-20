package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
)

func awaitInputState(t *testing.T, dir string, state string) inputState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(dir, inputStateFileName))
		if err == nil {
			var file inputStateFile
			if json.Unmarshal(data, &file) == nil && file.valid() && *file.State == state {
				return file.value()
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("input state %s was not written", state)
	return inputState{}
}

func TestInputTextCommandInsertsCommittedTextOnce(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	writeStateFile(t, dir, inputCommandFileName, map[string]any{
		"id":           1,
		"action":       inputActionText,
		"text":         "你好 world",
		"requested_at": time.Now().Unix(),
	})

	command := f.awaitIn("session-1", "Input.insertText")
	var params struct {
		Text string `json:"text"`
	}
	decodeParams(t, command, &params)
	assert.Equal(t, "你好 world", params.Text)

	state := awaitInputState(t, dir, inputStateDone)
	assert.Equal(t, int64(1), state.ID)
	assert.Equal(t, inputActionText, state.Action)
	assert.Empty(t, state.Error)
	run.assertRunning(100 * time.Millisecond)
}

func TestInputKeyCommandDispatchesRawKeyDownAndKeyUp(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	writeStateFile(t, dir, inputCommandFileName, map[string]any{
		"id":           1,
		"action":       inputActionKey,
		"key":          "a",
		"modifiers":    []string{"ctrl"},
		"requested_at": time.Now().Unix(),
	})

	down := f.awaitIn("session-1", "Input.dispatchKeyEvent")
	up := f.awaitIn("session-1", "Input.dispatchKeyEvent")
	var downParams struct {
		Type      string `json:"type"`
		Key       string `json:"key"`
		Modifiers int    `json:"modifiers"`
	}
	var upParams struct {
		Type      string `json:"type"`
		Modifiers int    `json:"modifiers"`
	}
	decodeParams(t, down, &downParams)
	decodeParams(t, up, &upParams)
	assert.Equal(t, "rawKeyDown", downParams.Type)
	assert.Equal(t, "a", downParams.Key)
	assert.Equal(t, 2, downParams.Modifiers)
	assert.Equal(t, "keyUp", upParams.Type)
	assert.Equal(t, 2, upParams.Modifiers)

	state := awaitInputState(t, dir, inputStateDone)
	assert.Equal(t, inputActionKey, state.Action)
	run.assertRunning(100 * time.Millisecond)
}

func TestInputCaretCommandReturnsOptionalRect(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	f.queueResult("Runtime.evaluate", map[string]any{
		"result": map[string]any{
			"type":  "object",
			"value": map[string]any{"x": 120.5, "y": 240.25, "width": 2, "height": 18},
		},
	})
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	writeStateFile(t, dir, inputCommandFileName, map[string]any{
		"id":           1,
		"action":       inputActionCaret,
		"requested_at": time.Now().Unix(),
	})
	f.awaitIn("session-1", "Runtime.evaluate")

	state := awaitInputState(t, dir, inputStateDone)
	require.NotNil(t, state.Caret)
	assert.InDelta(t, 120.5, state.Caret.X, 0.001)
	assert.InDelta(t, 240.25, state.Caret.Y, 0.001)
	assert.InDelta(t, 2, state.Caret.Width, 0.001)
	assert.InDelta(t, 18, state.Caret.Height, 0.001)
	run.assertRunning(100 * time.Millisecond)
}

func TestInputCommandDeduplicatesByID(t *testing.T) {
	f := newFakeCDP(t)
	dir := t.TempDir()
	run := startGuardWithState(t, f, policy.ModeLocked, &logBuffer{}, dir)
	attachPage(t, f, "session-1", "target-1")

	writeStateFile(t, dir, inputCommandFileName, map[string]any{
		"id":           7,
		"action":       inputActionText,
		"text":         "one",
		"requested_at": time.Now().Unix(),
	})
	f.awaitIn("session-1", "Input.insertText")
	awaitInputState(t, dir, inputStateDone)

	writeStateFile(t, dir, inputCommandFileName, map[string]any{
		"id":           7,
		"action":       inputActionText,
		"text":         "two",
		"requested_at": time.Now().Unix(),
	})
	time.Sleep(inputPollInterval + 100*time.Millisecond)
	assert.Equal(t, 1, countInputInsertText(f))
	run.assertRunning(100 * time.Millisecond)
}

func countInputInsertText(f *fakeCDP) int {
	count := 0
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, message := range f.buffered {
		if message.SessionID == "session-1" && message.Method == "Input.insertText" {
			count++
		}
	}
	return count
}
