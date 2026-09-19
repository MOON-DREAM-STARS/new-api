package manager

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
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
	projectCreationName    = "project-creation.json"
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
// DisplayName is empty for the legacy manual flow, where the operator creates
// the project inside the remote browser themselves.
type guardPermit struct {
	PermitID    string `json:"permit_id"`
	Kind        string `json:"kind"`
	IssuedAt    int64  `json:"issued_at"`
	ExpiresAt   int64  `json:"expires_at"`
	DisplayName string `json:"display_name"`
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

// The runtime container runs as the identity that owns the workspace mount, so
// a runtime process can replace any entry below /workspace - including .guard
// and the guard state files - with a symbolic link. Every agent side read and
// write therefore resolves names below an open workspace root instead of a
// path: names that leave the workspace fail closed, and the workspace entry
// itself must be a real directory. (checklist §31 symlink/path traversal)

// openWorkspaceRoot opens the workspace directory of a workspace without
// following a symbolic link and returns a handle scoped to it. The data root is
// opened first, the workspace entry is required to be a real directory that is
// still the directory that was opened, and every later operation on the handle
// refuses a name that resolves outside the workspace.
func openWorkspaceRoot(workspaceDir string) (*os.Root, error) {
	parent, err := os.OpenRoot(filepath.Dir(workspaceDir))
	if err != nil {
		return nil, err
	}
	defer parent.Close()

	name := filepath.Base(workspaceDir)
	entry, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir() {
		return nil, fmt.Errorf("workspace path %s is not a directory", workspaceDir)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if !os.SameFile(entry, opened) {
		_ = root.Close()
		return nil, fmt.Errorf("workspace path %s changed while it was opened", workspaceDir)
	}
	return root, nil
}

// ensureWorkspaceDir makes sure the workspace directory exists as a real
// directory and returns a handle scoped to it.
func ensureWorkspaceDir(workspaceDir string) (*os.Root, error) {
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		return nil, err
	}
	return openWorkspaceRoot(workspaceDir)
}

// EnsureWorkspaceDirs creates the workspace directory tree if it is missing,
// keeps the profile directory across runtime restarts and hands the tree to the
// non-root identity the runtime image runs as (runtime.RuntimeUID /
// runtime.RuntimeGID). The runtime container cannot write /workspace otherwise.
//
// chown performs the ownership handover; nil selects (*os.Root).Chown. The
// handover fails closed: a chown error is tolerated only when the path already
// belongs to the runtime identity, which is the normal case when the agent
// itself runs as that identity and is not allowed to chown. Every directory is
// created, checked and handed over below an open workspace root, so a directory
// that was replaced by a symbolic link is refused instead of being followed.
// Every other failure is returned to the caller.
func EnsureWorkspaceDirs(workspaceDir string, chown func(root *os.Root, name string, uid int, gid int) error) error {
	return ensureWorkspaceDirs(workspaceDir, chown, ownerOfRoot)
}

func ensureWorkspaceDirs(workspaceDir string, chown func(root *os.Root, name string, uid int, gid int) error, owner func(root *os.Root, name string) (int, int, bool)) error {
	root, err := ensureWorkspaceDir(workspaceDir)
	if err != nil {
		return err
	}
	defer root.Close()

	names := workspaceTreeNames()
	for _, name := range names {
		if name == "." {
			continue
		}
		if err := root.MkdirAll(name, 0o700); err != nil {
			return err
		}
		entry, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return fmt.Errorf("workspace path %s is not a directory", filepath.Join(workspaceDir, name))
		}
	}
	for _, name := range names {
		if err := handOverToRuntime(root, name, chown, owner); err != nil {
			return err
		}
	}
	return nil
}

