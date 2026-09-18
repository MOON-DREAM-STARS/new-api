package router

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/webworkspace"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type webWorkspaceRouterFixture struct {
	engine        *gin.Engine
	settings      *system_setting.WebWorkspaceSettings
	agent         *routerFakeAgent
	userA         *model.User
	userB         *model.User
	workspaceA    *model.WebWorkspace
	workspaceB    *model.WebWorkspace
	projectA      *model.WebProject
	projectB      *model.WebProject
	conversationB *model.WebConversation
}

type webWorkspaceErrorBody struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

func setupWebWorkspaceRouterTest(t *testing.T) *webWorkspaceRouterFixture {
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
		&model.Option{},
		&model.WebWorkspace{},
		&model.WebProject{},
		&model.WebConversation{},
	))
	model.DB = db
	common.RedisEnabled = false
	common.SessionSecret = "web-workspace-router-test-secret"

	settings.Enabled = true
	settings.MinimumRole = common.RoleCommonUser
	settings.AllowedGroups = []string{}
	agent := newRouterFakeAgent(t)
	settings.AgentBaseURL = agent.server.URL
	t.Setenv("WEB_WORKSPACE_AGENT_TOKEN", routerTestAgentToken)

	userA := createWebWorkspaceRouterUser(t, db, "ws-router-a")
	userB := createWebWorkspaceRouterUser(t, db, "ws-router-b")

	workspaceA := &model.WebWorkspace{UserId: userA.Id, Provider: "chatgpt", Status: model.WebWorkspaceStatusActive}
	workspaceB := &model.WebWorkspace{UserId: userB.Id, Provider: "chatgpt", Status: model.WebWorkspaceStatusActive}
	require.NoError(t, db.Create(workspaceA).Error)
	require.NoError(t, db.Create(workspaceB).Error)
	agent.addRuntime(workspaceA.Id)
	agent.addRuntime(workspaceB.Id)

	projectA := &model.WebProject{WorkspaceId: workspaceA.Id, Provider: "chatgpt", ExternalProjectId: "ext-router-a", Name: "Project A"}
	projectB := &model.WebProject{WorkspaceId: workspaceB.Id, Provider: "chatgpt", ExternalProjectId: "ext-router-b", Name: "Project B"}
	require.NoError(t, db.Create(projectA).Error)
	require.NoError(t, db.Create(projectB).Error)

	conversationB := &model.WebConversation{ProjectId: projectB.Id, Provider: "chatgpt", ExternalConversationId: "conv-router-b", Title: "Conversation B"}
	require.NoError(t, db.Create(conversationB).Error)

	engine := gin.New()
	api := engine.Group("/api")
	SetWebWorkspaceRouter(api)

	return &webWorkspaceRouterFixture{
		engine:        engine,
		settings:      settings,
		agent:         agent,
		userA:         userA,
		userB:         userB,
		workspaceA:    workspaceA,
		workspaceB:    workspaceB,
		projectA:      projectA,
		projectB:      projectB,
		conversationB: conversationB,
	}
}

