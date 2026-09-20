package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

const (
	fileChooserStateName   = "file-chooser.json"
	fileChooserCommandName = "file-chooser-command.json"

	fileChooserPollInterval       = 100 * time.Millisecond
	fileChooserWaitTimeout        = 30 * time.Second
	fileChooserSuccessCleanupWait = 60 * time.Second

	fileChooserMaxFiles          = 5
	fileChooserMaxFileBytes      = int64(50 << 20)
	fileChooserMaxWorkspaceBytes = int64(250 << 20)

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
	fileChooserErrorBridgeBusy   = "WEB_WORKSPACE_FILE_BRIDGE_BUSY"
	fileChooserErrorTooLarge     = "WEB_WORKSPACE_FILE_TOO_LARGE"
	fileChooserErrorLimit        = "WEB_WORKSPACE_FILE_LIMIT_EXCEEDED"
	fileChooserErrorInjectFailed = "WEB_WORKSPACE_FILE_INJECT_FAILED"
)

var (
	ErrFileChooserExpired = errors.New("file chooser expired")
	ErrFileTooLarge       = errors.New("file too large")
	ErrFileLimitExceeded  = errors.New("file limit exceeded")
	ErrFileInjectFailed   = errors.New("file injection failed")
	ErrFileBridgeBusy     = errors.New("file bridge busy")
	fileChooserIDPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)
)

