package webworkspace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"sync"
	"time"
)

// DefaultProvider is the only Web Workspace provider enabled in the first
// version. Clients never choose the provider.
const DefaultProvider = "chatgpt"

// ErrSessionNotFound covers unknown sessions and sessions owned by another
// user, so callers cannot tell the two apart.
var ErrSessionNotFound = errors.New("web workspace session not found")

// ErrInvalidNavigationAction rejects values outside the frozen navigation
// command contract before any agent request is made.
var ErrInvalidNavigationAction = errors.New("web workspace navigation action invalid")

// Session is the control plane view of one workspace browser session. Sessions
// are ephemeral: they live in memory and are re-derived from the agent on the
// next start after a New API restart.
type Session struct {
	Id              string
	UserId          int
	WorkspaceId     int
	RuntimeId       string
	State           string
	Mode            string
	CreatedAt       int64
	LastSeenAt      int64
	IdleDeadlineAt  int64
	StreamBytesOut  int64
	StreamBytesIn   int64
	Navigation      *AgentNavigation
	Page            *AgentPageStatus
	ProjectCreation *AgentProjectCreation
	IMEState        string
}

// NormalizeIMEState keeps the presentation-only IBus field inside its frozen
// READY/UNAVAILABLE contract. Unknown, empty and malformed values fail closed
// to UNAVAILABLE without affecting the session.
func NormalizeIMEState(value string) string {
	if value == "READY" {
		return "READY"
	}
	return "UNAVAILABLE"
}

type sessionStore struct {
	mutex   sync.RWMutex
	entries map[string]Session
	starts  map[int]*sync.Mutex
}

var sessions = sessionStore{
	entries: make(map[string]Session),
	starts:  make(map[int]*sync.Mutex),
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func (s *sessionStore) startLock(userId int) *sync.Mutex {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if lock, ok := s.starts[userId]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.starts[userId] = lock
	return lock
}

func (s *sessionStore) get(sessionId string) (Session, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	session, ok := s.entries[sessionId]
	return session, ok
}

func (s *sessionStore) put(session Session) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.entries[session.Id] = session
}

func (s *sessionStore) remove(sessionId string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.entries, sessionId)
}

// currentForUser returns the newest session of the user. A user owns exactly
// one workspace, so at most one session is expected at a time.
func (s *sessionStore) currentForUser(userId int) (Session, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	var current Session
	found := false
	for _, session := range s.entries {
		if session.UserId != userId {
			continue
		}
		if !found || session.CreatedAt > current.CreatedAt {
			current = session
			found = true
		}
	}
	return current, found
}

func (s *sessionStore) touch(sessionId string, now int64) (Session, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	session, ok := s.entries[sessionId]
	if !ok {
		return Session{}, false
	}
	session.LastSeenAt = now
	s.entries[sessionId] = session
	return session, true
}

func (s *sessionStore) applyRuntime(sessionId string, runtime *AgentRuntime) (Session, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	session, ok := s.entries[sessionId]
	if !ok {
		return Session{}, false
	}
	session.RuntimeId = runtime.RuntimeId
	session.State = runtime.State
	session.Mode = runtime.Mode
	session.IdleDeadlineAt = runtime.IdleDeadlineAt
	session.StreamBytesOut = runtime.StreamBytesOut
	session.StreamBytesIn = runtime.StreamBytesIn
	session.Navigation = runtime.Navigation
	session.Page = runtime.Page.normalized()
	session.ProjectCreation = runtime.ProjectCreation.normalized()
	session.IMEState = NormalizeIMEState(runtime.IMEState)
	if runtime.LastActivityAt > session.LastSeenAt {
		session.LastSeenAt = runtime.LastActivityAt
	}
	s.entries[sessionId] = session
	return session, true
}

func (s *sessionStore) setNavigation(sessionId string, navigation *AgentNavigation) (Session, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	session, ok := s.entries[sessionId]
	if !ok {
		return Session{}, false
	}
	session.Navigation = navigation
	s.entries[sessionId] = session
	return session, true
}

