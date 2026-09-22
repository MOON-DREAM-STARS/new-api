package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/QuantumNous/new-api/browser-agent/internal/policy"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime/runtimetest"
)

const testToken = "agent-service-token"

type harness struct {
	server   *httptest.Server
	mgr      *manager.Manager
	driver   *runtimetest.FakeDriver
	probe    *runtimetest.FakeDisplayProbe
	dataRoot string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dataRoot := t.TempDir()
	driver := runtimetest.NewFakeDriver()
	probe := &runtimetest.FakeDisplayProbe{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := manager.New(driver, probe, manager.Options{
		DataRoot:       dataRoot,
		EgressProxyURL: "http://ws-agent:8731",
		IdleTimeout:    10 * time.Minute,
		ScanInterval:   time.Minute,
		Logger:         logger,
		Chown:          workspaceChownHandover(),
	})
	server := httptest.NewServer(New(mgr, testToken, logger))
	t.Cleanup(server.Close)
	return &harness{server: server, mgr: mgr, driver: driver, probe: probe, dataRoot: dataRoot}
}

// workspaceChownHandover keeps the HTTP tests independent of the process uid;
// the real ownership handover is asserted in the manager filesystem tests.
func workspaceChownHandover() func(root *os.Root, name string, uid int, gid int) error {
	if os.Geteuid() == 0 {
		return nil
	}
	return func(*os.Root, string, int, int) error { return nil }
}

func (h *harness) request(t *testing.T, method string, path string, authorization string, body string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, h.server.URL+path, reader)
	require.NoError(t, err)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response, payload
}

func (h *harness) startRuntime(t *testing.T, workspaceID int64) runtimeJSON {
	t.Helper()
	payload := fmt.Sprintf(`{"workspace_id":%d,"provider":"chatgpt"}`, workspaceID)
	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes", "Bearer "+testToken, payload)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var parsed runtimeJSON
	require.NoError(t, json.Unmarshal(body, &parsed))
	return parsed
}

type runtimeJSON struct {
	RuntimeID      string                    `json:"runtime_id"`
	WorkspaceID    int64                     `json:"workspace_id"`
	State          string                    `json:"state"`
	CreatedAt      int64                     `json:"created_at"`
	LastActivityAt int64                     `json:"last_activity_at"`
	IdleDeadlineAt int64                     `json:"idle_deadline_at"`
	Navigation     *manager.NavigationStatus `json:"navigation"`
	Page           *manager.PageStatus       `json:"page"`
}

func TestHealthzIsPublicAndMinimal(t *testing.T) {
	h := newHarness(t)
	response, body := h.request(t, http.MethodGet, "/healthz", "", "")
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.JSONEq(t, `{"status":"ok"}`, string(body))
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))
}

func TestProtectedEndpointsRequireBearerToken(t *testing.T) {
	h := newHarness(t)
	endpoints := [][2]string{
		{http.MethodPost, "/internal/v1/runtimes"},
		{http.MethodGet, "/internal/v1/runtimes/1"},
		{http.MethodPost, "/internal/v1/runtimes/1/stop"},
		{http.MethodPost, "/internal/v1/runtimes/1/restart"},
		{http.MethodPost, "/internal/v1/runtimes/1/activity"},
		{http.MethodPost, "/internal/v1/runtimes/1/navigation"},
		{http.MethodPut, "/internal/v1/runtimes/1/ownership"},
		{http.MethodPost, "/internal/v1/runtimes/1/permits"},
		{http.MethodGet, "/internal/v1/runtimes/1/observations"},
		{http.MethodPost, "/internal/v1/runtimes/1/observations/ack"},
		{http.MethodPost, "/internal/v1/runtimes/1/clipboard/copy"},
		{http.MethodPost, "/internal/v1/runtimes/1/clipboard/paste"},
		{http.MethodGet, "/internal/v1/runtimes/1/kasm/vnc.html"},
		{http.MethodGet, "/internal/v1/unknown"},
	}
	for _, endpoint := range endpoints {
		for _, authorization := range []string{"", "Bearer wrong-token-value", "Basic " + testToken, testToken} {
			response, body := h.request(t, endpoint[0], endpoint[1], authorization, "")
			assert.Equal(t, http.StatusUnauthorized, response.StatusCode, "%s %s", endpoint[0], endpoint[1])
			assert.JSONEq(t, `{"success":false,"error":"unauthorized"}`, string(body))
		}
	}
}

