package manager

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime/runtimetest"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type harness struct {
	mgr      *Manager
	driver   *runtimetest.FakeDriver
	display  *runtimetest.FakeDisplay
	clock    *testClock
	dataRoot string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWithRoots(t, t.TempDir(), "")
}

// newHarnessWithRoots builds a manager with an explicit host data root. An
// empty hostDataRoot exercises the default of reusing dataRoot.
func newHarnessWithRoots(t *testing.T, dataRoot string, hostDataRoot string) *harness {
	t.Helper()
	driver := runtimetest.NewFakeDriver()
	display := &runtimetest.FakeDisplay{}
	clock := newTestClock()
	mgr := New(driver, display, Options{
		DataRoot:     dataRoot,
		HostDataRoot: hostDataRoot,
		IdleTimeout:  10 * time.Minute,
		ScanInterval: time.Minute,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:          clock.Now,
		Chown:        workspaceChownHandover(),
	})
	return &harness{mgr: mgr, driver: driver, display: display, clock: clock, dataRoot: dataRoot}
}

// workspaceChownHandover uses the real ownership handover when the test process
// may chown and a no-op otherwise, so lifecycle tests stay independent of the
// process uid. The real handover is asserted in the filesystem tests.
func workspaceChownHandover() func(path string, uid int, gid int) error {
	if os.Geteuid() == 0 {
		return nil
	}
	return func(string, int, int) error { return nil }
}

func TestStartCreatesRuntimeAndWorkspaceMount(t *testing.T) {
	h := newHarness(t)

	snapshot, err := h.mgr.Start(context.Background(), 123, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	assert.Equal(t, "ws-123", snapshot.RuntimeID)
	assert.Equal(t, int64(123), snapshot.WorkspaceID)
	assert.Equal(t, StateRunning, snapshot.State)
	assert.Equal(t, h.clock.Now().Unix(), snapshot.CreatedAt)
	assert.Equal(t, h.clock.Now().Add(10*time.Minute).Unix(), snapshot.IdleDeadlineAt)
	assert.Equal(t, 1, h.driver.CreateCalls)
	assert.Equal(t, 1, h.driver.StartCalls)
	assert.Equal(t, 1, h.driver.ContainerCount())

	workspaceDir := WorkspaceDir(h.dataRoot, 123)
	for _, name := range []string{"profile", "uploads", "downloads", "cache", "tmp"} {
		assert.DirExists(t, filepath.Join(workspaceDir, name))
	}

	spec := h.driver.LastCreateSpec()
	assert.Equal(t, []string{workspaceDir + ":" + runtime.WorkspaceMountTarget}, spec.Binds)
	assert.Contains(t, spec.Env, "WW_WORKSPACE_DIR=/workspace")
	assert.Contains(t, spec.Env, "WW_DISPLAY=:99")
	assert.Contains(t, spec.Env, "WW_SCREEN_WIDTH=1280")
	assert.Contains(t, spec.Env, "WW_SCREEN_HEIGHT=720")
	assert.Contains(t, spec.Env, "WW_VNC_PORT=5900")
	assert.Contains(t, spec.Env, "WW_PROVIDER=chatgpt")
}

func TestStartIsIdempotent(t *testing.T) {
	h := newHarness(t)

	first, err := h.mgr.Start(context.Background(), 7, StartOptions{Provider: "chatgpt", Width: 1920, Height: 1080})
	require.NoError(t, err)
	second, err := h.mgr.Start(context.Background(), 7, StartOptions{Provider: "chatgpt", Width: 640, Height: 360})
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, h.driver.CreateCalls)
	assert.Equal(t, 1, h.driver.ContainerCount())
}

func TestConcurrentStartCreatesOneRuntime(t *testing.T) {
	h := newHarness(t)

	const workers = 8
	var waitGroup sync.WaitGroup
	snapshots := make([]Snapshot, workers)
	errorsSeen := make([]error, workers)
	for index := 0; index < workers; index++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			snapshots[worker], errorsSeen[worker] = h.mgr.Start(context.Background(), 11, StartOptions{Provider: "chatgpt"})
		}(index)
	}
	waitGroup.Wait()

	for index := range errorsSeen {
		require.NoError(t, errorsSeen[index])
		assert.Equal(t, "ws-11", snapshots[index].RuntimeID)
	}
	assert.Equal(t, 1, h.driver.CreateCalls)
	assert.Equal(t, 1, h.driver.ContainerCount())
}

