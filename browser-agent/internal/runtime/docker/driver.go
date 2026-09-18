package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

// Frozen container specification. None of these values may be overridden by an
// API request: they are compiled into the driver.
const (
	containerNamePrefix = "newapi-ws-runtime-"
	managedLabelKey     = "newapi.web-workspace"
	managedLabelValue   = "1"
	workspaceIDLabelKey = "newapi.web-workspace.workspace-id"

	tmpfsTarget     = "/tmp"
	tmpfsOptions    = "rw,size=256m,mode=1777"
	runTmpfsTarget  = "/run"
	runTmpfsOptions = "rw,size=16m,mode=755"
	shmSizeBytes    = int64(536870912)

	capDropAll         = "ALL"
	noNewPrivileges    = "no-new-privileges:true"
	stopTimeoutSeconds = 10
	connectTimeout     = 10 * time.Second
)

// Options carries the deployment values every runtime container receives.
type Options struct {
	Image       string
	Network     string
	MemoryBytes int64
	NanoCPUs    int64
	PidsLimit   int64
}

// Driver implements runtime.Driver and runtime.DisplayTransport.
type Driver struct {
	client *Client
	opts   Options
}

// NewDriver returns a Docker backed runtime driver.
func NewDriver(client *Client, opts Options) *Driver {
	return &Driver{client: client, opts: opts}
}

// EnsureRuntimeNetwork verifies that the configured runtime network exists and
// is internal. A missing network is created as an internal bridge and then
// inspected again; an existing non-internal network is a fatal configuration
// error because it would leave a runtime with an unchecked egress path.
func (d *Driver) EnsureRuntimeNetwork(ctx context.Context) error {
	name := strings.TrimSpace(d.opts.Network)
	if name == "" {
		return errors.New("runtime network name must be non-empty")
	}

	network, err := d.client.NetworkInspect(ctx, name)
	if err == nil {
		if !network.Internal {
			return fmt.Errorf("runtime network %q exists but is not internal", name)
		}
		return nil
	}
	if !isStatus(err, http.StatusNotFound) {
		return fmt.Errorf("inspect runtime network %q: %w", name, err)
	}

	if err := d.client.NetworkCreate(ctx, name, true); err != nil {
		return fmt.Errorf("create runtime network %q: %w", name, err)
	}
	network, err = d.client.NetworkInspect(ctx, name)
	if err != nil {
		return fmt.Errorf("re-inspect runtime network %q: %w", name, err)
	}
	if !network.Internal {
		return fmt.Errorf("runtime network %q was created but is not internal", name)
	}
	return nil
}

// ContainerName returns the deterministic container name for a workspace.
func ContainerName(workspaceID int64) string {
	return containerNamePrefix + strconv.FormatInt(workspaceID, 10)
}

// Create creates the runtime container for a workspace. A leftover container
// with the same deterministic name is adopted when it still runs and removed
// when it does not.
func (d *Driver) Create(ctx context.Context, spec runtime.CreateSpec) (runtime.ContainerInfo, error) {
	if spec.WorkspaceID <= 0 {
		return runtime.ContainerInfo{}, errors.New("workspace id must be positive")
	}
	body := d.createRequest(spec)
	info, err := d.create(ctx, spec.WorkspaceID, body)
	if err == nil {
		return info, nil
	}
	if !isStatus(err, http.StatusConflict) {
		return runtime.ContainerInfo{}, err
	}

	existing, inspectErr := d.Inspect(ctx, spec.WorkspaceID)
	switch {
	case inspectErr == nil && existing.Running:
		existing.Reused = true
		return existing, nil
	case inspectErr != nil && !errors.Is(inspectErr, runtime.ErrNotFound):
		return runtime.ContainerInfo{}, inspectErr
	}
	if removeErr := d.Remove(ctx, spec.WorkspaceID); removeErr != nil && !errors.Is(removeErr, runtime.ErrNotFound) {
		return runtime.ContainerInfo{}, removeErr
	}
	return d.create(ctx, spec.WorkspaceID, body)
}

func (d *Driver) create(ctx context.Context, workspaceID int64, body createRequest) (runtime.ContainerInfo, error) {
	query := url.Values{"name": []string{ContainerName(workspaceID)}}
	var response struct {
		ID string `json:"Id"`
	}
	if err := d.client.do(ctx, http.MethodPost, "/containers/create", query, body, &response); err != nil {
		return runtime.ContainerInfo{}, err
	}
	return runtime.ContainerInfo{
		ID:          response.ID,
		State:       runtime.ContainerCreated,
		WorkspaceID: workspaceID,
	}, nil
}

// Start starts the container. An already started container is not an error.
func (d *Driver) Start(ctx context.Context, workspaceID int64) error {
	err := d.client.do(ctx, http.MethodPost, containerPath(workspaceID)+"/start", nil, nil, nil)
	if err != nil && !isStatus(err, http.StatusNotModified) {
		return notFoundAs(err, runtime.ErrNotFound)
	}
	return nil
}

// Inspect reports the container state straight from the Docker daemon.
func (d *Driver) Inspect(ctx context.Context, workspaceID int64) (runtime.ContainerInfo, error) {
	var response inspectResponse
	if err := d.client.do(ctx, http.MethodGet, containerPath(workspaceID)+"/json", nil, nil, &response); err != nil {
		return runtime.ContainerInfo{}, notFoundAs(err, runtime.ErrNotFound)
	}
	return d.toContainerInfo(workspaceID, response), nil
}

