package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/manager"
	"github.com/QuantumNous/new-api/browser-agent/internal/runtime/runtimetest"
)

const testToken = "agent-service-token"

type harness struct {
	server   *httptest.Server
	mgr      *manager.Manager
	driver   *runtimetest.FakeDriver
	display  *runtimetest.FakeDisplay
	pipes    *displayPipes
	dataRoot string
}

// displayPipes hands out a fresh net.Pipe pair per display connection so the
// readiness probe and the stream proxy never share a connection. Pair 0 is the
// readiness probe, pair 1 the proxied stream.
type displayPipes struct {
	mu    sync.Mutex
	pairs []displayPair
}

type displayPair struct {
	agent net.Conn
	peer  net.Conn
}

func (p *displayPipes) connect(context.Context, int64) (io.ReadWriteCloser, error) {
	agent, peer := net.Pipe()
	p.mu.Lock()
	p.pairs = append(p.pairs, displayPair{agent: agent, peer: peer})
	p.mu.Unlock()
	return agent, nil
}

func (p *displayPipes) pair(t *testing.T, index int) (net.Conn, net.Conn) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Lessf(t, index, len(p.pairs), "display connection %d was never opened", index)
	return p.pairs[index].agent, p.pairs[index].peer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dataRoot := t.TempDir()
	driver := runtimetest.NewFakeDriver()
	pipes := &displayPipes{}
	display := &runtimetest.FakeDisplay{ConnectFunc: pipes.connect}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mgr := manager.New(driver, display, manager.Options{
		DataRoot:     dataRoot,
		IdleTimeout:  10 * time.Minute,
		ScanInterval: time.Minute,
		Logger:       logger,
		Chown:        workspaceChownHandover(),
	})
	server := httptest.NewServer(New(mgr, testToken, logger))
	t.Cleanup(server.Close)
	return &harness{server: server, mgr: mgr, driver: driver, display: display, pipes: pipes, dataRoot: dataRoot}
}

// workspaceChownHandover keeps the HTTP tests independent of the process uid;
// the real ownership handover is asserted in the manager filesystem tests.
func workspaceChownHandover() func(path string, uid int, gid int) error {
	if os.Geteuid() == 0 {
		return nil
	}
	return func(string, int, int) error { return nil }
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

func (h *harness) dialStream(t *testing.T, workspaceID int64) *websocket.Conn {
	t.Helper()
	endpoint := "ws" + strings.TrimPrefix(h.server.URL, "http") + fmt.Sprintf("/internal/v1/runtimes/%d/stream", workspaceID)
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, http.Header{"Authorization": []string{"Bearer " + testToken}})
	require.NoError(t, err)
	return conn
}

type runtimeJSON struct {
	RuntimeID      string `json:"runtime_id"`
	WorkspaceID    int64  `json:"workspace_id"`
	State          string `json:"state"`
	CreatedAt      int64  `json:"created_at"`
	LastActivityAt int64  `json:"last_activity_at"`
	IdleDeadlineAt int64  `json:"idle_deadline_at"`
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
		{http.MethodGet, "/internal/v1/runtimes/1/stream"},
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

func TestValidationRejectsBadRequests(t *testing.T) {
	h := newHarness(t)
	cases := []string{
		`{"provider":"chatgpt"}`,
		`{"workspace_id":1}`,
		`{"workspace_id":1,"provider":"bad provider"}`,
		`{"workspace_id":1,"provider":"chatgpt","width":100}`,
		`{"workspace_id":1,"provider":"chatgpt","height":5000}`,
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

func TestRuntimeResponsesDoNotLeakInternals(t *testing.T) {
	h := newHarness(t)
	h.startRuntime(t, 321)

	response, body := h.request(t, http.MethodGet, "/internal/v1/runtimes/321", "Bearer "+testToken, "")
	require.Equal(t, http.StatusOK, response.StatusCode)

	for _, forbidden := range []string{h.dataRoot, testToken, "10.77.0.1", "container-1", "/workspace", "5900", "newapi-ws-runtime-321", "WW_"} {
		assert.NotContains(t, string(body), forbidden)
	}

	var raw map[string]any
	require.NoError(t, json.Unmarshal(body, &raw))
	assert.ElementsMatch(t, []string{
		"runtime_id", "workspace_id", "state", "created_at", "last_activity_at", "idle_deadline_at",
	}, mapKeys(raw))
}

func TestStreamReturnsConflictWhenRuntimeIsNotRunning(t *testing.T) {
	h := newHarness(t)
	response, body := h.request(t, http.MethodGet, "/internal/v1/runtimes/404/stream", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_running"}`, string(body))
}

func TestStreamProxiesRawRFBBytes(t *testing.T) {
	h := newHarness(t)
	h.startRuntime(t, 321)

	conn := h.dialStream(t, 321)
	defer conn.Close()

	_, proxySide := h.pipes.pair(t, 1)
	defer proxySide.Close()

	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("RFB 003.008\n")))
	_ = proxySide.SetReadDeadline(time.Now().Add(3 * time.Second))
	buffer := make([]byte, 64)
	read, err := proxySide.Read(buffer)
	require.NoError(t, err)
	assert.Equal(t, "RFB 003.008\n", string(buffer[:read]))

	_, err = proxySide.Write([]byte{1, 2, 3, 4, 5})
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, messageType)
	assert.Equal(t, []byte{1, 2, 3, 4, 5}, payload)
}

func TestStreamClosesWhenRuntimeFails(t *testing.T) {
	h := newHarness(t)
	h.startRuntime(t, 900)

	conn := h.dialStream(t, 900)
	defer conn.Close()

	_, proxySide := h.pipes.pair(t, 1)
	defer proxySide.Close()

	h.driver.SetRunning(900, false)
	h.mgr.ScanOnce(context.Background())

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err := conn.ReadMessage()
	require.Error(t, err)

	snapshot, found := h.mgr.Get(900)
	require.True(t, found)
	assert.Equal(t, string(manager.StateFailed), string(snapshot.State))
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

	response, body = h.request(t, http.MethodGet, "/internal/v1/runtimes/15/stream", "Bearer "+testToken, "")
	assert.Equal(t, http.StatusConflict, response.StatusCode)
	assert.JSONEq(t, `{"success":false,"error":"runtime_not_running"}`, string(body))
}
func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
