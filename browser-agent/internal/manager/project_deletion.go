package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const (
	projectDeletionCommandFileName = "project-delete-command.json"
	projectDeletionStatusFileName  = "project-delete.json"
	projectDeletionPollInterval    = 100 * time.Millisecond
	projectDeletionWaitTimeout     = 15 * time.Second
	projectDeletionStateDone       = "DONE"
	projectDeletionStateFailed     = "FAILED"
)

var (
	ErrProjectDeletionUnavailable = errors.New("project deletion unavailable")
	ErrProjectDeletionTimeout     = errors.New("project deletion timeout")
	ErrProjectDeletionRejected    = errors.New("project deletion rejected")
)

type ProjectDeletionStatus struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"project_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

type projectDeletionCommandFile struct {
	ID          int64  `json:"id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	RequestedAt int64  `json:"requested_at"`
}

type projectDeletionStatusFile struct {
	ID        *int64  `json:"id"`
	ProjectID *string `json:"project_id"`
	State     *string `json:"state"`
	Error     *string `json:"error"`
	UpdatedAt *int64  `json:"updated_at"`
}

func (file projectDeletionStatusFile) valid() bool {
	if file.ID == nil || *file.ID < 0 || file.ProjectID == nil || file.State == nil || file.Error == nil || file.UpdatedAt == nil {
		return false
	}
	if *file.State != projectDeletionStateDone && *file.State != projectDeletionStateFailed {
		return false
	}
	if *file.State == projectDeletionStateDone && *file.Error != "" {
		return false
	}
	return *file.UpdatedAt >= 0
}

func (file projectDeletionStatusFile) status() ProjectDeletionStatus {
	return ProjectDeletionStatus{
		ID:        *file.ID,
		ProjectID: *file.ProjectID,
		State:     *file.State,
		Error:     *file.Error,
		UpdatedAt: *file.UpdatedAt,
	}
}

// DeleteProject asks the guard to delete one provider-side project through the
// existing CDP consumer. The local mapping is removed by the caller only after
// this returns success.
func (m *Manager) DeleteProject(ctx context.Context, workspaceID int64, projectID string, projectName string) (ProjectDeletionStatus, error) {
	projectID = strings.TrimSpace(projectID)
	projectName = strings.TrimSpace(projectName)
	if workspaceID <= 0 || !projectIDPattern.MatchString(projectID) || len(projectName) > 255 {
		return ProjectDeletionStatus{}, fmt.Errorf("%w: project deletion request is invalid", ErrInvalidRequest)
	}

	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		m.mu.Unlock()
		return ProjectDeletionStatus{}, ErrNotFound
	}
	state := rt.state
	m.mu.Unlock()
	if state != StateRunning && state != StateIdle {
		return ProjectDeletionStatus{}, fmt.Errorf("%w: runtime is %s", runtime.ErrNotRunning, state)
	}

	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return ProjectDeletionStatus{}, fmt.Errorf("%w: prepare guard state directory: %v", ErrProjectDeletionUnavailable, err)
	}
	defer guardRoot.Close()

	lastID := int64(0)
	if data, err := guardRoot.ReadFile(projectDeletionStatusFileName); err == nil {
		var file projectDeletionStatusFile
		if json.Unmarshal(data, &file) == nil && file.valid() {
			lastID = *file.ID
		}
	}
	commandID := lastID + 1
	payload, err := json.Marshal(projectDeletionCommandFile{
		ID:          commandID,
		ProjectID:   projectID,
		ProjectName: projectName,
		RequestedAt: m.now().Unix(),
	})
	if err != nil {
		return ProjectDeletionStatus{}, fmt.Errorf("%w: encode project deletion command: %v", ErrProjectDeletionUnavailable, err)
	}
	if err := writeStateFileAtomic(guardRoot, projectDeletionCommandFileName, payload); err != nil {
		return ProjectDeletionStatus{}, fmt.Errorf("%w: write project deletion command: %v", ErrProjectDeletionUnavailable, err)
	}

	deadline := time.Now().Add(projectDeletionWaitTimeout)
	ticker := time.NewTicker(projectDeletionPollInterval)
	defer ticker.Stop()
	for {
		status, ok, readErr := readProjectDeletionStatus(guardRoot)
		if readErr == nil && ok {
			if status.ID != commandID || status.ProjectID != projectID || status.UpdatedAt < 0 {
				// A stale receipt must not satisfy this request.
			} else if status.State == projectDeletionStateDone {
				return status, nil
			} else if status.State == projectDeletionStateFailed {
				code := strings.TrimSpace(status.Error)
				if code == "" {
					code = "ERR_PROJECT_DELETE_FAILED"
				}
				return status, fmt.Errorf("%w: %s", ErrProjectDeletionRejected, code)
			}
		}
		if !time.Now().Before(deadline) {
			return ProjectDeletionStatus{}, fmt.Errorf("%w: command %d was not acknowledged", ErrProjectDeletionTimeout, commandID)
		}
		select {
		case <-ctx.Done():
			return ProjectDeletionStatus{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func readProjectDeletionStatus(root *os.Root) (ProjectDeletionStatus, bool, error) {
	data, err := root.ReadFile(projectDeletionStatusFileName)
	if errors.Is(err, os.ErrNotExist) {
		return ProjectDeletionStatus{}, false, nil
	}
	if err != nil {
		return ProjectDeletionStatus{}, false, err
	}
	var file projectDeletionStatusFile
	if err := json.Unmarshal(data, &file); err != nil || !file.valid() {
		return ProjectDeletionStatus{}, false, nil
	}
	return file.status(), true, nil
}
