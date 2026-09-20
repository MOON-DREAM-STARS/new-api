package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (a *routerFakeAgent) inputCallsFor(method string) []routerInputCall {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	calls := make([]routerInputCall, 0, len(a.inputCalls))
	for _, call := range a.inputCalls {
		if call.Method == method {
			calls = append(calls, call)
		}
	}
	return calls
}

func TestWebWorkspaceRouterInjectsTextAndForwardsKey(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)

	text := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/input/text",
		token,
		`{"text":"你好 world"}`,
	)
	require.Equal(t, http.StatusOK, text.Code, text.Body.String())

	key := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/input/key",
		token,
		`{"key":"a","modifiers":["ctrl"]}`,
	)
	require.Equal(t, http.StatusOK, key.Code, key.Body.String())

	require.Len(t, agent.inputCallsFor("text"), 1)
	assert.Equal(t, "你好 world", agent.inputCallsFor("text")[0].Text)
	require.Len(t, agent.inputCallsFor("key"), 1)
	assert.Equal(t, "a", agent.inputCallsFor("key")[0].Key)
	assert.Equal(t, []string{"ctrl"}, agent.inputCallsFor("key")[0].Modifiers)
}

func TestWebWorkspaceRouterReturnsCaret(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)

	recorder := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodGet,
		"/api/web-workspace/session/"+sessionId+"/input/caret",
		token,
		"",
	)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"x":100`)
	assert.Contains(t, recorder.Body.String(), `"y":200`)
	require.Len(t, agent.inputCallsFor("caret"), 1)
}

func TestWebWorkspaceRouterInputOwnership(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	tokenB := webWorkspaceBearer(t, fixture.userB)
	sessionId := startWebWorkspaceRouterSession(t, fixture, tokenA)

	foreign := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/input/text",
		tokenB,
		`{"text":"foreign"}`,
	)
	forged := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/forged-session/input/text",
		tokenB,
		`{"text":"foreign"}`,
	)
	assert.Equal(t, http.StatusNotFound, foreign.Code)
	assert.Equal(t, http.StatusNotFound, forged.Code)
	assert.Equal(t, "WEB_WORKSPACE_SESSION_NOT_FOUND", decodeWebWorkspaceError(t, foreign).Code)
	assert.Equal(t, foreign.Body.String(), forged.Body.String())
}

func TestWebWorkspaceRouterInputMapsAgentErrors(t *testing.T) {
	cases := []struct {
		name       string
		failure    string
		wantStatus int
		wantCode   string
	}{
		{name: "unavailable", failure: "input_unavailable", wantStatus: http.StatusServiceUnavailable, wantCode: "WEB_WORKSPACE_INPUT_UNAVAILABLE"},
		{name: "timeout", failure: "input_timeout", wantStatus: http.StatusGatewayTimeout, wantCode: "WEB_WORKSPACE_INPUT_TIMEOUT"},
		{name: "rejected", failure: "input_rejected", wantStatus: http.StatusUnprocessableEntity, wantCode: "WEB_WORKSPACE_INPUT_REJECTED"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			agent := newRouterFakeAgent(t)
			fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
			token := webWorkspaceBearer(t, fixture.userA)
			sessionId := startWebWorkspaceRouterSession(t, fixture, token)
			agent.mutex.Lock()
			agent.inputFailure = testCase.failure
			agent.mutex.Unlock()

			recorder := doWebWorkspaceRequest(
				fixture.engine,
				http.MethodPost,
				"/api/web-workspace/session/"+sessionId+"/input/text",
				token,
				`{"text":"hello"}`,
			)
			assert.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, decodeWebWorkspaceError(t, recorder).Code)
		})
	}
}