func createWebWorkspaceRouterUser(t *testing.T, db *gorm.DB, username string) *model.User {
	t.Helper()
	user := &model.User{
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

func webWorkspaceBearer(t *testing.T, user *model.User) string {
	t.Helper()
	bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "web-workspace-router-test")
	require.NoError(t, err)
	return "Bearer " + bundle.AccessToken
}

func doWebWorkspaceRequest(engine *gin.Engine, method string, path string, token string, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, reader)
	if token != "" {
		request.Header.Set("Authorization", token)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeWebWorkspaceError(t *testing.T, recorder *httptest.ResponseRecorder) webWorkspaceErrorBody {
	t.Helper()
	var payload webWorkspaceErrorBody
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload
}

func TestWebWorkspaceRouterRequiresAuthentication(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)

	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", "", "")
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestWebWorkspaceRouterRequiresEntitlement(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	fixture.settings.Enabled = false

	token := webWorkspaceBearer(t, fixture.userA)
	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/status", token, "")
	assert.Equal(t, http.StatusForbidden, recorder.Code)

	payload := decodeWebWorkspaceError(t, recorder)
	assert.False(t, payload.Success)
	assert.Equal(t, "WEB_WORKSPACE_ENTITLEMENT_DENIED", payload.Code)
	assert.Equal(t, "global_disabled", payload.Reason)

	// The capability probe stays callable while the feature is disabled.
	configRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/config", token, "")
	assert.Equal(t, http.StatusOK, configRecorder.Code)
	assert.Contains(t, configRecorder.Body.String(), `"entitled":false`)
}

func TestWebWorkspaceRouterCrossUserAccessDenied(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)

	foreignProject := fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectB.Id)
	unknownProject := "/api/web-workspace/projects/987654321"

	foreignGet := doWebWorkspaceRequest(fixture.engine, http.MethodGet, foreignProject, tokenA, "")
	unknownGet := doWebWorkspaceRequest(fixture.engine, http.MethodGet, unknownProject, tokenA, "")
	assert.Equal(t, http.StatusNotFound, foreignGet.Code)
	assert.Equal(t, http.StatusNotFound, unknownGet.Code)
	// A foreign resource and an unknown one must be indistinguishable.
	assert.Equal(t, unknownGet.Body.String(), foreignGet.Body.String())

	foreignPatch := doWebWorkspaceRequest(fixture.engine, http.MethodPatch, foreignProject, tokenA, `{"name":"stolen"}`)
	unknownPatch := doWebWorkspaceRequest(fixture.engine, http.MethodPatch, unknownProject, tokenA, `{"name":"stolen"}`)
	assert.Equal(t, http.StatusNotFound, foreignPatch.Code)
	assert.Equal(t, unknownPatch.Body.String(), foreignPatch.Body.String())

	foreignDelete := doWebWorkspaceRequest(fixture.engine, http.MethodDelete, foreignProject, tokenA, "")
	assert.Equal(t, http.StatusNotFound, foreignDelete.Code)

	foreignConversations := doWebWorkspaceRequest(fixture.engine, http.MethodGet, foreignProject+"/conversations", tokenA, "")
	assert.Equal(t, http.StatusNotFound, foreignConversations.Code)

	var stored model.WebProject
	require.NoError(t, model.DB.First(&stored, fixture.projectB.Id).Error)
	assert.Equal(t, "Project B", stored.Name)

	list := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", tokenA, "")
	assert.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `"Project A"`)
	assert.NotContains(t, list.Body.String(), `"Project B"`)
	assert.NotContains(t, list.Body.String(), "ext-router-a")
	assert.NotContains(t, list.Body.String(), "ext-router-b")
	assert.NotContains(t, list.Body.String(), "workspace_id")

	status := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/status", tokenA, "")
	assert.Equal(t, http.StatusOK, status.Code)
	assert.Contains(t, status.Body.String(), `"entitled":true`)
	assert.NotContains(t, status.Body.String(), "ext-router")
}

func TestWebWorkspaceRouterIgnoresForgedIdentifiers(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)

	list := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodGet,
		fmt.Sprintf("/api/web-workspace/projects?workspace_id=%d", fixture.workspaceB.Id),
		tokenA,
		"",
	)
	assert.Equal(t, http.StatusOK, list.Code)
	assert.Contains(t, list.Body.String(), `"Project A"`)
	assert.NotContains(t, list.Body.String(), `"Project B"`)

	rename := doWebWorkspaceRequest(
		fixture.engine,
		http.MethodPatch,
		fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectA.Id),
		tokenA,
		fmt.Sprintf(`{"name":"Project A renamed","workspace_id":%d}`, fixture.workspaceB.Id),
	)
	assert.Equal(t, http.StatusOK, rename.Code)

	var foreign model.WebProject
	require.NoError(t, model.DB.First(&foreign, fixture.projectB.Id).Error)
	assert.Equal(t, "Project B", foreign.Name)

	var own model.WebProject
	require.NoError(t, model.DB.First(&own, fixture.projectA.Id).Error)
	assert.Equal(t, "Project A renamed", own.Name)
}

func TestWebWorkspaceRouterOwnProjectLifecycle(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	ownProject := fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectA.Id)

	get := doWebWorkspaceRequest(fixture.engine, http.MethodGet, ownProject, tokenA, "")
	assert.Equal(t, http.StatusOK, get.Code)
	assert.Contains(t, get.Body.String(), `"Project A"`)

	conversations := doWebWorkspaceRequest(fixture.engine, http.MethodGet, ownProject+"/conversations", tokenA, "")
	assert.Equal(t, http.StatusOK, conversations.Code)
	assert.Contains(t, conversations.Body.String(), `"items":[]`)

	rename := doWebWorkspaceRequest(fixture.engine, http.MethodPatch, ownProject, tokenA, `{"name":"  Project A renamed  "}`)
	assert.Equal(t, http.StatusOK, rename.Code)
	assert.Contains(t, rename.Body.String(), `"name":"Project A renamed"`)

	invalidRename := doWebWorkspaceRequest(fixture.engine, http.MethodPatch, ownProject, tokenA, `{"name":"   "}`)
	assert.Equal(t, http.StatusBadRequest, invalidRename.Code)
	assert.Equal(t, "WEB_WORKSPACE_INVALID_REQUEST", decodeWebWorkspaceError(t, invalidRename).Code)

	deleteRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodDelete, ownProject, tokenA, "")
	assert.Equal(t, http.StatusOK, deleteRecorder.Code)

	afterDelete := doWebWorkspaceRequest(fixture.engine, http.MethodGet, ownProject, tokenA, "")
	assert.Equal(t, http.StatusNotFound, afterDelete.Code)
}
func TestWebWorkspaceRouterFollowsRegisteredOptionSetting(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)

	// Admins enable the feature through the option API, which drives the same
	// registered configuration the entitlement check reads.
	previousOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() { common.OptionMap = previousOptionMap })
	require.NoError(t, model.UpdateOption("web_workspace.enabled", "false"))

	token := webWorkspaceBearer(t, fixture.userA)
	recorder := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", token, "")
	assert.Equal(t, http.StatusForbidden, recorder.Code)

	payload := decodeWebWorkspaceError(t, recorder)
	assert.Equal(t, "WEB_WORKSPACE_ENTITLEMENT_DENIED", payload.Code)
	assert.Equal(t, "global_disabled", payload.Reason)
}

