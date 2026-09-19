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
	// ErrAgentNavigationTimeout means the agent reported that a navigation
	// command exceeded its execution deadline.
	ErrAgentNavigationTimeout = errors.New("web workspace navigation timeout")
	// ErrAgentNavigationUnavailable means the runtime cannot accept navigation
	// commands in its current state.
	ErrAgentNavigationUnavailable = errors.New("web workspace navigation unavailable")
)

// AgentNavigation is the browser navigation snapshot exposed by the agent.
// It never contains a URL, page address or runtime endpoint.
type AgentNavigation struct {
	CanGoBack    bool  `json:"can_go_back"`
	CanGoForward bool  `json:"can_go_forward"`
	UpdatedAt    int64 `json:"updated_at"`
}

// AgentPageStatus is the URL-free health state of the remote page.
type AgentPageStatus struct {
	State     string `json:"state"`
	Error     string `json:"error"`
	Attempts  int    `json:"attempts"`
	UpdatedAt int64  `json:"updated_at"`
}

func (page *AgentPageStatus) normalized() *AgentPageStatus {
	if page == nil {
		return nil
	}
	if !validAgentPageState(page.State) || page.Attempts < 0 || !validAgentPageError(page.Error) {
		return nil
	}
	if page.State == "READY" && (page.Attempts != 0 || page.Error != "") {
		return nil
	}
	return &AgentPageStatus{
		State:     page.State,
		Error:     page.Error,
		Attempts:  page.Attempts,
		UpdatedAt: page.UpdatedAt,
	}
}

func validAgentPageState(state string) bool {
	switch state {
	case "READY", "RETRYING", "FAILED":
		return true
	default:
		return false
	}
}

