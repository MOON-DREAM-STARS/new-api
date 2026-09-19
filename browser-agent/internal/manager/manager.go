// Package manager owns the per-workspace runtime state machine: it serialises
// lifecycle operations per workspace, enforces the idle timeout, detects
// crashed containers and tracks the display streams attached to a runtime.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// State is the runtime state machine value exposed to the control plane.
type State string

const (
	StateStarting State = "STARTING"
	StateRunning  State = "RUNNING"
	StateIdle     State = "IDLE"
	StateStopping State = "STOPPING"
	StateStopped  State = "STOPPED"
	StateFailed   State = "FAILED"
)

// Display dimensions accepted from the control plane.
const (
	DefaultWidth  = 1280
	DefaultHeight = 720
	MinWidth      = 640
	MaxWidth      = 3840
	MinHeight     = 360
	MaxHeight     = 2160
)

const (
	displayNumber = ":99"

	envWorkspaceDir = "WW_WORKSPACE_DIR"
	envDisplay      = "WW_DISPLAY"
	envScreenWidth  = "WW_SCREEN_WIDTH"
	envScreenHeight = "WW_SCREEN_HEIGHT"
	envVNCPort      = "WW_VNC_PORT"
	envProvider     = "WW_PROVIDER"
	envProxyServer  = "WW_PROXY_SERVER"
	envGuardMode    = "WW_GUARD_MODE"

	// A started container is not a ready display: Xvfb and x11vnc need a moment
	// before the RFB port accepts connections, so the runtime only becomes
	// RUNNING once a probe connection succeeded.
	displayProbeInterval = 250 * time.Millisecond
	displayProbeTimeout  = 20 * time.Second

	cleanupTimeout = 30 * time.Second
)

// Creation permit bounds (phase 4 contract §4).
const (
	permitKindProjectCreate = "project_create"
	minPermitIDLength       = 8
	maxPermitIDLength       = 64
	minPermitTTLSeconds     = 1
	maxPermitTTLSeconds     = 3600
)

var (
	// ErrNotFound reports that the agent has no runtime state for a workspace.
	ErrNotFound = errors.New("runtime not found")
	// ErrInvalidRequest reports a rejected start or restart parameter set.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrProviderRequired reports a restart that cannot be resolved because the
	// provider of the previous runtime is unknown.
	ErrProviderRequired = errors.New("provider is required")

	providerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

	projectIDPattern      = regexp.MustCompile(`^g-p-[0-9a-f]{32}$`)
	conversationIDPattern = regexp.MustCompile(`^[0-9A-Za-z-]{8,64}$`)
)

// Snapshot is the runtime JSON shape exposed by the HTTP API. It never contains
// container addresses, ports, Docker identifiers or file system paths.
type Snapshot struct {
	RuntimeID      string            `json:"runtime_id"`
	WorkspaceID    int64             `json:"workspace_id"`
	State          State             `json:"state"`
	CreatedAt      int64             `json:"created_at"`
	LastActivityAt int64             `json:"last_activity_at"`
	IdleDeadlineAt int64             `json:"idle_deadline_at"`
	Navigation     *NavigationStatus `json:"navigation"`
}

// StartOptions carries the request parameters of a runtime start.
type StartOptions struct {
	Provider string
	Width    int
	Height   int
	Mode     policy.Mode
}

// Options configures the manager.
type Options struct {
	DataRoot       string
	EgressProxyURL string
	IdleTimeout    time.Duration
	ScanInterval   time.Duration
	Logger         *slog.Logger
	Now            func() time.Time
	// Chown hands a directory below an open workspace root to the runtime image
	// identity. It is overridden only by tests; nil selects (*os.Root).Chown,
	// which resolves the name below the already opened root and refuses a name
	// that leaves the workspace.
	Chown func(root *os.Root, name string, uid int, gid int) error
	// HostDataRoot is the data root as visible to the Docker daemon. Only the
	// bind source of a runtime container is built from it; every local file
	// operation uses DataRoot. Empty falls back to DataRoot.
	HostDataRoot string
}

// Ownership is the resource snapshot the control plane pushes to the guard. The
// guard state files never carry client supplied paths; ids are validated first.
type Ownership struct {
	Generation    int64
	Projects      []string
	Conversations []string
}

// OwnershipCounts is the API result of an ownership push.
type OwnershipCounts struct {
	Generation    int64 `json:"generation"`
	Projects      int   `json:"projects"`
	Conversations int   `json:"conversations"`
}

