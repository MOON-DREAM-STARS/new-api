package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	navigationCommandFileName = "command.json"
	navigationStatusFileName  = "navigation.json"

	// navigationPollInterval is the frozen command-file polling cadence. A
	// repeated command file is ignored after its id has been applied.
	navigationPollInterval = 300 * time.Millisecond
)

type navigationCommand struct {
	ID          int64  `json:"id"`
	Action      string `json:"action"`
	RequestedAt int64  `json:"requested_at"`
}

type navigationStatus struct {
	ID           int64  `json:"id"`
	CanGoBack    bool   `json:"can_go_back"`
	CanGoForward bool   `json:"can_go_forward"`
	UpdatedAt    int64  `json:"updated_at"`
	PageState    string `json:"page_state"`
	PageError    string `json:"page_error"`
	PageAttempts int    `json:"page_attempts"`
}

type navigationReceiptFile struct {
	ID           *int64  `json:"id"`
	CanGoBack    *bool   `json:"can_go_back"`
	CanGoForward *bool   `json:"can_go_forward"`
	UpdatedAt    *int64  `json:"updated_at"`
	PageState    *string `json:"page_state"`
	PageError    *string `json:"page_error"`
	PageAttempts *int    `json:"page_attempts"`
}

func (file navigationReceiptFile) valid() bool {
	return file.ID != nil && *file.ID >= 0 && file.CanGoBack != nil &&
		file.CanGoForward != nil && file.UpdatedAt != nil
}

type navigationHistory struct {
	CurrentIndex int `json:"currentIndex"`
	Entries      []struct {
		ID int64 `json:"id"`
	} `json:"entries"`
}

// navigationController owns the command-file bridge and the latest status
// receipt. It shares the existing CDP client and never creates another
// connection or event consumer.
type navigationController struct {
	dir    string
	client *cdpClient
	logger *slog.Logger

	commandMu   sync.Mutex
	lastSeen    int64
	lastApplied int64

	sessionMu  sync.Mutex
	sessionIDs []string

	writeMu      sync.Mutex
	lastWritten  int64
	canGoBack    bool
	canGoForward bool
	pageState    string
	pageError    string
	pageAttempts int
	onReload     func(context.Context, string) (bool, error)
}

func newNavigationController(dir string, client *cdpClient, logger *slog.Logger) *navigationController {
	if logger == nil {
		logger = slog.Default()
	}
	controller := &navigationController{
		dir:    strings.TrimSpace(dir),
		client: client,
		logger: logger,
	}
	if data, err := os.ReadFile(controller.filePath(navigationStatusFileName)); err == nil {
		var file navigationReceiptFile
		if json.Unmarshal(data, &file) == nil && file.valid() {
			controller.lastSeen = *file.ID
			controller.lastApplied = *file.ID
			controller.lastWritten = *file.ID
			controller.canGoBack = *file.CanGoBack
			controller.canGoForward = *file.CanGoForward
			if file.PageState != nil && file.PageError != nil && file.PageAttempts != nil &&
				validPageState(*file.PageState) && *file.PageAttempts >= 0 {
				controller.pageState = *file.PageState
				controller.pageError = normalizePageError(*file.PageError)
				controller.pageAttempts = *file.PageAttempts
			}
		}
	}
	return controller
}

func (n *navigationController) filePath(name string) string {
	if n == nil || n.dir == "" {
		return ""
	}
	return filepath.Join(n.dir, name)
}

// setSession records page targets in attach order. The runtime's app window is
// attached first, so currentSession always prefers it over later popups.
func (n *navigationController) setSession(sessionID string) {
	if n == nil || sessionID == "" {
		return
	}
	n.sessionMu.Lock()
	for _, existing := range n.sessionIDs {
		if existing == sessionID {
			n.sessionMu.Unlock()
			return
		}
	}
	n.sessionIDs = append(n.sessionIDs, sessionID)
	n.sessionMu.Unlock()
}

func (n *navigationController) clearSession(sessionID string) {
	if n == nil || sessionID == "" {
		return
	}
	n.sessionMu.Lock()
	for index, existing := range n.sessionIDs {
		if existing == sessionID {
			n.sessionIDs = append(n.sessionIDs[:index], n.sessionIDs[index+1:]...)
			break
		}
	}
	n.sessionMu.Unlock()
}

func (n *navigationController) clearFromEvent(params json.RawMessage) {
	if n == nil {
		return
	}
	var event struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(params, &event); err != nil {
		return
	}
	n.clearSession(event.SessionID)
}

func (n *navigationController) currentSession() string {
	if n == nil {
		return ""
	}
	n.sessionMu.Lock()
	defer n.sessionMu.Unlock()
	if len(n.sessionIDs) == 0 {
		return ""
	}
	return n.sessionIDs[0]
}

func (n *navigationController) appliedID() int64 {
	if n == nil {
		return 0
	}
	n.commandMu.Lock()
	defer n.commandMu.Unlock()
	return n.lastApplied
}

func (n *navigationController) markApplied(id int64) {
	n.commandMu.Lock()
	if id > n.lastApplied {
		n.lastApplied = id
	}
	n.commandMu.Unlock()
}

// pollNavigationState reads the command file until the guard stops. Missing,
// malformed and already-applied commands are ignored without touching CDP.
func (g *guard) refreshNavigation(ctx context.Context, sessionID string) {
	if g.navigation == nil {
		return
	}
	g.runBackground(func() { g.navigation.refreshCurrent(ctx, sessionID) })
}