// FileChooser is the URL-free pending chooser handed to the browser client. It
// never contains the CDP backend node id, session id or a filesystem path.
type FileChooser struct {
	ChooserID string `json:"chooser_id"`
	Mode      string `json:"mode"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// FileUpload is one streaming multipart part. The caller owns the reader and
// the manager never buffers the body.
type FileUpload struct {
	Name   string
	Reader io.Reader
}

// FileUploadSource yields multipart file parts one at a time.
type FileUploadSource interface {
	Next() (FileUpload, error)
}

type FileChooserResult struct {
	ChooserID string `json:"chooser_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

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

type fileChooserCommandFile struct {
	ID          int64    `json:"id"`
	ChooserID   string   `json:"chooser_id"`
	Action      string   `json:"action"`
	Files       []string `json:"files,omitempty"`
	RequestedAt int64    `json:"requested_at"`
}

func (file fileChooserStateFile) valid() bool {
	if file.ID == nil || *file.ID < 0 || file.ChooserID == nil || file.State == nil || file.Error == nil ||
		file.Mode == nil || file.BackendNodeID == nil || file.SessionID == nil ||
		file.CreatedAt == nil || file.ExpiresAt == nil || file.UpdatedAt == nil {
		return false
	}
	if !fileChooserIDPattern.MatchString(*file.ChooserID) || *file.BackendNodeID <= 0 || strings.TrimSpace(*file.SessionID) == "" {
		return false
	}
	switch *file.State {
	case fileChooserStatePending, fileChooserStateDone, fileChooserStateCancelled, fileChooserStateFailed, fileChooserStateExpired:
	default:
		return false
	}
	switch *file.Mode {
	case fileChooserModeSingle, fileChooserModeMultiple:
	default:
		return false
	}
	return *file.CreatedAt >= 0 && *file.ExpiresAt >= *file.CreatedAt && *file.UpdatedAt >= 0
}

func (file fileChooserStateFile) value() fileChooserState {
	return fileChooserState{
		ID:            *file.ID,
		ChooserID:     *file.ChooserID,
		State:         *file.State,
		Error:         *file.Error,
		Mode:          *file.Mode,
		BackendNodeID: *file.BackendNodeID,
		SessionID:     *file.SessionID,
		CreatedAt:     *file.CreatedAt,
		ExpiresAt:     *file.ExpiresAt,
		UpdatedAt:     *file.UpdatedAt,
	}
}

func readFileChooserState(root *os.Root) (fileChooserState, bool, error) {
	data, err := root.ReadFile(fileChooserStateName)
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

func readFileChooserCommandID(root *os.Root) int64 {
	data, err := root.ReadFile(fileChooserCommandName)
	if err != nil {
		return 0
	}
	var file fileChooserCommandFile
	if json.Unmarshal(data, &file) != nil || file.ID <= 0 {
		return 0
	}
	return file.ID
}

func nextFileChooserCommandID(root *os.Root) int64 {
	last := readFileChooserCommandID(root)
	if state, ok, err := readFileChooserState(root); err == nil && ok && state.ID > last {
		last = state.ID
	}
	return last + 1
}

// WaitFileChooser long-polls the guard state for one pending chooser. A timeout
// returns (nil, nil); an expired or malformed pending state is a real error.
func (m *Manager) WaitFileChooser(ctx context.Context, workspaceID int64, wait time.Duration) (*FileChooser, error) {
	if workspaceID <= 0 {
		return nil, fmt.Errorf("%w: workspace id is invalid", ErrInvalidRequest)
	}
	if wait < 0 {
		wait = 0
	}
	state, err := m.liveRuntimeState(workspaceID)
	if err != nil {
		return nil, err
	}
	if state != StateRunning && state != StateIdle {
		return nil, fmt.Errorf("%w: runtime is %s", runtime.ErrNotRunning, state)
	}

	deadline := time.Now().Add(wait)
	ticker := time.NewTicker(fileChooserPollInterval)
	defer ticker.Stop()
	for {
		guardRoot, openErr := openGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID))
		if openErr == nil {
			current, ok, readErr := readFileChooserState(guardRoot)
			_ = guardRoot.Close()
			if readErr != nil {
				return nil, fmt.Errorf("%w: read file chooser state: %v", ErrFileInjectFailed, readErr)
			}
			if ok && current.State == fileChooserStatePending {
				if current.ExpiresAt <= m.now().Unix() {
					return nil, ErrFileChooserExpired
				}
				return &FileChooser{ChooserID: current.ChooserID, Mode: current.Mode, CreatedAt: current.CreatedAt, ExpiresAt: current.ExpiresAt}, nil
			}
		} else if !errors.Is(openErr, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: open guard state directory: %v", ErrNotFound, openErr)
		}
		if !time.Now().Before(deadline) {
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// UploadFileChooserFiles stages streaming multipart parts and asks the guard to
// inject them into the real provider file input. Files are never buffered whole.
func (m *Manager) UploadFileChooserFiles(ctx context.Context, workspaceID int64, chooserID string, source FileUploadSource) (FileChooserResult, error) {
	chooserID = strings.TrimSpace(chooserID)
	if workspaceID <= 0 || !fileChooserIDPattern.MatchString(chooserID) || source == nil {
		return FileChooserResult{}, fmt.Errorf("%w: file chooser request is invalid", ErrInvalidRequest)
	}

	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()
	if err := m.requireLiveRuntime(workspaceID); err != nil {
		return FileChooserResult{}, err
	}
	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: prepare guard state directory: %v", ErrFileInjectFailed, err)
	}
	defer guardRoot.Close()
	current, ok, err := readFileChooserState(guardRoot)
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: read file chooser state: %v", ErrFileInjectFailed, err)
	}
	if !ok || current.State != fileChooserStatePending {
		return FileChooserResult{}, ErrFileChooserExpired
	}
	if current.ChooserID != chooserID {
		return FileChooserResult{}, ErrFileBridgeBusy
	}
	if current.ExpiresAt <= m.now().Unix() {
		return FileChooserResult{}, ErrFileChooserExpired
	}

	workspaceRoot, err := ensureWorkspaceDir(WorkspaceDir(m.dataRoot, workspaceID))
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: open workspace: %v", ErrFileInjectFailed, err)
	}
	defer workspaceRoot.Close()
	if err := ensureFileChooserStagingDirs(workspaceRoot, chooserID, m.chown); err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: prepare staging directory: %v", ErrFileInjectFailed, err)
	}
	if err := resetFileChooserStagingDir(workspaceRoot, chooserID); err != nil {
		return FileChooserResult{}, err
	}
	existingBytes, err := fileChooserStagedBytes(workspaceRoot)
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: inspect staging bytes: %v", ErrFileInjectFailed, err)
	}

	stagedNames := make([]string, 0, fileChooserMaxFiles)
	var stagedBytes int64
	cleanup := func() { _ = removeFileChooserStagingFiles(workspaceRoot, chooserID, stagedNames) }

	fileCount := 0
	for {
		upload, nextErr := source.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			cleanup()
			return FileChooserResult{}, fmt.Errorf("%w: read multipart file: %v", ErrFileInjectFailed, nextErr)
		}
		fileCount++
		if fileCount > fileChooserMaxFiles {
			cleanup()
			return FileChooserResult{}, ErrFileLimitExceeded
		}
		safeName := safeFileChooserName(upload.Name)
		if safeName == "" {
			cleanup()
			return FileChooserResult{}, fmt.Errorf("%w: invalid file name", ErrInvalidRequest)
		}
		safeName = uniqueFileChooserName(workspaceRoot, chooserID, safeName, stagedNames)
		remainingBytes := fileChooserMaxWorkspaceBytes - existingBytes - stagedBytes
		if remainingBytes <= 0 {
			cleanup()
			return FileChooserResult{}, ErrFileLimitExceeded
		}
		fileLimitBytes := fileChooserMaxFileBytes
		if remainingBytes < fileLimitBytes {
			fileLimitBytes = remainingBytes
		}
		stagedPath := path.Join("uploads", ".bridge", chooserID, safeName)
		file, createErr := workspaceRoot.OpenFile(stagedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if createErr != nil {
			if closer, ok := upload.Reader.(io.Closer); ok {
				_ = closer.Close()
			}
			cleanup()
			return FileChooserResult{}, fmt.Errorf("%w: create staged file: %v", ErrFileInjectFailed, createErr)
		}
		stagedNames = append(stagedNames, safeName)
		written, copyErr := io.Copy(file, io.LimitReader(upload.Reader, fileLimitBytes+1))
		closeErr := file.Close()
		if closer, ok := upload.Reader.(io.Closer); ok {
			_ = closer.Close()
		}
		if copyErr != nil {
			cleanup()
			return FileChooserResult{}, fmt.Errorf("%w: stage file: %v", ErrFileInjectFailed, copyErr)
		}
		if closeErr != nil {
			cleanup()
			return FileChooserResult{}, fmt.Errorf("%w: close staged file: %v", ErrFileInjectFailed, closeErr)
		}
		if written > fileLimitBytes {
			cleanup()
			if fileLimitBytes < fileChooserMaxFileBytes {
				return FileChooserResult{}, ErrFileLimitExceeded
			}
			return FileChooserResult{}, ErrFileTooLarge
		}
		stagedBytes += written
	}
	if fileCount == 0 {
		cleanup()
		return FileChooserResult{}, fmt.Errorf("%w: at least one file is required", ErrInvalidRequest)
	}

	commandID := nextFileChooserCommandID(guardRoot)
	command := fileChooserCommandFile{
		ID:          commandID,
		ChooserID:   chooserID,
		Action:      fileChooserActionAttach,
		Files:       fileChooserContainerPaths(chooserID, stagedNames),
		RequestedAt: m.now().Unix(),
	}
	payload, err := json.Marshal(command)
	if err != nil {
		cleanup()
		return FileChooserResult{}, fmt.Errorf("%w: encode command: %v", ErrFileInjectFailed, err)
	}
	if err := writeStateFileAtomic(guardRoot, fileChooserCommandName, payload); err != nil {
		cleanup()
		return FileChooserResult{}, fmt.Errorf("%w: write command: %v", ErrFileInjectFailed, err)
	}
	result, err := m.waitFileChooserResult(ctx, guardRoot, commandID, chooserID)
	if err != nil {
		cleanup()
		return FileChooserResult{}, err
	}
	if result.State != fileChooserStateDone {
		cleanup()
		return FileChooserResult{}, fileChooserResultError(result)
	}
	m.scheduleFileChooserCleanup(workspaceID, chooserID, stagedNames)
	return result, nil
}

