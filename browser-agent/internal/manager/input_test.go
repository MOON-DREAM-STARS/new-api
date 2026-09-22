package manager

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startInputRuntime(t *testing.T, workspaceID int64) *harness {
	t.Helper()
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	return h
}

func writeInputResponse(t *testing.T, guardPath string, action string, caret *InputCaret) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			data, err := os.ReadFile(filepath.Join(guardPath, inputCommandName))
			if err == nil {
				var command inputCommandFile
				if json.Unmarshal(data, &command) == nil && command.Action == action {
					now := time.Now().Unix()
					payload, marshalErr := json.Marshal(inputStateFile{
						ID:        command.ID,
						Action:    command.Action,
						State:     inputStateDone,
						Error:     "",
						Caret:     caret,
						UpdatedAt: now,
					})
					if marshalErr != nil {
						done <- marshalErr
						return
					}
					if writeErr := os.WriteFile(filepath.Join(guardPath, inputStateName), payload, 0o600); writeErr != nil {
						done <- writeErr
						return
					}
					done <- nil
					return
				}
			}
			if time.Now().After(deadline) {
				done <- errors.New("input command was not written")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return done
}

func TestInputTextWritesCommandAndWaitsForDone(t *testing.T) {
	workspaceID := int64(301)
	h := startInputRuntime(t, workspaceID)
	guardPath := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	done := writeInputResponse(t, guardPath, inputActionText, nil)

	err := h.mgr.InputText(context.Background(), workspaceID, "你好 world")
	require.NoError(t, err)
	require.NoError(t, <-done)

	data, err := os.ReadFile(filepath.Join(guardPath, inputCommandName))
	require.NoError(t, err)
	var command inputCommandFile
	require.NoError(t, json.Unmarshal(data, &command))
	assert.Equal(t, inputActionText, command.Action)
	assert.Equal(t, "你好 world", command.Text)
	assert.Greater(t, command.ID, int64(0))
}

func TestInputCaretAcceptsNoActiveCaret(t *testing.T) {
	workspaceID := int64(302)
	h := startInputRuntime(t, workspaceID)
	guardPath := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	done := writeInputResponse(t, guardPath, inputActionCaret, nil)

	caret, err := h.mgr.InputCaret(context.Background(), workspaceID)
	require.NoError(t, err)
	assert.Nil(t, caret)
	require.NoError(t, <-done)
}

func respondToPendingInput(t *testing.T, guardPath string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(guardPath, inputCommandName))
	require.NoError(t, err)
	var command inputCommandFile
	require.NoError(t, json.Unmarshal(data, &command))
	payload, err := json.Marshal(inputStateFile{
		ID:        command.ID,
		Action:    command.Action,
		State:     inputStateDone,
		Error:     "",
		UpdatedAt: time.Now().Unix(),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(guardPath, inputStateName), payload, 0o600))
}

func TestNavigationDoesNotWaitForSlowInputProbe(t *testing.T) {
	workspaceID := int64(305)
	h := startInputRuntime(t, workspaceID)
	guardPath := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))

	inputDone := make(chan error, 1)
	go func() {
		_, err := h.mgr.InputCaret(context.Background(), workspaceID)
		inputDone <- err
	}()

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(guardPath, inputCommandName))
		if err != nil {
			return false
		}
		var command inputCommandFile
		return json.Unmarshal(data, &command) == nil && command.Action == inputActionCaret
	}, time.Second, 10*time.Millisecond)
	defer respondToPendingInput(t, guardPath)

	commands, stop := serveNavigationCommand(t, guardPath)
	defer stop()
	navDone := make(chan error, 1)
	go func() {
		_, err := h.mgr.Navigate(workspaceID, "state")
		navDone <- err
	}()

	select {
	case command := <-commands:
		require.Equal(t, "state", command.Action)
	case <-time.After(time.Second):
		t.Fatal("navigation command was not written while input was pending")
	}
	select {
	case err := <-navDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("navigation waited for the slow input probe")
	}

	respondToPendingInput(t, guardPath)
	require.NoError(t, <-inputDone)
}

func TestInputKeyRejectsUnknownKeyAndUnmodifiedA(t *testing.T) {
	h := newHarness(t)
	err := h.mgr.InputKey(context.Background(), 303, "F1", nil)
	require.ErrorIs(t, err, ErrInputRejected)
	err = h.mgr.InputKey(context.Background(), 303, "a", nil)
	require.ErrorIs(t, err, ErrInputRejected)
}

func TestInputTextRejectsEmptyAndTooLong(t *testing.T) {
	h := newHarness(t)
	require.ErrorIs(t, h.mgr.InputText(context.Background(), 304, "  "), ErrInputRejected)
	require.ErrorIs(t, h.mgr.InputText(context.Background(), 304, string(make([]rune, inputTextMaxRunes+1))), ErrInputRejected)
}
