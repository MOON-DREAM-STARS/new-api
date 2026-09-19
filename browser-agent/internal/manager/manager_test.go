package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
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
		DataRoot:       dataRoot,
		HostDataRoot:   hostDataRoot,
		EgressProxyURL: "http://ws-agent:8731",
		IdleTimeout:    10 * time.Minute,
		ScanInterval:   time.Minute,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:            clock.Now,
		Chown:          workspaceChownHandover(),
	})
	return &harness{mgr: mgr, driver: driver, display: display, clock: clock, dataRoot: dataRoot}
}

// workspaceChownHandover uses the real ownership handover when the test process
// may chown and a no-op otherwise, so lifecycle tests stay independent of the
// process uid. The real handover is asserted in the filesystem tests.
func workspaceChownHandover() func(root *os.Root, name string, uid int, gid int) error {
	if os.Geteuid() == 0 {
		return nil
	}
	return func(*os.Root, string, int, int) error { return nil }
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
	assert.Contains(t, spec.Env, "WW_PROXY_SERVER=http://ws-agent:8731")
	assert.Contains(t, spec.Env, "WW_GUARD_MODE=LOCKED")
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
func TestStartDefaultAndLoginModesAreInjectedAndIndexed(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 101, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	spec := h.driver.LastCreateSpec()
	assert.Contains(t, spec.Env, "WW_GUARD_MODE=LOCKED")
	assert.Contains(t, spec.Env, "WW_PROXY_SERVER=http://ws-agent:8731")
	mode, workspaceID, ok := h.mgr.LookupSource("10.77.0.1")
	require.True(t, ok)
	assert.Equal(t, policy.ModeLocked, mode)
	assert.Equal(t, int64(101), workspaceID)

	_, err = h.mgr.Start(context.Background(), 102, StartOptions{Provider: "chatgpt", Mode: policy.ModeLogin})
	require.NoError(t, err)
	spec = h.driver.LastCreateSpec()
	assert.Contains(t, spec.Env, "WW_GUARD_MODE=LOGIN")
	mode, workspaceID, ok = h.mgr.LookupSource("10.77.0.2")
	require.True(t, ok)
	assert.Equal(t, policy.ModeLogin, mode)
	assert.Equal(t, int64(102), workspaceID)
}

func TestStartRejectsInvalidMode(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 1, StartOptions{Provider: "chatgpt", Mode: policy.Mode("OPEN")})
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Equal(t, 0, h.driver.CreateCalls)
}

func TestLookupSourceClearsOnStopAndFailure(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 201, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	_, _, ok := h.mgr.LookupSource("10.77.0.1")
	require.True(t, ok)

	_, err = h.mgr.Stop(context.Background(), 201)
	require.NoError(t, err)
	_, _, ok = h.mgr.LookupSource("10.77.0.1")
	assert.False(t, ok)
	_, _, ok = h.mgr.LookupSource("not-an-ip")
	assert.False(t, ok)

	_, err = h.mgr.Start(context.Background(), 202, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	_, _, ok = h.mgr.LookupSource("10.77.0.2")
	require.True(t, ok)
	h.driver.SetRunning(202, false)
	h.mgr.ScanOnce(context.Background())
	_, _, ok = h.mgr.LookupSource("10.77.0.2")
	assert.False(t, ok)
}

func TestReconcileIndexesRuntimeModeAndNormalizesIP(t *testing.T) {
	h := newHarness(t)
	h.driver.Seed(301, runtimetest.Container{
		ID:        "container-reconciled",
		Running:   true,
		IP:        "::ffff:10.77.1.9",
		Env:       map[string]string{"WW_PROVIDER": "chatgpt", "WW_GUARD_MODE": "LOGIN"},
		StartedAt: h.clock.Now(),
	})
	require.NoError(t, h.mgr.Reconcile(context.Background()))

	mode, workspaceID, ok := h.mgr.LookupSource("10.77.1.9")
	require.True(t, ok)
	assert.Equal(t, policy.ModeLogin, mode)
	assert.Equal(t, int64(301), workspaceID)
	_, _, ok = h.mgr.LookupSource("[::ffff:10.77.1.9]:443")
	assert.True(t, ok)
}

func TestNormalizeRuntimeIP(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "ipv4", raw: "10.77.0.1", want: "10.77.0.1", ok: true},
		{name: "ipv4 with port", raw: "10.77.0.1:443", want: "10.77.0.1", ok: true},
		{name: "ipv4 in ipv6", raw: "::ffff:10.77.0.1", want: "10.77.0.1", ok: true},
		{name: "bracketed ipv6 with port", raw: "[::1]:443", want: "::1", ok: true},
		{name: "zone removed", raw: "fe80::1%eth0", want: "fe80::1", ok: true},
		{name: "invalid", raw: "not-an-ip", ok: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := normalizeRuntimeIP(testCase.raw)
			assert.Equal(t, testCase.ok, ok)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestWorkspaceDirIsDerivedFromIntegerID(t *testing.T) {
	assert.Equal(t, filepath.Join("/data/web-workspaces", "workspace-42"), WorkspaceDir("/data/web-workspaces", 42))
}

func TestStartCreatesGuardStateDirectoryWithRuntimeOwnership(t *testing.T) {
	h := newHarness(t)
	_, err := h.mgr.Start(context.Background(), 77, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, 77))
	info, err := os.Stat(guardDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), guardDir)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	if os.Geteuid() == 0 {
		uid, gid, known := ownerOf(guardDir)
		require.True(t, known)
		assert.Equal(t, runtime.RuntimeUID, uid)
		assert.Equal(t, runtime.RuntimeGID, gid)
	}
}

