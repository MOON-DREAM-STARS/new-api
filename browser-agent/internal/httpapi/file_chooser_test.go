package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const httpFileChooserID = "chooser-0000000000000001"

func writeHTTPFileChooserPending(t *testing.T, h *harness, workspaceID int64, chooserID string) string {
	t.Helper()
	guardPath := filepath.Join(h.dataRoot, "workspace-"+itoa(workspaceID), ".guard")
	require.NoError(t, os.MkdirAll(guardPath, 0o700))
	now := time.Now().Unix()
	payload, err := json.Marshal(map[string]any{
		"id":              0,
		"chooser_id":      chooserID,
		"state":           "PENDING",
		"error":           "",
		"mode":            "selectSingle",
		"backend_node_id": 11,
		"session_id":      "session-1",
		"created_at":      now,
		"expires_at":      now + 300,
		"updated_at":      now,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(guardPath, "file-chooser.json"), payload, 0o644))
	return guardPath
}

func itoa(value int64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}

func respondToHTTPFileChooserCommand(t *testing.T, guardPath string, chooserID string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			data, err := os.ReadFile(filepath.Join(guardPath, "file-chooser-command.json"))
			if err == nil {
				var command struct {
					ID        int64  `json:"id"`
					ChooserID string `json:"chooser_id"`
				}
				if json.Unmarshal(data, &command) == nil && command.ChooserID == chooserID {
					now := time.Now().Unix()
					status, marshalErr := json.Marshal(map[string]any{
						"id":              command.ID,
						"chooser_id":      chooserID,
						"state":           "DONE",
						"error":           "",
						"mode":            "selectSingle",
						"backend_node_id": 11,
						"session_id":      "session-1",
						"created_at":      now,
						"expires_at":      now + 300,
						"updated_at":      now,
					})
					if marshalErr != nil {
						done <- marshalErr
						return
					}
					if writeErr := os.WriteFile(filepath.Join(guardPath, "file-chooser.json"), status, 0o644); writeErr != nil {
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

func TestFileChooserRoutesRequireBearerToken(t *testing.T) {
	h := newHarness(t)
	paths := [][2]string{
		{http.MethodGet, "/internal/v1/runtimes/1/file-chooser"},
		{http.MethodPost, "/internal/v1/runtimes/1/file-chooser/" + httpFileChooserID + "/files"},
		{http.MethodPost, "/internal/v1/runtimes/1/file-chooser/" + httpFileChooserID + "/cancel"},
	}
	for _, path := range paths {
		response, body := h.request(t, path[0], path[1], "", "")
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		assert.JSONEq(t, `{"success":false,"error":"unauthorized"}`, string(body))
	}
}

func TestFileChooserUploadStreamsPastJSONBodyLimit(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(201)
	h.startRuntime(t, workspaceID)
	guardPath := writeHTTPFileChooserPending(t, h, workspaceID, httpFileChooserID)
	done := respondToHTTPFileChooserCommand(t, guardPath, httpFileChooserID)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "large.txt")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("a"), 70<<10))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	request, err := http.NewRequest(http.MethodPost, h.server.URL+"/internal/v1/runtimes/201/file-chooser/"+httpFileChooserID+"/files", &body)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, <-done)
	assert.Equal(t, http.StatusOK, response.StatusCode, string(raw))
	assert.Contains(t, string(raw), `"state":"DONE"`)
}
