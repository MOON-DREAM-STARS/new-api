package manager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// MaxClipboardPayloadBytes is the shared clipboard bridge body bound.
	MaxClipboardPayloadBytes int64 = 8 << 20

	clipboardSocketPath = "tmp/clipd.sock"
	imeStateFileName    = "ime-state.json"
)

var (
	ErrClipboardUnavailable     = errors.New("clipboard unavailable")
	ErrClipboardPayloadTooLarge = errors.New("clipboard payload too large")
	ErrClipboardMIMEUnsupported = errors.New("clipboard mime unsupported")
	ErrClipboardFailed          = errors.New("clipboard failed")
)

type clipboardRequest struct {
	Op     string `json:"op"`
	MIME   string `json:"mime,omitempty"`
	Length *int64 `json:"length,omitempty"`
}

type clipboardCopyReply struct {
	OK     bool   `json:"ok"`
	MIME   string `json:"mime"`
	Length *int64 `json:"length"`
	Error  string `json:"error"`
}

type clipboardPasteReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// CopyClipboard asks the in-runtime clipd helper for the current X11
// clipboard selection.
func (m *Manager) CopyClipboard(ctx context.Context, workspaceID int64) (string, []byte, error) {
	if workspaceID <= 0 {
		return "", nil, fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}

	conn, reader, err := m.dialClipd(ctx, workspaceID)
	if err != nil {
		return "", nil, err
	}
	defer conn.Close()

	if err := writeClipboardJSONLine(conn, clipboardRequest{Op: "copy"}); err != nil {
		return "", nil, clipboardFailed(err)
	}
	line, err := readClipboardReplyLine(reader)
	if err != nil {
		return "", nil, clipboardFailed(err)
	}
	var reply clipboardCopyReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return "", nil, clipboardFailed(fmt.Errorf("invalid copy reply: %w", err))
	}
	if !reply.OK {
		return "", nil, clipboardError(reply.Error)
	}
	mimeType, err := normalizeClipboardMIME(reply.MIME)
	if err != nil {
		return "", nil, err
	}
	if reply.Length == nil || *reply.Length < 0 {
		return "", nil, clipboardFailed(errors.New("copy reply is missing length"))
	}
	if *reply.Length > MaxClipboardPayloadBytes {
		return "", nil, ErrClipboardPayloadTooLarge
	}

	payload := make([]byte, int(*reply.Length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return "", nil, clipboardFailed(err)
	}
	return mimeType, payload, nil
}

// PasteClipboard sends one raw clipboard body to the in-runtime clipd helper.
func (m *Manager) PasteClipboard(ctx context.Context, workspaceID int64, mimeType string, body io.Reader) error {
	if workspaceID <= 0 {
		return fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}
	normalized, err := normalizeClipboardMIME(mimeType)
	if err != nil {
		return err
	}
	payload, err := readClipboardPayload(body)
	if err != nil {
		return err
	}

	conn, reader, err := m.dialClipd(ctx, workspaceID)
	if err != nil {
		return err
	}
	defer conn.Close()

	length := int64(len(payload))
	request := clipboardRequest{Op: "paste", MIME: normalized, Length: &length}
	if err := writeClipboardJSONLine(conn, request); err != nil {
		return clipboardFailed(err)
	}
	if err := writeClipboardAll(conn, payload); err != nil {
		return clipboardFailed(err)
	}
	line, err := readClipboardReplyLine(reader)
	if err != nil {
		return clipboardFailed(err)
	}
	var reply clipboardPasteReply
	if err := json.Unmarshal(line, &reply); err != nil {
		return clipboardFailed(fmt.Errorf("invalid paste reply: %w", err))
	}
	if !reply.OK {
		return clipboardError(reply.Error)
	}
	return nil
}