// ensureGuardStateDir creates the guard state directory when it is missing and
// hands it to the runtime identity the guard runs as. The returned handle is
// scoped to .guard: a .guard entry that is not a real directory is refused, so
// the guard state the agent writes always belongs to that workspace.
func ensureGuardStateDir(workspaceDir string, chown func(root *os.Root, name string, uid int, gid int) error) (*os.Root, error) {
	root, err := ensureWorkspaceDir(workspaceDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	if err := root.MkdirAll(guardStateDirName, 0o700); err != nil {
		return nil, err
	}
	entry, err := root.Lstat(guardStateDirName)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir() {
		return nil, fmt.Errorf("guard state path %s is not a directory", guardStateDir(workspaceDir))
	}
	if err := handOverToRuntime(root, guardStateDirName, chown, ownerOfRoot); err != nil {
		return nil, err
	}
	return root.OpenRoot(guardStateDirName)
}

// openGuardStateDir opens the guard state directory of an existing workspace
// without creating anything. A missing workspace directory or guard state
// directory reports os.ErrNotExist; a .guard entry that is not a real directory
// is an error.
func openGuardStateDir(workspaceDir string) (*os.Root, error) {
	root, err := openWorkspaceRoot(workspaceDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	entry, err := root.Lstat(guardStateDirName)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir() {
		return nil, fmt.Errorf("guard state path %s is not a directory", guardStateDir(workspaceDir))
	}
	return root.OpenRoot(guardStateDirName)
}

// handOverToRuntime gives a directory below an open workspace root to the
// runtime identity. A chown error is tolerated only when the directory already
// belongs to that identity.
func handOverToRuntime(root *os.Root, name string, chown func(root *os.Root, name string, uid int, gid int) error, owner func(root *os.Root, name string) (int, int, bool)) error {
	if chown == nil {
		chown = (*os.Root).Chown
	}
	if err := chown(root, name, runtime.RuntimeUID, runtime.RuntimeGID); err != nil {
		uid, gid, known := owner(root, name)
		if !known || uid != runtime.RuntimeUID || gid != runtime.RuntimeGID {
			return fmt.Errorf("hand over %s to %d:%d: %w", rootName(root, name), runtime.RuntimeUID, runtime.RuntimeGID, err)
		}
	}
	return nil
}

// rootName renders a name below an open root for error messages.
func rootName(root *os.Root, name string) string {
	if name == "." {
		return root.Name()
	}
	return filepath.Join(root.Name(), name)
}

// writeStateFileAtomic replaces a guard state file through a temporary file in
// the same directory, so the guard only ever reads the old or the new file. The
// replacement is left readable by other identities on purpose: the guard runs as
// uid 10001 while the agent may write as root, and the 0700 state directory
// already keeps every other identity out of the directory. Both names resolve
// below the guard root, so a state file that was replaced by a link out of the
// workspace is replaced rather than followed.
func writeStateFileAtomic(root *os.Root, name string, payload []byte) error {
	temp, file, err := createStateTemp(root, name)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		_ = root.Remove(temp)
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		_ = root.Remove(temp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = root.Remove(temp)
		return err
	}
	if err := root.Rename(temp, name); err != nil {
		_ = root.Remove(temp)
		return err
	}
	return nil
}

// createStateTemp creates an exclusive temporary file for a guard state write.
func createStateTemp(root *os.Root, name string) (string, *os.File, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", nil, err
	}
	temp := name + "." + hex.EncodeToString(suffix[:]) + ".tmp"
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, err
	}
	return temp, file, nil
}

// readObservationOffset returns the acknowledged observation offset. A missing,
// unparsable or negative offset restarts at the beginning of the file: the
// control plane applies observations idempotently, so re-reading is safe.
func readObservationOffset(root *os.Root) int64 {
	data, err := root.ReadFile(observationsOffsetName)
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
func observationFileSize(root *os.Root) (int64, error) {
	info, err := root.Stat(observationsFileName)
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
func readObservationLines(root *os.Root) ([]json.RawMessage, int64, error) {
	observations := make([]json.RawMessage, 0)
	offset := readObservationOffset(root)
	data, err := root.ReadFile(observationsFileName)
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

// workspaceTreeNames lists the workspace directory itself and its fixed
// subdirectories in creation order, as names below an open workspace root.
func workspaceTreeNames() []string {
	names := make([]string, 0, len(workspaceSubdirectories)+1)
	names = append(names, ".")
	names = append(names, workspaceSubdirectories...)
	return names
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

// ownerOfRoot reports the owning uid and gid of a directory below an open
// workspace root. known is false when the path cannot be inspected or the
// platform has no POSIX owner model.
func ownerOfRoot(root *os.Root, name string) (int, int, bool) {
	info, err := root.Lstat(name)
	if err != nil {
		return 0, 0, false
	}
	return fileUIDGID(info)
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