func TestPutOwnershipWritesValidatedDeduplicatedState(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(88)
	projectA := "g-p-" + strings.Repeat("a", 32)
	projectB := "g-p-" + strings.Repeat("b", 32)
	conversationA := "abcd-1234"
	conversationB := "EFGH-5678"

	counts, err := h.mgr.PutOwnership(workspaceID, Ownership{
		Generation:    7,
		Projects:      []string{projectB, projectA, projectA},
		Conversations: []string{conversationA, conversationB, conversationA},
	})
	require.NoError(t, err)
	assert.Equal(t, OwnershipCounts{Generation: 7, Projects: 2, Conversations: 2}, counts)

	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	info, err := os.Stat(guardDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	data, err := os.ReadFile(filepath.Join(guardDir, ownershipFileName))
	require.NoError(t, err)
	var stored guardOwnership
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, int64(7), stored.Generation)
	assert.Equal(t, []string{projectB, projectA}, stored.Projects)
	assert.Equal(t, []string{conversationA, conversationB}, stored.Conversations)
	assert.Equal(t, h.clock.Now().Unix(), stored.UpdatedAt)
}

func TestPutOwnershipRejectsInvalidResources(t *testing.T) {
	h := newHarness(t)
	cases := []struct {
		name   string
		update Ownership
	}{
		{name: "negative generation", update: Ownership{Generation: -1}},
		{name: "project without prefix", update: Ownership{Projects: []string{strings.Repeat("a", 32)}}},
		{name: "project with uppercase hex", update: Ownership{Projects: []string{"g-p-" + strings.Repeat("A", 32)}}},
		{name: "project with short hex", update: Ownership{Projects: []string{"g-p-" + strings.Repeat("a", 31)}}},
		{name: "conversation too short", update: Ownership{Conversations: []string{"short"}}},
		{name: "conversation with underscore", update: Ownership{Conversations: []string{"abcd_1234"}}},
		{name: "conversation too long", update: Ownership{Conversations: []string{strings.Repeat("c", 65)}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := h.mgr.PutOwnership(101, testCase.update)
			require.ErrorIs(t, err, ErrInvalidRequest)
		})
	}
	assert.NoFileExists(t, filepath.Join(guardStateDir(WorkspaceDir(h.dataRoot, 101)), ownershipFileName))
}

func TestPutOwnershipFailsWhenGuardStateDirectoryCannotBeCreated(t *testing.T) {
	h := newHarness(t)
	workspaceDir := WorkspaceDir(h.dataRoot, 99)
	require.NoError(t, os.WriteFile(workspaceDir, []byte("not a directory"), 0o600))

	_, err := h.mgr.PutOwnership(99, Ownership{})
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(guardStateDir(workspaceDir), ownershipFileName))
}

