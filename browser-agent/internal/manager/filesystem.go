package manager

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// Guard state layout (phase 4 contract §2). The guard appends observations.jsonl
// itself; the agent owns every other file below.
const (
	guardStateDirName      = ".guard"
	ownershipFileName      = "ownership.json"
	permitFileName         = "permit.json"
	permitConsumedFileName = "permit.consumed"
	observationsFileName   = "observations.jsonl"
	observationsOffsetName = "observations.offset"
)

// guardOwnership is the on-disk ownership.json shape. The guard only reads it.
type guardOwnership struct {
	Generation    int64    `json:"generation"`
	Projects      []string `json:"projects"`
	Conversations []string `json:"conversations"`
	UpdatedAt     int64    `json:"updated_at"`
}

// guardPermit is the on-disk permit.json shape. The guard only reads it.
type guardPermit struct {
	PermitID  string `json:"permit_id"`
	Kind      string `json:"kind"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// guardConsumedPermit is the permit.consumed shape the guard writes.
type guardConsumedPermit struct {
	PermitID string `json:"permit_id"`
}

// workspaceSubdirectories is the frozen per-workspace data layout. Only the
// agent derives these paths, and it derives them from an integer workspace id.
var workspaceSubdirectories = []string{"profile", "uploads", "downloads", "cache", "tmp", guardStateDirName}

// WorkspaceDir returns the host directory of a workspace. The path is derived
// from the integer id only; client supplied paths are never accepted.
func WorkspaceDir(dataRoot string, workspaceID int64) string {
	return filepath.Join(dataRoot, fmt.Sprintf("workspace-%d", workspaceID))
}

// guardStateDir returns the guard state directory of a workspace directory.
func guardStateDir(workspaceDir string) string {
	return filepath.Join(workspaceDir, guardStateDirName)
}

// EnsureWorkspaceDirs creates the workspace directory tree if it is missing,
// keeps the profile directory across runtime restarts and hands the tree to the
// non-root identity the runtime image runs as (runtime.RuntimeUID /
// runtime.RuntimeGID). The runtime container cannot write /workspace otherwise.
//
// chown performs the ownership handover; nil selects the process default
// (os.Chown). The handover fails closed: a chown error is tolerated only when
// the path already belongs to the runtime identity, which is the normal case
// when the agent itself runs as that identity and is not allowed to chown.
// Every other failure is returned to the caller.
func EnsureWorkspaceDirs(workspaceDir string, chown func(path string, uid int, gid int) error) error {
	return ensureWorkspaceDirs(workspaceDir, chown, ownerOf)
}

func ensureWorkspaceDirs(workspaceDir string, chown func(path string, uid int, gid int) error, owner func(path string) (int, int, bool)) error {
	if chown == nil {
		chown = os.Chown
	}
	targets := workspaceTreePaths(workspaceDir)
	for _, path := range targets {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	for _, path := range targets {
		if err := handOverToRuntime(path, chown, owner); err != nil {
			return err
		}
	}
	return nil
}

// ensureGuardStateDir creates the guard state directory when it is missing and
// hands it to the runtime identity the guard runs as. A directory that cannot be
// created or handed over is an error, never a silent skip: the guard must be
// able to append observations.jsonl next to the state files the agent writes.
func ensureGuardStateDir(workspaceDir string, chown func(path string, uid int, gid int) error) (string, error) {
	if chown == nil {
		chown = os.Chown
	}
	dir := guardStateDir(workspaceDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := handOverToRuntime(dir, chown, ownerOf); err != nil {
		return "", err
	}
	return dir, nil
}

// handOverToRuntime gives a workspace path to the runtime identity. A chown
// error is tolerated only when the path already belongs to that identity.
func handOverToRuntime(path string, chown func(path string, uid int, gid int) error, owner func(path string) (int, int, bool)) error {
	if err := chown(path, runtime.RuntimeUID, runtime.RuntimeGID); err != nil {
		uid, gid, known := owner(path)
		if !known || uid != runtime.RuntimeUID || gid != runtime.RuntimeGID {
			return fmt.Errorf("hand over %s to %d:%d: %w", path, runtime.RuntimeUID, runtime.RuntimeGID, err)
		}
	}
	return nil
}

// writeStateFileAtomic replaces a guard state file through a temporary file in
// the same directory, so the guard only ever reads the old or the new file. The
// replacement is left readable by other identities on purpose: the guard runs as
// uid 10001 while the agent may write as root, and the 0700 state directory
// already keeps every other identity out of the directory.
func writeStateFileAtomic(dir string, name string, payload []byte) error {
	temp, err := os.CreateTemp(dir, name+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

// readObservationOffset returns the acknowledged observation offset. A missing,
// unparsable or negative offset restarts at the beginning of the file: the
// control plane applies observations idempotently, so re-reading is safe.
func readObservationOffset(dir string) int64 {
	data, err := os.ReadFile(filepath.Join(dir, observationsOffsetName))
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

// observationFileSize reports the size of the guard observation file. A missing
// file counts as empty so a workspace whose guard never observed anything still
// supports both a pull and an ack.
func observationFileSize(dir string) (int64, error) {
	info, err := os.Stat(filepath.Join(dir, observationsFileName))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// readObservationLines returns the complete observation objects that follow the
// acknowledged offset, together with the offset that follows them. A trailing
// line without a newline is an in-flight guard append and is left unread, so the
// acknowledged offset never advances past an incomplete line.
func readObservationLines(dir string) ([]json.RawMessage, int64, error) {
	observations := make([]json.RawMessage, 0)
	offset := readObservationOffset(dir)
	data, err := os.ReadFile(filepath.Join(dir, observationsFileName))
	if errors.Is(err, os.ErrNotExist) {
		return observations, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	chunk := data[offset:]
	end := bytes.LastIndexByte(chunk, '\n')
	if end < 0 {
		return observations, offset, nil
	}
	for _, line := range bytes.Split(chunk[:end+1], []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if !isJSONObject(trimmed) {
			return nil, 0, fmt.Errorf("observation line is not a JSON object")
		}
		observations = append(observations, json.RawMessage(append([]byte(nil), trimmed...)))
	}
	return observations, offset + int64(end) + 1, nil
}

// isJSONObject reports whether a complete observation line is a JSON object.
func isJSONObject(line []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(line, &object) == nil
}

// workspaceTreePaths lists the workspace directory and its fixed subdirectories
// in creation order.
func workspaceTreePaths(workspaceDir string) []string {
	paths := make([]string, 0, len(workspaceSubdirectories)+1)
	paths = append(paths, workspaceDir)
	for _, name := range workspaceSubdirectories {
		paths = append(paths, filepath.Join(workspaceDir, name))
	}
	return paths
}

// ownerOf reports the owning uid and gid of a path. known is false when the
// path cannot be inspected or the platform has no POSIX owner model.
func ownerOf(path string) (int, int, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, false
	}
	return fileUIDGID(info)
}