func (s *sessionStore) setState(sessionId string, state string) (Session, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	session, ok := s.entries[sessionId]
	if !ok {
		return Session{}, false
	}
	session.State = state
	session.LastSeenAt = time.Now().Unix()
	s.entries[sessionId] = session
	return session, true
}

// StartSession starts (or returns) the browser runtime for the user's
// workspace. It is idempotent: an existing live runtime is reused, while a
// stale session whose runtime disappeared is replaced.
func StartSession(ctx context.Context, userId int, screenWidth int, screenHeight int) (*Session, error) {
	if userId <= 0 {
		return nil, ErrSessionNotFound
	}
	lock := sessions.startLock(userId)
	lock.Lock()
	defer lock.Unlock()

	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	if existing, ok := sessions.currentForUser(userId); ok {
		runtime, err := client.GetRuntime(ctx, existing.WorkspaceId)
		switch {
		case err == nil && LiveRuntimeState(runtime.State):
			updated, _ := sessions.applyRuntime(existing.Id, runtime)
			if err := pushOwnershipWithClient(ctx, client, existing.WorkspaceId); err != nil {
				return nil, err
			}
			return &updated, nil
		case err == nil:
			sessions.remove(existing.Id)
		case errors.Is(err, ErrAgentRuntimeNotFound):
			sessions.remove(existing.Id)
		default:
			return nil, err
		}
	}

	workspace, err := EnsureWorkspace(userId, DefaultProvider)
	if err != nil {
		return nil, err
	}
	runtime, err := client.CreateRuntime(ctx, workspace.Id, workspace.Provider, screenWidth, screenHeight)
	if err != nil {
		return nil, err
	}
	if !LiveRuntimeState(runtime.State) {
		return nil, fmt.Errorf("%w: runtime state %s", ErrAgentRejected, runtime.State)
	}
	sessionId, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	session := Session{
		Id:             sessionId,
		UserId:         userId,
		WorkspaceId:    workspace.Id,
		RuntimeId:      runtime.RuntimeId,
		State:          runtime.State,
		Mode:           runtime.Mode,
		CreatedAt:      now,
		LastSeenAt:     now,
		IdleDeadlineAt: runtime.IdleDeadlineAt,
		StreamBytesOut: runtime.StreamBytesOut,
		StreamBytesIn:  runtime.StreamBytesIn,
		Navigation:     runtime.Navigation,
		Page:           runtime.Page.normalized(),
		IMEState:       NormalizeIMEState(runtime.IMEState),
	}
	sessions.put(session)
	// The guard denies every unregistered provider resource, so the session is
	// only usable once the ownership document reached the agent.
	if err := pushOwnershipWithClient(ctx, client, workspace.Id); err != nil {
		return nil, err
	}
	return &session, nil
}

func GetSession(userId int, sessionId string) (*Session, error) {
	if userId <= 0 || sessionId == "" {
		return nil, ErrSessionNotFound
	}
	session, ok := sessions.get(sessionId)
	if !ok || session.UserId != userId {
		return nil, ErrSessionNotFound
	}
	return &session, nil
}

func CurrentSession(userId int) (*Session, bool) {
	if userId <= 0 {
		return nil, false
	}
	session, ok := sessions.currentForUser(userId)
	if !ok {
		return nil, false
	}
	return &session, true
}

func StopSession(ctx context.Context, userId int, sessionId string) error {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return err
	}
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	if err := client.StopRuntime(ctx, session.WorkspaceId); err != nil && !errors.Is(err, ErrAgentRuntimeNotFound) {
		return err
	}
	sessions.remove(sessionId)
	return nil
}

func RestartSession(ctx context.Context, userId int, sessionId string, mode string, screenWidth int, screenHeight int) (*Session, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	runtime, err := client.RestartRuntime(ctx, session.WorkspaceId, mode, screenWidth, screenHeight)
	if err != nil {
		return nil, err
	}
	updated, ok := sessions.applyRuntime(sessionId, runtime)
	if !ok {
		return nil, ErrSessionNotFound
	}
	return &updated, nil
}

