package webworkspace

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testAgentToken = "web-workspace-test-agent-token"

type fakeAgentServer struct {
	server       *httptest.Server
	mutex        sync.Mutex
	runtimes     map[int]AgentRuntime
	createCalls  int
	stopCalls    int
	restartModes []string
	createdSizes [][2]int
	restartSizes [][2]int
	failCreate   bool
}

func newFakeAgentServer(t *testing.T) *fakeAgentServer {
	t.Helper()
	agent := &fakeAgentServer{runtimes: make(map[int]AgentRuntime)}
	agent.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testAgentToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		path := strings.TrimPrefix(r.URL.Path, "/internal/v1/runtimes")
		switch {
		case path == "" && r.Method == http.MethodPost:
			var request agentCreateRuntimeRequest
			if err := common.Unmarshal(body, &request); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			agent.mutex.Lock()
			agent.createCalls++
			agent.createdSizes = append(agent.createdSizes, [2]int{request.Width, request.Height})
			failCreate := agent.failCreate
			runtime, ok := agent.runtimes[request.WorkspaceId]
			if !ok && !failCreate {
				now := time.Now().Unix()
				runtime = AgentRuntime{
					RuntimeId:      "ws-" + strconv.Itoa(request.WorkspaceId),
					WorkspaceId:    request.WorkspaceId,
					State:          AgentStateRunning,
					Mode:           "LOCKED",
					CreatedAt:      now,
					LastActivityAt: now,
					IdleDeadlineAt: now + 600,
				}
				agent.runtimes[request.WorkspaceId] = runtime
			}
			agent.mutex.Unlock()
			if failCreate {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeFakeAgentJSON(t, w, runtime)
		case path != "":
			parts := strings.Split(strings.Trim(path, "/"), "/")
			workspaceId, err := strconv.Atoi(parts[0])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			suffix := ""
			if len(parts) > 1 {
				suffix = parts[1]
			}
			agent.mutex.Lock()
			runtime, ok := agent.runtimes[workspaceId]
			switch {
			case suffix == "stop" && r.Method == http.MethodPost:
				agent.stopCalls++
				if ok {
					runtime.State = AgentStateStopped
					agent.runtimes[workspaceId] = runtime
				}
			case suffix == "restart" && r.Method == http.MethodPost:
				var restartRequest agentRestartRuntimeRequest
				if len(body) > 0 {
					if err := common.Unmarshal(body, &restartRequest); err != nil {
						agent.mutex.Unlock()
						w.WriteHeader(http.StatusBadRequest)
						return
					}
				}
				agent.restartModes = append(agent.restartModes, restartRequest.Mode)
				agent.restartSizes = append(agent.restartSizes, [2]int{restartRequest.Width, restartRequest.Height})
				if ok {
					runtime.State = AgentStateRunning
					if restartRequest.Mode != "" {
						runtime.Mode = restartRequest.Mode
					}
					runtime.LastActivityAt = time.Now().Unix()
					runtime.IdleDeadlineAt = time.Now().Unix() + 600
					agent.runtimes[workspaceId] = runtime
				}
			}
			agent.mutex.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"success":false,"error":"runtime_not_found"}`))
				return
			}
			writeFakeAgentJSON(t, w, runtime)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(agent.server.Close)
	return agent
}

func (a *fakeAgentServer) url() string { return a.server.URL }

func (a *fakeAgentServer) setFailCreate(fail bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.failCreate = fail
}

func (a *fakeAgentServer) setState(workspaceId int, state string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	runtime, ok := a.runtimes[workspaceId]
	if !ok {
		return
	}
	runtime.State = state
	a.runtimes[workspaceId] = runtime
}

func (a *fakeAgentServer) removeRuntime(workspaceId int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	delete(a.runtimes, workspaceId)
}

func (a *fakeAgentServer) counts() (int, int) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.createCalls, a.stopCalls
}

func writeFakeAgentJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	raw, err := common.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func setupWebWorkspaceSessionTest(t *testing.T) *gorm.DB {
	t.Helper()
	settings := system_setting.GetWebWorkspaceSettings()
	previousSettings := *settings
	t.Cleanup(func() { *settings = previousSettings })
	settings.Enabled = true
	settings.MinimumRole = common.RoleCommonUser
	settings.AllowedGroups = []string{}
	settings.AgentBaseURL = ""

	sessions = sessionStore{entries: make(map[string]Session), starts: make(map[int]*sync.Mutex)}
	tickets = ticketStore{entries: make(map[string]StreamTicket)}
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", testAgentToken)

	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.WebWorkspace{}, &model.WebProject{}, &model.WebConversation{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	user := &model.User{
		Username: "ws-session-user", AffCode: "aff-ws-session-user", Password: "unused",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
	}
	require.NoError(t, db.Create(user).Error)
	return db
}

func firstSessionUser(t *testing.T, db *gorm.DB) *model.User {
	t.Helper()
	var user model.User
	require.NoError(t, db.Order("id ASC").First(&user).Error)
	return &user
}

func TestWebWorkspaceSessionLifecycle(t *testing.T) {
	db := setupWebWorkspaceSessionTest(t)
	agent := newFakeAgentServer(t)
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = agent.url()
	user := firstSessionUser(t, db)

	ctx := context.Background()
	session, err := StartSession(ctx, user.Id, 0, 0)
	require.NoError(t, err)
	require.NotEmpty(t, session.Id)
	assert.Equal(t, AgentStateRunning, session.State)
	assert.Equal(t, user.Id, session.UserId)

	var workspace model.WebWorkspace
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&workspace).Error)
	assert.Equal(t, session.WorkspaceId, workspace.Id)

	again, err := StartSession(ctx, user.Id, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, session.Id, again.Id)
	createCalls, _ := agent.counts()
	assert.Equal(t, 1, createCalls)

	restarted, err := RestartSession(ctx, user.Id, session.Id, "LOCKED", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, AgentStateRunning, restarted.State)
	assert.Equal(t, "LOCKED", restarted.Mode)
	agent.mutex.Lock()
	modes := append([]string{}, agent.restartModes...)
	agent.mutex.Unlock()
	assert.Equal(t, []string{"LOCKED"}, modes)

	require.NoError(t, TouchSession(user.Id, session.Id))
	require.NoError(t, MarkSessionIdle(user.Id, session.Id))
	idleSession, err := GetSession(user.Id, session.Id)
	require.NoError(t, err)
	assert.Equal(t, AgentStateIdle, idleSession.State)

	_, err = GetSession(user.Id+1, session.Id)
	assert.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, StopSession(ctx, user.Id, session.Id))
	_, err = GetSession(user.Id, session.Id)
	assert.ErrorIs(t, err, ErrSessionNotFound)
	_, stopCalls := agent.counts()
	assert.Equal(t, 1, stopCalls)
}

func TestWebWorkspaceSessionProposesRemoteScreenSize(t *testing.T) {
	db := setupWebWorkspaceSessionTest(t)
	agent := newFakeAgentServer(t)
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = agent.url()
	user := firstSessionUser(t, db)

	ctx := context.Background()
	session, err := StartSession(ctx, user.Id, 1540, 720)
	require.NoError(t, err)

	agent.mutex.Lock()
	created := append([][2]int{}, agent.createdSizes...)
	agent.mutex.Unlock()
	assert.Equal(t, [][2]int{{1540, 720}}, created, "the start must forward the proposed size")

	_, err = RestartSession(ctx, user.Id, session.Id, "", 2180, 900)
	require.NoError(t, err)

	agent.mutex.Lock()
	restarted := append([][2]int{}, agent.restartSizes...)
	agent.mutex.Unlock()
	assert.Equal(t, [][2]int{{2180, 900}}, restarted, "the restart must forward the proposed size")
}
func TestWebWorkspaceSessionReplacesStaleRuntime(t *testing.T) {
	db := setupWebWorkspaceSessionTest(t)
	agent := newFakeAgentServer(t)
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = agent.url()
	user := firstSessionUser(t, db)

	ctx := context.Background()
	session, err := StartSession(ctx, user.Id, 0, 0)
	require.NoError(t, err)

	agent.removeRuntime(session.WorkspaceId)
	replacement, err := StartSession(ctx, user.Id, 0, 0)
	require.NoError(t, err)
	assert.NotEqual(t, session.Id, replacement.Id)
	createCalls, _ := agent.counts()
	assert.Equal(t, 2, createCalls)
}

func TestWebWorkspaceSessionFailsClosed(t *testing.T) {
	db := setupWebWorkspaceSessionTest(t)
	user := firstSessionUser(t, db)
	ctx := context.Background()

	// No agent base url configured.
	_, err := StartSession(ctx, user.Id, 0, 0)
	assert.ErrorIs(t, err, ErrAgentUnavailable)

	agent := newFakeAgentServer(t)
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = agent.url()
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", "")
	_, err = StartSession(ctx, user.Id, 0, 0)
	assert.ErrorIs(t, err, ErrAgentUnavailable)

	// Agent rejects the create request.
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", testAgentToken)
	agent.setFailCreate(true)
	_, err = StartSession(ctx, user.Id, 0, 0)
	assert.ErrorIs(t, err, ErrAgentRejected)

	// Unreachable agent.
	agent.setFailCreate(false)
	stopped := newFakeAgentServer(t)
	url := stopped.url()
	stopped.server.Close()
	system_setting.GetWebWorkspaceSettings().AgentBaseURL = url
	_, err = StartSession(ctx, user.Id, 0, 0)
	assert.ErrorIs(t, err, ErrAgentUnavailable)
}