func (g *guard) pollNavigationState(ctx context.Context) {
	if g.navigation == nil {
		return
	}
	ticker := time.NewTicker(navigationPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := g.navigation.processCommand(ctx); err != nil {
				g.logger.Debug("navigation command failed",
					"event", "navigation_command_failed",
					"component", "guard",
					"error", err,
				)
			}
		}
	}
}

func (n *navigationController) processCommand(ctx context.Context) error {
	if n == nil || n.client == nil {
		return nil
	}
	data, err := os.ReadFile(n.filePath(navigationCommandFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var command navigationCommand
	if err := json.Unmarshal(data, &command); err != nil || command.ID <= 0 || strings.TrimSpace(command.Action) == "" {
		return nil
	}

	n.commandMu.Lock()
	if command.ID <= n.lastSeen {
		n.commandMu.Unlock()
		return nil
	}
	n.lastSeen = command.ID
	n.commandMu.Unlock()

	if err := n.applyCommand(ctx, command); err != nil {
		return err
	}
	n.markApplied(command.ID)
	return nil
}

func (n *navigationController) applyCommand(ctx context.Context, command navigationCommand) error {
	sessionID := n.currentSession()
	if sessionID == "" {
		return errors.New("navigation page session unavailable")
	}

	switch command.Action {
	case "back", "forward":
		history, err := n.history(ctx, sessionID)
		if err != nil {
			return err
		}
		switch command.Action {
		case "back":
			if history.CurrentIndex > 0 && history.CurrentIndex < len(history.Entries) {
				entryID := history.Entries[history.CurrentIndex-1].ID
				if err := n.client.call(ctx, sessionID, "Page.navigateToHistoryEntry", map[string]any{"entryId": entryID}); err != nil {
					return err
				}
			}
		case "forward":
			if history.CurrentIndex >= 0 && history.CurrentIndex < len(history.Entries)-1 {
				entryID := history.Entries[history.CurrentIndex+1].ID
				if err := n.client.call(ctx, sessionID, "Page.navigateToHistoryEntry", map[string]any{"entryId": entryID}); err != nil {
					return err
				}
			}
		}
	case "reload":
		if n.onReload != nil {
			handled, err := n.onReload(ctx, sessionID)
			if err != nil {
				return err
			}
			if handled {
				n.markApplied(command.ID)
				return n.refresh(ctx, sessionID, command.ID)
			}
		}
		if err := n.client.call(ctx, sessionID, "Page.reload", nil); err != nil {
			return err
		}
	case "state":
		// No navigation is performed; the receipt still refreshes from the
		// current page history.
	default:
		// Unknown actions are recorded as applied, never executed, and never
		// allowed to stop the guard.
	}
	n.markApplied(command.ID)
	return n.refresh(ctx, sessionID, command.ID)
}

func (n *navigationController) history(ctx context.Context, sessionID string) (navigationHistory, error) {
	result, err := n.client.callResult(ctx, sessionID, "Page.getNavigationHistory", nil)
	if err != nil {
		return navigationHistory{}, err
	}
	var history navigationHistory
	if err := json.Unmarshal(result, &history); err != nil {
		return navigationHistory{}, fmt.Errorf("parse Page.getNavigationHistory: %w", err)
	}
	return history, nil
}

// refreshCurrent writes the current history with the most recently applied
// command id. Event-driven refreshes are best effort and never stop the guard.
func (n *navigationController) refreshCurrent(ctx context.Context, sessionID string) {
	if n == nil {
		return
	}
	if sessionID == "" {
		sessionID = n.currentSession()
	}
	if sessionID == "" {
		return
	}
	if err := n.refresh(ctx, sessionID, n.appliedID()); err != nil {
		n.logger.Debug("navigation status refresh failed",
			"event", "navigation_status_refresh_failed",
			"component", "guard",
			"error", err,
		)
	}
}

func (n *navigationController) refresh(ctx context.Context, sessionID string, id int64) error {
	history, err := n.history(ctx, sessionID)
	if err != nil {
		return err
	}
	return n.writeNavigation(
		id,
		history.CurrentIndex > 0 && history.CurrentIndex < len(history.Entries),
		history.CurrentIndex >= 0 && history.CurrentIndex < len(history.Entries)-1,
		time.Now().Unix(),
	)
}

func (n *navigationController) writeNavigation(id int64, canGoBack, canGoForward bool, updatedAt int64) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	if id < n.lastWritten {
		return nil
	}
	n.canGoBack = canGoBack
	n.canGoForward = canGoForward
	status := navigationStatus{
		ID:           id,
		CanGoBack:    n.canGoBack,
		CanGoForward: n.canGoForward,
		UpdatedAt:    updatedAt,
		PageState:    n.pageState,
		PageError:    n.pageError,
		PageAttempts: n.pageAttempts,
	}
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if err := writeNavigationStatusAtomic(n.filePath(navigationStatusFileName), payload); err != nil {
		return err
	}
	n.lastWritten = id
	return nil
}

func (n *navigationController) setPageStatus(state, pageError string, attempts int, updatedAt int64) error {
	if n == nil {
		return nil
	}
	id := n.appliedID()
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	if id < n.lastWritten {
		id = n.lastWritten
	}
	n.pageState = state
	n.pageError = pageError
	n.pageAttempts = attempts
	status := navigationStatus{
		ID:           id,
		CanGoBack:    n.canGoBack,
		CanGoForward: n.canGoForward,
		UpdatedAt:    updatedAt,
		PageState:    n.pageState,
		PageError:    n.pageError,
		PageAttempts: n.pageAttempts,
	}
	payload, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if err := writeNavigationStatusAtomic(n.filePath(navigationStatusFileName), payload); err != nil {
		return err
	}
	n.lastWritten = id
	return nil
}

func writeNavigationStatusAtomic(path string, payload []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
