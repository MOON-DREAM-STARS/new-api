package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const (
	navigationCommandFileName = "command.json"
	navigationReceiptFileName = "navigation.json"
	navigationPollInterval    = 100 * time.Millisecond
	// The guard acknowledges a dispatched navigation from cached state without
	// waiting for Page.navigate or Page.getNavigationHistory on the renderer,
	// and the manager no longer holds the lifecycle lock across this wait. Local
	// acceptance of the real KasmVNC project-open path measured 0.30s-2.25s
	// end-to-end while the file-chooser long poll and the Kasm websocket were
	// both active; 10s is a bounded provider-slow allowance above that observed
	// p99, not a lock-contention workaround. navigation_timeout stays distinct
	// from navigation_unavailable so a slow provider is not reported as an
	// unavailable agent.
	navigationWaitTimeout = 10 * time.Second
)

// ErrNavigationUnavailable reports that the guard state directory could not be
// prepared or the command could not be written.
var ErrNavigationUnavailable = errors.New("navigation unavailable")

// ErrNavigationTimeout reports that the guard did not acknowledge the command
// within the bounded receipt window.
var ErrNavigationTimeout = errors.New("navigation timeout")

// NavigationStatus is the URL-free navigation state exposed in a runtime
// snapshot.
type NavigationStatus struct {
	CanGoBack    bool  `json:"can_go_back"`
	CanGoForward bool  `json:"can_go_forward"`
	UpdatedAt    int64 `json:"updated_at"`
}

// PageState is the URL-free health state reported by the guard's page monitor.
type PageState string

const (
	PageStateReady    PageState = "READY"
	PageStateRetrying PageState = "RETRYING"
	PageStateFailed   PageState = "FAILED"
)

// PageStatus is the URL-free page health exposed in a runtime snapshot.
type PageStatus struct {
	State     PageState `json:"state"`
	Error     string    `json:"error"`
	Attempts  int       `json:"attempts"`
	UpdatedAt int64     `json:"updated_at"`
}

type navigationCommandFile struct {
	ID          int64  `json:"id"`
	Action      string `json:"action"`
	ProjectID   string `json:"project_id,omitempty"`
	RequestedAt int64  `json:"requested_at"`
}

type navigationReceipt struct {
	ID           int64
	CanGoBack    bool
	CanGoForward bool
	UpdatedAt    int64
	Page         *PageStatus
}

type navigationReceiptFile struct {
	ID           *int64          `json:"id"`
	CanGoBack    *bool           `json:"can_go_back"`
	CanGoForward *bool           `json:"can_go_forward"`
	UpdatedAt    *int64          `json:"updated_at"`
	PageState    json.RawMessage `json:"page_state"`
	PageError    json.RawMessage `json:"page_error"`
	PageAttempts json.RawMessage `json:"page_attempts"`
}

func (file navigationReceiptFile) valid() bool {
	return file.ID != nil && *file.ID >= 0 && file.CanGoBack != nil &&
		file.CanGoForward != nil && file.UpdatedAt != nil
}

func (receipt navigationReceipt) status() NavigationStatus {
	return NavigationStatus{
		CanGoBack:    receipt.CanGoBack,
		CanGoForward: receipt.CanGoForward,
		UpdatedAt:    receipt.UpdatedAt,
	}
}

func (file navigationReceiptFile) pageStatus() (*PageStatus, bool) {
	stateText, stateOK := optionalString(file.PageState)
	errorText, errorOK := optionalString(file.PageError)
	attempts, attemptsOK := optionalInt(file.PageAttempts)
	if !stateOK || !errorOK || !attemptsOK || file.UpdatedAt == nil {
		return nil, false
	}
	state := PageState(stateText)
	if !state.valid() || attempts < 0 || !validPageError(errorText) {
		return nil, false
	}
	if state == PageStateReady && (attempts != 0 || errorText != "") {
		return nil, false
	}
	return &PageStatus{
		State:     state,
		Error:     errorText,
		Attempts:  attempts,
		UpdatedAt: *file.UpdatedAt,
	}, true
}

func optionalString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func optionalInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func (state PageState) valid() bool {
	switch state {
	case PageStateReady, PageStateRetrying, PageStateFailed:
		return true
	default:
		return false
	}
}