func TestPermitLifecycleIssuesConsumesAndReplaces(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(55)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	issued, err := h.mgr.IssuePermit(workspaceID, "permit-0001", "project_create", 300, "Quarterly Review")
	require.NoError(t, err)
	assert.Equal(t, "permit-0001", issued.PermitID)
	assert.Equal(t, h.clock.Now().Add(300*time.Second).Unix(), issued.ExpiresAt)

	data, err := os.ReadFile(filepath.Join(guardDir, permitFileName))
	require.NoError(t, err)
	var stored guardPermit
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, guardPermit{
		PermitID:    "permit-0001",
		Kind:        "project_create",
		IssuedAt:    h.clock.Now().Unix(),
		ExpiresAt:   issued.ExpiresAt,
		DisplayName: "Quarterly Review",
	}, stored)

	// The guard consumes the permit and records the consumed marker.
	consumedPath := filepath.Join(guardDir, permitConsumedFileName)
	require.NoError(t, os.WriteFile(consumedPath, []byte(`{"permit_id":"permit-0001","consumed_at":1758192000}`), 0o600))

	h.clock.Advance(time.Minute)
	reissued, err := h.mgr.IssuePermit(workspaceID, "permit-0002", "project_create", 60, "")
	require.NoError(t, err)
	assert.Equal(t, h.clock.Now().Add(time.Minute).Unix(), reissued.ExpiresAt)
	assert.NoFileExists(t, consumedPath)

	data, err = os.ReadFile(filepath.Join(guardDir, permitFileName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, "permit-0002", stored.PermitID)
	assert.Equal(t, h.clock.Now().Unix(), stored.IssuedAt)
	assert.Empty(t, stored.DisplayName, "a permit without a display name stays the manual flow")
}

func TestIssuePermitKeepsConsumedMarkerOfTheNewPermit(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(56)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	consumedPath := filepath.Join(guardDir, permitConsumedFileName)
	require.NoError(t, os.WriteFile(consumedPath, []byte(`{"permit_id":"permit-0003","consumed_at":1758192000}`), 0o600))

	_, err = h.mgr.IssuePermit(workspaceID, "permit-0003", "project_create", 60, "")
	require.NoError(t, err)

	// The guard consumed the permit while the issue request was in flight;
	// deleting the marker would let the same permit be spent twice.
	assert.FileExists(t, consumedPath)
}

func TestIssuePermitRequiresRunningRuntime(t *testing.T) {
	h := newHarness(t)

	_, err := h.mgr.IssuePermit(404, "permit-0001", "project_create", 60, "")
	require.ErrorIs(t, err, ErrNotFound)

	workspaceID := int64(66)
	_, err = h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)
	_, err = h.mgr.Stop(context.Background(), workspaceID)
	require.NoError(t, err)

	_, err = h.mgr.IssuePermit(workspaceID, "permit-0001", "project_create", 60, "")
	require.ErrorIs(t, err, runtime.ErrNotRunning)
	assert.NoFileExists(t, filepath.Join(guardStateDir(WorkspaceDir(h.dataRoot, workspaceID)), permitFileName))
}

func TestIssuePermitRejectsInvalidRequest(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(67)
	_, err := h.mgr.Start(context.Background(), workspaceID, StartOptions{Provider: "chatgpt"})
	require.NoError(t, err)

	cases := []struct {
		name        string
		permitID    string
		kind        string
		ttl         int
		displayName string
	}{
		{name: "permit id too short", permitID: "short", kind: "project_create", ttl: 60},
		{name: "permit id too long", permitID: strings.Repeat("p", 65), kind: "project_create", ttl: 60},
		{name: "unknown kind", permitID: "permit-0001", kind: "project_delete", ttl: 60},
		{name: "ttl zero", permitID: "permit-0001", kind: "project_create", ttl: 0},
		{name: "ttl above limit", permitID: "permit-0001", kind: "project_create", ttl: 3601},
		{name: "display name too long", permitID: "permit-0001", kind: "project_create", ttl: 60, displayName: strings.Repeat("x", 65)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := h.mgr.IssuePermit(workspaceID, testCase.permitID, testCase.kind, testCase.ttl, testCase.displayName)
			require.ErrorIs(t, err, ErrInvalidRequest)
		})
	}
	assert.NoFileExists(t, filepath.Join(guardStateDir(WorkspaceDir(h.dataRoot, workspaceID)), permitFileName))
}

func TestObservationsWithoutGuardStateIsEmpty(t *testing.T) {
	h := newHarness(t)

	page, err := h.mgr.Observations(4242)
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.NextOffset)
	assert.Empty(t, page.Observations)
	require.NotNil(t, page.Observations)

	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	assert.JSONEq(t, `{"observations":[],"next_offset":0}`, string(encoded))
}

