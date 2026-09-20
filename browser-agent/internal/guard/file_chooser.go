package guard

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const (
	fileChooserStateFileName   = "file-chooser.json"
	fileChooserCommandFileName = "file-chooser-command.json"
	fileChooserPollInterval    = 300 * time.Millisecond
	fileChooserPendingTTL      = 2 * time.Minute
)

const (
	fileChooserStatePending   = "PENDING"
	fileChooserStateDone      = "DONE"
	fileChooserStateCancelled = "CANCELLED"
	fileChooserStateFailed    = "FAILED"
	fileChooserStateExpired   = "EXPIRED"

	fileChooserActionAttach = "attach"
	fileChooserActionCancel = "cancel"

	fileChooserModeSingle   = "selectSingle"
	fileChooserModeMultiple = "selectMultiple"

	fileChooserErrorExpired      = "WEB_WORKSPACE_FILE_CHOOSER_EXPIRED"
	fileChooserErrorInjectFailed = "WEB_WORKSPACE_FILE_INJECT_FAILED"
)

type fileChooserState struct {
	ID            int64  `json:"id"`
	ChooserID     string `json:"chooser_id"`
	State         string `json:"state"`
	Error         string `json:"error"`
	Mode          string `json:"mode"`
	BackendNodeID int64  `json:"backend_node_id"`
	SessionID     string `json:"session_id"`
	CreatedAt     int64  `json:"created_at"`
	ExpiresAt     int64  `json:"expires_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type fileChooserStateFile struct {
	ID            *int64  `json:"id"`
	ChooserID     *string `json:"chooser_id"`
	State         *string `json:"state"`
	Error         *string `json:"error"`
	Mode          *string `json:"mode"`
	BackendNodeID *int64  `json:"backend_node_id"`
	SessionID     *string `json:"session_id"`
	CreatedAt     *int64  `json:"created_at"`
	ExpiresAt     *int64  `json:"expires_at"`
	UpdatedAt     *int64  `json:"updated_at"`
}

func (state fileChooserStateFile) valid() bool {
	if state.ID == nil || *state.ID < 0 || state.ChooserID == nil || state.State == nil || state.Error == nil ||
		state.Mode == nil || state.BackendNodeID == nil || state.SessionID == nil ||
		state.CreatedAt == nil || state.ExpiresAt == nil || state.UpdatedAt == nil {
		return false
	}
	if strings.TrimSpace(*state.ChooserID) == "" || strings.TrimSpace(*state.SessionID) == "" || *state.BackendNodeID <= 0 {
		return false
	}
	switch *state.State {
	case fileChooserStatePending, fileChooserStateDone, fileChooserStateCancelled, fileChooserStateFailed, fileChooserStateExpired:
	default:
		return false
	}
	switch *state.Mode {
	case fileChooserModeSingle, fileChooserModeMultiple:
	default:
		return false
	}
	return *state.CreatedAt >= 0 && *state.ExpiresAt >= *state.CreatedAt && *state.UpdatedAt >= 0
}

func (state fileChooserStateFile) value() fileChooserState {
	return fileChooserState{
		ID:            *state.ID,
		ChooserID:     *state.ChooserID,
		State:         *state.State,
		Error:         *state.Error,
		Mode:          *state.Mode,
		BackendNodeID: *state.BackendNodeID,
		SessionID:     *state.SessionID,
		CreatedAt:     *state.CreatedAt,
		ExpiresAt:     *state.ExpiresAt,
		UpdatedAt:     *state.UpdatedAt,
	}
}

type fileChooserCommand struct {
	ID          int64    `json:"id"`
	ChooserID   string   `json:"chooser_id"`
	Action      string   `json:"action"`
	Files       []string `json:"files,omitempty"`
	RequestedAt int64    `json:"requested_at"`
}

type fileChooserController struct {
	dir    string
	client *cdpClient
	logger *slog.Logger
	now    func() time.Time

	stateMu     sync.Mutex
	lastSeen    int64
	lastWritten int64
}

func newFileChooserController(dir string, client *cdpClient, logger *slog.Logger) *fileChooserController {
	if logger == nil {
		logger = slog.Default()
	}
	controller := &fileChooserController{
		dir:    strings.TrimSpace(dir),
		client: client,
		logger: logger,
		now:    time.Now,
	}
	if data, err := os.ReadFile(controller.filePath(fileChooserStateFileName)); err == nil {
		var file fileChooserStateFile
		if json.Unmarshal(data, &file) == nil && file.valid() {
			controller.lastSeen = *file.ID
			controller.lastWritten = *file.ID
		}
	}
	return controller
}

func (c *fileChooserController) filePath(name string) string {
	if c == nil || c.dir == "" {
		return ""
	}
	return filepath.Join(c.dir, name)
}

func (c *fileChooserController) run(ctx context.Context) {
	ticker := time.NewTicker(fileChooserPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.expirePending(); err != nil {
				c.logger.Debug("file chooser expiry failed", "event", "file_chooser_expiry_failed", "component", "guard", "error", err)
			}
			if err := c.processCommand(ctx); err != nil {
				c.logger.Debug("file chooser command failed", "event", "file_chooser_command_failed", "component", "guard", "error", err)
			}
		}
	}
}

func (c *fileChooserController) onOpened(_ context.Context, sessionID string, params json.RawMessage) error {
	if c == nil {
		return nil
	}
	var event struct {
		Mode          string `json:"mode"`
		BackendNodeID int64  `json:"backendNodeId"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" || event.BackendNodeID <= 0 {
		return nil
	}
	if event.Mode != fileChooserModeSingle && event.Mode != fileChooserModeMultiple {
		return nil
	}
	chooserID, err := newFileChooserID()
	if err != nil {
		return err
	}
	now := c.now().Unix()
	next := fileChooserState{
		ChooserID:     chooserID,
		State:         fileChooserStatePending,
		Mode:          event.Mode,
		BackendNodeID: event.BackendNodeID,
		SessionID:     strings.TrimSpace(sessionID),
		CreatedAt:     now,
		ExpiresAt:     now + int64(fileChooserPendingTTL/time.Second),
		UpdatedAt:     now,
	}

	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	current, ok, err := c.readStateLocked()
	if err != nil {
		return err
	}
	if ok && current.State == fileChooserStatePending && current.ExpiresAt > now {
		return nil
	}
	if ok && current.State == fileChooserStatePending && current.ExpiresAt <= now {
		current.State = fileChooserStateExpired
		current.Error = fileChooserErrorExpired
		current.UpdatedAt = now
		if err := c.writeStateLocked(current); err != nil {
			return err
		}
	}
	c.lastWritten = 0
	return c.writeStateLocked(next)
}