// NavigateSession sends one validated navigation command through the agent and
// writes the returned snapshot back into the caller's session.
func NavigateSession(ctx context.Context, userId int, sessionId string, action string, projectID int) (*Session, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	var externalProjectID string
	switch action {
	case "back", "forward", "reload", "state":
	case "project":
		if projectID <= 0 {
			return nil, ErrInvalidNavigationAction
		}
		project, projectErr := GetOwnedProject(userId, projectID)
		if projectErr != nil {
			return nil, projectErr
		}
		if project.WorkspaceId != session.WorkspaceId {
			return nil, ErrResourceNotFound
		}
		externalProjectID = project.ExternalProjectId
	default:
		return nil, ErrInvalidNavigationAction
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	var navigation *AgentNavigation
	if action == "project" {
		navigation, err = client.NavigateProject(ctx, session.WorkspaceId, externalProjectID)
	} else {
		navigation, err = client.NavigateRuntime(ctx, session.WorkspaceId, action)
	}
	if err != nil {
		return nil, err
	}
	updated, ok := sessions.setNavigation(sessionId, navigation)
	if !ok {
		return nil, ErrSessionNotFound
	}
	return &updated, nil
}

// FileChooser is the control-plane view of one pending local file chooser. It
// never contains the CDP backend node id, session id or a staging path.
type FileChooser struct {
	ChooserID string
	Mode      string
	CreatedAt int64
	ExpiresAt int64
}

// FileChooserResult is the real guard result of one attach or cancel command.
type FileChooserResult struct {
	ChooserID string
	State     string
	Error     string
	UpdatedAt int64
}

// WaitFileChooser long-polls the agent for the caller's pending chooser.
func WaitFileChooser(ctx context.Context, userId int, sessionId string, wait time.Duration) (*FileChooser, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	chooser, err := client.WaitFileChooser(ctx, session.WorkspaceId, wait)
	if err != nil {
		return nil, err
	}
	if chooser == nil {
		return nil, nil
	}
	return &FileChooser{
		ChooserID: chooser.ChooserID,
		Mode:      chooser.Mode,
		CreatedAt: chooser.CreatedAt,
		ExpiresAt: chooser.ExpiresAt,
	}, nil
}

// UploadFileChooserFiles streams multipart parts to the agent after checking
// session ownership. The caller must never supply a filesystem path.
func UploadFileChooserFiles(ctx context.Context, userId int, sessionId string, chooserID string, contentType string, body io.Reader) (*FileChooserResult, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	if chooserID == "" {
		return nil, ErrInvalidFileChooser
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	result, err := client.UploadFileChooserFiles(ctx, session.WorkspaceId, chooserID, contentType, body)
	if err != nil {
		return nil, err
	}
	return &FileChooserResult{
		ChooserID: result.ChooserID,
		State:     result.State,
		Error:     result.Error,
		UpdatedAt: result.UpdatedAt,
	}, nil
}

// CancelFileChooser dismisses a pending chooser after checking ownership.
func CancelFileChooser(ctx context.Context, userId int, sessionId string, chooserID string) (*FileChooserResult, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	if chooserID == "" {
		return nil, ErrInvalidFileChooser
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	result, err := client.CancelFileChooser(ctx, session.WorkspaceId, chooserID)
	if err != nil {
		return nil, err
	}
	return &FileChooserResult{
		ChooserID: result.ChooserID,
		State:     result.State,
		Error:     result.Error,
		UpdatedAt: result.UpdatedAt,
	}, nil
}

// CopyClipboard checks session ownership and returns the raw remote selection.
func CopyClipboard(ctx context.Context, userId int, sessionId string) (string, []byte, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return "", nil, err
	}
	client, err := newAgentClient()
	if err != nil {
		return "", nil, err
	}
	return client.CopyClipboard(ctx, session.WorkspaceId)
}

// PasteClipboard checks session ownership, bounds the raw body and forwards it
// to the agent without a JSON wrapper.
func PasteClipboard(ctx context.Context, userId int, sessionId string, mimeType string, body io.Reader) error {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return err
	}
	normalized, err := normalizeClipboardMIME(mimeType)
	if err != nil {
		return err
	}
	payload, err := readClipboardPayload(body)
	if err != nil {
		return err
	}
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	return client.PasteClipboard(ctx, session.WorkspaceId, normalized, payload)
}

// InsertInputText checks session ownership and injects one committed local
// text value through the agent.
func InsertInputText(ctx context.Context, userId int, sessionId string, text string) error {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return err
	}
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	return client.InsertInputText(ctx, session.WorkspaceId, text)
}