// CancelFileChooser dismisses a pending chooser through the same guard bridge.
func (m *Manager) CancelFileChooser(ctx context.Context, workspaceID int64, chooserID string) (FileChooserResult, error) {
	chooserID = strings.TrimSpace(chooserID)
	if workspaceID <= 0 || !fileChooserIDPattern.MatchString(chooserID) {
		return FileChooserResult{}, fmt.Errorf("%w: file chooser request is invalid", ErrInvalidRequest)
	}
	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()
	if err := m.requireLiveRuntime(workspaceID); err != nil {
		return FileChooserResult{}, err
	}
	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: prepare guard state directory: %v", ErrFileInjectFailed, err)
	}
	defer guardRoot.Close()
	current, ok, err := readFileChooserState(guardRoot)
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: read file chooser state: %v", ErrFileInjectFailed, err)
	}
	if !ok {
		return FileChooserResult{}, ErrFileChooserExpired
	}
	if current.ChooserID != chooserID {
		return FileChooserResult{}, ErrFileBridgeBusy
	}
	if current.State != fileChooserStatePending || current.ExpiresAt <= m.now().Unix() {
		_ = m.cleanupFileChooserStaging(workspaceID, chooserID)
		return FileChooserResult{}, ErrFileChooserExpired
	}
	commandID := nextFileChooserCommandID(guardRoot)
	payload, err := json.Marshal(fileChooserCommandFile{
		ID:          commandID,
		ChooserID:   chooserID,
		Action:      fileChooserActionCancel,
		RequestedAt: m.now().Unix(),
	})
	if err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: encode cancel command: %v", ErrFileInjectFailed, err)
	}
	if err := writeStateFileAtomic(guardRoot, fileChooserCommandName, payload); err != nil {
		return FileChooserResult{}, fmt.Errorf("%w: write cancel command: %v", ErrFileInjectFailed, err)
	}
	result, err := m.waitFileChooserResult(ctx, guardRoot, commandID, chooserID)
	if err != nil {
		_ = m.cleanupFileChooserStaging(workspaceID, chooserID)
		return FileChooserResult{}, err
	}
	_ = m.cleanupFileChooserStaging(workspaceID, chooserID)
	if result.State != fileChooserStateCancelled {
		return result, fileChooserResultError(result)
	}
	return result, nil
}

