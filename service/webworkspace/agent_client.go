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
	// ErrAgentProjectDeletionTimeout means the guard did not complete a
	// provider-side project deletion before its deadline.
	ErrAgentProjectDeletionTimeout = errors.New("web workspace project deletion timeout")
	// ErrAgentProjectDeletionUnavailable means the runtime cannot accept a
	// provider-side deletion command in its current state.
	ErrAgentProjectDeletionUnavailable = errors.New("web workspace project deletion unavailable")
	// ErrAgentProjectDeletionRejected means the provider UI rejected deletion
	// with a stable guard error code.
	ErrAgentProjectDeletionRejected = errors.New("web workspace project deletion rejected")
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

// Project creation states reported by the guard creation controller.
const (
	ProjectCreationStateRunning = "RUNNING"
	ProjectCreationStateCreated = "CREATED"
	ProjectCreationStateFailed  = "FAILED"
)

// AgentProjectCreation is the URL-free project creation state the guard
// publishes. It never contains a provider identifier, address or credential.
type AgentProjectCreation struct {
	PermitId  string `json:"permit_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

// normalized rejects a creation state the control plane must not publish.
// A missing, malformed or unknown state is reported as absent instead.
func (creation *AgentProjectCreation) normalized() *AgentProjectCreation {
	if creation == nil {
		return nil
	}
	switch creation.State {
	case ProjectCreationStateRunning, ProjectCreationStateCreated, ProjectCreationStateFailed:
	default:
		return nil
	}
	if creation.UpdatedAt < 0 || !validAgentPageError(creation.Error) {
		return nil
	}
	return &AgentProjectCreation{
		PermitId:  creation.PermitId,
		State:     creation.State,
		Error:     creation.Error,
		UpdatedAt: creation.UpdatedAt,
	}
}

// AgentFileChooser is the URL-free pending chooser returned by the agent. The
// backend node id, CDP session id and staging paths never cross this boundary.
type AgentFileChooser struct {
	ChooserID string `json:"chooser_id"`
	Mode      string `json:"mode"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// InputCaret is the remote browser caret rectangle in remote CSS pixels. The
// control plane exposes the geometry only; it never exposes a DOM node or CDP
// session identifier.
type InputCaret struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type agentInputCaretResponse struct {
	Caret *InputCaret `json:"caret"`
}

type agentFileChooserResponse struct {
	Chooser *AgentFileChooser `json:"chooser"`
}

type agentFileChooserResult struct {
	ChooserID string `json:"chooser_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

// AgentRuntime is the metadata the agent exposes about one workspace runtime.
// It never contains container addresses, ports or file system paths.
type AgentRuntime struct {
	RuntimeId       string                `json:"runtime_id"`
	WorkspaceId     int                   `json:"workspace_id"`
	State           string                `json:"state"`
	Mode            string                `json:"mode"`
	CreatedAt       int64                 `json:"created_at"`
	LastActivityAt  int64                 `json:"last_activity_at"`
	IdleDeadlineAt  int64                 `json:"idle_deadline_at"`
	StreamBytesOut  int64                 `json:"stream_bytes_out"`
	StreamBytesIn   int64                 `json:"stream_bytes_in"`
	Navigation      *AgentNavigation      `json:"navigation"`
	Page            *AgentPageStatus      `json:"page"`
	ProjectCreation *AgentProjectCreation `json:"project_creation"`
	IMEState        string                `json:"ime_state"`
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
	PermitId    string `json:"permit_id"`
	Kind        string `json:"kind"`
	TtlSeconds  int    `json:"ttl_seconds"`
	DisplayName string `json:"display_name,omitempty"`
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
	Action    string `json:"action"`
	ProjectID string `json:"project_id,omitempty"`
}

type agentNavigationResponse struct {
	Action     string           `json:"action"`
	Navigation *AgentNavigation `json:"navigation"`
}

type agentProjectDeletionRequest struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
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
		case "invalid_request":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentInvalidRequest, code, response.StatusCode)
		case "navigation_timeout":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentNavigationTimeout, code, response.StatusCode)
		case "navigation_unavailable", "runtime_not_running":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentNavigationUnavailable, code, response.StatusCode)
		case "project_deletion_timeout":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentProjectDeletionTimeout, code, response.StatusCode)
		case "project_deletion_unavailable":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentProjectDeletionUnavailable, code, response.StatusCode)
		case "project_deletion_rejected":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrAgentProjectDeletionRejected, code, response.StatusCode)
		case "input_unavailable":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrInputUnavailable, code, response.StatusCode)
		case "input_timeout":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrInputTimeout, code, response.StatusCode)
		case "input_rejected":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrInputRejected, code, response.StatusCode)
		case "file_chooser_expired":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileChooserExpired, code, response.StatusCode)
		case "file_too_large":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileTooLarge, code, response.StatusCode)
		case "file_limit_exceeded":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileLimitExceeded, code, response.StatusCode)
		case "file_inject_failed":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileInjectFailed, code, response.StatusCode)
		case "file_bridge_busy":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileBridgeBusy, code, response.StatusCode)
		case "runtime_capacity_reached":
			return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrCapacityReached, code, response.StatusCode)
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
	return c.navigate(ctx, workspaceId, action, "")
}

// NavigateProject asks the agent to open one already-authorized provider
// project. The external project id stays inside the control-plane-to-agent
// channel and is never returned to the browser client.
func (c *AgentClient) NavigateProject(ctx context.Context, workspaceId int, projectID string) (*AgentNavigation, error) {
	return c.navigate(ctx, workspaceId, "project", projectID)
}

func (c *AgentClient) navigate(ctx context.Context, workspaceId int, action string, projectID string) (*AgentNavigation, error) {
	var result agentNavigationResponse
	path := fmt.Sprintf("/internal/v1/runtimes/%d/navigation", workspaceId)
	payload := agentNavigationRequest{Action: action, ProjectID: projectID}
	if err := c.do(ctx, http.MethodPost, path, payload, &result); err != nil {
		return nil, err
	}
	if result.Navigation == nil {
		return nil, fmt.Errorf("%w: navigation response is missing navigation", ErrAgentUnavailable)
	}
	return result.Navigation, nil
}

// DeleteProject asks the Agent to delete one provider-side project through the
// runtime's unique CDP consumer.
func (c *AgentClient) DeleteProject(ctx context.Context, workspaceId int, projectID string, projectName string) error {
	path := fmt.Sprintf("/internal/v1/runtimes/%d/projects/delete", workspaceId)
	payload := agentProjectDeletionRequest{ProjectID: projectID, ProjectName: projectName}
	return c.do(ctx, http.MethodPost, path, payload, nil)
}

// InsertInputText inserts one already-committed local text value at the remote
// browser's current caret.
func (c *AgentClient) InsertInputText(ctx context.Context, workspaceId int, text string) error {
	if workspaceId <= 0 || strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: invalid input text", ErrInputRejected)
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/input/text", workspaceId)
	return c.do(ctx, http.MethodPost, path, struct {
		Text string `json:"text"`
	}{Text: text}, nil)
}

// DispatchInputKey forwards one approved key and its modifiers to the remote
// browser.
func (c *AgentClient) DispatchInputKey(ctx context.Context, workspaceId int, key string, modifiers []string) error {
	if workspaceId <= 0 || strings.TrimSpace(key) == "" {
		return fmt.Errorf("%w: invalid input key", ErrInputRejected)
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/input/key", workspaceId)
	return c.do(ctx, http.MethodPost, path, struct {
		Key       string   `json:"key"`
		Modifiers []string `json:"modifiers,omitempty"`
	}{Key: key, Modifiers: modifiers}, nil)
}

// ProbeInputCaret returns the remote caret geometry, or nil when the remote
// page has no active editable element.
func (c *AgentClient) ProbeInputCaret(ctx context.Context, workspaceId int) (*InputCaret, error) {
	if workspaceId <= 0 {
		return nil, fmt.Errorf("%w: invalid caret probe", ErrInputRejected)
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/input/caret", workspaceId)
	var response agentInputCaretResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	if response.Caret == nil {
		return nil, nil
	}
	caret := *response.Caret
	if caret.X < 0 || caret.Y < 0 || caret.Width < 0 || caret.Height < 0 ||
		caret.X >= 100000 || caret.Y >= 100000 || caret.Width >= 100000 || caret.Height >= 100000 {
		return nil, fmt.Errorf("%w: caret response is invalid", ErrAgentUnavailable)
	}
	return &caret, nil
}

// WaitFileChooser long-polls the agent for one pending local file chooser.
func (c *AgentClient) WaitFileChooser(ctx context.Context, workspaceId int, wait time.Duration) (*AgentFileChooser, error) {
	if wait < 0 {
		wait = 0
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/file-chooser?wait_ms=%d", workspaceId, wait.Milliseconds())
	var result agentFileChooserResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	if result.Chooser == nil {
		return nil, nil
	}
	if !validAgentFileChooser(result.Chooser) {
		return nil, fmt.Errorf("%w: invalid file chooser response", ErrAgentUnavailable)
	}
	return result.Chooser, nil
}

// UploadFileChooserFiles streams the caller's multipart body to the agent. The
// body is never decoded or buffered by the control plane.
func (c *AgentClient) UploadFileChooserFiles(ctx context.Context, workspaceId int, chooserID string, contentType string, body io.Reader) (*agentFileChooserResult, error) {
	if workspaceId <= 0 || strings.TrimSpace(chooserID) == "" || strings.TrimSpace(contentType) == "" || body == nil {
		return nil, fmt.Errorf("%w: invalid file chooser upload", ErrAgentUnavailable)
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/file-chooser/%s/files", workspaceId, url.PathEscape(chooserID))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", contentType)
	client := &http.Client{Transport: c.client.Transport}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrAgentRuntimeNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, mapAgentFileChooserError(raw, response.StatusCode)
	}
	var result agentFileChooserResult
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
		}
	}
	return &result, nil
}

// CancelFileChooser asks the guard to dismiss a pending chooser.
func (c *AgentClient) CancelFileChooser(ctx context.Context, workspaceId int, chooserID string) (*agentFileChooserResult, error) {
	path := fmt.Sprintf("/internal/v1/runtimes/%d/file-chooser/%s/cancel", workspaceId, url.PathEscape(chooserID))
	var result agentFileChooserResult
	if err := c.do(ctx, http.MethodPost, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

const maxClipboardPayloadBytes = int64(8 << 20)

// CopyClipboard returns the raw selection bytes and MIME type from the agent.
func (c *AgentClient) CopyClipboard(ctx context.Context, workspaceId int) (string, []byte, error) {
	if workspaceId <= 0 {
		return "", nil, fmt.Errorf("%w: invalid clipboard copy", ErrInvalidClipboard)
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/clipboard/copy", workspaceId)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxClipboardPayloadBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", nil, mapAgentClipboardError(body, response.StatusCode)
	}
	if int64(len(body)) > maxClipboardPayloadBytes {
		return "", nil, ErrClipboardPayloadTooLarge
	}
	mimeType := strings.TrimSpace(response.Header.Get("Content-Type"))
	if separator := strings.IndexByte(mimeType, ';'); separator >= 0 {
		mimeType = strings.TrimSpace(mimeType[:separator])
	}
	if mimeType == "" {
		return "", nil, fmt.Errorf("%w: clipboard copy has no content type", ErrClipboardFailed)
	}
	return mimeType, body, nil
}

// PasteClipboard sends one raw clipboard body to the agent.
func (c *AgentClient) PasteClipboard(ctx context.Context, workspaceId int, mimeType string, payload []byte) error {
	if workspaceId <= 0 || strings.TrimSpace(mimeType) == "" {
		return fmt.Errorf("%w: invalid clipboard paste", ErrInvalidClipboard)
	}
	if int64(len(payload)) > maxClipboardPayloadBytes {
		return ErrClipboardPayloadTooLarge
	}
	path := fmt.Sprintf("/internal/v1/runtimes/%d/clipboard/paste", workspaceId)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", mimeType)
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAgentUnavailable, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return mapAgentClipboardError(raw, response.StatusCode)
	}
	return nil
}

func mapAgentClipboardError(raw []byte, status int) error {
	code := ""
	var agentError agentErrorResponse
	if len(raw) > 0 && common.Unmarshal(raw, &agentError) == nil {
		code = strings.TrimSpace(agentError.Code)
		if code == "" {
			code = strings.TrimSpace(agentError.Error)
		}
	}
	switch code {
	case "clipboard_unavailable", "runtime_not_running":
		return ErrClipboardUnavailable
	case "clipboard_payload_too_large":
		return ErrClipboardPayloadTooLarge
	case "clipboard_mime_unsupported":
		return ErrClipboardMIMEUnsupported
	case "clipboard_failed":
		return ErrClipboardFailed
	case "invalid_request":
		return ErrInvalidClipboard
	}
	if status == http.StatusNotFound {
		return ErrAgentRuntimeNotFound
	}
	return fmt.Errorf("%w: code=%s status=%d", ErrAgentRejected, code, status)
}

func validAgentFileChooser(chooser *AgentFileChooser) bool {
	if chooser == nil || strings.TrimSpace(chooser.ChooserID) == "" || chooser.ExpiresAt <= 0 {
		return false
	}
	switch chooser.Mode {
	case "selectSingle", "selectMultiple":
		return true
	default:
		return false
	}
}

func mapAgentFileChooserError(raw []byte, status int) error {
	code := ""
	var agentError agentErrorResponse
	if len(raw) > 0 && common.Unmarshal(raw, &agentError) == nil {
		code = strings.TrimSpace(agentError.Code)
		if code == "" {
			code = strings.TrimSpace(agentError.Error)
		}
	}
	switch code {
	case "invalid_request":
		return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrInvalidFileChooser, code, status)
	case "file_chooser_expired":
		return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileChooserExpired, code, status)
	case "file_too_large":
		return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileTooLarge, code, status)
	case "file_limit_exceeded":
		return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileLimitExceeded, code, status)
	case "file_inject_failed":
		return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileInjectFailed, code, status)
	case "file_bridge_busy":
		return fmt.Errorf("%w: %w: code=%s status=%d", ErrAgentRejected, ErrFileBridgeBusy, code, status)
	}
	if code != "" {
		return fmt.Errorf("%w: code=%s status=%d", ErrAgentRejected, code, status)
	}
	return fmt.Errorf("%w: status=%d", ErrAgentRejected, status)
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
func (c *AgentClient) IssueProjectPermit(ctx context.Context, workspaceId int, permitId string, ttlSeconds int, displayName string) (int64, error) {
	payload := agentPermitRequest{PermitId: permitId, Kind: ProjectPermitKind, TtlSeconds: ttlSeconds, DisplayName: displayName}
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
