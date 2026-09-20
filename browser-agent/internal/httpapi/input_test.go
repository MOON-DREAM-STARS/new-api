package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHTTPInputResponse(t *testing.T, guardPath string, action string, caret *manager.InputCaret) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			data, err := os.ReadFile(filepath.Join(guardPath, "input-command.json"))
			if err == nil {
				var command struct {
					ID     int64  `json:"id"`
					Action string `json:"action"`
				}
				if json.Unmarshal(data, &command) == nil && command.Action == action {
					payload, marshalErr := json.Marshal(map[string]any{
						"id":         command.ID,
						"action":     command.Action,
						"state":      "DONE",
						"error":      "",
						"caret":      caret,
						"updated_at": time.Now().Unix(),
					})
					if marshalErr != nil {
						done <- marshalErr
						return
					}
					if writeErr := os.WriteFile(filepath.Join(guardPath, "input.json"), payload, 0o600); writeErr != nil {
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

func TestInputRoutesRequireBearerToken(t *testing.T) {
	h := newHarness(t)
	paths := [][2]string{
		{http.MethodPost, "/internal/v1/runtimes/1/input/text"},
		{http.MethodPost, "/internal/v1/runtimes/1/input/key"},
		{http.MethodGet, "/internal/v1/runtimes/1/input/caret"},
	}
	for _, path := range paths {
		response, body := h.request(t, path[0], path[1], "", "")
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		assert.JSONEq(t, `{"success":false,"error":"unauthorized"}`, string(body))
	}
}

func TestInputTextRouteReturnsOK(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(401)
	h.startRuntime(t, workspaceID)
	guardPath := filepath.Join(h.dataRoot, "workspace-"+itoa(workspaceID), ".guard")
	done := writeHTTPInputResponse(t, guardPath, "input_text", nil)

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/401/input/text", "Bearer "+testToken, `{"text":"你好"}`)
	require.NoError(t, <-done)
	assert.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, `{"ok":true}`, string(body))
}

func TestInputKeyRouteRejectsUnknownKey(t *testing.T) {
	h := newHarness(t)
	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/1/input/key", "Bearer "+testToken, `{"key":"F1"}`)
	assert.Equal(t, http.StatusUnprocessableEntity, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"input_rejected"}`, string(body))
}

func TestInputCaretRouteReturnsNullWithoutActiveCaret(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(402)
	h.startRuntime(t, workspaceID)
	guardPath := filepath.Join(h.dataRoot, "workspace-"+itoa(workspaceID), ".guard")
	done := writeHTTPInputResponse(t, guardPath, "caret_probe", nil)

	response, body := h.request(t, http.MethodGet, "/internal/v1/runtimes/402/input/caret", "Bearer "+testToken, "")
	require.NoError(t, <-done)
	assert.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, `{"caret":null}`, string(body))
}