func TestStartGetStopLifecycle(t *testing.T) {
	h := newHarness(t)

	started := h.startRuntime(t, 123)
	assert.Equal(t, "ws-123", started.RuntimeID)
	assert.Equal(t, int64(123), started.WorkspaceID)
	assert.Equal(t, string(manager.StateRunning), started.State)
	assert.NotZero(t, started.CreatedAt)
	assert.NotZero(t, started.LastActivityAt)
	assert.NotZero(t, started.IdleDeadlineAt)

	response, body := h.request(t, http.MethodGet, "/internal/v1/runtimes/123", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)
	var fetched runtimeJSON
	require.NoError(t, json.Unmarshal(body, &fetched))
	assert.Equal(t, started, fetched)

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/123/stop", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)
	var stopped runtimeJSON
	require.NoError(t, json.Unmarshal(body, &stopped))
	assert.Equal(t, string(manager.StateStopped), stopped.State)
	assert.Zero(t, stopped.IdleDeadlineAt)
	assert.DirExists(t, filepath.Join(h.dataRoot, "workspace-123", "profile"))

	response, _ = h.request(t, http.MethodGet, "/internal/v1/runtimes/123", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusOK, response.StatusCode)

	response, body = h.request(t, http.MethodGet, "/internal/v1/runtimes/900", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_found"}`, string(body))

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/900/stop", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_found"}`, string(body))
}

func TestStartIsIdempotentAcrossRequests(t *testing.T) {
	h := newHarness(t)
	first := h.startRuntime(t, 321)
	second := h.startRuntime(t, 321)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, h.driver.CreateCalls)
	assert.Equal(t, 1, h.driver.ContainerCount())
}

func TestRestartReusesStoredProviderWithoutBody(t *testing.T) {
	h := newHarness(t)
	h.startRuntime(t, 321)

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/321/restart", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var restarted runtimeJSON
	require.NoError(t, json.Unmarshal(body, &restarted))
	assert.Equal(t, "ws-321", restarted.RuntimeID)
	assert.Equal(t, string(manager.StateRunning), restarted.State)
	assert.Equal(t, 2, h.driver.CreateCalls)
	assert.Contains(t, h.driver.LastCreateSpec().Env, "WW_PROVIDER=chatgpt")

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/555/restart", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_found"}`, string(body))
}

func TestActivityRefreshesIdleDeadline(t *testing.T) {
	h := newHarness(t)
	started := h.startRuntime(t, 77)

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/77/activity", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)
	var refreshed runtimeJSON
	require.NoError(t, json.Unmarshal(body, &refreshed))
	assert.GreaterOrEqual(t, refreshed.IdleDeadlineAt, started.IdleDeadlineAt)
	assert.Equal(t, string(manager.StateRunning), refreshed.State)

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/7788/activity", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_found"}`, string(body))
}

func TestStartInjectsGuardMode(t *testing.T) {
	h := newHarness(t)
	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes", "Bearer "+testToken, `{"workspace_id":91,"provider":"chatgpt","mode":"login"}`)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Contains(t, h.driver.LastCreateSpec().Env, "WW_GUARD_MODE=LOGIN")
	assert.Contains(t, h.driver.LastCreateSpec().Env, "WW_PROXY_SERVER=http://ws-agent:8731")

	mode, workspaceID, ok := h.mgr.LookupSource("10.77.0.1")
	require.True(t, ok)
	assert.Equal(t, policy.ModeLogin, mode)
	assert.Equal(t, int64(91), workspaceID)
}

func TestValidationRejectsBadRequests(t *testing.T) {
	h := newHarness(t)
	cases := []string{
		`{"provider":"chatgpt"}`,
		`{"workspace_id":1}`,
		`{"workspace_id":1,"provider":"bad provider"}`,
		`{"workspace_id":1,"provider":"chatgpt","width":100}`,
		`{"workspace_id":1,"provider":"chatgpt","height":5000}`,
		`{"workspace_id":1,"provider":"chatgpt","mode":"open"}`,
		`not-json`,
	}
	for _, payload := range cases {
		response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes", "Bearer "+testToken, payload)
		assert.Equal(t, http.StatusBadRequest, response.StatusCode, payload)
		assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))
	}

	response, body := h.request(t, http.MethodGet, "/internal/v1/runtimes/abc", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))

	response, body = h.request(t, http.MethodGet, "/internal/v1/unknown", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"not_found"}`, string(body))
	assert.Equal(t, 0, h.driver.CreateCalls)
}