func validAgentPageError(value string) bool {
	if value == "" {
		return true
	}
	if !strings.HasPrefix(value, "ERR_") {
		return false
	}
	for _, char := range value {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

// AgentRuntime is the metadata the agent exposes about one workspace runtime.
// It never contains container addresses, ports or file system paths.
type AgentRuntime struct {
	RuntimeId      string           `json:"runtime_id"`
	WorkspaceId    int              `json:"workspace_id"`
	State          string           `json:"state"`
	Mode           string           `json:"mode"`
	CreatedAt      int64            `json:"created_at"`
	LastActivityAt int64            `json:"last_activity_at"`
	IdleDeadlineAt int64            `json:"idle_deadline_at"`
	StreamBytesOut int64            `json:"stream_bytes_out"`
	StreamBytesIn  int64            `json:"stream_bytes_in"`
	Navigation     *AgentNavigation `json:"navigation"`
	Page           *AgentPageStatus `json:"page"`
}

type agentRestartRuntimeRequest struct {
	Mode   string `json:"mode,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type agentCreateRuntimeRequest struct {
	WorkspaceId int    `json:"workspace_id"`
	Provider    string `json:"provider"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// Ownership, permit and observation payloads of the Browser Agent internal API
// (Phase 4 contract §4). They carry provider-side identifiers only; runtime
// addresses, ports and service tokens never appear here.
type agentOwnershipRequest struct {
	Generation    int64    `json:"generation"`
	Projects      []string `json:"projects"`
	Conversations []string `json:"conversations"`
}

type agentOwnershipResult struct {
	Generation    int64 `json:"generation"`
	Projects      int   `json:"projects"`
	Conversations int   `json:"conversations"`
}

type agentPermitRequest struct {
	PermitId   string `json:"permit_id"`
	Kind       string `json:"kind"`
	TtlSeconds int    `json:"ttl_seconds"`
}

type agentPermitResult struct {
	PermitId  string `json:"permit_id"`
	ExpiresAt int64  `json:"expires_at"`
}

type agentObservationsResult struct {
	Observations []Observation `json:"observations"`
	NextOffset   int64         `json:"next_offset"`
}

type agentNavigationRequest struct {
	Action string `json:"action"`
}

type agentNavigationResponse struct {
	Action     string           `json:"action"`
	Navigation *AgentNavigation `json:"navigation"`
}

type agentErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type agentAckRequest struct {
	Offset int64 `json:"offset"`
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
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %w: %v", ErrAgentUnavailable, ErrAgentNavigationTimeout, err)
		}
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
		code := ""
		var agentError agentErrorResponse
		if len(raw) > 0 && common.Unmarshal(raw, &agentError) == nil {
			code = strings.TrimSpace(agentError.Code)
			if code == "" {
				code = strings.TrimSpace(agentError.Error)
			}
		}
		switch code {
		case "navigation_timeout":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentNavigationTimeout, code, response.StatusCode)
		case "navigation_unavailable", "runtime_not_running":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentNavigationUnavailable, code, response.StatusCode)
		}
		if code != "" {
			return fmt.Errorf("%w: code=%s status=%d", ErrAgentRejected, code, response.StatusCode)
		}
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

func (c *AgentClient) RestartRuntime(ctx context.Context, workspaceId int, mode string, width int, height int) (*AgentRuntime, error) {
	var runtime AgentRuntime
	path := fmt.Sprintf("/internal/v1/runtimes/%d/restart", workspaceId)
	payload := agentRestartRuntimeRequest{Mode: strings.TrimSpace(mode), Width: width, Height: height}
	if err := c.do(ctx, http.MethodPost, path, payload, &runtime); err != nil {
		return nil, err
	}
	return &runtime, nil
}

// NavigateRuntime sends one navigation command to the workspace runtime and
// returns the agent's updated navigation snapshot.
func (c *AgentClient) NavigateRuntime(ctx context.Context, workspaceId int, action string) (*AgentNavigation, error) {
	var result agentNavigationResponse
	path := fmt.Sprintf("/internal/v1/runtimes/%d/navigation", workspaceId)
	if err := c.do(ctx, http.MethodPost, path, agentNavigationRequest{Action: action}, &result); err != nil {
		return nil, err
	}
	if result.Navigation == nil {
		return nil, fmt.Errorf("%w: navigation response is missing navigation", ErrAgentUnavailable)
	}
	return result.Navigation, nil
}

// TouchRuntime refreshes the idle deadline of a workspace runtime and returns
// its updated snapshot.
func (c *AgentClient) TouchRuntime(ctx context.Context, workspaceId int) (*AgentRuntime, error) {
	var runtime AgentRuntime
	path := fmt.Sprintf("/internal/v1/runtimes/%d/activity", workspaceId)
	if err := c.do(ctx, http.MethodPost, path, nil, &runtime); err != nil {
		return nil, err
	}
	return &runtime, nil
}

// PutOwnership publishes the workspace ownership document the guard enforces.
// Generation is the newest project row timestamp of the workspace.
func (c *AgentClient) PutOwnership(ctx context.Context, workspaceId int, generation int64, projects []string, conversations []string) error {
	if projects == nil {
		projects = []string{}
	}
	if conversations == nil {
		conversations = []string{}
	}
	payload := agentOwnershipRequest{Generation: generation, Projects: projects, Conversations: conversations}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/ownership", workspaceId)
	var result agentOwnershipResult
	return c.do(ctx, http.MethodPut, path, payload, &result)
}

// IssueProjectPermit asks the agent for one short-lived project creation
// permit and returns the expiry the guard will enforce.
func (c *AgentClient) IssueProjectPermit(ctx context.Context, workspaceId int, permitId string, ttlSeconds int) (int64, error) {
	payload := agentPermitRequest{PermitId: permitId, Kind: ProjectPermitKind, TtlSeconds: ttlSeconds}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/permits", workspaceId)
	var result agentPermitResult
	if err := c.do(ctx, http.MethodPost, path, payload, &result); err != nil {
		return 0, err
	}
	if result.ExpiresAt <= 0 {
		return 0, fmt.Errorf("%w: permit response is missing expires_at", ErrAgentUnavailable)
	}
	return result.ExpiresAt, nil
}

// PullObservations returns the guard observations that were not acknowledged
// yet together with the offset that acknowledges them.
func (c *AgentClient) PullObservations(ctx context.Context, workspaceId int) ([]Observation, int64, error) {
	path := fmt.Sprintf("/internal/v1/runtimes/%d/observations", workspaceId)
	var result agentObservationsResult
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, 0, err
	}
	if result.NextOffset < 0 {
		return nil, 0, fmt.Errorf("%w: negative observations offset", ErrAgentUnavailable)
	}
	return result.Observations, result.NextOffset, nil
}

// AckObservations confirms that every observation below the offset was applied
// so the next pull starts after it.
func (c *AgentClient) AckObservations(ctx context.Context, workspaceId int, offset int64) error {
	path := fmt.Sprintf("/internal/v1/runtimes/%d/observations/ack", workspaceId)
	return c.do(ctx, http.MethodPost, path, agentAckRequest{Offset: offset}, nil)
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
