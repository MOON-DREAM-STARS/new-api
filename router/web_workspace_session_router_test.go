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
// the New API session broker and the WSS gateway.
type routerFakeAgent struct {
	server   *httptest.Server
	mutex    sync.Mutex
	runtimes map[int]webworkspace.AgentRuntime
	create   int
	stop     int
	echo     bool
}

func newRouterFakeAgent(t *testing.T) *routerFakeAgent {
	t.Helper()
	agent := &routerFakeAgent{runtimes: make(map[int]webworkspace.AgentRuntime), echo: true}
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
		path := strings.TrimPrefix(r.URL.Path, "/internal/v1/runtimes")
		if path == "" && r.Method == http.MethodPost {
			var request struct {
				WorkspaceId int `json:"workspace_id"`
			}
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.create++
			runtime, ok := agent.runtimes[request.WorkspaceId]
			if !ok {
				now := time.Now().Unix()
				runtime = webworkspace.AgentRuntime{
					RuntimeId:      fmt.Sprintf("ws-%d", request.WorkspaceId),
					WorkspaceId:    request.WorkspaceId,
					State:          webworkspace.AgentStateRunning,
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
		if len(parts) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		workspaceId := 0
		_, _ = fmt.Sscanf(parts[0], "%d", &workspaceId)
		agent.mutex.Lock()
		runtime, ok := agent.runtimes[workspaceId]
		if ok && len(parts) > 1 && parts[1] == "stop" {
			agent.stop++
			runtime.State = webworkspace.AgentStateStopped
			agent.runtimes[workspaceId] = runtime
		}
		agent.mutex.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeRouterAgentJSON(t, w, runtime)
	}))
	t.Cleanup(agent.server.Close)
	return agent
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