func TestRestartModeOverrideIsValidatedAndInjected(t *testing.T) {
	h := newHarness(t)
	h.startRuntime(t, 92)

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/92/restart", "Bearer "+testToken, `{"mode":"LOGIN"}`)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.Contains(t, h.driver.LastCreateSpec().Env, "WW_GUARD_MODE=LOGIN")

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/92/restart", "Bearer "+testToken, `{"mode":"OPEN"}`)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/555/restart", "Bearer "+testToken, `{"mode":"OPEN"}`)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))
}

func TestRuntimeResponsesDoNotLeakInternals(t *testing.T) {
	h := newHarness(t)
	h.startRuntime(t, 321)

	response, body := h.request(t, http.MethodGet, "/internal/v1/runtimes/321", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)

	for _, forbidden := range []string{h.dataRoot, testToken, "10.77.0.1", "container-1", "/workspace", "6901", "newapi-ws-runtime-321", "WW_"} {
		assert.NotContains(t, string(body), forbidden)
	}

	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	assert.ElementsMatch(t, []string{
		"runtime_id", "workspace_id", "state", "mode", "created_at", "last_activity_at", "idle_deadline_at",
		"navigation", "page", "project_creation", "stream_bytes_out", "stream_bytes_in", "ime_state",
	}, mapKeys(raw))
}

func TestStartFailsClosedWhenContainerExitsImmediately(t *testing.T) {
	h := newHarness(t)
	h.driver.ExitOnStart = true

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes", "Bearer "+testToken, `{"workspace_id":15,"provider":"chatgpt"}`)
	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"internal_error"}`, string(body))
	assert.NotContains(t, string(body), "runtime_id", "the control plane must never receive RUNNING for a dead container")

	response, body = h.request(t, http.MethodGet, "/internal/v1/runtimes/15", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)
	var stored runtimeJSON
	require.NoError(t, json.Unmarshal(body, &stored))
	assert.Equal(t, string(manager.StateFailed), stored.State)

}
func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func TestOwnershipEndpointWritesGuardState(t *testing.T) {
	h := newHarness(t)
	project := "g-p-" + strings.Repeat("a", 32)
	conversation := "abcd-1234"
	payload := fmt.Sprintf(
		`{"generation":3,"projects":["%s","%s"],"conversations":["%s","%s"]}`,
		project, project, conversation, conversation,
	)

	response, body := h.request(t, http.MethodPut, "/internal/v1/runtimes/41/ownership", "Bearer "+testToken, payload)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, `{"generation":3,"projects":1,"conversations":1}`, string(body))
	assert.NotContains(t, string(body), h.dataRoot)

	data, err := os.ReadFile(filepath.Join(manager.WorkspaceDir(h.dataRoot, 41), ".guard", "ownership.json"))
	require.NoError(t, err)
	var stored struct {
		Generation    int64    `json:"generation"`
		Projects      []string `json:"projects"`
		Conversations []string `json:"conversations"`
	}
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, int64(3), stored.Generation)
	assert.Equal(t, []string{project}, stored.Projects)
	assert.Equal(t, []string{conversation}, stored.Conversations)
}

func TestOwnershipEndpointRejectsInvalidRequest(t *testing.T) {
	h := newHarness(t)
	cases := []string{
		`{"generation":-1,"projects":[],"conversations":[]}`,
		`{"generation":0,"projects":["not-a-project-id"],"conversations":[]}`,
		`{"generation":0,"projects":[],"conversations":["short"]}`,
		`not-json`,
	}
	for _, payload := range cases {
		response, body := h.request(t, http.MethodPut, "/internal/v1/runtimes/42/ownership", "Bearer "+testToken, payload)
		assert.Equal(t, http.StatusBadRequest, response.StatusCode, payload)
		assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))
	}
	assert.NoDirExists(t, filepath.Join(manager.WorkspaceDir(h.dataRoot, 42), ".guard"))
}

func TestPermitEndpointRequiresRunningRuntime(t *testing.T) {
	h := newHarness(t)
	payload := `{"permit_id":"permit-0001","kind":"project_create","ttl_seconds":300}`

	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/404/permits", "Bearer "+testToken, payload)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_found"}`, string(body))

	h.startRuntime(t, 51)
	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/51/stop", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/51/permits", "Bearer "+testToken, payload)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_running"}`, string(body))
}

