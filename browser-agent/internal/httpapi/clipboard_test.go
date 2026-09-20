package httpapi

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startHTTPClipd(t *testing.T, h *harness, workspaceID int64, handler func(net.Conn)) {
	t.Helper()
	socketPath := filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), "tmp", "clipd.sock")
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0o700))
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				handler(conn)
			}()
		}
	}()
}

func clipboardRequestLine(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	require.NoError(t, err)
	var request map[string]any
	require.NoError(t, json.Unmarshal(line, &request))
	return request
}

func TestClipboardRoutesRequireBearerToken(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/internal/v1/runtimes/1/clipboard/copy",
		"/internal/v1/runtimes/1/clipboard/paste",
	} {
		response, body := h.request(t, http.MethodPost, path, "", "")
		assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		assert.JSONEq(t, `{"success":false,"error":"unauthorized"}`, string(body))
	}
}

func TestClipboardCopyReturnsRawMimeAndBytes(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(501)
	h.startRuntime(t, workspaceID)
	startHTTPClipd(t, h, workspaceID, func(conn net.Conn) {
		request := clipboardRequestLine(t, bufio.NewReader(conn))
		if request["op"] != "copy" {
			return
		}
		_, _ = io.WriteString(conn, "{\"ok\":true,\"mime\":\"text/plain\",\"length\":6}\nremote")
	})

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/501/clipboard/copy", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Equal(t, "text/plain", response.Header.Get("Content-Type"))
	assert.Equal(t, "remote", string(body))
}

func TestClipboardPasteStreamsPastJSONBodyLimit(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(502)
	h.startRuntime(t, workspaceID)
	received := make(chan int, 1)
	startHTTPClipd(t, h, workspaceID, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		request := clipboardRequestLine(t, reader)
		length, ok := request["length"].(float64)
		if !ok {
			return
		}
		payload := make([]byte, int(length))
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}
		received <- len(payload)
		_, _ = io.WriteString(conn, "{\"ok\":true}\n")
	})

	payload := bytes.Repeat([]byte("x"), 70<<10)
	request, err := http.NewRequest(http.MethodPost, h.server.URL+"/internal/v1/runtimes/502/clipboard/paste", bytes.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "text/plain")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(raw))
	assert.Equal(t, len(payload), <-received)
}

func TestClipboardPasteMapsUnsupportedMimeAndPayloadLimit(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(503)
	h.startRuntime(t, workspaceID)

	unsupported, err := http.NewRequest(http.MethodPost, h.server.URL+"/internal/v1/runtimes/503/clipboard/paste", strings.NewReader("{}"))
	require.NoError(t, err)
	unsupported.Header.Set("Authorization", "Bearer "+testToken)
	unsupported.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(unsupported)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnsupportedMediaType, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"clipboard_mime_unsupported"}`, string(raw))

	oversized, err := http.NewRequest(http.MethodPost, h.server.URL+"/internal/v1/runtimes/503/clipboard/paste", strings.NewReader(strings.Repeat("a", int(manager.MaxClipboardPayloadBytes+1))))
	require.NoError(t, err)
	oversized.Header.Set("Authorization", "Bearer "+testToken)
	oversized.Header.Set("Content-Type", "text/plain")
	response, err = http.DefaultClient.Do(oversized)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"clipboard_payload_too_large"}`, string(raw))
}
