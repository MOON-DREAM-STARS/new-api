package webworkspace

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
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
	Id             string
	UserId         int
	WorkspaceId    int
	RuntimeId      string
	State          string
	Mode           string
	CreatedAt      int64
	LastSeenAt     int64
	IdleDeadlineAt int64
	Navigation     *AgentNavigation
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
	session.Navigation = runtime.Navigation
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
		Navigation:     runtime.Navigation,
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
func NavigateSession(ctx context.Context, userId int, sessionId string, action string) (*Session, error) {
	session, err := GetSession(userId, sessionId)
	if err != nil {
		return nil, err
	}
	switch action {
	case "back", "forward", "reload", "state":
	default:
		return nil, ErrInvalidNavigationAction
	}
	client, err := newAgentClient()
	if err != nil {
		return nil, err
	}
	navigation, err := client.NavigateRuntime(ctx, session.WorkspaceId, action)
	if err != nil {
		return nil, err
	}
	updated, ok := sessions.setNavigation(sessionId, navigation)
	if !ok {
		return nil, ErrSessionNotFound
	}
	return &updated, nil
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
