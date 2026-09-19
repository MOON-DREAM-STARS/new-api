package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const (
	navigationCommandFileName = "command.json"
	navigationReceiptFileName = "navigation.json"
	navigationPollInterval    = 100 * time.Millisecond
	navigationWaitTimeout     = 2 * time.Second
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

type navigationCommandFile struct {
	ID          int64  `json:"id"`
	Action      string `json:"action"`
	RequestedAt int64  `json:"requested_at"`
}

type navigationReceipt struct {
	ID           int64
	CanGoBack    bool
	CanGoForward bool
	UpdatedAt    int64
}

type navigationReceiptFile struct {
	ID           *int64 `json:"id"`
	CanGoBack    *bool  `json:"can_go_back"`
	CanGoForward *bool  `json:"can_go_forward"`
	UpdatedAt    *int64 `json:"updated_at"`
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

func validNavigationAction(action string) bool {
	switch action {
	case "back", "forward", "reload", "state":
		return true
	default:
		return false
	}
}

// Navigate writes one navigation command and waits for the guard's matching
// receipt. It never synthesises a successful state when the guard is silent.
func (m *Manager) Navigate(workspaceID int64, action string) (NavigationStatus, error) {
	if workspaceID <= 0 || !validNavigationAction(action) {
		return NavigationStatus{}, fmt.Errorf("%w: navigation request is invalid", ErrInvalidRequest)
	}

	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		m.mu.Unlock()
		return NavigationStatus{}, ErrNotFound
	}
	state := rt.state
	lastKnownID := rt.navCommandID
	m.mu.Unlock()
	if state != StateRunning && state != StateIdle {
		return NavigationStatus{}, fmt.Errorf("%w: runtime is %s", runtime.ErrNotRunning, state)
	}

	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return NavigationStatus{}, fmt.Errorf("%w: prepare guard state directory: %v", ErrNavigationUnavailable, err)
	}
	defer guardRoot.Close()

	receipt, hasReceipt, err := readNavigationReceipt(guardRoot)
	if err != nil {
		return NavigationStatus{}, fmt.Errorf("%w: read navigation receipt: %v", ErrNavigationUnavailable, err)
	}
	if hasReceipt && receipt.ID > lastKnownID {
		lastKnownID = receipt.ID
	}
	commandID := lastKnownID + 1
	payload, err := json.Marshal(navigationCommandFile{
		ID:          commandID,
		Action:      action,
		RequestedAt: m.now().Unix(),
	})
	if err != nil {
		return NavigationStatus{}, fmt.Errorf("%w: encode navigation command: %v", ErrNavigationUnavailable, err)
	}
	if err := writeStateFileAtomic(guardRoot, navigationCommandFileName, payload); err != nil {
		return NavigationStatus{}, fmt.Errorf("%w: write navigation command: %v", ErrNavigationUnavailable, err)
	}

	m.mu.Lock()
	if current := m.runtimes[workspaceID]; current == rt && commandID > current.navCommandID {
		current.navCommandID = commandID
	}
	m.mu.Unlock()

	deadline := time.Now().Add(navigationWaitTimeout)
	ticker := time.NewTicker(navigationPollInterval)
	defer ticker.Stop()
	for {
		receipt, ok, readErr := readNavigationReceipt(guardRoot)
		if readErr == nil && ok && receipt.ID == commandID {
			status := receipt.status()
			m.mu.Lock()
			if current := m.runtimes[workspaceID]; current == rt {
				current.navigation = &status
				if receipt.ID > current.navCommandID {
					current.navCommandID = receipt.ID
				}
			}
			m.mu.Unlock()
			return status, nil
		}
		if !time.Now().Before(deadline) {
			return NavigationStatus{}, fmt.Errorf("%w: command %d was not acknowledged", ErrNavigationTimeout, commandID)
		}
		<-ticker.C
	}
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
	return navigationReceipt{
		ID:           *file.ID,
		CanGoBack:    *file.CanGoBack,
		CanGoForward: *file.CanGoForward,
		UpdatedAt:    *file.UpdatedAt,
	}, true, nil
}

// refreshNavigationLocked refreshes the cached receipt while the manager lock is
// held. Missing, corrupt or unreadable files simply leave navigation nil.
func (m *Manager) refreshNavigationLocked(rt *runtimeState, workspaceID int64) {
	root, err := openGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID))
	if err != nil {
		rt.navigation = nil
		return
	}
	defer root.Close()
	receipt, ok, err := readNavigationReceipt(root)
	if err != nil || !ok {
		rt.navigation = nil
		return
	}
	status := receipt.status()
	rt.navigation = &status
	if receipt.ID > rt.navCommandID {
		rt.navCommandID = receipt.ID
	}
}