func (m *Manager) waitFileChooserResult(ctx context.Context, guardRoot *os.Root, commandID int64, chooserID string) (FileChooserResult, error) {
	deadline := time.Now().Add(fileChooserWaitTimeout)
	ticker := time.NewTicker(fileChooserPollInterval)
	defer ticker.Stop()
	for {
		state, ok, err := readFileChooserState(guardRoot)
		if err != nil {
			return FileChooserResult{}, fmt.Errorf("%w: read chooser result: %v", ErrFileInjectFailed, err)
		}
		if ok && state.ID == commandID && state.ChooserID == chooserID {
			switch state.State {
			case fileChooserStateDone, fileChooserStateCancelled:
				return FileChooserResult{ChooserID: state.ChooserID, State: state.State, Error: state.Error, UpdatedAt: state.UpdatedAt}, nil
			case fileChooserStateExpired:
				return FileChooserResult{ChooserID: state.ChooserID, State: state.State, Error: state.Error, UpdatedAt: state.UpdatedAt}, ErrFileChooserExpired
			case fileChooserStateFailed:
				result := FileChooserResult{ChooserID: state.ChooserID, State: state.State, Error: state.Error, UpdatedAt: state.UpdatedAt}
				return result, fileChooserResultError(result)
			}
		}
		if !time.Now().Before(deadline) {
			return FileChooserResult{}, fmt.Errorf("%w: guard did not acknowledge command %d", ErrFileInjectFailed, commandID)
		}
		select {
		case <-ctx.Done():
			return FileChooserResult{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func fileChooserResultError(result FileChooserResult) error {
	switch strings.TrimSpace(result.Error) {
	case fileChooserErrorExpired:
		return ErrFileChooserExpired
	case fileChooserErrorBridgeBusy:
		return ErrFileBridgeBusy
	case fileChooserErrorTooLarge:
		return ErrFileTooLarge
	case fileChooserErrorLimit:
		return ErrFileLimitExceeded
	default:
		return ErrFileInjectFailed
	}
}

func (m *Manager) liveRuntimeState(workspaceID int64) (State, error) {
	if workspaceID <= 0 {
		return "", fmt.Errorf("%w: workspace id is invalid", ErrInvalidRequest)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		return "", ErrNotFound
	}
	return rt.state, nil
}

func (m *Manager) requireLiveRuntime(workspaceID int64) error {
	state, err := m.liveRuntimeState(workspaceID)
	if err != nil {
		return err
	}
	if state != StateRunning && state != StateIdle {
		return fmt.Errorf("%w: runtime is %s", runtime.ErrNotRunning, state)
	}
	return nil
}

func ensureFileChooserStagingDirs(root *os.Root, chooserID string, chown func(root *os.Root, name string, uid int, gid int) error) error {
	for _, name := range []string{"uploads", "uploads/.bridge", path.Join("uploads", ".bridge", chooserID)} {
		if err := ensureRootDirectory(root, name); err != nil {
			return err
		}
		if err := handOverToRuntime(root, name, chown, ownerOfRoot); err != nil {
			return err
		}
	}
	return nil
}

func ensureRootDirectory(root *os.Root, name string) error {
	current := ""
	for _, component := range strings.Split(name, "/") {
		if component == "" || component == "." || component == ".." || strings.Contains(component, `\`) {
			return fmt.Errorf("invalid workspace directory name %q", name)
		}
		if current == "" {
			current = component
		} else {
			current = path.Join(current, component)
		}
		entry, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(current, 0o700); err != nil {
				return err
			}
			entry, err = root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() {
			return fmt.Errorf("workspace path %s is not a real directory", current)
		}
	}
	return nil
}

func readRootDir(root *os.Root, name string) ([]os.DirEntry, error) {
	dir, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}
func resetFileChooserStagingDir(root *os.Root, chooserID string) error {
	dir := path.Join("uploads", ".bridge", chooserID)
	entries, err := readRootDir(root, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: staging path contains an unexpected entry", ErrFileInjectFailed)
		}
		if err := root.Remove(path.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func fileChooserStagedBytes(root *os.Root) (int64, error) {
	bridge := path.Join("uploads", ".bridge")
	entries, err := readRootDir(root, bridge)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		entryPath := path.Join(bridge, entry.Name())
		entryInfo, err := root.Lstat(entryPath)
		if err != nil {
			return 0, err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.IsDir() {
			return 0, fmt.Errorf("staging path %s is not a real directory", entryPath)
		}
		files, err := readRootDir(root, entryPath)
		if err != nil {
			return 0, err
		}
		for _, file := range files {
			info, err := root.Lstat(path.Join(entryPath, file.Name()))
			if err != nil {
				return 0, err
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return 0, fmt.Errorf("staging path %s is not a regular file", path.Join(entryPath, file.Name()))
			}
			total += info.Size()
			if total > fileChooserMaxWorkspaceBytes {
				return total, nil
			}
		}
	}
	return total, nil
}

func removeFileChooserStagingFiles(root *os.Root, chooserID string, names []string) error {
	dir := path.Join("uploads", ".bridge", chooserID)
	for _, name := range names {
		_ = root.Remove(path.Join(dir, name))
	}
	_ = root.Remove(dir)
	return nil
}

func safeFileChooserName(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
	base := path.Base(raw)
	if base == "." || base == ".." || base == "/" || base == "" {
		return ""
	}
	var builder strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	name := strings.Trim(builder.String(), "._-")
	if name == "" {
		return ""
	}
	if len(name) > 128 {
		name = name[:128]
	}
	return name
}

func uniqueFileChooserName(root *os.Root, chooserID string, name string, existing []string) string {
	used := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		used[item] = struct{}{}
	}
	if _, ok := used[name]; !ok {
		if _, err := root.Lstat(path.Join("uploads", ".bridge", chooserID, name)); errors.Is(err, os.ErrNotExist) {
			return name
		}
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for index := 1; ; index++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, index, ext)
		if _, ok := used[candidate]; ok {
			continue
		}
		if _, err := root.Lstat(path.Join("uploads", ".bridge", chooserID, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

func fileChooserContainerPaths(chooserID string, names []string) []string {
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, path.Join(runtime.WorkspaceMountTarget, "uploads", ".bridge", chooserID, name))
	}
	return paths
}

func (m *Manager) scheduleFileChooserCleanup(workspaceID int64, chooserID string, names []string) {
	go func() {
		timer := time.NewTimer(fileChooserSuccessCleanupWait)
		defer timer.Stop()
		<-timer.C
		lock := m.lockFor(workspaceID)
		lock.Lock()
		defer lock.Unlock()
		root, err := ensureWorkspaceDir(WorkspaceDir(m.dataRoot, workspaceID))
		if err != nil {
			return
		}
		defer root.Close()
		_ = removeFileChooserStagingFiles(root, chooserID, names)
	}()
}

func (m *Manager) cleanupFileChooserStaging(workspaceID int64, chooserID string) error {
	root, err := ensureWorkspaceDir(WorkspaceDir(m.dataRoot, workspaceID))
	if err != nil {
		return err
	}
	defer root.Close()
	dir := path.Join("uploads", ".bridge", chooserID)
	entries, err := readRootDir(root, dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("staging path contains an unexpected entry")
		}
		if err := root.Remove(path.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return root.Remove(dir)
}
