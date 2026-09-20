package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/webworkspace"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const routerTestAgentToken = "router-test-agent-token"

var routerTestUserId atomic.Int64

func nextRouterTestUserId() int {
	return int(routerTestUserId.Add(10) + 8000)
}

// routerFakeAgent implements the Browser Agent contract v1 well enough to test
// the New API session broker, the control-plane sync and the WSS gateway. Every
// runtime-scoped endpoint requires a runtime, like the real agent.
type routerFakeAgent struct {
	server   *httptest.Server
	mutex    sync.Mutex
	runtimes map[int]webworkspace.AgentRuntime
	create   int
	stop     int
	echo     bool

	ownership       map[int]webworkspace.OwnershipSnapshot
	observations    map[int][]webworkspace.Observation
	acked           map[int]int
	permitCalls     []routerPermitCall
	navigationCalls []routerNavigationCall
	restartModes    []string
	createdSizes    [][2]int
	activityCalls   int
	restartSizes    [][2]int
	failPermits     bool
	navigationFail  string
}

type routerPermitCall struct {
	WorkspaceId int
	PermitId    string
	Kind        string
	TtlSeconds  int
	DisplayName string
}

type routerNavigationCall struct {
	WorkspaceId int
	Action      string
}

func newRouterFakeAgent(t *testing.T) *routerFakeAgent {
	t.Helper()
	agent := &routerFakeAgent{
		runtimes:     make(map[int]webworkspace.AgentRuntime),
		ownership:    make(map[int]webworkspace.OwnershipSnapshot),
		observations: make(map[int][]webworkspace.Observation),
		acked:        make(map[int]int),
		echo:         true,
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	agent.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+routerTestAgentToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/stream") {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				messageType, payload, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if agent.echo {
					if err := conn.WriteMessage(messageType, payload); err != nil {
						return
					}
				}
			}
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		path := strings.TrimPrefix(r.URL.Path, "/internal/v1/runtimes")
		if path == "" && r.Method == http.MethodPost {
			var request struct {
				WorkspaceId int `json:"workspace_id"`
				Width       int `json:"width"`
				Height      int `json:"height"`
			}
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.create++
			agent.createdSizes = append(agent.createdSizes, [2]int{request.Width, request.Height})
			runtime, ok := agent.runtimes[request.WorkspaceId]
			if !ok {
				now := time.Now().Unix()
				runtime = webworkspace.AgentRuntime{
					RuntimeId:      fmt.Sprintf("ws-%d", request.WorkspaceId),
					WorkspaceId:    request.WorkspaceId,
					State:          webworkspace.AgentStateRunning,
					Mode:           "LOCKED",
					CreatedAt:      now,
					LastActivityAt: now,
					IdleDeadlineAt: now + 600,
				}
				agent.runtimes[request.WorkspaceId] = runtime
			}
			agent.mutex.Unlock()
			writeRouterAgentJSON(t, w, runtime)
			return
		}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		workspaceId := 0
		_, _ = fmt.Sscanf(parts[0], "%d", &workspaceId)
		suffix := ""
		if len(parts) > 1 {
			suffix = strings.Join(parts[1:], "/")
		}
		agent.mutex.Lock()
		runtime, ok := agent.runtimes[workspaceId]
		agent.mutex.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"runtime_not_found"}`))
			return
		}
		switch {
		case suffix == "stop" && r.Method == http.MethodPost:
			agent.mutex.Lock()
			agent.stop++
			runtime.State = webworkspace.AgentStateStopped
			agent.runtimes[workspaceId] = runtime
			agent.mutex.Unlock()
			writeRouterAgentJSON(t, w, runtime)
		case suffix == "restart" && r.Method == http.MethodPost:
			var restartRequest struct {
				Mode   string `json:"mode"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			}
			if len(body) > 0 {
				if err := common.Unmarshal(body, &restartRequest); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"success":false,"error":"invalid_request"}`))
					return
				}
			}
			agent.mutex.Lock()
			agent.restartModes = append(agent.restartModes, restartRequest.Mode)
			agent.restartSizes = append(agent.restartSizes, [2]int{restartRequest.Width, restartRequest.Height})
			if restartRequest.Mode != "" {
				runtime.Mode = restartRequest.Mode
			}
			runtime.State = webworkspace.AgentStateRunning
			runtime.LastActivityAt = time.Now().Unix()
			runtime.IdleDeadlineAt = time.Now().Unix() + 600
			agent.runtimes[workspaceId] = runtime
			agent.mutex.Unlock()
			writeRouterAgentJSON(t, w, runtime)
		case suffix == "activity" && r.Method == http.MethodPost:
			agent.mutex.Lock()
			agent.activityCalls++
			runtime.LastActivityAt = time.Now().Unix()
			runtime.IdleDeadlineAt = time.Now().Unix() + 600
			runtime.StreamBytesOut = 4096
			runtime.StreamBytesIn = 128
			agent.runtimes[workspaceId] = runtime
			agent.mutex.Unlock()
			writeRouterAgentJSON(t, w, runtime)
		case suffix == "navigation" && r.Method == http.MethodPost:
			var request struct {
				Action string `json:"action"`
			}
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"success":false,"error":"invalid_request"}`))
				return
			}
			agent.mutex.Lock()
			agent.navigationCalls = append(agent.navigationCalls, routerNavigationCall{WorkspaceId: workspaceId, Action: request.Action})
			failure := agent.navigationFail
			if failure == "" {
				runtime.Navigation = &webworkspace.AgentNavigation{
					CanGoBack:    true,
					CanGoForward: false,
					UpdatedAt:    1700000000,
				}
				agent.runtimes[workspaceId] = runtime
			}
			agent.mutex.Unlock()
			switch failure {
			case "timeout":
				w.WriteHeader(http.StatusGatewayTimeout)
				_, _ = w.Write([]byte(`{"success":false,"error":"navigation_timeout"}`))
				return
			case "navigation_unavailable", "runtime_not_running":
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"success":false,"error":"` + failure + `"}`))
				return
			}
			writeRouterAgentJSON(t, w, struct {
				Action     string                        `json:"action"`
				Navigation *webworkspace.AgentNavigation `json:"navigation"`
			}{Action: request.Action, Navigation: runtime.Navigation})
		case suffix == "ownership" && r.Method == http.MethodPut:
			var request webworkspace.OwnershipSnapshot
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.ownership[workspaceId] = request
			agent.mutex.Unlock()
			writeRouterAgentJSON(t, w, struct {
				Generation    int64 `json:"generation"`
				Projects      int   `json:"projects"`
				Conversations int   `json:"conversations"`
			}{request.Generation, len(request.Projects), len(request.Conversations)})
		case suffix == "permits" && r.Method == http.MethodPost:
			var request struct {
				PermitId    string `json:"permit_id"`
				Kind        string `json:"kind"`
				TtlSeconds  int    `json:"ttl_seconds"`
				DisplayName string `json:"display_name"`
			}
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.permitCalls = append(agent.permitCalls, routerPermitCall{
				WorkspaceId: workspaceId,
				PermitId:    request.PermitId,
				Kind:        request.Kind,
				TtlSeconds:  request.TtlSeconds,
				DisplayName: request.DisplayName,
			})
			fail := agent.failPermits
			agent.mutex.Unlock()
			if fail {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"success":false,"error":"runtime_not_running"}`))
				return
			}
			writeRouterAgentJSON(t, w, struct {
				PermitId  string `json:"permit_id"`
				ExpiresAt int64  `json:"expires_at"`
			}{request.PermitId, time.Now().Unix() + 300})
		case suffix == "observations" && r.Method == http.MethodGet:
			agent.mutex.Lock()
			offset := agent.acked[workspaceId]
			log := append([]webworkspace.Observation{}, agent.observations[workspaceId]...)
			agent.mutex.Unlock()
			if offset > len(log) {
				offset = len(log)
			}
			writeRouterAgentJSON(t, w, struct {
				Observations []webworkspace.Observation `json:"observations"`
				NextOffset   int64                      `json:"next_offset"`
			}{log[offset:], int64(len(log))})
		case suffix == "observations/ack" && r.Method == http.MethodPost:
			var request struct {
				Offset int64 `json:"offset"`
			}
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.acked[workspaceId] = int(request.Offset)
			agent.mutex.Unlock()
			writeRouterAgentJSON(t, w, struct {
				Offset int64 `json:"offset"`
			}{request.Offset})
		default:
			writeRouterAgentJSON(t, w, runtime)
		}
	}))
	t.Cleanup(agent.server.Close)
	return agent
}