// IssuedPermit is the API result of a creation permit issue.
type IssuedPermit struct {
	PermitID  string `json:"permit_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// ObservationPage is the API result of an observation pull: the raw observation
// objects the guard appended and the offset the control plane must acknowledge.
type ObservationPage struct {
	Observations []json.RawMessage `json:"observations"`
	NextOffset   int64             `json:"next_offset"`
}

type runtimeState struct {
	state        State
	provider     string
	width        int
	height       int
	mode         policy.Mode
	containerID  string
	ip           string
	createdAt    time.Time
	lastActivity time.Time
	idleDeadline time.Time
	navCommandID int64
	navigation   *NavigationStatus
}

func (rt *runtimeState) snapshot(workspaceID int64) Snapshot {
	snapshot := Snapshot{
		RuntimeID:      runtimeID(workspaceID),
		WorkspaceID:    workspaceID,
		State:          rt.state,
		CreatedAt:      rt.createdAt.Unix(),
		LastActivityAt: rt.lastActivity.Unix(),
	}
	if !rt.idleDeadline.IsZero() {
		snapshot.IdleDeadlineAt = rt.idleDeadline.Unix()
	}
	if rt.navigation != nil {
		navigation := *rt.navigation
		snapshot.Navigation = &navigation
	}
	return snapshot
}

type streamHandle struct {
	conn io.Closer
	done chan struct{}
	once sync.Once
}

func (h *streamHandle) close() {
	h.once.Do(func() {
		close(h.done)
		if h.conn != nil {
			_ = h.conn.Close()
		}
	})
}

// Manager implements the runtime scheduling and stream attachment contract.
type Manager struct {
	driver         runtime.Driver
	display        runtime.DisplayTransport
	dataRoot       string
	hostDataRoot   string
	egressProxyURL string
	idleTimeout    time.Duration
	scanInterval   time.Duration
	logger         *slog.Logger
	now            func() time.Time
	chown          func(root *os.Root, name string, uid int, gid int) error

	mu       sync.Mutex
	runtimes map[int64]*runtimeState
	locks    map[int64]*sync.Mutex
	streams  map[int64]map[*streamHandle]struct{}
	ipIndex  map[string]int64
}

// New returns a manager bound to a runtime driver and a display transport.
func New(driver runtime.Driver, display runtime.DisplayTransport, opts Options) *Manager {
	manager := &Manager{
		driver:         driver,
		display:        display,
		dataRoot:       opts.DataRoot,
		hostDataRoot:   opts.HostDataRoot,
		egressProxyURL: opts.EgressProxyURL,
		idleTimeout:    opts.IdleTimeout,
		scanInterval:   opts.ScanInterval,
		logger:         opts.Logger,
		now:            opts.Now,
		chown:          opts.Chown,
		runtimes:       map[int64]*runtimeState{},
		locks:          map[int64]*sync.Mutex{},
		streams:        map[int64]map[*streamHandle]struct{}{},
		ipIndex:        map[string]int64{},
	}
	if manager.logger == nil {
		manager.logger = slog.Default()
	}
	if manager.now == nil {
		manager.now = time.Now
	}
	if manager.scanInterval <= 0 {
		manager.scanInterval = 15 * time.Second
	}
	if manager.hostDataRoot == "" {
		manager.hostDataRoot = manager.dataRoot
	}
	return manager
}

// LookupSource resolves a source IP observed by the egress proxy to the
// workspace and mode of its live runtime. Unknown, stopped or failed runtimes
// return false so the proxy denies the request.
func (m *Manager) LookupSource(remoteIP string) (policy.Mode, int64, bool) {
	normalized, ok := normalizeRuntimeIP(remoteIP)
	if !ok {
		return "", 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	workspaceID, ok := m.ipIndex[normalized]
	if !ok {
		return "", 0, false
	}
	rt := m.runtimes[workspaceID]
	if rt == nil || (rt.state != StateRunning && rt.state != StateIdle) {
		delete(m.ipIndex, normalized)
		return "", 0, false
	}
	return rt.mode, workspaceID, true
}

// Get returns the current runtime snapshot of a workspace.
func (m *Manager) Get(workspaceID int64) (Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		return Snapshot{}, false
	}
	m.refreshNavigationLocked(rt, workspaceID)
	return rt.snapshot(workspaceID), true
}

// Start creates and starts the runtime of a workspace. It is idempotent: a
// workspace with a non-terminal runtime keeps that runtime.
func (m *Manager) Start(ctx context.Context, workspaceID int64, opts StartOptions) (Snapshot, error) {
	if err := validateOptions(workspaceID, opts); err != nil {
		return Snapshot{}, err
	}
	opts = withDefaults(opts)

	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	if snapshot, ok := m.Get(workspaceID); ok && snapshot.State != StateStopped && snapshot.State != StateFailed {
		return snapshot, nil
	}
	return m.startLocked(ctx, workspaceID, opts)
}

// Stop stops and removes the runtime container of a workspace while keeping the
// profile directory. It returns ErrNotFound when no runtime state exists.
func (m *Manager) Stop(ctx context.Context, workspaceID int64) (Snapshot, error) {
	if workspaceID <= 0 {
		return Snapshot{}, fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}
	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	snapshot, existed, err := m.stopLocked(ctx, workspaceID)
	if err != nil {
		return snapshot, err
	}
	if !existed {
		return Snapshot{}, ErrNotFound
	}
	return snapshot, nil
}

// Restart stops the existing runtime and starts a fresh one, reusing the
// previous provider and screen size unless overrides are supplied.
func (m *Manager) Restart(ctx context.Context, workspaceID int64, opts StartOptions) (Snapshot, error) {
	if workspaceID <= 0 {
		return Snapshot{}, fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}
	if opts.Mode != "" {
		if _, ok := policy.ParseMode(string(opts.Mode)); !ok {
			return Snapshot{}, fmt.Errorf("%w: mode is invalid", ErrInvalidRequest)
		}
	}
	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	current, exists := m.currentOptions(workspaceID)
	effective := current
	if opts.Mode != "" {
		effective.Mode = opts.Mode
	}
	if provider := strings.TrimSpace(opts.Provider); provider != "" {
		effective.Provider = provider
	}
	if opts.Width != 0 {
		effective.Width = opts.Width
	}
	if opts.Height != 0 {
		effective.Height = opts.Height
	}
	if !exists && effective.Provider == "" {
		// There is no runtime to restart and no provider to start a new one.
		return Snapshot{}, ErrNotFound
	}
	if effective.Provider == "" {
		return Snapshot{}, ErrProviderRequired
	}
	if err := validateOptions(workspaceID, effective); err != nil {
		return Snapshot{}, err
	}
	effective = withDefaults(effective)

	if snapshot, _, err := m.stopLocked(ctx, workspaceID); err != nil {
		return snapshot, err
	}
	return m.startLocked(ctx, workspaceID, effective)
}

// Activity refreshes the idle deadline of a live runtime.
func (m *Manager) Activity(workspaceID int64) (Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		return Snapshot{}, false
	}
	m.refreshNavigationLocked(rt, workspaceID)
	if rt.state == StateRunning || rt.state == StateIdle {
		m.touchRuntime(rt, m.now())
	}
	return rt.snapshot(workspaceID), true
}

// Touch records stream activity for a workspace.
func (m *Manager) Touch(workspaceID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtimes[workspaceID]
	if rt == nil || (rt.state != StateRunning && rt.state != StateIdle) {
		return
	}
	m.touchRuntime(rt, m.now())
}

// PutOwnership atomically replaces the guard ownership snapshot of a workspace.
// External ids are validated and de-duplicated before they reach the guard, and
// the guard state directory is created (0700, runtime identity) when missing.
func (m *Manager) PutOwnership(workspaceID int64, update Ownership) (OwnershipCounts, error) {
	if workspaceID <= 0 {
		return OwnershipCounts{}, fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}
	if update.Generation < 0 {
		return OwnershipCounts{}, fmt.Errorf("%w: generation must not be negative", ErrInvalidRequest)
	}
	projects, err := normalizeExternalIDs(update.Projects, projectIDPattern, "project id")
	if err != nil {
		return OwnershipCounts{}, err
	}
	conversations, err := normalizeExternalIDs(update.Conversations, conversationIDPattern, "conversation id")
	if err != nil {
		return OwnershipCounts{}, err
	}

	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return OwnershipCounts{}, fmt.Errorf("prepare guard state directory: %w", err)
	}
	defer guardRoot.Close()
	payload, err := json.Marshal(guardOwnership{
		Generation:    update.Generation,
		Projects:      projects,
		Conversations: conversations,
		UpdatedAt:     m.now().Unix(),
	})
	if err != nil {
		return OwnershipCounts{}, fmt.Errorf("encode ownership state: %w", err)
	}
	if err := writeStateFileAtomic(guardRoot, ownershipFileName, payload); err != nil {
		return OwnershipCounts{}, fmt.Errorf("write ownership state: %w", err)
	}
	return OwnershipCounts{
		Generation:    update.Generation,
		Projects:      len(projects),
		Conversations: len(conversations),
	}, nil
}

// IssuePermit writes a short-lived single-use creation permit for the runtime of
// a workspace and drops the consumed marker of the previous permit, so the guard
// only treats the new permit as spendable.
func (m *Manager) IssuePermit(workspaceID int64, permitID string, kind string, ttlSeconds int) (IssuedPermit, error) {
	if workspaceID <= 0 || len(permitID) < minPermitIDLength || len(permitID) > maxPermitIDLength ||
		kind != permitKindProjectCreate || ttlSeconds < minPermitTTLSeconds || ttlSeconds > maxPermitTTLSeconds {
		return IssuedPermit{}, fmt.Errorf("%w: permit request is invalid", ErrInvalidRequest)
	}

	// Serialise with the lifecycle operations so a permit is never issued for a
	// runtime that is stopping, failing or restarting.
	lock := m.lockFor(workspaceID)
	lock.Lock()
	defer lock.Unlock()

	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	state := State("")
	if rt != nil {
		state = rt.state
	}
	m.mu.Unlock()
	if rt == nil {
		return IssuedPermit{}, ErrNotFound
	}
	if state != StateRunning && state != StateIdle {
		return IssuedPermit{}, fmt.Errorf("%w: runtime is %s", runtime.ErrNotRunning, state)
	}

	guardRoot, err := ensureGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID), m.chown)
	if err != nil {
		return IssuedPermit{}, fmt.Errorf("prepare guard state directory: %w", err)
	}
	defer guardRoot.Close()
	now := m.now()
	permit := guardPermit{
		PermitID:  permitID,
		Kind:      permitKindProjectCreate,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(time.Duration(ttlSeconds) * time.Second).Unix(),
	}
	payload, err := json.Marshal(permit)
	if err != nil {
		return IssuedPermit{}, fmt.Errorf("encode permit state: %w", err)
	}
	if err := writeStateFileAtomic(guardRoot, permitFileName, payload); err != nil {
		return IssuedPermit{}, fmt.Errorf("write permit state: %w", err)
	}
	if err := clearConsumedPermit(guardRoot, permitID); err != nil {
		return IssuedPermit{}, fmt.Errorf("clear consumed permit: %w", err)
	}
	return IssuedPermit{PermitID: permitID, ExpiresAt: permit.ExpiresAt}, nil
}

// Observations returns every complete observation object the guard appended
// after the acknowledged offset. The offset only moves when the control plane
// acknowledges it, so repeated pulls are idempotent.
func (m *Manager) Observations(workspaceID int64) (ObservationPage, error) {
	if workspaceID <= 0 {
		return ObservationPage{}, fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}
	guardRoot, err := openGuardStateDir(WorkspaceDir(m.dataRoot, workspaceID))
	if errors.Is(err, os.ErrNotExist) {
		return ObservationPage{Observations: []json.RawMessage{}}, nil
	}
	if err != nil {
		return ObservationPage{}, fmt.Errorf("read observations: %w", err)
	}
	defer guardRoot.Close()

	observations, nextOffset, err := readObservationLines(guardRoot)
	if err != nil {
		return ObservationPage{}, fmt.Errorf("read observations: %w", err)
	}
	return ObservationPage{Observations: observations, NextOffset: nextOffset}, nil
}

// AckObservations persists the offset the control plane has applied. The offset
// must never move past the end of the observation file.
func (m *Manager) AckObservations(workspaceID int64, offset int64) (int64, error) {
	if workspaceID <= 0 || offset < 0 {
		return 0, fmt.Errorf("%w: observation offset is invalid", ErrInvalidRequest)
	}
	workspaceDir := WorkspaceDir(m.dataRoot, workspaceID)
	size := int64(0)
	guardRoot, err := openGuardStateDir(workspaceDir)
	switch {
	case err == nil:
		size, err = observationFileSize(guardRoot)
		_ = guardRoot.Close()
		if err != nil {
			return 0, fmt.Errorf("read observations: %w", err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return 0, fmt.Errorf("read observations: %w", err)
	}
	if offset > size {
		return 0, fmt.Errorf("%w: observation offset %d exceeds file size %d", ErrInvalidRequest, offset, size)
	}
	guardRoot, err = ensureGuardStateDir(workspaceDir, m.chown)
	if err != nil {
		return 0, fmt.Errorf("prepare guard state directory: %w", err)
	}
	defer guardRoot.Close()
	if err := writeStateFileAtomic(guardRoot, observationsOffsetName, []byte(strconv.FormatInt(offset, 10))); err != nil {
		return 0, fmt.Errorf("write observation offset: %w", err)
	}
	return offset, nil
}

// OpenDisplayStream returns a raw display connection for a streamable runtime.
// It returns runtime.ErrNotRunning when the workspace has no runtime that can
// serve a display connection.
func (m *Manager) OpenDisplayStream(ctx context.Context, workspaceID int64) (io.ReadWriteCloser, error) {
	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	streamable := rt != nil && (rt.state == StateRunning || rt.state == StateIdle)
	m.mu.Unlock()
	if !streamable {
		return nil, runtime.ErrNotRunning
	}

	conn, err := m.display.Connect(ctx, workspaceID)
	if err != nil {
		m.logger.Warn("display transport connect failed", "workspace_id", workspaceID, "error", err)
		return nil, runtime.ErrNotRunning
	}
	m.Touch(workspaceID)
	return conn, nil
}

// AttachStream registers a stream connection with the runtime. The returned
// channel is closed, and the connection closed, as soon as the runtime stops or
// fails. The returned function must be called when the stream ends.
func (m *Manager) AttachStream(workspaceID int64, conn io.Closer) (<-chan struct{}, func()) {
	handle := &streamHandle{conn: conn, done: make(chan struct{})}

	m.mu.Lock()
	if m.streams[workspaceID] == nil {
		m.streams[workspaceID] = map[*streamHandle]struct{}{}
	}
	m.streams[workspaceID][handle] = struct{}{}
	if rt := m.runtimes[workspaceID]; rt != nil && (rt.state == StateRunning || rt.state == StateIdle) {
		if rt.state == StateIdle {
			rt.state = StateRunning
		}
		m.touchRuntime(rt, m.now())
	}
	m.mu.Unlock()

	release := func() { m.releaseStream(workspaceID, handle) }
	return handle.done, release
}

// Reconcile adopts the runtime containers that survived an agent restart and
// removes the managed containers that are no longer running.
func (m *Manager) Reconcile(ctx context.Context) error {
	listed, err := m.driver.List(ctx)
	if err != nil {
		return fmt.Errorf("list runtime containers: %w", err)
	}

	adopted, removed := 0, 0
	for _, item := range listed {
		if item.WorkspaceID <= 0 {
			m.logger.Warn("ignoring runtime container without a valid workspace label", "container_id", item.ID)
			continue
		}
		info, err := m.driver.Inspect(ctx, item.WorkspaceID)
		if errors.Is(err, runtime.ErrNotFound) {
			m.clearRuntimeIP(item.WorkspaceID)
			continue
		}
		if err != nil {
			m.logger.Warn("inspecting runtime container during reconcile failed", "workspace_id", item.WorkspaceID, "error", err)
			continue
		}
		if !info.Running {
			if err := m.driver.Remove(ctx, item.WorkspaceID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
				m.logger.Warn("removing stopped runtime container failed", "workspace_id", item.WorkspaceID, "error", err)
				continue
			}
			m.clearRuntimeIP(item.WorkspaceID)
			removed++
			continue
		}

		now := m.now()
		createdAt := info.StartedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		width := envInt(info.Env[envScreenWidth], DefaultWidth)
		height := envInt(info.Env[envScreenHeight], DefaultHeight)
		if !validSize(width, height) {
			width, height = DefaultWidth, DefaultHeight
		}
		mode, validMode := policy.ParseMode(info.Env[envGuardMode])
		if !validMode {
			mode = policy.ModeLocked
		}
		recovered := &runtimeState{
			state:        StateRunning,
			provider:     strings.TrimSpace(info.Env[envProvider]),
			width:        width,
			height:       height,
			mode:         mode,
			containerID:  info.ID,
			ip:           info.IP,
			createdAt:    createdAt,
			lastActivity: now,
			idleDeadline: now.Add(m.idleTimeout),
		}
		m.mu.Lock()
		m.clearRuntimeIPLocked(item.WorkspaceID)
		m.runtimes[item.WorkspaceID] = recovered
		m.setRuntimeIPLocked(item.WorkspaceID, info.IP)
		m.mu.Unlock()
		adopted++
	}

	if adopted > 0 || removed > 0 {
		m.logger.Info("reconciled runtime containers", "adopted", adopted, "removed", removed)
	}
	return nil
}

// Supervise runs the crash detection and idle timeout loop until ctx is done.
func (m *Manager) Supervise(ctx context.Context) {
	ticker := time.NewTicker(m.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.ScanOnce(ctx)
		}
	}
}

// ScanOnce performs one supervision pass: it fails runtimes whose container is
// gone and stops runtimes that passed their idle deadline.
func (m *Manager) ScanOnce(ctx context.Context) {
	for _, workspaceID := range m.workspaceIDs() {
		m.scanWorkspace(ctx, workspaceID)
	}
}

func (m *Manager) scanWorkspace(ctx context.Context, workspaceID int64) {
	lock := m.lockFor(workspaceID)
	if !lock.TryLock() {
		return
	}
	defer lock.Unlock()

	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		m.mu.Unlock()
		return
	}
	state := rt.state
	deadline := rt.idleDeadline
	hasStreams := len(m.streams[workspaceID]) > 0
	m.mu.Unlock()

	if state != StateRunning && state != StateIdle {
		return
	}

	info, err := m.driver.Inspect(ctx, workspaceID)
	switch {
	case errors.Is(err, runtime.ErrNotFound):
		m.markFailed(workspaceID, errors.New("runtime container disappeared"), true)
		return
	case err != nil:
		m.logger.Warn("inspecting runtime container failed", "workspace_id", workspaceID, "error", err)
		return
	case !info.Running:
		m.markFailed(workspaceID, fmt.Errorf("runtime container is %s", info.State), true)
		return
	}

	if hasStreams || m.now().Before(deadline) {
		return
	}
	m.logger.Info("stopping idle workspace runtime", "workspace_id", workspaceID)
	if _, _, err := m.stopLocked(ctx, workspaceID); err != nil {
		m.logger.Warn("stopping idle runtime failed", "workspace_id", workspaceID, "error", err)
	}
}

func (m *Manager) startLocked(ctx context.Context, workspaceID int64, opts StartOptions) (Snapshot, error) {
	workspaceDir := WorkspaceDir(m.dataRoot, workspaceID)
	if err := EnsureWorkspaceDirs(workspaceDir, m.chown); err != nil {
		return Snapshot{}, fmt.Errorf("prepare workspace directory: %w", err)
	}

	now := m.now()
	rt := &runtimeState{
		state:        StateStarting,
		provider:     opts.Provider,
		width:        opts.Width,
		height:       opts.Height,
		mode:         opts.Mode,
		createdAt:    now,
		lastActivity: now,
		idleDeadline: now.Add(m.idleTimeout),
	}
	m.mu.Lock()
	m.runtimes[workspaceID] = rt
	m.mu.Unlock()

	// When the agent itself runs in a container, the local workspace path only
	// exists inside that container. The runtime container is created by the
	// Docker daemon on the host, so its bind source must be the host visible
	// path derived from the same workspace id.
	bindSource := WorkspaceDir(m.hostDataRoot, workspaceID)
	spec := runtime.CreateSpec{
		WorkspaceID: workspaceID,
		Binds:       []string{bindSource + ":" + runtime.WorkspaceMountTarget},
		Env: []string{
			envWorkspaceDir + "=" + runtime.WorkspaceMountTarget,
			envDisplay + "=" + displayNumber,
			envScreenWidth + "=" + strconv.Itoa(opts.Width),
			envScreenHeight + "=" + strconv.Itoa(opts.Height),
			envVNCPort + "=" + strconv.Itoa(runtime.VNCPort),
			envProvider + "=" + opts.Provider,
			envProxyServer + "=" + m.egressProxyURL,
			envGuardMode + "=" + string(opts.Mode),
		},
	}

	info, err := m.driver.Create(ctx, spec)
	if err != nil {
		m.markFailed(workspaceID, err, false)
		return Snapshot{}, err
	}
	m.mu.Lock()
	rt.containerID = info.ID
	m.mu.Unlock()

	if !info.Reused {
		if err := m.driver.Start(ctx, workspaceID); err != nil {
			m.removeContainer(ctx, workspaceID)
			m.markFailed(workspaceID, err, false)
			return Snapshot{}, err
		}
	}

	running, err := m.driver.Inspect(ctx, workspaceID)
	if err != nil {
		m.removeContainer(ctx, workspaceID)
		m.markFailed(workspaceID, err, false)
		return Snapshot{}, err
	}
	if !running.Running || running.State != runtime.ContainerRunning {
		exitErr := fmt.Errorf("runtime container is not running after start (state %s)", running.State)
		m.removeContainer(ctx, workspaceID)
		m.markFailed(workspaceID, exitErr, false)
		return Snapshot{}, exitErr
	}
	if err := m.waitForDisplayReady(ctx, workspaceID); err != nil {
		m.removeContainer(ctx, workspaceID)
		m.markFailed(workspaceID, err, false)
		return Snapshot{}, err
	}

	m.mu.Lock()
	rt.containerID = running.ID
	rt.state = StateRunning
	m.setRuntimeIPLocked(workspaceID, running.IP)
	m.touchRuntime(rt, m.now())
	snapshot := rt.snapshot(workspaceID)
	m.mu.Unlock()
	return snapshot, nil
}

// waitForDisplayReady polls the display transport until Xvfb and x11vnc accept
// a connection. A started container is not a ready display: without this gate
// the control plane could attach a stream during the seconds between container
// start and display readiness and observe a failing runtime.
func (m *Manager) waitForDisplayReady(ctx context.Context, workspaceID int64) error {
	deadline := time.Now().Add(displayProbeTimeout)
	for {
		conn, err := m.display.Connect(ctx, workspaceID)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("runtime display probe aborted: %w", ctxErr)
		}
		info, inspectErr := m.driver.Inspect(ctx, workspaceID)
		switch {
		case errors.Is(inspectErr, runtime.ErrNotFound):
			return errors.New("runtime container disappeared while waiting for the display")
		case inspectErr != nil:
			m.logger.Warn("inspecting runtime container during display probe failed", "workspace_id", workspaceID, "error", inspectErr)
		case !info.Running:
			return fmt.Errorf("runtime container is %s while waiting for the display", info.State)
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("runtime display was not ready within %s: %w", displayProbeTimeout, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("runtime display probe aborted: %w", ctx.Err())
		case <-time.After(displayProbeInterval):
		}
	}
}

// stopLocked stops and removes the container of a workspace. The caller must
// hold the per-workspace lock.
func (m *Manager) stopLocked(ctx context.Context, workspaceID int64) (Snapshot, bool, error) {
	m.mu.Lock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		m.mu.Unlock()
		return Snapshot{}, false, nil
	}
	rt.state = StateStopping
	handles := m.detachStreamsLocked(workspaceID)
	snapshot := rt.snapshot(workspaceID)
	m.mu.Unlock()

	closeStreams(handles)

	operationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()

	if err := m.driver.Stop(operationCtx, workspaceID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
		m.markFailed(workspaceID, err, false)
		failed, _ := m.Get(workspaceID)
		return failed, true, err
	}
	if err := m.driver.Remove(operationCtx, workspaceID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
		m.markFailed(workspaceID, err, false)
		failed, _ := m.Get(workspaceID)
		return failed, true, err
	}

	m.mu.Lock()
	if current := m.runtimes[workspaceID]; current != nil {
		current.state = StateStopped
		current.containerID = ""
		m.clearRuntimeIPLocked(workspaceID)
		current.lastActivity = m.now()
		current.idleDeadline = time.Time{}
		snapshot = current.snapshot(workspaceID)
	}
	m.mu.Unlock()
	return snapshot, true, nil
}

// markFailed records a failed runtime, drops its streams and optionally removes
// the container corpse.
func (m *Manager) markFailed(workspaceID int64, cause error, removeContainer bool) {
	m.mu.Lock()
	if rt := m.runtimes[workspaceID]; rt != nil {
		rt.state = StateFailed
		rt.containerID = ""
		m.clearRuntimeIPLocked(workspaceID)
		rt.idleDeadline = time.Time{}
	}
	handles := m.detachStreamsLocked(workspaceID)
	m.mu.Unlock()

	closeStreams(handles)
	if removeContainer {
		m.removeContainer(context.Background(), workspaceID)
	}
	m.logger.Error("workspace runtime failed", "workspace_id", workspaceID, "error", cause)
}

func (m *Manager) removeContainer(ctx context.Context, workspaceID int64) {
	m.clearRuntimeIP(workspaceID)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := m.driver.Remove(cleanupCtx, workspaceID); err != nil && !errors.Is(err, runtime.ErrNotFound) {
		m.logger.Warn("removing runtime container failed", "workspace_id", workspaceID, "error", err)
	}
}

func (m *Manager) releaseStream(workspaceID int64, handle *streamHandle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if set, ok := m.streams[workspaceID]; ok {
		delete(set, handle)
		if len(set) == 0 {
			delete(m.streams, workspaceID)
		}
	}
	if rt := m.runtimes[workspaceID]; rt != nil && rt.state == StateRunning && len(m.streams[workspaceID]) == 0 {
		rt.state = StateIdle
		m.touchRuntime(rt, m.now())
	}
}

func (m *Manager) detachStreamsLocked(workspaceID int64) []*streamHandle {
	set := m.streams[workspaceID]
	if len(set) == 0 {
		return nil
	}
	handles := make([]*streamHandle, 0, len(set))
	for handle := range set {
		handles = append(handles, handle)
	}
	delete(m.streams, workspaceID)
	return handles
}

func (m *Manager) currentOptions(workspaceID int64) (StartOptions, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := m.runtimes[workspaceID]
	if rt == nil {
		return StartOptions{}, false
	}
	return StartOptions{Provider: rt.provider, Width: rt.width, Height: rt.height, Mode: rt.mode}, true
}

func (m *Manager) setRuntimeIPLocked(workspaceID int64, rawIP string) {
	rt := m.runtimes[workspaceID]
	normalized, ok := normalizeRuntimeIP(rawIP)
	if !ok {
		m.clearRuntimeIPLocked(workspaceID)
		return
	}
	if rt != nil && rt.ip != "" {
		if previous, previousOK := normalizeRuntimeIP(rt.ip); previousOK && previous != normalized && m.ipIndex[previous] == workspaceID {
			delete(m.ipIndex, previous)
		}
	}
	if previousWorkspaceID, exists := m.ipIndex[normalized]; exists && previousWorkspaceID != workspaceID {
		if previous := m.runtimes[previousWorkspaceID]; previous != nil {
			if previousIP, previousOK := normalizeRuntimeIP(previous.ip); previousOK && previousIP == normalized {
				previous.ip = ""
			}
		}
	}
	m.ipIndex[normalized] = workspaceID
	if rt != nil {
		rt.ip = rawIP
	}
}

func (m *Manager) clearRuntimeIPLocked(workspaceID int64) {
	for indexedIP, indexedWorkspaceID := range m.ipIndex {
		if indexedWorkspaceID == workspaceID {
			delete(m.ipIndex, indexedIP)
		}
	}
	if rt := m.runtimes[workspaceID]; rt != nil {
		rt.ip = ""
	}
}

func (m *Manager) clearRuntimeIP(workspaceID int64) {
	m.mu.Lock()
	m.clearRuntimeIPLocked(workspaceID)
	m.mu.Unlock()
}

func (m *Manager) touchRuntime(rt *runtimeState, now time.Time) {
	rt.lastActivity = now
	rt.idleDeadline = now.Add(m.idleTimeout)
}

func (m *Manager) lockFor(workspaceID int64) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.locks[workspaceID]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[workspaceID] = lock
	}
	return lock
}

func (m *Manager) workspaceIDs() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]int64, 0, len(m.runtimes))
	for workspaceID := range m.runtimes {
		ids = append(ids, workspaceID)
	}
	return ids
}

func closeStreams(handles []*streamHandle) {
	for _, handle := range handles {
		handle.close()
	}
}

// normalizeExternalIDs rejects malformed external ids and removes duplicates
// while keeping the first occurrence order.
func normalizeExternalIDs(ids []string, pattern *regexp.Regexp, label string) ([]string, error) {
	normalized := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !pattern.MatchString(id) {
			return nil, fmt.Errorf("%w: %s is invalid", ErrInvalidRequest, label)
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized, nil
}

// clearConsumedPermit drops the consumed marker of the previous permit so the
// guard does not treat a fresh permit as already spent. A marker that already
// names the new permit is kept: the guard may have consumed that permit between
// the permit write and this cleanup, and deleting it would allow a second use.
func clearConsumedPermit(root *os.Root, permitID string) error {
	data, err := root.ReadFile(permitConsumedFileName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var consumed guardConsumedPermit
	if json.Unmarshal(data, &consumed) == nil && consumed.PermitID == permitID {
		return nil
	}
	if err := root.Remove(permitConsumedFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func validateOptions(workspaceID int64, opts StartOptions) error {
	if workspaceID <= 0 {
		return fmt.Errorf("%w: workspace id must be positive", ErrInvalidRequest)
	}
	if !providerPattern.MatchString(opts.Provider) {
		return fmt.Errorf("%w: provider is invalid", ErrInvalidRequest)
	}
	if opts.Width != 0 && (opts.Width < MinWidth || opts.Width > MaxWidth) {
		return fmt.Errorf("%w: width %d is outside %d..%d", ErrInvalidRequest, opts.Width, MinWidth, MaxWidth)
	}
	if opts.Height != 0 && (opts.Height < MinHeight || opts.Height > MaxHeight) {
		return fmt.Errorf("%w: height %d is outside %d..%d", ErrInvalidRequest, opts.Height, MinHeight, MaxHeight)
	}
	if _, ok := policy.ParseMode(string(opts.Mode)); !ok {
		return fmt.Errorf("%w: mode is invalid", ErrInvalidRequest)
	}
	return nil
}

func withDefaults(opts StartOptions) StartOptions {
	if mode, ok := policy.ParseMode(string(opts.Mode)); ok {
		opts.Mode = mode
	}
	if opts.Width == 0 {
		opts.Width = DefaultWidth
	}
	if opts.Height == 0 {
		opts.Height = DefaultHeight
	}
	return opts
}

func validSize(width int, height int) bool {
	return width >= MinWidth && width <= MaxWidth && height >= MinHeight && height <= MaxHeight
}

func envInt(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func runtimeID(workspaceID int64) string {
	return "ws-" + strconv.FormatInt(workspaceID, 10)
}

func normalizeRuntimeIP(rawIP string) (string, bool) {
	value := strings.TrimSpace(rawIP)
	if value == "" {
		return "", false
	}
	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, portErr := strconv.Atoi(portText)
		if portErr != nil || port < 1 || port > 65535 {
			return "", false
		}
		value = host
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, "]"), "[")
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", false
	}
	if addr.Zone() != "" {
		addr = addr.WithZone("")
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return addr.String(), true
}