func TestPermitEndpointIssuesPermitAndClearsConsumedMarker(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(52)
	h.startRuntime(t, workspaceID)

	consumedPath := filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), ".guard", "permit.consumed")
	require.NoError(t, os.WriteFile(consumedPath, []byte(`{"permit_id":"permit-old","consumed_at":1758192000}`), 0o644))

	response, body := h.request(t, http.MethodPost, fmt.Sprintf("/internal/v1/runtimes/%d/permits", workspaceID), "Bearer "+testToken,
		`{"permit_id":"permit-0002","kind":"project_create","ttl_seconds":300,"display_name":"Quarterly Review"}`)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))

	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	assert.ElementsMatch(t, []string{"permit_id", "expires_at"}, mapKeys(raw))
	assert.Equal(t, "permit-0002", raw["permit_id"])
	assert.NotZero(t, raw["expires_at"])
	assert.NotContains(t, string(body), h.dataRoot)
	assert.NoFileExists(t, consumedPath)

	permitData, err := os.ReadFile(filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), ".guard", "permit.json"))
	require.NoError(t, err)
	var stored struct {
		PermitID    string `json:"permit_id"`
		Kind        string `json:"kind"`
		IssuedAt    int64  `json:"issued_at"`
		ExpiresAt   int64  `json:"expires_at"`
		DisplayName string `json:"display_name"`
	}
	require.NoError(t, json.Unmarshal(permitData, &stored))
	assert.Equal(t, "permit-0002", stored.PermitID)
	assert.Equal(t, "project_create", stored.Kind)
	assert.NotZero(t, stored.IssuedAt)
	assert.Equal(t, int64(raw["expires_at"].(float64)), stored.ExpiresAt)
	assert.Equal(t, "Quarterly Review", stored.DisplayName)
}

func TestPermitEndpointRejectsInvalidRequest(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(53)
	h.startRuntime(t, workspaceID)
	endpoint := fmt.Sprintf("/internal/v1/runtimes/%d/permits", workspaceID)

	cases := []string{
		`{"permit_id":"short","kind":"project_create","ttl_seconds":300}`,
		fmt.Sprintf(`{"permit_id":"%s","kind":"project_create","ttl_seconds":300}`, strings.Repeat("p", 65)),
		`{"permit_id":"permit-0001","kind":"project_delete","ttl_seconds":300}`,
		`{"permit_id":"permit-0001","kind":"project_create","ttl_seconds":0}`,
		`{"permit_id":"permit-0001","kind":"project_create","ttl_seconds":3601}`,
		fmt.Sprintf(`{"permit_id":"permit-0001","kind":"project_create","ttl_seconds":300,"display_name":"%s"}`, strings.Repeat("x", 65)),
		`not-json`,
	}
	for _, payload := range cases {
		response, body := h.request(t, http.MethodPost, endpoint, "Bearer "+testToken, payload)
		assert.Equal(t, http.StatusBadRequest, response.StatusCode, payload)
		assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))
	}
	assert.NoFileExists(t, filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), ".guard", "permit.json"))
}

