package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebWorkspaceRouterAgentEndToEnd drives the complete KasmVNC path against
// a real Browser Agent: authenticated HTTP API -> session -> one-time KasmVNC
// ticket -> same-origin Kasm document served by the runtime. It only runs when
// the acceptance harness provides the agent endpoint and service token.
func TestWebWorkspaceRouterAgentEndToEnd(t *testing.T) {
	agentURL := strings.TrimSpace(os.Getenv("WEB_WORKSPACE_E2E_AGENT_URL"))
	agentToken := strings.TrimSpace(os.Getenv("WEB_WORKSPACE_E2E_AGENT_TOKEN"))
	if agentURL == "" || agentToken == "" {
		t.Skip("WEB_WORKSPACE_E2E_AGENT_URL / WEB_WORKSPACE_E2E_AGENT_TOKEN are not configured")
	}

	fixture := setupWebWorkspaceSessionRouterTest(t, agentURL)
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", agentToken)
	gin.SetMode(gin.TestMode)
	authToken := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", authToken, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var startPayload struct {
		Data struct {
			SessionId string `json:"session_id"`
			State     string `json:"state"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &startPayload))
	sessionId := startPayload.Data.SessionId
	require.NotEmpty(t, sessionId)
	require.Equal(t, "RUNNING", startPayload.Data.State)
	t.Cleanup(func() {
		_ = doWebWorkspaceRequest(fixture.engine, http.MethodDelete, "/api/web-workspace/session/"+sessionId, authToken, "")
	})

	ticketRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session/"+sessionId+"/kasm-ticket", authToken, "")
	require.Equal(t, http.StatusOK, ticketRecorder.Code, ticketRecorder.Body.String())
	var ticketPayload struct {
		Data struct {
			Ticket    string `json:"ticket"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(ticketRecorder.Body.Bytes(), &ticketPayload))
	require.NotEmpty(t, ticketPayload.Data.Ticket)
	assert.Positive(t, ticketPayload.Data.ExpiresAt)

	server := httptest.NewServer(fixture.engine)
	t.Cleanup(server.Close)
	documentURL := server.URL + "/api/web-workspace/session/" + sessionId + "/kasm/t/" + ticketPayload.Data.Ticket + "/vnc.html?autoconnect=1"

	response, err := http.Get(documentURL)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.NotEmpty(t, body, "the KasmVNC document must be proxied from the runtime")

	// The ticket stays bound to its session: another session must not accept it.
	foreign, err := http.Get(server.URL + "/api/web-workspace/session/foreign-session/kasm/t/" + ticketPayload.Data.Ticket + "/vnc.html")
	require.NoError(t, err)
	defer foreign.Body.Close()
	assert.Equal(t, http.StatusForbidden, foreign.StatusCode)
}