func (a *routerFakeAgent) addRuntime(workspaceId int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	now := time.Now().Unix()
	a.runtimes[workspaceId] = webworkspace.AgentRuntime{
		RuntimeId:      fmt.Sprintf("ws-%d", workspaceId),
		WorkspaceId:    workspaceId,
		State:          webworkspace.AgentStateRunning,
		CreatedAt:      now,
		LastActivityAt: now,
		IdleDeadlineAt: now + 600,
	}
}

func (a *routerFakeAgent) removeRuntime(workspaceId int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	delete(a.runtimes, workspaceId)
}

func (a *routerFakeAgent) setObservations(workspaceId int, observations ...webworkspace.Observation) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.observations[workspaceId] = append([]webworkspace.Observation{}, observations...)
	a.acked[workspaceId] = 0
}

func (a *routerFakeAgent) ownershipOf(workspaceId int) (webworkspace.OwnershipSnapshot, bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	snapshot, ok := a.ownership[workspaceId]
	return snapshot, ok
}

func (a *routerFakeAgent) ackedOffset(workspaceId int) int {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.acked[workspaceId]
}

func (a *routerFakeAgent) permitCallsFor(workspaceId int) []routerPermitCall {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	calls := make([]routerPermitCall, 0, len(a.permitCalls))
	for _, call := range a.permitCalls {
		if call.WorkspaceId == workspaceId {
			calls = append(calls, call)
		}
	}
	return calls
}