func TestWebWorkspaceRouterProjectsSyncObservations(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	const syncedProjectId = "g-p-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixture.agent.setObservations(fixture.workspaceA.Id, webworkspace.Observation{
		Event:             webworkspace.ObservationProjectCreated,
		PermitId:          "permit-router-unmatched",
		ExternalProjectId: syncedProjectId,
		Slug:              "Synced Project",
		ObservedAt:        100,
	})

	list := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", tokenA, "")
	require.Equal(t, http.StatusOK, list.Code, list.Body.String())
	assert.Contains(t, list.Body.String(), `"Synced Project"`)

	// The applied batch is acknowledged and ownership is republished with both
	// the pre-existing and the newly observed project.
	assert.Equal(t, 1, fixture.agent.ackedOffset(fixture.workspaceA.Id))
	snapshot, ok := fixture.agent.ownershipOf(fixture.workspaceA.Id)
	require.True(t, ok)
	assert.Contains(t, snapshot.Projects, "ext-router-a")
	assert.Contains(t, snapshot.Projects, syncedProjectId)

	var stored model.WebProject
	require.NoError(t, model.DB.Where("external_project_id = ?", syncedProjectId).First(&stored).Error)
	assert.Equal(t, fixture.workspaceA.Id, stored.WorkspaceId)
	assert.Equal(t, stored.UpdatedAt, snapshot.Generation)

	// Replaying the acknowledged batch must not duplicate the row.
	again := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", tokenA, "")
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	var projectCount int64
	require.NoError(t, model.DB.Model(&model.WebProject{}).Where("external_project_id = ?", syncedProjectId).Count(&projectCount).Error)
	assert.EqualValues(t, 1, projectCount)
}

func TestWebWorkspaceRouterProjectsFailClosedWithoutAgent(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	fixture.settings.AgentBaseURL = ""

	list := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", tokenA, "")
	assert.Equal(t, http.StatusServiceUnavailable, list.Code)
	assert.Equal(t, "WEB_WORKSPACE_AGENT_UNAVAILABLE", decodeWebWorkspaceError(t, list).Code)
	assert.NotContains(t, list.Body.String(), "Project A")
}

func TestWebWorkspaceRouterProjectsRequireRuntime(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	fixture.agent.removeRuntime(fixture.workspaceA.Id)

	list := doWebWorkspaceRequest(fixture.engine, http.MethodGet, "/api/web-workspace/projects", tokenA, "")
	assert.Equal(t, http.StatusNotFound, list.Code)
	assert.Equal(t, "WEB_WORKSPACE_SESSION_NOT_FOUND", decodeWebWorkspaceError(t, list).Code)
}

func TestWebWorkspaceRouterProjectMutationsPushOwnership(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	tokenA := webWorkspaceBearer(t, fixture.userA)
	ownProject := fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectA.Id)

	rename := doWebWorkspaceRequest(fixture.engine, http.MethodPatch, ownProject, tokenA, `{"name":"Renamed A"}`)
	require.Equal(t, http.StatusOK, rename.Code, rename.Body.String())
	snapshot, ok := fixture.agent.ownershipOf(fixture.workspaceA.Id)
	require.True(t, ok)
	assert.Equal(t, []string{"ext-router-a"}, snapshot.Projects)
	var renamed model.WebProject
	require.NoError(t, model.DB.First(&renamed, fixture.projectA.Id).Error)
	assert.Equal(t, renamed.UpdatedAt, snapshot.Generation)

	deleteRecorder := doWebWorkspaceRequest(fixture.engine, http.MethodDelete, ownProject, tokenA, "")
	require.Equal(t, http.StatusOK, deleteRecorder.Code, deleteRecorder.Body.String())
	snapshot, ok = fixture.agent.ownershipOf(fixture.workspaceA.Id)
	require.True(t, ok)
	assert.Empty(t, snapshot.Projects)
	assert.Zero(t, snapshot.Generation)

	// A rejected ownership push is never reported as a successful mutation.
	fixture.settings.AgentBaseURL = ""
	foreignOwn := fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectB.Id)
	failing := doWebWorkspaceRequest(fixture.engine, http.MethodPatch, foreignOwn, webWorkspaceBearer(t, fixture.userB), `{"name":"Renamed B"}`)
	assert.Equal(t, http.StatusServiceUnavailable, failing.Code)
	assert.Equal(t, "WEB_WORKSPACE_AGENT_UNAVAILABLE", decodeWebWorkspaceError(t, failing).Code)
}
