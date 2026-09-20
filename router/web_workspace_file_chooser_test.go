package router

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fileChooserID = "chooser-0000000000000001"

func fileChooserWorkspaceID(t *testing.T, fixture *webWorkspaceSessionRouterFixture, userId int) int {
	t.Helper()
	var workspace model.WebWorkspace
	require.NoError(t, fixture.db.Where("user_id = ?", userId).First(&workspace).Error)
	return workspace.Id
}

func multipartFileChooserRequest(t *testing.T, engine *gin.Engine, path string, token string, size int) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "local.txt")
	require.NoError(t, err)
	_, err = part.Write(bytes.Repeat([]byte("a"), size))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Authorization", token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestWebWorkspaceRouterFileChooserOwnership(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	tokenB := webWorkspaceBearer(t, fixture.userB)
	sessionID := startWebWorkspaceRouterSession(t, fixture, tokenA)

	get := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session/"+sessionID+"/file-chooser?wait_ms=0", tokenB, "")
	upload := multipartFileChooserRequest(t, fixture.engine, "/api/web-workspace/session/"+sessionID+"/file-chooser/"+fileChooserID+"/files", tokenB, 1)
	cancel := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session/"+sessionID+"/file-chooser/"+fileChooserID+"/cancel", tokenB, "")

	for _, recorder := range []*httptest.ResponseRecorder{get, upload, cancel} {
		assert.Equal(t, http.StatusNotFound, recorder.Code)
		assert.Equal(t, "WEB_WORKSPACE_SESSION_NOT_FOUND", decodeWebWorkspaceError(t, recorder).Code)
	}
	assert.Empty(t, agent.fileChooserCallsFor(fileChooserWorkspaceID(t, fixture, fixture.userA.Id)))
}

func TestWebWorkspaceRouterFileChooserStreamsMultipartToAgent(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionID := startWebWorkspaceRouterSession(t, fixture, token)
	workspaceID := fileChooserWorkspaceID(t, fixture, fixture.userA.Id)

	get := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session/"+sessionID+"/file-chooser?wait_ms=0", token, "")
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	assert.Contains(t, get.Body.String(), fileChooserID)

	recorder := multipartFileChooserRequest(t, fixture.engine, "/api/web-workspace/session/"+sessionID+"/file-chooser/"+fileChooserID+"/files", token, 70<<10)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"state":"DONE"`)

	calls := agent.fileChooserCallsFor(workspaceID)
	require.Len(t, calls, 1)
	assert.Equal(t, "files", calls[0].Method)
	assert.Equal(t, fileChooserID, calls[0].ChooserID)
	assert.True(t, strings.HasPrefix(calls[0].ContentType, "multipart/form-data"))
	assert.Greater(t, calls[0].BodySize, 64<<10)
}

func TestWebWorkspaceRouterFileChooserErrorStatuses(t *testing.T) {
	cases := []struct {
		code   string
		status int
		public string
	}{
		{code: "file_chooser_expired", status: http.StatusGone, public: "WEB_WORKSPACE_FILE_CHOOSER_EXPIRED"},
		{code: "file_too_large", status: http.StatusRequestEntityTooLarge, public: "WEB_WORKSPACE_FILE_TOO_LARGE"},
		{code: "file_limit_exceeded", status: http.StatusBadRequest, public: "WEB_WORKSPACE_FILE_LIMIT_EXCEEDED"},
		{code: "file_inject_failed", status: http.StatusUnprocessableEntity, public: "WEB_WORKSPACE_FILE_INJECT_FAILED"},
		{code: "file_bridge_busy", status: http.StatusConflict, public: "WEB_WORKSPACE_FILE_BRIDGE_BUSY"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			agent := newRouterFakeAgent(t)
			fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
			token := webWorkspaceBearer(t, fixture.userA)
			sessionID := startWebWorkspaceRouterSession(t, fixture, token)
			agent.setFileChooserFailure(tc.code, http.StatusUnprocessableEntity)

			recorder := multipartFileChooserRequest(t, fixture.engine, "/api/web-workspace/session/"+sessionID+"/file-chooser/"+fileChooserID+"/files", token, 1)
			assert.Equal(t, tc.status, recorder.Code)
			assert.Equal(t, tc.public, decodeWebWorkspaceError(t, recorder).Code)
		})
	}
}
