package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/model"
)

// TestWebWorkspaceRouterFailsClosedWhenDatabaseUnavailable pins the phase 6
// fail-closed requirement for the control plane: once the database that
// entitlement and ownership decisions are read from is gone, every
// authenticated Web Workspace route must answer with an error instead of
// serving resources, entitlements or an empty project list. (checklist §31:
// DB unavailable)
func TestWebWorkspaceRouterFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	fixture := setupWebWorkspaceRouterTest(t)
	token := webWorkspaceBearer(t, fixture.userA)

	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/web-workspace/config", ""},
		{http.MethodGet, "/api/web-workspace/status", ""},
		{http.MethodGet, "/api/web-workspace/projects", ""},
		{http.MethodPost, "/api/web-workspace/projects", `{}`},
		{http.MethodGet, fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectA.Id), ""},
		{http.MethodPatch, fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectA.Id), `{"name":"renamed-project"}`},
		{http.MethodDelete, fmt.Sprintf("/api/web-workspace/projects/%d", fixture.projectA.Id), ""},
		{http.MethodGet, fmt.Sprintf("/api/web-workspace/projects/%d/conversations", fixture.projectA.Id), ""},
		{http.MethodGet, "/api/web-workspace/session", ""},
		{http.MethodPost, "/api/web-workspace/session", ""},
	}
	for _, request := range requests {
		recorder := doWebWorkspaceRequest(fixture.engine, request.method, request.path, token, request.body)
		assert.Equal(t, http.StatusInternalServerError, recorder.Code, "%s %s", request.method, request.path)
		assert.NotContains(t, recorder.Body.String(), "Project A", "%s %s must not serve resource data", request.method, request.path)
		assert.NotContains(t, recorder.Body.String(), `"success":true`, "%s %s must not report success", request.method, request.path)
	}
}