func (a *routerFakeAgent) setFailPermits(fail bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.failPermits = fail
}

func (a *routerFakeAgent) setNavigationFail(failure string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.navigationFail = failure
}

func writeRouterAgentJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	raw, err := common.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

type webWorkspaceSessionRouterFixture struct {
	engine *gin.Engine
	db     *gorm.DB
	userA  *model.User
	userB  *model.User
}

func setupWebWorkspaceSessionRouterTest(t *testing.T, agentBaseURL string) *webWorkspaceSessionRouterFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	previousDB := model.DB
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	settings := system_setting.GetWebWorkspaceSettings()
	previousSettings := *settings
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
		*settings = previousSettings
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSession{},
		&model.WebWorkspace{},
		&model.WebProject{},
		&model.WebConversation{},
	))
	model.DB = db
	common.RedisEnabled = false
	common.SessionSecret = "web-workspace-session-router-test-secret"
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", routerTestAgentToken)

	settings.Enabled = true
	settings.MinimumRole = common.RoleCommonUser
	settings.AllowedGroups = []string{}
	settings.AgentBaseURL = agentBaseURL

	userA := createWebWorkspaceRouterUserWithID(t, db, nextRouterTestUserId(), fmt.Sprintf("ws-session-a-%d", nextRouterTestUserId()))
	userB := createWebWorkspaceRouterUserWithID(t, db, nextRouterTestUserId(), fmt.Sprintf("ws-session-b-%d", nextRouterTestUserId()))

	engine := gin.New()
	api := engine.Group("/api")
	SetWebWorkspaceRouter(api)

	return &webWorkspaceSessionRouterFixture{engine: engine, db: db, userA: userA, userB: userB}
}

