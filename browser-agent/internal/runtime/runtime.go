// Package runtime defines the contracts shared by the workspace Manager, the
// Docker driver and the display transport. The Manager depends only on these
// interfaces so that Docker specific behaviour stays replaceable in tests.
package runtime

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	// WorkspaceMountTarget is the only mount point the runtime container gets.
	WorkspaceMountTarget = "/workspace"
	// VNCPort is the RFB display port exposed inside the runtime container.
	VNCPort = 5900
	// KasmPort is the KasmVNC web client and websocket port exposed inside the
	// runtime container. Both display ports remain private to the runtime
	// network; the agent is the only proxy in front of them.
	KasmPort = 6901

	// RuntimeUID and RuntimeGID are the non-root identity the runtime image
	// runs as (browser-agent/runtime/Dockerfile declares the user webworkspace
	// with uid/gid 10001). The agent hands every workspace directory to this
	// identity, otherwise the runtime container cannot write its profile.
	RuntimeUID = 10001
	RuntimeGID = 10001
)

var (
	// ErrNotFound reports that a managed runtime container does not exist.
	ErrNotFound = errors.New("container not found")
	// ErrNotRunning reports that a runtime exists but cannot serve a display
	// connection.
	ErrNotRunning = errors.New("runtime not running")
)

// ContainerState is the subset of Docker container states the agent uses.
type ContainerState string

const (
	ContainerCreated ContainerState = "created"
	ContainerRunning ContainerState = "running"
	ContainerExited  ContainerState = "exited"
	ContainerDead    ContainerState = "dead"
	ContainerUnknown ContainerState = "unknown"
)

// ContainerInfo describes one managed runtime container. IP and Env are
// internal-only values and must never be serialised into an API response.
type ContainerInfo struct {
	ID          string
	State       ContainerState
	Running     bool
	IP          string
	WorkspaceID int64
	Env         map[string]string
	StartedAt   time.Time
	// Reused is set when Create found an already running container with the
	// deterministic runtime name instead of creating a new one.
	Reused bool
}

// CreateSpec carries the per-workspace part of a runtime container. Image,
// network, labels, mounts, tmpfs, capabilities and resource limits are owned by
// the driver so that request parameters cannot override them.
type CreateSpec struct {
	WorkspaceID int64
	Binds       []string
	Env         []string
}

// Driver manages the lifecycle of per-workspace runtime containers.
type Driver interface {
	Create(ctx context.Context, spec CreateSpec) (ContainerInfo, error)
	Start(ctx context.Context, workspaceID int64) error
	Inspect(ctx context.Context, workspaceID int64) (ContainerInfo, error)
	Stop(ctx context.Context, workspaceID int64) error
	Remove(ctx context.Context, workspaceID int64) error
	List(ctx context.Context) ([]ContainerInfo, error)
}

// DisplayTransport opens the raw RFB byte stream of a running workspace runtime.
type DisplayTransport interface {
	Connect(ctx context.Context, workspaceID int64) (io.ReadWriteCloser, error)
}