// Stop stops the container, keeping the container and the profile directory.
func (d *Driver) Stop(ctx context.Context, workspaceID int64) error {
	query := url.Values{"t": []string{strconv.Itoa(stopTimeoutSeconds)}}
	err := d.client.do(ctx, http.MethodPost, containerPath(workspaceID)+"/stop", query, nil, nil)
	if err != nil && !isStatus(err, http.StatusNotModified) {
		return notFoundAs(err, runtime.ErrNotFound)
	}
	return nil
}

// Remove deletes the container. The workspace profile is never removed.
func (d *Driver) Remove(ctx context.Context, workspaceID int64) error {
	query := url.Values{"force": []string{"1"}}
	err := d.client.do(ctx, http.MethodDelete, containerPath(workspaceID), query, nil, nil)
	if err != nil {
		return notFoundAs(err, runtime.ErrNotFound)
	}
	return nil
}

// List returns every container carrying the web-workspace managed label.
func (d *Driver) List(ctx context.Context) ([]runtime.ContainerInfo, error) {
	filters, err := json.Marshal(map[string][]string{"label": {managedLabelKey + "=" + managedLabelValue}})
	if err != nil {
		return nil, fmt.Errorf("encode container filter: %w", err)
	}
	query := url.Values{"all": []string{"1"}, "filters": []string{string(filters)}}

	var response []struct {
		ID     string            `json:"Id"`
		State  string            `json:"State"`
		Labels map[string]string `json:"Labels"`
	}
	if err := d.client.do(ctx, http.MethodGet, "/containers/json", query, nil, &response); err != nil {
		return nil, err
	}

	infos := make([]runtime.ContainerInfo, 0, len(response))
	for _, item := range response {
		workspaceID, err := strconv.ParseInt(item.Labels[workspaceIDLabelKey], 10, 64)
		if err != nil {
			workspaceID = 0
		}
		infos = append(infos, runtime.ContainerInfo{
			ID:          item.ID,
			State:       containerState(item.State),
			Running:     item.State == string(runtime.ContainerRunning),
			WorkspaceID: workspaceID,
		})
	}
	return infos, nil
}

// Connect implements runtime.DisplayTransport: it dials the RFB port of the
// workspace container directly without publishing anything on the host.
func (d *Driver) Connect(ctx context.Context, workspaceID int64) (io.ReadWriteCloser, error) {
	info, err := d.Inspect(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if !info.Running {
		return nil, runtime.ErrNotRunning
	}
	if info.IP == "" {
		return nil, fmt.Errorf("runtime container for workspace %d has no usable network address", workspaceID)
	}
	dialer := &net.Dialer{Timeout: connectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(info.IP, strconv.Itoa(runtime.VNCPort)))
	if err != nil {
		return nil, fmt.Errorf("dial runtime display: %w", err)
	}
	return conn, nil
}

func (d *Driver) createRequest(spec runtime.CreateSpec) createRequest {
	// Older daemons read the top-level Tmpfs field, newer ones the HostConfig
	// field; both are sent with the same frozen values. /tmp carries the X11
	// socket directory and /run keeps runtime state off the read-only rootfs.
	tmpfs := map[string]string{
		tmpfsTarget:    tmpfsOptions,
		runTmpfsTarget: runTmpfsOptions,
	}
	return createRequest{
		Image: d.opts.Image,
		Env:   spec.Env,
		Labels: map[string]string{
			managedLabelKey:     managedLabelValue,
			workspaceIDLabelKey: strconv.FormatInt(spec.WorkspaceID, 10),
		},
		Tmpfs: tmpfs,
		HostConfig: hostConfig{
			Binds:          spec.Binds,
			NetworkMode:    d.opts.Network,
			PortBindings:   map[string]any{},
			ReadonlyRootfs: true,
			ShmSize:        shmSizeBytes,
			CapDrop:        []string{capDropAll},
			SecurityOpt:    []string{noNewPrivileges},
			Privileged:     false,
			AutoRemove:     false,
			RestartPolicy:  restartPolicy{Name: "no"},
			Memory:         d.opts.MemoryBytes,
			NanoCpus:       d.opts.NanoCPUs,
			PidsLimit:      d.opts.PidsLimit,
			Tmpfs:          tmpfs,
		},
	}
}

func (d *Driver) toContainerInfo(workspaceID int64, response inspectResponse) runtime.ContainerInfo {
	info := runtime.ContainerInfo{
		ID:          response.ID,
		State:       containerState(response.State.Status),
		Running:     response.State.Running,
		WorkspaceID: workspaceID,
		Env:         envMap(response.Config.Env),
		IP:          d.containerIP(response),
	}
	if started, err := time.Parse(time.RFC3339Nano, response.State.StartedAt); err == nil {
		info.StartedAt = started
	}
	return info
}

func (d *Driver) containerIP(response inspectResponse) string {
	if network, ok := response.NetworkSettings.Networks[d.opts.Network]; ok && network.IPAddress != "" {
		return network.IPAddress
	}
	for name, network := range response.NetworkSettings.Networks {
		if name == "host" || name == "none" {
			continue
		}
		if network.IPAddress != "" {
			return network.IPAddress
		}
	}
	return ""
}

func containerPath(workspaceID int64) string {
	return "/containers/" + ContainerName(workspaceID)
}

func containerState(status string) runtime.ContainerState {
	switch status {
	case string(runtime.ContainerCreated):
		return runtime.ContainerCreated
	case string(runtime.ContainerRunning):
		return runtime.ContainerRunning
	case string(runtime.ContainerExited):
		return runtime.ContainerExited
	case string(runtime.ContainerDead):
		return runtime.ContainerDead
	default:
		return runtime.ContainerUnknown
	}
}

func notFoundAs(err error, target error) error {
	if isStatus(err, http.StatusNotFound) {
		return target
	}
	return err
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
