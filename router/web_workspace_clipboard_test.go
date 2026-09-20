package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clipboardRouterRequest(engine *gin.Engine, path string, token string, contentType string, body []byte) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Authorization", token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func clipboardRouterWorkspaceID(t *testing.T, fixture *webWorkspaceSessionRouterFixture, userId int) int {
	t.Helper()
	var workspace model.WebWorkspace
	require.NoError(t, fixture.db.Where("user_id = ?", userId).First(&workspace).Error)
	return workspace.Id
}

func TestWebWorkspaceRouterClipboardOwnership(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	tokenB := webWorkspaceBearer(t, fixture.userB)
	sessionID := startWebWorkspaceRouterSession(t, fixture, tokenA)

	copyResponse := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/copy", tokenB, "", nil)
	pasteResponse := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/paste", tokenB, "text/plain", []byte("local"))

	for _, recorder := range []*httptest.ResponseRecorder{copyResponse, pasteResponse} {
		assert.Equal(t, http.StatusNotFound, recorder.Code)
		assert.Equal(t, "WEB_WORKSPACE_SESSION_NOT_FOUND", decodeWebWorkspaceError(t, recorder).Code)
	}
	assert.Empty(t, agent.clipboardCallsFor(clipboardRouterWorkspaceID(t, fixture, fixture.userA.Id)))
}

func TestWebWorkspaceRouterClipboardRoundTrip(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionID := startWebWorkspaceRouterSession(t, fixture, token)
	workspaceID := clipboardRouterWorkspaceID(t, fixture, fixture.userA.Id)

	copyResponse := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/copy", token, "", nil)
	require.Equal(t, http.StatusOK, copyResponse.Code, copyResponse.Body.String())
	assert.Equal(t, "text/plain", copyResponse.Header().Get("Content-Type"))
	assert.Equal(t, "remote selection", copyResponse.Body.String())

	payload := bytes.Repeat([]byte("x"), 70<<10)
	pasteResponse := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/paste", token, "text/plain", payload)
	require.Equal(t, http.StatusOK, pasteResponse.Code, pasteResponse.Body.String())

	calls := agent.clipboardCallsFor(workspaceID)
	require.Len(t, calls, 2)
	assert.Equal(t, "copy", calls[0].Method)
	assert.Equal(t, "paste", calls[1].Method)
	assert.Equal(t, "text/plain", calls[1].MIME)
	assert.Equal(t, len(payload), calls[1].BodySize)
}

func TestWebWorkspaceRouterClipboardErrorStatuses(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionID := startWebWorkspaceRouterSession(t, fixture, token)

	unsupported := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/paste", token, "application/json", []byte("{}"))
	assert.Equal(t, http.StatusUnsupportedMediaType, unsupported.Code)
	assert.Equal(t, "WEB_WORKSPACE_CLIPBOARD_MIME_UNSUPPORTED", decodeWebWorkspaceError(t, unsupported).Code)

	oversized := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/paste", token, "text/plain", bytes.Repeat([]byte("x"), (8<<20)+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, oversized.Code)
	assert.Equal(t, "WEB_WORKSPACE_CLIPBOARD_PAYLOAD_TOO_LARGE", decodeWebWorkspaceError(t, oversized).Code)

	agent.setClipboardFailure("clipboard_unavailable")
	unavailable := clipboardRouterRequest(fixture.engine, "/api/web-workspace/session/"+sessionID+"/clipboard/copy", token, "", nil)
	assert.Equal(t, http.StatusServiceUnavailable, unavailable.Code)
	assert.Equal(t, "WEB_WORKSPACE_CLIPBOARD_UNAVAILABLE", decodeWebWorkspaceError(t, unavailable).Code)
}