func createWebWorkspaceRouterUserWithID(t *testing.T, db *gorm.DB, id int, username string) *model.User {
	t.Helper()
	user := &model.User{
		Id:          id,
		Username:    username,
		AffCode:     "aff-" + username,
		Password:    "unused",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func TestWebWorkspaceRouterSessionRequiresAgentConfiguration(t *testing.T) {
	fixture := setupWebWorkspaceSessionRouterTest(t, "")
	token := webWorkspaceBearer(t, fixture.userA)

	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, "WEB_WORKSPACE_AGENT_UNAVAILABLE", decodeWebWorkspaceError(t, recorder).Code)
}

func TestWebWorkspaceRouterSessionLifecycle(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var startPayload struct {
		Data struct {
			SessionId string `json:"session_id"`
			State     string `json:"state"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &startPayload))
	require.NotEmpty(t, startPayload.Data.SessionId)
	assert.Equal(t, webworkspace.AgentStateRunning, startPayload.Data.State)

	current := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session", token, "")
	assert.Equal(t, http.StatusOK, current.Code)
	assert.Contains(t, current.Body.String(), startPayload.Data.SessionId)

	// A second user must not see or control the first user's session.
	otherToken := webWorkspaceBearer(t, fixture.userB)
	otherCurrent := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session", otherToken, "")
	assert.Equal(t, http.StatusOK, otherCurrent.Code)
	assert.Contains(t, otherCurrent.Body.String(), `"data":null`)

	foreignStop := doWebWorkspaceRequest(fixture.engine, http.MethodDelete, "/api/web-workspace/session/"+startPayload.Data.SessionId, otherToken, "")
	assert.Equal(t, http.StatusNotFound, foreignStop.Code)

	foreignTicket := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session/"+startPayload.Data.SessionId+"/stream-ticket", otherToken, "")
	assert.Equal(t, http.StatusNotFound, foreignTicket.Code)

	ticket := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session/"+startPayload.Data.SessionId+"/stream-ticket", token, "")
	require.Equal(t, http.StatusOK, ticket.Code, ticket.Body.String())
	assert.Contains(t, ticket.Body.String(), `"stream_url":"/api/web-workspace/session/`)

	stop := doWebWorkspaceRequest(fixture.engine, http.MethodDelete, "/api/web-workspace/session/"+startPayload.Data.SessionId, token, "")
	assert.Equal(t, http.StatusOK, stop.Code)

	afterStop := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session", token, "")
	assert.Equal(t, http.StatusOK, afterStop.Code)
	assert.Contains(t, afterStop.Body.String(), `"data":null`)
}

func startWebWorkspaceRouterSession(t *testing.T, fixture *webWorkspaceSessionRouterFixture, token string) string {
	t.Helper()
	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var payload struct {
		Data struct {
			SessionId string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &payload))
	require.NotEmpty(t, payload.Data.SessionId)
	return payload.Data.SessionId
}

func TestWebWorkspaceRouterNavigationSuccess(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)

	recorder := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/navigation",
		token,
		`{"action":"back"}`,
	)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			SessionId  string `json:"session_id"`
			Navigation *struct {
				CanGoBack    bool  `json:"can_go_back"`
				CanGoForward bool  `json:"can_go_forward"`
				UpdatedAt    int64 `json:"updated_at"`
			} `json:"navigation"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, sessionId, payload.Data.SessionId)
	require.NotNil(t, payload.Data.Navigation)
	assert.True(t, payload.Data.Navigation.CanGoBack)
	assert.False(t, payload.Data.Navigation.CanGoForward)
	assert.EqualValues(t, 1700000000, payload.Data.Navigation.UpdatedAt)

	body := recorder.Body.String()
	assert.NotContains(t, body, `"url"`)
	assert.NotContains(t, body, `"address"`)
	assert.NotContains(t, body, `"workspace_id"`)
	assert.NotContains(t, body, `"runtime_id"`)

	agent.mutex.Lock()
	calls := append([]routerNavigationCall{}, agent.navigationCalls...)
	agent.mutex.Unlock()
	require.Len(t, calls, 1)
	assert.Equal(t, "back", calls[0].Action)

	current := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	assert.Contains(t, current.Body.String(), `"can_go_back":true`)
}

func TestWebWorkspaceRouterNavigationOwnership(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	tokenB := webWorkspaceBearer(t, fixture.userB)
	sessionId := startWebWorkspaceRouterSession(t, fixture, tokenA)

	foreign := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/navigation",
		tokenB,
		`{"action":"back"}`,
	)
	forged := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/forged-session/navigation",
		tokenB,
		`{"action":"back"}`,
	)
	assert.Equal(t, http.StatusNotFound, foreign.Code)
	assert.Equal(t, http.StatusNotFound, forged.Code)
	assert.Equal(t, "WEB_WORKSPACE_SESSION_NOT_FOUND", decodeWebWorkspaceError(t, foreign).Code)
	assert.Equal(t, foreign.Body.String(), forged.Body.String())
}

func TestWebWorkspaceRouterNavigationRejectsInvalidAction(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)

	recorder := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/navigation",
		token,
		`{"action":"jump"}`,
	)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, "WEB_WORKSPACE_INVALID_REQUEST", decodeWebWorkspaceError(t, recorder).Code)
}

func TestWebWorkspaceRouterNavigationMapsAgentErrors(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)
	path := "/api/web-workspace/session/" + sessionId + "/navigation"

	cases := []struct {
		name       string
		failure    string
		wantStatus int
		wantCode   string
	}{
		{name: "navigation timeout", failure: "timeout", wantStatus: http.StatusGatewayTimeout, wantCode: "WEB_WORKSPACE_NAVIGATION_TIMEOUT"},
		{name: "navigation unavailable", failure: "navigation_unavailable", wantStatus: http.StatusConflict, wantCode: "WEB_WORKSPACE_NAVIGATION_UNAVAILABLE"},
		{name: "runtime not running", failure: "runtime_not_running", wantStatus: http.StatusConflict, wantCode: "WEB_WORKSPACE_NAVIGATION_UNAVAILABLE"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			agent.setNavigationFail(testCase.failure)
			recorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, path, token, `{"action":"reload"}`)
			assert.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			assert.Equal(t, testCase.wantCode, decodeWebWorkspaceError(t, recorder).Code)
		})
	}
}

func TestWebWorkspaceRouterNavigationFailsClosedWithoutAgent(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = ""

	recorder := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+sessionId+"/navigation",
		token,
		`{"action":"reload"}`,
	)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Equal(t, "WEB_WORKSPACE_AGENT_UNAVAILABLE", decodeWebWorkspaceError(t, recorder).Code)
}

func TestWebWorkspaceRouterRestartModeTransition(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	sessionId := startWebWorkspaceRouterSession(t, fixture, token)
	path := "/api/web-workspace/session/" + sessionId + "/restart"

	// The operator opened a sign-in window: the runtime mode is LOGIN and the
	// client must be able to see it.
	agent.mutex.Lock()
	for id, runtime := range agent.runtimes {
		runtime.Mode = "LOGIN"
		agent.runtimes[id] = runtime
	}
	agent.mutex.Unlock()

	locked := doWebWorkspaceRequest(fixture.engine, http.MethodPost, path, token, `{"mode":"LOCKED"}`)
	require.Equal(t, http.StatusOK, locked.Code, locked.Body.String())
	assert.Contains(t, locked.Body.String(), `"mode":"LOCKED"`)

	agent.mutex.Lock()
	modes := append([]string{}, agent.restartModes...)
	agent.mutex.Unlock()
	require.Len(t, modes, 1)
	assert.Equal(t, "LOCKED", modes[0])

	// A restart without a body stays valid and keeps the current mode.
	reused := doWebWorkspaceRequest(fixture.engine, http.MethodPost, path, token, "")
	require.Equal(t, http.StatusOK, reused.Code, reused.Body.String())

	// Entering LOGIN stays an operator action: the client API rejects it, and
	// the agent never receives the request.
	opened := doWebWorkspaceRequest(fixture.engine, http.MethodPost, path, token, `{"mode":"LOGIN"}`)
	assert.Equal(t, http.StatusBadRequest, opened.Code)
	assert.Equal(t, "WEB_WORKSPACE_INVALID_REQUEST", decodeWebWorkspaceError(t, opened).Code)
	agent.mutex.Lock()
	modes = append([]string{}, agent.restartModes...)
	agent.mutex.Unlock()
	assert.Equal(t, []string{"LOCKED", ""}, modes)
}
func TestWebWorkspaceRouterScreenSizeProposal(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session",
		token,
		`{"screen_width":1540,"screen_height":720}`,
	)
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())

	agent.mutex.Lock()
	created := append([][2]int{}, agent.createdSizes...)
	agent.mutex.Unlock()
	assert.Equal(t, [][2]int{{1540, 720}}, created)

	var payload struct {
		Data struct {
			SessionId string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &payload))
	require.NotEmpty(t, payload.Data.SessionId)

	restart := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPost,
		"/api/web-workspace/session/"+payload.Data.SessionId+"/restart",
		token,
		`{"screen_width":2180,"screen_height":900}`,
	)
	require.Equal(t, http.StatusOK, restart.Code, restart.Body.String())

	agent.mutex.Lock()
	restarted := append([][2]int{}, agent.restartSizes...)
	agent.mutex.Unlock()
	assert.Equal(t, [][2]int{{2180, 900}}, restarted)

	for _, body := range []string{
		`{"screen_width":5000,"screen_height":720}`,
		`{"screen_width":1540,"screen_height":100}`,
		`{"screen_width":1540}`,
	} {
		rejected := doWebWorkspaceRequest(
			fixture.engine,
			http.MethodPost,
			"/api/web-workspace/session",
			token,
			body,
		)
		assert.Equal(t, http.StatusBadRequest, rejected.Code, body)
		assert.Equal(t, "WEB_WORKSPACE_INVALID_REQUEST", decodeWebWorkspaceError(t, rejected).Code)
	}
}
func TestWebWorkspaceRouterActivityKeepAlive(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	tokenB := webWorkspaceBearer(t, fixture.userB)
	sessionId := startWebWorkspaceRouterSession(t, fixture, tokenA)
	path := "/api/web-workspace/session/" + sessionId + "/activity"

	kept := doWebWorkspaceRequest(fixture.engine, http.MethodPost, path, tokenA, "")
	require.Equal(t, http.StatusOK, kept.Code, kept.Body.String())
	assert.Contains(t, kept.Body.String(), `"stream_bytes_out":4096`)
	assert.Contains(t, kept.Body.String(), `"stream_bytes_in":128`)

	foreign := doWebWorkspaceRequest(fixture.engine, http.MethodPost, path, tokenB, "")
	assert.Equal(t, http.StatusNotFound, foreign.Code)
	assert.Equal(t, "WEB_WORKSPACE_SESSION_NOT_FOUND", decodeWebWorkspaceError(t, foreign).Code)

	agent.mutex.Lock()
	calls := agent.activityCalls
	agent.mutex.Unlock()
	assert.Equal(t, 1, calls, "a foreign session must not reach the agent")
}
func TestWebWorkspaceRouterSessionPollRefreshesFromAgent(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)
	_ = startWebWorkspaceRouterSession(t, fixture, token)

	// The runtime moved on: the poll must report the agent's real state, not the
	// state captured when the session was created.
	agent.mutex.Lock()
	for id, runtime := range agent.runtimes {
		runtime.Navigation = &webworkspace.AgentNavigation{CanGoBack: true, CanGoForward: false, UpdatedAt: 42}
		runtime.StreamBytesOut = 123456
		runtime.StreamBytesIn = 789
		agent.runtimes[id] = runtime
	}
	agent.mutex.Unlock()

	current := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	assert.Contains(t, current.Body.String(), `"can_go_back":true`)
	assert.Contains(t, current.Body.String(), `"stream_bytes_out":123456`)
	assert.Contains(t, current.Body.String(), `"stream_bytes_in":789`)
}
func TestWebWorkspaceRouterStreamGateway(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var startPayload struct {
		Data struct {
			SessionId string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(start.Body.Bytes(), &startPayload))
	sessionId := startPayload.Data.SessionId
	require.NotEmpty(t, sessionId)

	ticketRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session/"+sessionId+"/stream-ticket", token, "")
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
	wsBase := "ws" + strings.TrimPrefix(server.URL, "http")
	streamPath := ticketPayload.Data.StreamUrl + "?ticket=" + ticketPayload.Data.Ticket

	conn, response, err := websocket.DefaultDialer.Dial(wsBase+streamPath, nil)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err, "stream dial failed")
	defer conn.Close()

	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("rfb-probe")))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, messageType)
	assert.Equal(t, "rfb-probe", string(payload))

	// The ticket is single use: replaying it must be rejected.
	replay, replayResponse, err := websocket.DefaultDialer.Dial(wsBase+streamPath, nil)
	if replay != nil {
		_ = replay.Close()
	}
	if replayResponse != nil && replayResponse.Body != nil {
		defer replayResponse.Body.Close()
	}
	require.Error(t, err)
	require.NotNil(t, replayResponse)
	assert.Equal(t, http.StatusForbidden, replayResponse.StatusCode)

	// No ticket at all is rejected as well.
	missing, missingResponse, err := websocket.DefaultDialer.Dial(wsBase+ticketPayload.Data.StreamUrl, nil)
	if missing != nil {
		_ = missing.Close()
	}
	if missingResponse != nil && missingResponse.Body != nil {
		defer missingResponse.Body.Close()
	}
	require.Error(t, err)
	require.NotNil(t, missingResponse)
	assert.Equal(t, http.StatusForbidden, missingResponse.StatusCode)
}

func TestWebWorkspaceRouterProjectPermitFlow(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	// No live session: the permit is refused before the agent is contacted.
	noSession := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/projects", token, `{"name":"Quarterly-Review"}`)
	require.Equal(t, http.StatusConflict, noSession.Code, noSession.Body.String())
	assert.Equal(t, "WEB_WORKSPACE_SESSION_REQUIRED", decodeWebWorkspaceError(t, noSession).Code)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var workspace model.WebWorkspace
	require.NoError(t, model.DB.Where("user_id = ?", fixture.userA.Id).First(&workspace).Error)

	permitRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/projects", token, `{"name":"Quarterly-Review"}`)
	require.Equal(t, http.StatusOK, permitRecorder.Code, permitRecorder.Body.String())
	var payload struct {
		Data struct {
			PermitId  string `json:"permit_id"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(permitRecorder.Body.Bytes(), &payload))
	assert.NotEmpty(t, payload.Data.PermitId)
	assert.Positive(t, payload.Data.ExpiresAt)

	calls := agent.permitCallsFor(workspace.Id)
	require.Len(t, calls, 1)
	assert.Equal(t, payload.Data.PermitId, calls[0].PermitId)
	assert.Equal(t, "project_create", calls[0].Kind)
	assert.Equal(t, 300, calls[0].TtlSeconds)
	assert.Equal(t, "Quarterly-Review", calls[0].DisplayName)

	// Issuing a permit must not create a local project row.
	var projectCount int64
	require.NoError(t, model.DB.Model(&model.WebProject{}).Where("workspace_id = ?", workspace.Id).Count(&projectCount).Error)
	assert.EqualValues(t, 0, projectCount)
}

