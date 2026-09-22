package manager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type clipboardWireRequest struct {
	Op     string `json:"op"`
	MIME   string `json:"mime"`
	Length *int64 `json:"length"`
}

func newClipboardTestManager(t *testing.T) (*Manager, int64) {
	t.Helper()
	dataRoot, err := os.MkdirTemp("", "wwc-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataRoot) })
	workspaceID := int64(42)
	require.NoError(t, os.MkdirAll(filepath.Join(WorkspaceDir(dataRoot, workspaceID), "tmp"), 0o700))
	m := New(nil, nil, Options{DataRoot: dataRoot})
	m.runtimes[workspaceID] = &runtimeState{state: StateRunning}
	return m, workspaceID
}

func startClipdTestServer(t *testing.T, m *Manager, workspaceID int64, handler func(net.Conn)) net.Listener {
	t.Helper()
	socketPath := filepath.Join(WorkspaceDir(m.dataRoot, workspaceID), "tmp", "clipd.sock")
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
	return listener
}

func readClipboardWireRequest(t *testing.T, reader *bufio.Reader) clipboardWireRequest {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	require.NoError(t, err)
	var request clipboardWireRequest
	require.NoError(t, json.Unmarshal(line, &request))
	return request
}

func TestClipboardClientFramesCopyAndPaste(t *testing.T) {
	m, workspaceID := newClipboardTestManager(t)
	requests := make(chan clipboardWireRequest, 2)
	errorsCh := make(chan error, 1)
	startClipdTestServer(t, m, workspaceID, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			errorsCh <- err
			return
		}
		var request clipboardWireRequest
		if err := json.Unmarshal(line, &request); err != nil {
			errorsCh <- err
			return
		}
		requests <- request

		switch request.Op {
		case "copy":
			_, err = io.WriteString(conn, "{\"ok\":true,\"mime\":\"text/plain\",\"length\":5}\nhello")
		case "paste":
			if request.Length == nil {
				errorsCh <- fmt.Errorf("paste request has no length")
				return
			}
			payload := make([]byte, *request.Length)
			if _, err = io.ReadFull(reader, payload); err != nil {
				errorsCh <- err
				return
			}
			if !bytes.Equal(payload, []byte("world")) {
				errorsCh <- fmt.Errorf("paste payload mismatch: %q", payload)
				return
			}
			_, err = io.WriteString(conn, "{\"ok\":true}\n")
		default:
			errorsCh <- fmt.Errorf("unexpected op %q", request.Op)
			return
		}
		if err != nil {
			errorsCh <- err
		}
	})

	mimeType, payload, err := m.CopyClipboard(context.Background(), workspaceID)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", mimeType)
	assert.Equal(t, []byte("hello"), payload)
	require.NoError(t, m.PasteClipboard(context.Background(), workspaceID, "text/plain; charset=utf-8", strings.NewReader("world")))

	copyRequest := <-requests
	pasteRequest := <-requests
	assert.Equal(t, "copy", copyRequest.Op)
	assert.Empty(t, copyRequest.MIME)
	assert.Nil(t, copyRequest.Length)
	assert.Equal(t, "paste", pasteRequest.Op)
	assert.Equal(t, "text/plain", pasteRequest.MIME)
	require.NotNil(t, pasteRequest.Length)
	assert.EqualValues(t, 5, *pasteRequest.Length)
	select {
	case err := <-errorsCh:
		require.NoError(t, err)
	default:
	}
}

func TestClipboardClientMapsClipdErrors(t *testing.T) {
	tests := []struct {
		code string
		want error
	}{
		{code: "ERR_CLIPD_MIME_UNSUPPORTED", want: ErrClipboardMIMEUnsupported},
		{code: "ERR_CLIPD_PAYLOAD_TOO_LARGE", want: ErrClipboardPayloadTooLarge},
		{code: "ERR_CLIPD_X11_FAILED", want: ErrClipboardFailed},
		{code: "ERR_CLIPD_XCLIP_FAILED", want: ErrClipboardFailed},
		{code: "ERR_CLIPD_PROTOCOL", want: ErrClipboardFailed},
	}
	for _, testCase := range tests {
		t.Run(testCase.code, func(t *testing.T) {
			m, workspaceID := newClipboardTestManager(t)
			startClipdTestServer(t, m, workspaceID, func(conn net.Conn) {
				reader := bufio.NewReader(conn)
				_, _ = reader.ReadBytes('\n')
				_, _ = fmt.Fprintf(conn, "{\"ok\":false,\"error\":%q}\n", testCase.code)
			})

			_, _, err := m.CopyClipboard(context.Background(), workspaceID)
			require.ErrorIs(t, err, testCase.want)
		})
	}
}

func TestClipboardClientEnforcesMimeAndPayloadLimit(t *testing.T) {
	m, workspaceID := newClipboardTestManager(t)

	err := m.PasteClipboard(context.Background(), workspaceID, "application/json", strings.NewReader("{}"))
	require.ErrorIs(t, err, ErrClipboardMIMEUnsupported)

	oversized := strings.NewReader(strings.Repeat("a", int(MaxClipboardPayloadBytes+1)))
	err = m.PasteClipboard(context.Background(), workspaceID, "text/plain", oversized)
	require.ErrorIs(t, err, ErrClipboardPayloadTooLarge)

	startClipdTestServer(t, m, workspaceID, func(conn net.Conn) {
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadBytes('\n')
		_, _ = fmt.Fprintf(conn, "{\"ok\":true,\"mime\":\"text/plain\",\"length\":%d}\n", MaxClipboardPayloadBytes+1)
	})
	_, _, err = m.CopyClipboard(context.Background(), workspaceID)
	require.ErrorIs(t, err, ErrClipboardPayloadTooLarge)
}

func TestClipboardClientReportsMissingSocketAsUnavailable(t *testing.T) {
	m, workspaceID := newClipboardTestManager(t)

	_, _, err := m.CopyClipboard(context.Background(), workspaceID)
	require.ErrorIs(t, err, ErrClipboardUnavailable)
	require.ErrorIs(t, m.PasteClipboard(context.Background(), workspaceID, "text/plain", strings.NewReader("hello")), ErrClipboardUnavailable)
}

func TestReadIMEState(t *testing.T) {
	workspaceDir := filepath.Join(t.TempDir(), "workspace-1")
	guardDir := filepath.Join(workspaceDir, ".guard")
	require.NoError(t, os.MkdirAll(guardDir, 0o700))
	statePath := filepath.Join(guardDir, imeStateFileName)

	require.NoError(t, os.WriteFile(statePath, []byte(`{"state":"READY"}`), 0o600))
	assert.Equal(t, "READY", readIMEState(workspaceDir))

	require.NoError(t, os.WriteFile(statePath, []byte(`{"state":"UNAVAILABLE"}`), 0o600))
	assert.Equal(t, "UNAVAILABLE", readIMEState(workspaceDir))

	require.NoError(t, os.WriteFile(statePath, []byte(`{"state":"BROKEN"}`), 0o600))
	assert.Equal(t, "UNAVAILABLE", readIMEState(workspaceDir))

	require.NoError(t, os.WriteFile(statePath, []byte(`{"state":"READY"}`), 0o600))
	require.NoError(t, os.Remove(statePath))
	assert.Equal(t, "UNAVAILABLE", readIMEState(workspaceDir))

	require.NoError(t, os.WriteFile(statePath, []byte(`{"state":"READY"`), 0o600))
	assert.Equal(t, "UNAVAILABLE", readIMEState(workspaceDir))
}