func validPageError(value string) bool {
	if value == "" {
		return true
	}
	if len(value) <= len("ERR_") || value[:len("ERR_")] != "ERR_" {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func validNavigationAction(action string) bool {
	switch action {
	case "back", "forward", "reload", "state", "project":
		return true
	default:
		return false
	}
}

// Navigate writes one navigation command and waits for the guard's matching
// receipt. It never synthesises a successful state when the guard is silent.
func (m *Manager) Navigate(workspaceID int64, action string) (NavigationStatus, error) {
	return m.navigate(workspaceID, action, "")
}

// NavigateProject opens one provider project by its stable external id. The
// caller must already have authorized the project through ownership; the guard
// still re-checks it on the resulting Page.navigate.
func (m *Manager) NavigateProject(workspaceID int64, projectID string) (NavigationStatus, error) {
	projectID = strings.TrimSpace(projectID)
	if !projectIDPattern.MatchString(projectID) {
		return NavigationStatus{}, fmt.Errorf("%w: project navigation request is invalid", ErrInvalidRequest)
	}
	return m.navigate(workspaceID, "project", projectID)
}

func (m *Manager) navigate(workspaceID int64, action string, projectID string) (NavigationStatus, error) {
	projectID = strings.TrimSpace(projectID)
	if workspaceID <= 0 || !validNavigationAction(action) || (action == "project" && !projectIDPattern.MatchString(projectID)) || (action != "project" && projectID != "") {
		return NavigationStatus{}, fmt.Errorf("%w: navigation request is invalid", ErrInvalidRequest)
	}

	feature := m.featureLock(featureNavigation, workspaceID)
	feature.Lock()
	defer feature.Unlock()

	guardRoot, rt, commandID, err := m.prepareNavigation(workspaceID, action, projectID)
	if err != nil {
		return NavigationStatus{}, err
	}
	defer guardRoot.Close()

	deadline := time.Now().Add(navigationWaitTimeout)
	ticker := time.NewTicker(navigationPollInterval)
	defer ticker.Stop()
	for {
		receipt, ok, readErr := readNavigationReceipt(guardRoot)
		if readErr == nil && ok && receipt.ID == commandID {
			status := receipt.status()
			m.mu.Lock()
			current := m.runtimes[workspaceID]
			alive := current == rt && (current.state == StateRunning || current.state == StateIdle)
			if alive {
				current.navigation = &status
				current.page = receipt.Page
				if receipt.ID > current.navCommandID {
					current.navCommandID = receipt.ID
				}
			}
			m.mu.Unlock()
			if !alive {
				return NavigationStatus{}, fmt.Errorf("%w: runtime changed while navigating", ErrNavigationUnavailable)
			}
			return status, nil
		}
		if !time.Now().Before(deadline) {
			return NavigationStatus{}, fmt.Errorf("%w: command %d was not acknowledged", ErrNavigationTimeout, commandID)
		}
		<-ticker.C
	}
}

// prepareNavigation holds the lifecycle lock only for the runtime check and
// command-file write. The caller waits for the receipt without that lock so a
// slow navigation cannot block input, uploads or other workspace work.
func (m *Manager) prepareNavigation(workspaceID int64, action string, projectID string) (*os.Root, *runtimeState, int64, error) {
	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		m.mu.Unlock()
		return nil, nil, 0, ErrNotFound
	}
	state := rt.state
	lastKnownID := rt.navCommandID
	m.mu.Unlock()
	if state != StateRunning && state != StateIdle {
		return nil, nil, 0, fmt.Errorf("%w: runtime is %s", runtime.ErrNotRunning, state)
	}

	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w: prepare guard state directory: %v", ErrNavigationUnavailable, err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = guardRoot.Close()
		}
	}()

	receipt, hasReceipt, err := readNavigationReceipt(guardRoot)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w: read navigation receipt: %v", ErrNavigationUnavailable, err)
	}
	if hasReceipt && receipt.ID > lastKnownID {
		lastKnownID = receipt.ID
	}
	commandID := lastKnownID + 1
	payload, err := json.Marshal(navigationCommandFile{
		ID:          commandID,
		Action:      action,
		ProjectID:   projectID,
		RequestedAt: m.now().Unix(),
	})
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w: encode navigation command: %v", ErrNavigationUnavailable, err)
	}
	if err := writeStateFileAtomic(guardRoot, navigationCommandFileName, payload); err != nil {
		return nil, nil, 0, fmt.Errorf("%w: write navigation command: %v", ErrNavigationUnavailable, err)
	}

	m.mu.Lock()
	if current := m.runtimes[workspaceID]; current == rt && commandID > current.navCommandID {
		current.navCommandID = commandID
	}
	m.mu.Unlock()
	closed = true
	return guardRoot, rt, commandID, nil
}

func readNavigationReceipt(root *os.Root) (navigationReceipt, bool, error) {
	data, err := root.ReadFile(navigationReceiptFileName)
	if errors.Is(err, os.ErrNotExist) {
		return navigationReceipt{}, false, nil
	}
	if err != nil {
		return navigationReceipt{}, false, err
	}
	var file navigationReceiptFile
	if err := json.Unmarshal(data, &file); err != nil || !file.valid() {
		return navigationReceipt{}, false, nil
	}
	page, _ := file.pageStatus()
	return navigationReceipt{
		ID:           *file.ID,
		CanGoBack:    *file.CanGoBack,
		CanGoForward: *file.CanGoForward,
		UpdatedAt:    *file.UpdatedAt,
		Page:         page,
	}, true, nil
}

// refreshNavigationLocked refreshes the cached receipt while the manager lock is
// held. Missing, corrupt or unreadable files simply leave navigation nil.
func (m *Manager) refreshNavigationLocked(rt *runtimeState, workspaceID int64) {
	root, err := openGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID))
	if err != nil {
		rt.navigation = nil
		rt.page = nil
		rt.projectCreation = nil
		return
	}
	defer root.Close()
	if receipt, ok, err := readNavigationReceipt(root); err == nil && ok {
		status := receipt.status()
		rt.navigation = &status
		rt.page = receipt.Page
		if receipt.ID > rt.navCommandID {
			rt.navCommandID = receipt.ID
		}
	} else {
		rt.navigation = nil
		rt.page = nil
	}
	if creation, ok, err := readProjectCreation(root); err == nil && ok {
		rt.projectCreation = &creation
	} else {
		rt.projectCreation = nil
	}
}
