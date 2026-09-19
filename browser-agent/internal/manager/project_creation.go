package manager

import (
	"encoding/json"
	"errors"
	"os"
)

// Project creation states the guard publishes. A snapshot only exposes these
// three values; anything else is reported as absent.
const (
	ProjectCreationStateRunning = "RUNNING"
	ProjectCreationStateCreated = "CREATED"
	ProjectCreationStateFailed  = "FAILED"
)

// ProjectCreation is the URL-free creation state exposed in a runtime snapshot.
// It carries no provider identifier, address or credential.
type ProjectCreation struct {
	PermitID  string `json:"permit_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

// projectCreationFile mirrors the project-creation.json the guard writes. Every
// field is a pointer so a missing field is refused instead of defaulting.
type projectCreationFile struct {
	PermitID  *string `json:"permit_id"`
	State     *string `json:"state"`
	Error     *string `json:"error"`
	UpdatedAt *int64  `json:"updated_at"`
}

// readProjectCreation reads the guard creation state. A missing, unreadable or
// malformed file reports ok=false, which leaves the snapshot without a creation
// state instead of publishing a value the guard never wrote.
func readProjectCreation(root *os.Root) (ProjectCreation, bool, error) {
	data, err := root.ReadFile(projectCreationName)
	if errors.Is(err, os.ErrNotExist) {
		return ProjectCreation{}, false, nil
	}
	if err != nil {
		return ProjectCreation{}, false, err
	}
	var file projectCreationFile
	if err := json.Unmarshal(data, &file); err != nil {
		return ProjectCreation{}, false, nil
	}
	if file.PermitID == nil || file.State == nil || file.Error == nil || file.UpdatedAt == nil {
		return ProjectCreation{}, false, nil
	}
	state := *file.State
	switch state {
	case ProjectCreationStateRunning, ProjectCreationStateCreated, ProjectCreationStateFailed:
	default:
		return ProjectCreation{}, false, nil
	}
	if *file.UpdatedAt < 0 || !validProjectCreationError(*file.Error) {
		return ProjectCreation{}, false, nil
	}
	return ProjectCreation{
		PermitID:  *file.PermitID,
		State:     state,
		Error:     *file.Error,
		UpdatedAt: *file.UpdatedAt,
	}, true, nil
}

// validProjectCreationError accepts the empty string and the ERR_* code shape
// the guard uses for every page and creation failure.
func validProjectCreationError(value string) bool {
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