func TestStopKeepsProfileDirectory(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 123, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	profileFile := filepath.Join(WorkspaceDir(h.dataRoot, 123), "profile", "Cookies")
	require.NoError(t, os.WriteFile(profileFile, []byte("profile-data"), 0o600))

	snapshot, err := h.mgr.Stop(context.Background(), 123)
	require.NoError(t, err)
	assert.Equal(t, StateStopped, snapshot.State)
	assert.Zero(t, snapshot.IdleDeadlineAt)
	assert.Equal(t, 0, h.driver.ContainerCount())
	assert.FileExists(t, profileFile)

	_, err = h.mgr.Stop(context.Background(), 999)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestStreamDisconnectIdlesAndIdleTimeoutStops(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 42, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	display, err := h.mgr.OpenDisplayStream(context.Background(), 42)
	require.NoError(t, err)
	defer display.Close()

	done, release := h.mgr.AttachStream(42, display)
	select {
	case <-done:
		t.Fatal("stream must stay open while the runtime is live")
	default:
	}
	snapshot, _ := h.mgr.Get(42)
	assert.Equal(t, StateRunning, snapshot.State)

	release()

	snapshot, _ = h.mgr.Get(42)
	assert.Equal(t, StateIdle, snapshot.State)
	assert.Equal(t, h.clock.Now().Add(10*time.Minute).Unix(), snapshot.IdleDeadlineAt)

	h.clock.Advance(10*time.Minute + time.Second)
	h.mgr.ScanOnce(context.Background())

	snapshot, found := h.mgr.Get(42)
	require.True(t, found)
	assert.Equal(t, StateStopped, snapshot.State)
	assert.Equal(t, 1, h.driver.StopCalls)
	assert.Equal(t, 0, h.driver.ContainerCount())
}

func TestActivityExtendsIdleDeadline(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 5, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	h.clock.Advance(9 * time.Minute)
	h.mgr.ScanOnce(context.Background())
	snapshot, _ := h.mgr.Get(5)
	assert.Equal(t, StateRunning, snapshot.State)

	_, found := h.mgr.Activity(5)
	require.True(t, found)
	h.clock.Advance(9 * time.Minute)
	h.mgr.ScanOnce(context.Background())
	snapshot, _ = h.mgr.Get(5)
	assert.Equal(t, StateRunning, snapshot.State)

	h.clock.Advance(2 * time.Minute)
	h.mgr.ScanOnce(context.Background())
	snapshot, _ = h.mgr.Get(5)
	assert.Equal(t, StateStopped, snapshot.State)
}

func TestCrashedContainerFailsRuntimeAndRestartRecovers(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 77, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	display, err := h.mgr.OpenDisplayStream(context.Background(), 77)
	require.NoError(t, err)
	done, release := h.mgr.AttachStream(77, display)
	defer release()

	h.driver.SetRunning(77, false)
	h.mgr.ScanOnce(context.Background())

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream was not closed after the runtime failed")
	}

	snapshot, found := h.mgr.Get(77)
	require.True(t, found)
	assert.Equal(t, StateFailed, snapshot.State)
	assert.Equal(t, 0, h.driver.ContainerCount())

	recovered, err := h.mgr.Restart(context.Background(), 77, StartOptions{})
	require.NoError(t, err)
	assert.Equal(t, StateRunning, recovered.State)
	assert.Equal(t, 2, h.driver.CreateCalls)
}