func TestWebWorkspaceRouterProjectPermitEnforcesLimit(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var workspace model.WebWorkspace
	require.NoError(t, model.DB.Where("user_id = ?", fixture.userA.Id).First(&workspace).Error)
	require.NoError(t, model.DB.Create(&model.WebProject{
		WorkspaceId:       workspace.Id,
		Provider:          "chatgpt",
		ExternalProjectId: "g-p-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Name:              "Existing",
	}).Error)

	system_setting.GetWebWorkspaceSettings().MaxProjects = 1
	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/projects", token, `{"name":"Quarterly-Review"}`)
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Equal(t, "WEB_WORKSPACE_PROJECT_LIMIT", decodeWebWorkspaceError(t, recorder).Code)
	assert.Empty(t, agent.permitCallsFor(workspace.Id))
}

func TestWebWorkspaceRouterRejectsDeletingTheLastProject(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	var workspace model.WebWorkspace
	require.NoError(t, model.DB.Where("user_id = ?", fixture.userA.Id).First(&workspace).Error)
	project := &model.WebProject{
		WorkspaceId:       workspace.Id,
		Provider:          "chatgpt",
		ExternalProjectId: "g-p-cccccccccccccccccccccccccccccccc",
		Name:              "Keep-Me",
	}
	require.NoError(t, model.DB.Create(project).Error)

	request := fmt.Sprintf("/api/web-workspace/projects/%d", project.Id)
	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodDelete, request, token, "")
	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Equal(t, "WEB_WORKSPACE_LAST_PROJECT_REQUIRED", decodeWebWorkspaceError(t, recorder).Code)

	var count int64
	require.NoError(t, model.DB.Model(&model.WebProject{}).Where("id = ?", project.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestWebWorkspaceRouterProjectPermitFailsClosedWhenAgentRejects(t *testing.T) {
	agent := newRouterFakeAgent(t)
	fixture := setupWebWorkspaceSessionRouterTest(t, agent.server.URL)
	token := webWorkspaceBearer(t, fixture.userA)

	start := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/session", token, "")
	require.Equal(t, http.StatusOK, start.Code, start.Body.String())
	agent.setFailPermits(true)

	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodPost, "/api/web-workspace/projects", token, `{"name":"Quarterly-Review"}`)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	assert.Equal(t, "WEB_WORKSPACE_AGENT_UNAVAILABLE", decodeWebWorkspaceError(t, recorder).Code)
}