func TestObservationsReadCompleteLinesAndIgnorePartialTail(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(44)
	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardDir, 0o700))

	projectID := "g-p-" + strings.Repeat("a", 32)
	first := `{"event":"project_created","permit_id":"permit-0001","external_project_id":"` + projectID + `","slug":"demo","observed_at":100}`
	second := `{"event":"conversation_created","external_project_id":"` + projectID + `","external_conversation_id":"abcd-1234","observed_at":101}`
	partial := `{"event":"project_not_found"`
	observationsPath := filepath.Join(guardDir, observationsFileName)
	require.NoError(t, os.WriteFile(observationsPath, []byte(first+"\n"+second+"\n"+partial), 0o644))

	page, err := h.mgr.Observations(workspaceID)
	require.NoError(t, err)
	require.Len(t, page.Observations, 2)
	assert.Equal(t, int64(len(first)+len(second)+2), page.NextOffset)
	assert.JSONEq(t, first, string(page.Observations[0]))
	assert.JSONEq(t, second, string(page.Observations[1]))

	// Pulling again without an ack returns the same objects and the same offset.
	repeated, err := h.mgr.Observations(workspaceID)
	require.NoError(t, err)
	assert.Equal(t, page.NextOffset, repeated.NextOffset)
	require.Len(t, repeated.Observations, 2)

	acked, err := h.mgr.AckObservations(workspaceID, page.NextOffset)
	require.NoError(t, err)
	assert.Equal(t, page.NextOffset, acked)

	afterAck, err := h.mgr.Observations(workspaceID)
	require.NoError(t, err)
	assert.Empty(t, afterAck.Observations)
	assert.Equal(t, acked, afterAck.NextOffset, "the incomplete tail must not advance the offset")

	// Once the guard finishes the line it becomes readable.
	require.NoError(t, os.WriteFile(observationsPath, []byte(first+"\n"+second+"\n"+partial+"}\n"), 0o644))
	completed, err := h.mgr.Observations(workspaceID)
	require.NoError(t, err)
	require.Len(t, completed.Observations, 1)
	assert.JSONEq(t, partial+"}", string(completed.Observations[0]))
	assert.Equal(t, int64(len(first)+len(second)+len(partial)+4), completed.NextOffset)
}

func TestObservationsRejectsCorruptCompleteLine(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(46)
	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(guardDir, observationsFileName), []byte("not json\n"), 0o644))

	_, err := h.mgr.Observations(workspaceID)
	require.Error(t, err)
}

func TestAckObservationsValidatesOffset(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(45)
	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))
	require.NoError(t, os.MkdirAll(guardDir, 0o700))
	observationsPath := filepath.Join(guardDir, observationsFileName)

	// A workspace whose guard never wrote anything accepts only offset 0.
	_, err := h.mgr.AckObservations(workspaceID, 1)
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = h.mgr.AckObservations(workspaceID, -1)
	require.ErrorIs(t, err, ErrInvalidRequest)
	acked, err := h.mgr.AckObservations(workspaceID, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(0), acked)

	line := `{"event":"project_not_found","external_project_id":"g-p-` + strings.Repeat("a", 32) + `","observed_at":100}` + "\n"
	require.NoError(t, os.WriteFile(observationsPath, []byte(line), 0o644))

	_, err = h.mgr.AckObservations(workspaceID, int64(len(line)+1))
	require.ErrorIs(t, err, ErrInvalidRequest)
	acked, err = h.mgr.AckObservations(workspaceID, int64(len(line)))
	require.NoError(t, err)
	assert.Equal(t, int64(len(line)), acked)

	offsetData, err := os.ReadFile(filepath.Join(guardDir, observationsOffsetName))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprint(len(line)), string(offsetData))
}

func TestConcurrentOwnershipPutsNeverExposePartialFiles(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(33)
	guardDir := guardStateDir(WorkspaceDir(h.dataRoot, workspaceID))

	const writers = 8
	const rounds = 20
	var waitGroup sync.WaitGroup
	writeErrors := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			project := fmt.Sprintf("g-p-%032x", index+1)
			for round := 0; round < rounds; round++ {
				_, err := h.mgr.PutOwnership(workspaceID, Ownership{
					Generation: int64(index*1000 + round),
					Projects:   []string{project},
				})
				if err != nil {
					writeErrors <- err
					return
				}
			}
		}(writer)
	}

	stop := make(chan struct{})
	readErrors := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				readErrors <- nil
				return
			default:
			}
			data, err := os.ReadFile(filepath.Join(guardDir, ownershipFileName))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				readErrors <- err
				return
			}
			var stored guardOwnership
			if err := json.Unmarshal(data, &stored); err != nil {
				readErrors <- fmt.Errorf("partial ownership state: %w", err)
				return
			}
			if len(stored.Projects) != 1 || len(stored.Conversations) != 0 {
				readErrors <- fmt.Errorf("unexpected ownership state: %s", data)
				return
			}
		}
	}()

	waitGroup.Wait()
	close(stop)
	require.NoError(t, <-readErrors)
	close(writeErrors)
	for err := range writeErrors {
		require.NoError(t, err)
	}
}