func (c *fileChooserController) expirePending() error {
	if c == nil {
		return nil
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	current, ok, err := c.readStateLocked()
	if err != nil || !ok || current.State != fileChooserStatePending {
		return err
	}
	now := c.now().Unix()
	if current.ExpiresAt > now {
		return nil
	}
	current.State = fileChooserStateExpired
	current.Error = fileChooserErrorExpired
	current.UpdatedAt = now
	return c.writeStateLocked(current)
}

func (c *fileChooserController) processCommand(ctx context.Context) error {
	if c == nil || c.client == nil {
		return nil
	}
	data, err := os.ReadFile(c.filePath(fileChooserCommandFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var command fileChooserCommand
	if err := json.Unmarshal(data, &command); err != nil || !validFileChooserCommand(command) {
		return nil
	}

	c.stateMu.Lock()
	if command.ID <= c.lastSeen {
		c.stateMu.Unlock()
		return nil
	}
	c.lastSeen = command.ID
	current, ok, err := c.readStateLocked()
	if err != nil {
		c.stateMu.Unlock()
		return err
	}
	if !ok || current.State != fileChooserStatePending || current.ChooserID != command.ChooserID {
		if ok {
			current.ID = command.ID
			current.State = fileChooserStateFailed
			current.Error = fileChooserErrorExpired
			current.UpdatedAt = c.now().Unix()
			_ = c.writeStateLocked(current)
		}
		c.stateMu.Unlock()
		return nil
	}
	if current.ExpiresAt <= c.now().Unix() {
		current.ID = command.ID
		current.State = fileChooserStateExpired
		current.Error = fileChooserErrorExpired
		current.UpdatedAt = c.now().Unix()
		_ = c.writeStateLocked(current)
		c.stateMu.Unlock()
		return nil
	}
	current.ID = command.ID
	sessionID := current.SessionID
	backendNodeID := current.BackendNodeID
	c.stateMu.Unlock()

	switch command.Action {
	case fileChooserActionCancel:
		return c.finish(current, fileChooserStateCancelled, "")
	case fileChooserActionAttach:
		if _, err := c.client.callResult(ctx, sessionID, "DOM.setFileInputFiles", map[string]any{
			"files":         command.Files,
			"backendNodeId": backendNodeID,
		}); err != nil {
			return c.finish(current, fileChooserStateFailed, fileChooserErrorInjectFailed)
		}
		return c.finish(current, fileChooserStateDone, "")
	default:
		return nil
	}
}

func (c *fileChooserController) finish(current fileChooserState, state string, errorCode string) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	latest, ok, err := c.readStateLocked()
	if err != nil {
		return err
	}
	if !ok || latest.ChooserID != current.ChooserID || latest.State != fileChooserStatePending {
		return nil
	}
	latest.ID = current.ID
	latest.State = state
	latest.Error = errorCode
	latest.UpdatedAt = c.now().Unix()
	return c.writeStateLocked(latest)
}

func (c *fileChooserController) readStateLocked() (fileChooserState, bool, error) {
	data, err := os.ReadFile(c.filePath(fileChooserStateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return fileChooserState{}, false, nil
	}
	if err != nil {
		return fileChooserState{}, false, err
	}
	var file fileChooserStateFile
	if err := json.Unmarshal(data, &file); err != nil || !file.valid() {
		return fileChooserState{}, false, nil
	}
	return file.value(), true, nil
}

func (c *fileChooserController) writeStateLocked(state fileChooserState) error {
	if state.ID < c.lastWritten {
		return nil
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := writeNavigationStatusAtomic(c.filePath(fileChooserStateFileName), payload); err != nil {
		return err
	}
	c.lastWritten = state.ID
	return nil
}

func validFileChooserCommand(command fileChooserCommand) bool {
	if command.ID <= 0 || strings.TrimSpace(command.ChooserID) == "" {
		return false
	}
	switch command.Action {
	case fileChooserActionCancel:
		return len(command.Files) == 0
	case fileChooserActionAttach:
		if len(command.Files) == 0 || len(command.Files) > 5 {
			return false
		}
		prefix := runtime.WorkspaceMountTarget + "/uploads/.bridge/" + command.ChooserID + "/"
		for _, file := range command.Files {
			if !strings.HasPrefix(file, prefix) || strings.Contains(file, "..") {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func newFileChooserID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate file chooser id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
