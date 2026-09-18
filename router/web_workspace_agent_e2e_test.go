package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebWorkspaceRouterAgentEndToEnd drives the complete Phase 2 path against
// a real Browser Agent: authenticated HTTP API -> session -> one-time stream
// ticket -> New API WSS gateway -> agent -> runtime VNC. It only runs when the
// acceptance harness provides the agent endpoint and service token.
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

	ticketRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session/"+sessionId+"/stream-ticket", authToken, "")
	require.Equal(t, http.StatusOK, ticketRecorder.Code, ticketRecorder.Body.String())
	var ticketPayload struct {
		Data struct {
			Ticket    string `json:"ticket"`
			StreamUrl string `json:"stream_url"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(ticketRecorder.Body.Bytes(), &ticketPayload))
	require.NotEmpty(t, ticketPayload.Data.Ticket)

	server := httptest.NewServer(fixture.engine)
	t.Cleanup(server.Close)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + ticketPayload.Data.StreamUrl + "?ticket=" + ticketPayload.Data.Ticket

	conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err, "stream dial through the New API gateway failed")
	defer conn.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(30*time.Second)))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err, "reading the display banner through the gateway failed")
	head := payload
	if len(head) > 20 {
		head = head[:20]
	}
	assert.Equal(t, websocket.BinaryMessage, messageType)
	assert.Truef(t, strings.HasPrefix(string(payload), "RFB "), "expected an RFB banner, got %q", string(head))
}