func TestObservationsEndpointPullAndAck(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(61)
	h.startRuntime(t, workspaceID)
	endpoint := fmt.Sprintf("/internal/v1/runtimes/%d/observations", workspaceID)

	response, body := h.request(t, http.MethodGet, endpoint, "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, `{"observations":[],"next_offset":0}`, string(body))

	complete := `{"event":"project_created","permit_id":"permit-0001","external_project_id":"g-p-` + strings.Repeat("a", 32) + `","slug":"demo","observed_at":100}`
	partial := `{"event":"conversation_created"`
	observationsPath := filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), ".guard", "observations.jsonl")
	require.NoError(t, os.WriteFile(observationsPath, []byte(complete+"\n"+partial), 0o644))

	response, body = h.request(t, http.MethodGet, endpoint, "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var page struct {
		Observations []json.RawMessage `json:"observations"`
		NextOffset   int64             `json:"next_offset"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	require.Len(t, page.Observations, 1)
	assert.JSONEq(t, complete, string(page.Observations[0]))
	assert.Equal(t, int64(len(complete)+1), page.NextOffset)

	response, body = h.request(t, http.MethodGet, endpoint, "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.JSONEq(t, fmt.Sprintf(`{"observations":[%s],"next_offset":%d}`, complete, page.NextOffset), string(body))

	ackEndpoint := fmt.Sprintf("/internal/v1/runtimes/%d/observations/ack", workspaceID)
	response, body = h.request(t, http.MethodPost, ackEndpoint, "Bearer "+testToken, fmt.Sprintf(`{"offset":%d}`, page.NextOffset))
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, fmt.Sprintf(`{"offset":%d}`, page.NextOffset), string(body))

	response, body = h.request(t, http.MethodGet, endpoint, "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)
	assert.JSONEq(t, fmt.Sprintf(`{"observations":[],"next_offset":%d}`, page.NextOffset), string(body))
}

func TestAckObservationsEndpointRejectsInvalidOffset(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(62)
	h.startRuntime(t, workspaceID)
	endpoint := fmt.Sprintf("/internal/v1/runtimes/%d/observations/ack", workspaceID)

	for _, payload := range []string{`{"offset":-1}`, `{"offset":1}`, `{}`, `not-json`} {
		response, body := h.request(t, http.MethodPost, endpoint, "Bearer "+testToken, payload)
		assert.Equal(t, http.StatusBadRequest, response.StatusCode, payload)
		assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))
	}

	response, body := h.request(t, http.MethodPost, endpoint, "Bearer "+testToken, `{"offset":0}`)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, `{"offset":0}`, string(body))
	assert.NotContains(t, string(body), h.dataRoot)
}

func serveNavigationReceipt(t *testing.T, dir string) func() {
	t.Helper()
	stop := make(chan struct{})
	go func() {
		commandPath := filepath.Join(dir, "command.json")
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(commandPath)
			if err == nil {
				var command struct {
					ID int64 `json:"id"`
				}
				if json.Unmarshal(data, &command) == nil && command.ID > 0 {
					payload, marshalErr := json.Marshal(map[string]any{
						"id":             command.ID,
						"can_go_back":    true,
						"can_go_forward": false,
						"updated_at":     2000,
					})
					if marshalErr == nil {
						_ = os.WriteFile(filepath.Join(dir, "navigation.json"), payload, 0o644)
					}
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return func() { close(stop) }
}

func TestNavigationEndpointReturnsReceiptAndSnapshot(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(81)
	h.startRuntime(t, workspaceID)
	dir := filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), ".guard")
	stop := serveNavigationReceipt(t, dir)
	defer stop()

	endpoint := fmt.Sprintf("/internal/v1/runtimes/%d/navigation", workspaceID)
	response, body := h.request(t, http.MethodPost, endpoint, "Bearer "+testToken, `{"action":"back"}`)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.JSONEq(t, `{"action":"back","navigation":{"can_go_back":true,"can_go_forward":false,"updated_at":2000}}`, string(body))

	response, body = h.request(t, http.MethodGet, fmt.Sprintf("/internal/v1/runtimes/%d", workspaceID), "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var snapshot runtimeJSON
	require.NoError(t, json.Unmarshal(body, &snapshot))
	require.NotNil(t, snapshot.Navigation)
	assert.True(t, snapshot.Navigation.CanGoBack)
	assert.False(t, snapshot.Navigation.CanGoForward)
	assert.Equal(t, int64(2000), snapshot.Navigation.UpdatedAt)
}

func TestSnapshotExposesPageHealthWithoutURLs(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(85)
	h.startRuntime(t, workspaceID)
	dir := filepath.Join(manager.WorkspaceDir(h.dataRoot, workspaceID), ".guard")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	payload, err := json.Marshal(map[string]any{
		"id":             7,
		"can_go_back":    false,
		"can_go_forward": false,
		"updated_at":     3000,
		"page_state":     "FAILED",
		"page_error":     "ERR_TUNNEL_CONNECTION_FAILED",
		"page_attempts":  3,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "navigation.json"), payload, 0o644))

	response, body := h.request(t, http.MethodGet, fmt.Sprintf("/internal/v1/runtimes/%d", workspaceID), "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	var snapshot runtimeJSON
	require.NoError(t, json.Unmarshal(body, &snapshot))
	require.NotNil(t, snapshot.Page)
	assert.Equal(t, manager.PageStateFailed, snapshot.Page.State)
	assert.Equal(t, "ERR_TUNNEL_CONNECTION_FAILED", snapshot.Page.Error)
	assert.Equal(t, 3, snapshot.Page.Attempts)
	assert.Equal(t, int64(3000), snapshot.Page.UpdatedAt)
	assert.NotContains(t, string(body), "https://")
	assert.NotContains(t, string(body), "chrome-error://")
}

func TestManagerCapacityErrorMapsToConflict(t *testing.T) {
	recorder := httptest.NewRecorder()
	server := &Server{}
	server.writeManagerError(recorder, 1, manager.ErrCapacityReached)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.JSONEq(t, `{"success":false,"error":"runtime_capacity_reached"}`, recorder.Body.String())
}

func TestNavigationEndpointMapsErrors(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(82)
	h.startRuntime(t, workspaceID)
	endpoint := fmt.Sprintf("/internal/v1/runtimes/%d/navigation", workspaceID)

	for _, payload := range []string{`{"action":"bogus"}`, `{}`, `not-json`} {
		response, body := h.request(t, http.MethodPost, endpoint, "Bearer "+testToken, payload)
		assert.Equal(t, http.StatusBadRequest, response.StatusCode, payload)
		assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))
	}
	response, body := h.request(t, http.MethodPost, "/internal/v1/runtimes/abc/navigation", "Bearer "+testToken, `{"action":"state"}`)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"invalid_request"}`, string(body))

	response, body = h.request(t, http.MethodPost, "/internal/v1/runtimes/999/navigation", "Bearer "+testToken, `{"action":"state"}`)
	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_found"}`, string(body))

	_, err := h.mgr.Stop(context.Background(), workspaceID)
	require.NoError(t, err)
	response, body = h.request(t, http.MethodPost, endpoint, "Bearer "+testToken, `{"action":"state"}`)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_running"}`, string(body))

	unavailableID := int64(83)
	h.startRuntime(t, unavailableID)
	guardDir := filepath.Join(manager.WorkspaceDir(h.dataRoot, unavailableID), ".guard")
	require.NoError(t, os.Mkdir(filepath.Join(guardDir, "command.json"), 0o700))
	response, body = h.request(t, http.MethodPost, fmt.Sprintf("/internal/v1/runtimes/%d/navigation", unavailableID), "Bearer "+testToken, `{"action":"reload"}`)
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"navigation_unavailable"}`, string(body))
}

func TestNavigationEndpointTimesOutWithoutGuardReceipt(t *testing.T) {
	h := newHarness(t)
	workspaceID := int64(84)
	h.startRuntime(t, workspaceID)
	response, body := h.request(t, http.MethodPost, fmt.Sprintf("/internal/v1/runtimes/%d/navigation", workspaceID), "Bearer "+testToken, `{"action":"state"}`)
	assert.Equal(t, http.StatusGatewayTimeout, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"navigation_timeout"}`, string(body))
}
