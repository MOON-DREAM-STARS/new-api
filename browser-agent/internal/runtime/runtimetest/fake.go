// Package runtimetest provides in-memory Driver and DisplayTransport fakes used
// by the manager, HTTP and stream tests.
package runtimetest

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// Container is one fake runtime container.
type Container struct {
	ID        string
	Running   bool
	IP        string
	Env       map[string]string
	StartedAt time.Time
}

// FakeDriver is an in-memory runtime.Driver.
type FakeDriver struct {
	mu         sync.Mutex
	containers map[int64]*Container
	nextID     int

	CreateCalls  int
	StartCalls   int
	InspectCalls int
	StopCalls    int
	RemoveCalls  int
	ListCalls    int

	CreateErr error
	// ExitOnStart makes Start leave the container stopped, simulating a runtime
	// image that starts and exits immediately.
	ExitOnStart bool
	lastSpec    runtime.CreateSpec
}

// NewFakeDriver returns an empty fake driver.
func NewFakeDriver() *FakeDriver {
	return &FakeDriver{containers: map[int64]*Container{}}
}

// Seed installs a container without going through Create.
func (f *FakeDriver) Seed(workspaceID int64, container Container) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := container
	f.containers[workspaceID] = &stored
}

// SetRunning flips the container state, simulating a crashed runtime.
func (f *FakeDriver) SetRunning(workspaceID int64, running bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if container, ok := f.containers[workspaceID]; ok {
		container.Running = running
	}
}

// ContainerCount reports how many containers the fake currently holds.
func (f *FakeDriver) ContainerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.containers)
}

// LastCreateSpec returns the spec of the most recent Create call.
func (f *FakeDriver) LastCreateSpec() runtime.CreateSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSpec
}

func (f *FakeDriver) Create(_ context.Context, spec runtime.CreateSpec) (runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CreateCalls++
	f.lastSpec = spec
	if f.CreateErr != nil {
		return runtime.ContainerInfo{}, f.CreateErr
	}
	if existing, ok := f.containers[spec.WorkspaceID]; ok {
		if existing.Running {
			info := f.infoLocked(spec.WorkspaceID, existing)
			info.Reused = true
			return info, nil
		}
		delete(f.containers, spec.WorkspaceID)
	}
	f.nextID++
	container := &Container{
		ID:        fmt.Sprintf("container-%d", f.nextID),
		IP:        fmt.Sprintf("10.77.0.%d", f.nextID),
		Env:       envMap(spec.Env),
		StartedAt: time.Now(),
	}
	f.containers[spec.WorkspaceID] = container
	return f.infoLocked(spec.WorkspaceID, container), nil
}

func (f *FakeDriver) Start(_ context.Context, workspaceID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StartCalls++
	container, ok := f.containers[workspaceID]
	if !ok {
		return runtime.ErrNotFound
	}
	container.StartedAt = time.Now()
	container.Running = !f.ExitOnStart
	return nil
}

func (f *FakeDriver) Inspect(_ context.Context, workspaceID int64) (runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InspectCalls++
	container, ok := f.containers[workspaceID]
	if !ok {
		return runtime.ContainerInfo{}, runtime.ErrNotFound
	}
	return f.infoLocked(workspaceID, container), nil
}

func (f *FakeDriver) Stop(_ context.Context, workspaceID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StopCalls++
	container, ok := f.containers[workspaceID]
	if !ok {
		return runtime.ErrNotFound
	}
	container.Running = false
	return nil
}

func (f *FakeDriver) Remove(_ context.Context, workspaceID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RemoveCalls++
	if _, ok := f.containers[workspaceID]; !ok {
		return runtime.ErrNotFound
	}
	delete(f.containers, workspaceID)
	return nil
}

func (f *FakeDriver) List(_ context.Context) ([]runtime.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListCalls++
	infos := make([]runtime.ContainerInfo, 0, len(f.containers))
	for workspaceID, container := range f.containers {
		infos = append(infos, f.infoLocked(workspaceID, container))
	}
	return infos, nil
}

func (f *FakeDriver) infoLocked(workspaceID int64, container *Container) runtime.ContainerInfo {
	state := runtime.ContainerExited
	if container.Running {
		state = runtime.ContainerRunning
	}
	env := make(map[string]string, len(container.Env))
	for key, value := range container.Env {
		env[key] = value
	}
	return runtime.ContainerInfo{
		ID:          container.ID,
		State:       state,
		Running:     container.Running,
		IP:          container.IP,
		WorkspaceID: workspaceID,
		Env:         env,
		StartedAt:   container.StartedAt,
	}
}

func envMap(entries []string) map[string]string {
	env := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		env[key] = value
	}
	return env
}

// FakeDisplay is an in-memory runtime.DisplayTransport. Every Connect call
// returns a fresh FakeConn unless ConnectFunc supplies one, so a readiness
// probe never shares a connection with a display stream.
type FakeDisplay struct {
	mu       sync.Mutex
	Connects int
	Err      error
	// FailFirst makes the first N Connect calls fail with FailErr (default
	// runtime.ErrNotRunning), simulating a display that is not ready yet.
	FailFirst   int
	FailErr     error
	ConnectFunc func(ctx context.Context, workspaceID int64) (io.ReadWriteCloser, error)
}

func (f *FakeDisplay) Connect(ctx context.Context, workspaceID int64) (io.ReadWriteCloser, error) {
	f.mu.Lock()
	f.Connects++
	failFirst := f.FailFirst > 0
	if failFirst {
		f.FailFirst--
	}
	failErr := f.FailErr
	displayErr := f.Err
	connect := f.ConnectFunc
	f.mu.Unlock()

	if failFirst {
		if failErr != nil {
			return nil, failErr
		}
		return nil, runtime.ErrNotRunning
	}
	if displayErr != nil {
		return nil, displayErr
	}
	if connect != nil {
		return connect(ctx, workspaceID)
	}
	return NewFakeConn(), nil
}

// FakeConn is an io.ReadWriteCloser that records whether it was closed.
type FakeConn struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	once   sync.Once
	Closed chan struct{}
}

// NewFakeConn returns a connected FakeConn.
func NewFakeConn() *FakeConn {
	reader, writer := io.Pipe()
	return &FakeConn{reader: reader, writer: writer, Closed: make(chan struct{})}
}

func (c *FakeConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *FakeConn) Write(p []byte) (int, error) {
	return c.writer.Write(p)
}

func (c *FakeConn) Close() error {
	c.once.Do(func() {
		close(c.Closed)
		_ = c.reader.Close()
		_ = c.writer.Close()
	})
	return nil
}
