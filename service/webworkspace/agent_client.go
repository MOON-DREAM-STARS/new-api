package webworkspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gorilla/websocket"
)

// Browser Agent runtime states reported by the execution plane.
const (
	AgentStateStarting = "STARTING"
	AgentStateRunning  = "RUNNING"
	AgentStateIdle     = "IDLE"
	AgentStateStopping = "STOPPING"
	AgentStateStopped  = "STOPPED"
	AgentStateFailed   = "FAILED"
)

var (
	// ErrAgentUnavailable means the control plane could not reach or is not
	// configured for the Browser Agent. Callers must fail closed.
	ErrAgentUnavailable = errors.New("web workspace agent unavailable")
	// ErrAgentRejected means the agent refused the request.
	ErrAgentRejected = errors.New("web workspace agent rejected the request")
	// ErrAgentRuntimeNotFound means the agent has no runtime for the workspace.
	ErrAgentRuntimeNotFound = errors.New("web workspace runtime not found")
)

// AgentRuntime is the metadata the agent exposes about one workspace runtime.
// It never contains container addresses, ports or file system paths.
type AgentRuntime struct {
	RuntimeId      string `json:"runtime_id"`
	WorkspaceId    int    `json:"workspace_id"`
	State          string `json:"state"`
	CreatedAt      int64  `json:"created_at"`
	LastActivityAt int64  `json:"last_activity_at"`
	IdleDeadlineAt int64  `json:"idle_deadline_at"`
}

type agentCreateRuntimeRequest struct {
	WorkspaceId int    `json:"workspace_id"`
	Provider    string `json:"provider"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// AgentConfigured reports whether the Web Workspace agent connection is
// configured. Session endpoints must return a closed failure when it is not.
func AgentConfigured() bool {
	settings := system_setting.GetWebWorkspaceSettings()
	if strings.TrimSpace(settings.AgentBaseURL) == "" {
		return false
	}
	return agentServiceToken() != ""
}

func agentServiceToken() string {
	return strings.TrimSpace(os.Getenv("WEB_WORKSPACE_AGENT_TOKEN"))
}

func agentHTTPTimeout() time.Duration {
	seconds := common.GetEnvOrDefault("WEB_WORKSPACE_AGENT_TIMEOUT_SECONDS", 30)
	if seconds <= 0 {
		seconds = 30
	}
	return time.Duration(seconds) * time.Second
}

// LiveRuntimeState reports whether the state still owns a running runtime.
func LiveRuntimeState(state string) bool {
	switch state {
	case AgentStateStarting, AgentStateRunning, AgentStateIdle, AgentStateStopping:
		return true
	default:
		return false
	}
}

type AgentClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newAgentClient() (*AgentClient, error) {
	settings := system_setting.GetWebWorkspaceSettings()
	base := strings.TrimRight(strings.TrimSpace(settings.AgentBaseURL), "/")
	if base == "" {
		return nil, ErrAgentUnavailable
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("%w: invalid agent base url", ErrAgentUnavailable)
	}
	token := agentServiceToken()
	if token == "" {
		return nil, fmt.Errorf("%w: agent service token is not configured", ErrAgentUnavailable)
	}
	return &AgentClient{
		baseURL: base,
		token:   token,
		client:  &http.Client{Timeout: agentHTTPTimeout()},
	}, nil
}

func (c *AgentClient) do(ctx context.Context, method string, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	if response.StatusCode == http.StatusNotFound {
		return ErrAgentRuntimeNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: status=%d", ErrAgentRejected, response.StatusCode)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := common.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	return nil
}

// CreateRuntime starts the workspace runtime. It is idempotent on the agent
// side: an existing live runtime is returned instead of a second one.
func (c *AgentClient) CreateRuntime(ctx context.Context, workspaceId int, provider string, width int, height int) (*AgentRuntime, error) {
	payload := agentCreateRuntimeRequest{WorkspaceId: workspaceId, Provider: provider, Width: width, Height: height}
	var runtime AgentRuntime
	if err := c.do(ctx, http.MethodPost, "/internal/v1/runtimes", payload, &runtime); err != nil {
		return nil, err
	}
	return &runtime, nil
}

func (c *AgentClient) GetRuntime(ctx context.Context, workspaceId int) (*AgentRuntime, error) {
	var runtime AgentRuntime
	path := fmt.Sprintf("/internal/v1/runtimes/%d", workspaceId)
	if err := c.do(ctx, http.MethodGet, path, nil, &runtime); err != nil {
		return nil, err
	}
	return &runtime, nil
}

func (c *AgentClient) StopRuntime(ctx context.Context, workspaceId int) error {
	path := fmt.Sprintf("/internal/v1/runtimes/%d/stop", workspaceId)
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

func (c *AgentClient) RestartRuntime(ctx context.Context, workspaceId int) (*AgentRuntime, error) {
	var runtime AgentRuntime
	path := fmt.Sprintf("/internal/v1/runtimes/%d/restart", workspaceId)
	if err := c.do(ctx, http.MethodPost, path, nil, &runtime); err != nil {
		return nil, err
	}
	return &runtime, nil
}

// DialStream opens the agent-side display stream for one workspace. The caller
// (New API) is the only client that ever holds this connection.
func (c *AgentClient) DialStream(ctx context.Context, workspaceId int) (*websocket.Conn, *http.Response, error) {
	endpoint, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	if endpoint.Scheme == "http" {
		endpoint.Scheme = "ws"
	} else {
		endpoint.Scheme = "wss"
	}
	endpoint.Path = fmt.Sprintf("/internal/v1/runtimes/%d/stream", workspaceId)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)
	dialer := websocket.Dialer{HandshakeTimeout: agentHTTPTimeout()}
	return dialer.DialContext(ctx, endpoint.String(), header)
}

// DialAgentStream opens the agent-side display stream for the control plane.
func DialAgentStream(ctx context.Context, workspaceId int) (*websocket.Conn, *http.Response, error) {
	client, err := newAgentClient()
	if err != nil {
		return nil, nil, err
	}
	return client.DialStream(ctx, workspaceId)
}