func (m *Manager) dialClipd(ctx context.Context, workspaceID int64) (net.Conn, *bufio.Reader, error) {
	state, err := m.liveRuntimeState(workspaceID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil, fmt.Errorf("%w: runtime is missing", ErrClipboardUnavailable)
		}
		return nil, nil, clipboardFailed(err)
	}
	if state != StateRunning && state != StateIdle {
		return nil, nil, fmt.Errorf("%w: runtime is %s", ErrClipboardUnavailable, state)
	}

	workspaceDir := WorkspaceDir(m.dataRoot, workspaceID)
	root, err := openWorkspaceRoot(workspaceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%w: workspace is missing", ErrClipboardUnavailable)
		}
		return nil, nil, clipboardFailed(err)
	}
	socketInfo, statErr := root.Lstat(clipboardSocketPath)
	_ = root.Close()
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%w: socket is missing", ErrClipboardUnavailable)
		}
		return nil, nil, clipboardFailed(statErr)
	}
	if socketInfo.Mode()&os.ModeSocket == 0 {
		return nil, nil, clipboardFailed(errors.New("clipd path is not a socket"))
	}

	socketPath := filepath.Join(workspaceDir, "tmp", "clipd.sock")
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%w: socket is missing", ErrClipboardUnavailable)
		}
		return nil, nil, clipboardFailed(err)
	}
	return conn, bufio.NewReader(conn), nil
}

func normalizeClipboardMIME(value string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrClipboardMIMEUnsupported, err)
	}
	switch strings.ToLower(mediaType) {
	case "text/plain", "image/png", "image/jpeg", "image/webp":
		return strings.ToLower(mediaType), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrClipboardMIMEUnsupported, mediaType)
	}
}

func readClipboardPayload(body io.Reader) ([]byte, error) {
	if body == nil {
		return []byte{}, nil
	}
	payload, err := io.ReadAll(io.LimitReader(body, MaxClipboardPayloadBytes+1))
	if err != nil {
		return nil, clipboardFailed(err)
	}
	if int64(len(payload)) > MaxClipboardPayloadBytes {
		return nil, ErrClipboardPayloadTooLarge
	}
	return payload, nil
}

func writeClipboardJSONLine(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := writeClipboardAll(writer, payload); err != nil {
		return err
	}
	return writeClipboardAll(writer, []byte{'\n'})
}

func readClipboardReplyLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 128)
	for {
		part, err := reader.ReadSlice('\n')
		if int64(len(line)+len(part)) > MaxClipboardPayloadBytes {
			return nil, ErrClipboardPayloadTooLarge
		}
		line = append(line, part...)
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, err
		}
	}
}

func writeClipboardAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}

func clipboardError(code string) error {
	switch strings.TrimSpace(code) {
	case "ERR_CLIPD_MIME_UNSUPPORTED":
		return ErrClipboardMIMEUnsupported
	case "ERR_CLIPD_PAYLOAD_TOO_LARGE":
		return ErrClipboardPayloadTooLarge
	case "ERR_CLIPD_X11_FAILED", "ERR_CLIPD_XCLIP_FAILED", "ERR_CLIPD_PROTOCOL":
		return fmt.Errorf("%w: %s", ErrClipboardFailed, code)
	default:
		return fmt.Errorf("%w: %s", ErrClipboardFailed, code)
	}
}

func clipboardFailed(err error) error {
	if err == nil {
		return ErrClipboardFailed
	}
	return fmt.Errorf("%w: %v", ErrClipboardFailed, err)
}

type imeStateFile struct {
	State string `json:"state"`
}

// readIMEState returns the presentation-only IBus state. A missing, corrupt or
// unknown state deliberately becomes UNAVAILABLE instead of failing a runtime
// snapshot.
func readIMEState(workspaceDir string) string {
	root, err := openWorkspaceRoot(workspaceDir)
	if err != nil {
		return "UNAVAILABLE"
	}
	defer root.Close()

	statePath := path.Join(guardStateDirName, imeStateFileName)
	info, err := root.Lstat(statePath)
	if err != nil || !info.Mode().IsRegular() {
		return "UNAVAILABLE"
	}
	payload, err := root.ReadFile(statePath)
	if err != nil {
		return "UNAVAILABLE"
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var state imeStateFile
	if err := decoder.Decode(&state); err != nil {
		return "UNAVAILABLE"
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "UNAVAILABLE"
	}
	if state.State == "READY" {
		return "READY"
	}
	return "UNAVAILABLE"
}