func TestRestartWithoutRuntimeIsNotFound(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Restart(context.Background(), 3, StartOptions{})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestReconcileAdoptsRunningAndRemovesStopped(t *testing.T) {
	h := newHarness(t)
	h.driver.Seed(21, runtimetest.Container{
		ID:        "container-adopted",
		Running:   true,
		IP:        "10.77.0.99",
		Env:       map[string]string{"WW_PROVIDER": "chatgpt", "WW_SCREEN_WIDTH": "1920", "WW_SCREEN_HEIGHT": "1080"},
		StartedAt: h.clock.Now().Add(-time.Hour),
	})
	h.driver.Seed(22, runtimetest.Container{ID: "container-stale"})

	require.NoError(t, h.mgr.Reconcile(context.Background()))

	snapshot, found := h.mgr.Get(21)
	require.True(t, found)
	assert.Equal(t, StateRunning, snapshot.State)
	assert.Equal(t, h.clock.Now().Add(-time.Hour).Unix(), snapshot.CreatedAt)

	_, found = h.mgr.Get(22)
	assert.False(t, found)
	assert.Equal(t, 1, h.driver.ContainerCount())

	restarted, err := h.mgr.Restart(context.Background(), 21, StartOptions{})
	require.NoError(t, err)
	assert.Equal(t, StateRunning, restarted.State)
	spec := h.driver.LastCreateSpec()
	assert.Contains(t, spec.Env, "WW_PROVIDER=chatgpt")
	assert.Contains(t, spec.Env, "WW_SCREEN_WIDTH=1920")
}

func TestOpenDisplayStreamRequiresLiveRuntime(t *testing.T) {
	h := newHarness(t)

	_, err := h.mgr.OpenDisplayStream(context.Background(), 1)
	require.ErrorIs(t, err, runtime.ErrNotRunning)

	_, err = h.mgr.Start(context.Background(), 1, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	_, err = h.mgr.Stop(context.Background(), 1)
	require.NoError(t, err)

	_, err = h.mgr.OpenDisplayStream(context.Background(), 1)
	require.ErrorIs(t, err, runtime.ErrNotRunning)
}

func TestStartRejectsInvalidParameters(t *testing.T) {
	h := newHarness(t)

	_, err := h.mgr.Start(context.Background(), 1, StartOptions{Provider: "bad provider"})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = h.mgr.Start(context.Background(), 1, StartOptions{Provider: "chatgpt", Width: 100})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = h.mgr.Start(context.Background(), 1, StartOptions{Provider: "chatgpt", Height: 5000})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = h.mgr.Start(context.Background(), 0, StartOptions{Provider: "chatgpt"})
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Equal(t, 0, h.driver.CreateCalls)
}

func TestStartUsesHostDataRootOnlyForTheBindSource(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "internal")
	hostDataRoot := filepath.Join(t.TempDir(), "host")
	require.NoError(t, os.MkdirAll(dataRoot, 0o700))
	h := newHarnessWithRoots(t, dataRoot, hostDataRoot)

	_, err := h.mgr.Start(context.Background(), 31, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	spec := h.driver.LastCreateSpec()
	assert.Equal(t, []string{WorkspaceDir(hostDataRoot, 31) + ":" + runtime.WorkspaceMountTarget}, spec.Binds,
		"the runtime bind source must be the path the Docker daemon can resolve")
	assert.Contains(t, spec.Env, "WW_WORKSPACE_DIR=/workspace")

	assert.DirExists(t, WorkspaceDir(dataRoot, 31), "local file operations must use the container data root")
	assert.NoDirExists(t, WorkspaceDir(hostDataRoot, 31), "the agent must not create the host visible path locally")
}

func TestStartFailsClosedWhenContainerExitsImmediately(t *testing.T) {
	h := newHarness(t)
	h.driver.ExitOnStart = true

	snapshot, err := h.mgr.Start(context.Background(), 15, StartOptions{Provider: "chatgpt"})
	require.Error(t, err)
	assert.Equal(t, Snapshot{}, snapshot)
	assert.Equal(t, 0, h.driver.ContainerCount())

	stored, found := h.mgr.Get(15)
	require.True(t, found)
	assert.Equal(t, StateFailed, stored.State)
	assert.Zero(t, stored.IdleDeadlineAt)

	_, err = h.mgr.OpenDisplayStream(context.Background(), 15)
	require.ErrorIs(t, err, runtime.ErrNotRunning)
}

func TestStartWaitsForDisplayReadiness(t *testing.T) {
	h := newHarness(t)
	probeConn := runtimetest.NewFakeConn()
	h.display.FailFirst = 2
	h.display.ConnectFunc = func(context.Context, int64) (io.ReadWriteCloser, error) {
		return probeConn, nil
	}

	started := time.Now()
	snapshot, err := h.mgr.Start(context.Background(), 61, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	assert.Equal(t, StateRunning, snapshot.State)
	assert.Equal(t, 3, h.display.Connects, "the probe must retry until the display answers")
	assert.GreaterOrEqual(t, time.Since(started), 2*displayProbeInterval)
	select {
	case <-probeConn.Closed:
	default:
		t.Fatal("the readiness probe connection must be closed")
	}
}

func TestStartFailsClosedWhenDisplayNeverBecomesReady(t *testing.T) {
	h := newHarness(t)
	h.display.Err = runtime.ErrNotRunning

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	snapshot, err := h.mgr.Start(ctx, 62, StartOptions{Provider: "chatgpt"})
	require.Error(t, err)
	assert.Equal(t, Snapshot{}, snapshot)
	assert.Equal(t, 0, h.driver.ContainerCount())

	stored, found := h.mgr.Get(62)
	require.True(t, found)
	assert.Equal(t, StateFailed, stored.State)

	_, err = h.mgr.OpenDisplayStream(context.Background(), 62)
	require.ErrorIs(t, err, runtime.ErrNotRunning)
}
func TestWorkspaceDirIsDerivedFromIntegerID(t *testing.T) {
	assert.Equal(t, filepath.Join("/data/web-workspaces", "workspace-42"), WorkspaceDir("/data/web-workspaces", 42))
}