// DispatchInputKey checks session ownership and forwards one approved key.
func DispatchInputKey(ctx context.Context, userId int, sessionId string, key string, modifiers []string) error {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return err
	}
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	return client.DispatchInputKey(ctx, session.WorkspaceId, key, modifiers)
}

// ProbeInputCaret checks session ownership and returns the remote caret, or nil
// when the remote page has no active editable element.
func ProbeInputCaret(ctx context.Context, userId int, sessionId string) (*InputCaret, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	return client.ProbeInputCaret(ctx, session.WorkspaceId)
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
	payload, err := io.ReadAll(io.LimitReader(body, maxClipboardPayloadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxClipboardPayloadBytes {
		return nil, ErrClipboardPayloadTooLarge
	}
	return payload, nil
}

// TouchRuntimeActivity keeps a live session alive without attaching a stream. A
// hidden tab uses it instead of holding the display stream open.
func TouchRuntimeActivity(ctx context.Context, userId int, sessionId string) (*Session, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	runtime, err := client.TouchRuntime(ctx, session.WorkspaceId)
	if err != nil {
		return nil, err
	}
	if _, ok := sessions.applyRuntime(sessionId, runtime); !ok {
		return nil, ErrSessionNotFound
	}
	if _, touched := sessions.touch(sessionId, time.Now().Unix()); !touched {
		return nil, ErrSessionNotFound
	}
	current, ok := sessions.get(sessionId)
	if !ok {
		return nil, ErrSessionNotFound
	}
	return &current, nil
}

// RefreshSession re-reads the runtime state from the agent so a runtime that the
// idle timeout stopped does not keep reporting RUNNING to the client.
func RefreshSession(ctx context.Context, userId int, sessionId string) (*Session, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	runtime, err := client.GetRuntime(ctx, session.WorkspaceId)
	if errors.Is(err, ErrAgentRuntimeNotFound) {
		stopped := &AgentRuntime{RuntimeId: session.RuntimeId, WorkspaceId: session.WorkspaceId, State: AgentStateStopped}
		updated, ok := sessions.applyRuntime(sessionId, stopped)
		if !ok {
			return nil, ErrSessionNotFound
		}
		return &updated, nil
	}
	if err != nil {
		return nil, err
	}
	updated, ok := sessions.applyRuntime(sessionId, runtime)
	if !ok {
		return nil, ErrSessionNotFound
	}
	return &updated, nil
}

// TouchSession records control-plane activity for one session. It is called
// when a stream attaches and while stream data flows.
func TouchSession(userId int, sessionId string) error {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return err
	}
	_, ok := sessions.touch(session.Id, time.Now().Unix())
	if !ok {
		return ErrSessionNotFound
	}
	return nil
}

// MarkSessionIdle moves a session to IDLE after its stream detached. The agent
// keeps enforcing the idle timeout and stops the runtime on its own.
func MarkSessionIdle(userId int, sessionId string) error {
	if _, err := GetSession(userId, sessionId); err != nil {
		return err
	}
	if _, ok := sessions.setState(sessionId, AgentStateIdle); !ok {
		return ErrSessionNotFound
	}
	return nil
}
