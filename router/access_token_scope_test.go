package router

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// accessTokenExemptRoutes are /api routes that never reach dashboard
// authentication, so a personal access token has no rule to satisfy there.
var accessTokenExemptRoutes = []string{
	// Anonymous pages and setup.
	"GET /api/setup",
	"POST /api/setup",
	"GET /api/status",
	"GET /api/uptime/status",
	"GET /api/notice",
	"GET /api/user-agreement",
	"GET /api/privacy-policy",
	"GET /api/about",
	"GET /api/home_page_content",
	"GET /api/ratio_config",
	"GET /api/user/groups",

	// Anonymous account entry points.
	"GET /api/verification",
	"GET /api/reset_password",
	"POST /api/user/reset",
	"POST /api/user/register",
	"GET /api/user/login/encryption-key",
	"POST /api/user/login",
	"POST /api/user/login/2fa",
	"POST /api/user/login/verify",
	"POST /api/user/login/passkey/begin",
	"POST /api/user/login/passkey/finish",
	"POST /api/user/passkey/login/begin",
	"POST /api/user/passkey/login/finish",
	"GET /api/oauth/wechat",
	"GET /api/oauth/telegram/login",
	"GET /api/oauth/telegram/bind/:flow_token",

	// Browser session cookie endpoints.
	"POST /api/user/auth/refresh",
	"POST /api/user/auth/logout",

	// Payment callbacks.
	"POST /api/stripe/webhook",
	"POST /api/creem/webhook",
	"POST /api/waffo/webhook",
	"POST /api/waffo-pancake/webhook/:env",
	"GET /api/user/epay/notify",
	"POST /api/user/epay/notify",
	"GET /api/subscription/epay/notify",
	"POST /api/subscription/epay/notify",
	"GET /api/subscription/epay/return",
	"POST /api/subscription/epay/return",

	// API key authentication (TokenAuthReadOnly).
	"GET /api/usage/token/",
	"GET /api/log/token",
}

func TestAuditRoutesEnforceProductionAuthorization(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	previousSecret, previousRateLimit := common.SessionSecret, common.GlobalApiRateLimitEnable
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.IsMasterNode = false, true
	common.SessionSecret, common.GlobalApiRateLimitEnable = "audit-route-test-secret", false
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.IsMasterNode = previousRedis, previousMaster
		common.SessionSecret, common.GlobalApiRateLimitEnable = previousSecret, previousRateLimit
	})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	requestAudit := func(path, credential string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if credential != "" {
			request.Header.Set("Authorization", "Bearer "+credential)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}

	for _, path := range []string{"/api/audit/self", "/api/audit"} {
		assert.Equal(t, http.StatusUnauthorized, requestAudit(path, "").Code)
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(fmt.Sprintf("role_%d", role), func(t *testing.T) {
			username := fmt.Sprintf("audit-route-%d", role)
			user := &model.User{Username: username, Role: role, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: username}
			require.NoError(t, db.Create(user).Error)
			session, err := service.CreateLoginSession(user.Id, "password", "192.0.2.1", "audit-route-test")
			require.NoError(t, err)
			pat := model.AccessTokenPrefix + username
			grant := &model.UserAccessToken{UserId: user.Id, Name: "audit route", TokenHash: model.AccessTokenFingerprint(pat)}
			require.NoError(t, grant.SetScopes([]string{"usage:read", "audit:read"}))
			require.NoError(t, db.Create(grant).Error)
			require.NoError(t, db.Create(&model.AuditLog{EventId: username, UserId: user.Id, Username: username, ActorRole: role, Category: model.AuditCategorySecurity}).Error)
			credentials := []struct{ name, value string }{{"session", session.AccessToken}, {"pat", pat}}
			selfPath := "/api/audit/self?category=security&username=another-user&user_id=999"

			for _, credential := range credentials {
				t.Run(credential.name, func(t *testing.T) {
					self := requestAudit(selfPath, credential.value)
					all := requestAudit("/api/audit?category=security", credential.value)
					if role == common.RoleCommonUser {
						assert.Equal(t, http.StatusForbidden, self.Code)
						assert.Contains(t, self.Body.String(), "AUTH_INSUFFICIENT_PRIVILEGE")
						assert.Equal(t, http.StatusForbidden, all.Code)
						return
					}
					require.Equal(t, http.StatusOK, self.Code)
					var result struct {
						Success bool
						Data    struct {
							Total int
							Items []model.AuditLog
						}
					}
					require.NoError(t, common.Unmarshal(self.Body.Bytes(), &result))
					assert.True(t, result.Success)
					assert.Equal(t, 1, result.Data.Total)
					require.Len(t, result.Data.Items, 1)
					assert.Equal(t, user.Id, result.Data.Items[0].UserId)
					if role == common.RoleAdminUser {
						assert.Equal(t, http.StatusForbidden, all.Code, "admin must also hold AuditRead")
					} else {
						assert.Equal(t, http.StatusOK, all.Code)
					}
				})
			}
			if role == common.RoleCommonUser {
				return
			}
			require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceAudit: {authz.ActionRead: true}}))
			for _, credential := range credentials {
				assert.Equal(t, http.StatusOK, requestAudit("/api/audit?category=security", credential.value).Code)
			}
			// A token's required route scope still applies to admin and root.
			require.NoError(t, grant.SetScopes([]string{"audit:read"}))
			require.NoError(t, db.Save(grant).Error)
			missingUsage := requestAudit(selfPath, pat)
			assert.Equal(t, http.StatusForbidden, missingUsage.Code)
			assert.Contains(t, missingUsage.Body.String(), "ACCESS_TOKEN_SCOPE_DENIED")
			require.NoError(t, grant.SetScopes([]string{"usage:read"}))
			require.NoError(t, db.Save(grant).Error)
			missingAudit := requestAudit("/api/audit", pat)
			assert.Equal(t, http.StatusForbidden, missingAudit.Code)
			assert.Contains(t, missingAudit.Body.String(), "ACCESS_TOKEN_SCOPE_DENIED")
			assert.Equal(t, http.StatusOK, requestAudit(selfPath, pat).Code)

			// Existing credentials must not retain access after a role downgrade.
			require.NoError(t, db.Model(user).Update("role", common.RoleCommonUser).Error)
			for _, credential := range credentials {
				for _, path := range []string{selfPath, "/api/audit"} {
					assert.Equal(t, http.StatusForbidden, requestAudit(path, credential.value).Code)
				}
			}
		})
	}
}

// accessTokenHelperRoutes are declared through handlePermissionRoute outside
// channelPermissionRoutes.
var accessTokenHelperRoutes = map[string]authz.Permission{
	"GET /api/task_plugin_options": authz.TaskPluginBind,
	"GET /api/audit":               authz.AuditRead,
}

func newAccessTokenScopeTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// A permission route that is also in the static table panics here.
	require.NotPanics(t, func() {
		SetApiRouter(engine)
		SetDashboardRouter(engine)
		SetRelayRouter(engine)
		SetTaskPluginProtocolRouter(engine)
		SetVideoRouter(engine)
		SetTaskRouter(engine)
	})
	return engine
}

func TestAccessTokenRouteRulesCoverEveryDashboardRoute(t *testing.T) {
	engine := newAccessTokenScopeTestEngine(t)
	exempt := map[string]bool{}
	for _, key := range accessTokenExemptRoutes {
		exempt[key] = true
	}
	registered := map[string]bool{}
	panel := map[string]bool{}
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/pg/") {
			continue
		}
		key := route.Method + " " + route.Path
		registered[key] = true
		if !exempt[key] {
			panel[key] = true
		}
	}
	for _, key := range accessTokenExemptRoutes {
		assert.True(t, registered[key], "exempt route %s is no longer registered", key)
	}

	declared := map[string]int{}
	for _, key := range middleware.AccessTokenRouteRuleKeys() {
		declared[key]++
	}
	for key := range panel {
		assert.Equal(t, 1, declared[key], "panel route %s must be declared exactly once", key)
	}
	for key := range declared {
		assert.True(t, panel[key], "declared route %s is not a registered panel route", key)
	}

	permissionRoutes := map[string]authz.Permission{}
	for _, route := range channelPermissionRoutes {
		permissionRoutes[route.method+" "+joinPaths("/api/channel", route.path)] = route.permission
	}
	for key, permission := range accessTokenHelperRoutes {
		permissionRoutes[key] = permission
	}
	for key, permission := range permissionRoutes {
		rule, ok := middleware.AccessTokenRouteRule(key)
		require.True(t, ok, key)
		assert.Equal(t, "scope", rule.Kind(), key)
		assert.Equal(t, service.AccessTokenScopeOf(permission), rule.Scope(), key)
	}
}

func TestSensitiveDashboardRoutesRequireBrowserSession(t *testing.T) {
	for _, key := range []string{
		"GET /api/update/release",
	} {
		rule, ok := middleware.AccessTokenRouteRule(key)
		require.True(t, ok, key)
		assert.Equal(t, "session", rule.Kind(), key)
	}
}

func TestAccessTokenCatalogCoversEveryPermission(t *testing.T) {
	catalog := service.AccessTokenCatalog(1, common.RoleRootUser)
	scopes := map[string]bool{}
	resources := map[string]int{}
	for _, group := range catalog {
		for _, resource := range group.Resources {
			resources[resource.Resource]++
			for _, action := range resource.Actions {
				scopes[resource.Resource+":"+action.Action] = true
			}
		}
	}
	for _, permission := range authz.AllPermissions() {
		assert.True(t, scopes[service.AccessTokenScopeOf(permission)], "root catalog lacks %s", service.AccessTokenScopeOf(permission))
	}
	for resource, count := range resources {
		assert.Equal(t, 1, count, "resource %s appears in more than one catalog entry", resource)
	}
}

func TestAccessTokenVerificationScopesAreAllListed(t *testing.T) {
	files, err := filepath.Glob("../service/*.go")
	require.NoError(t, err)
	fileSet := token.NewFileSet()
	var scopes []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, file, nil, 0)
		require.NoError(t, err)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if !strings.HasPrefix(name.Name, "VerificationScope") || i >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					require.True(t, ok, name.Name)
					scope, err := strconv.Unquote(literal.Value)
					require.NoError(t, err)
					scopes = append(scopes, scope)
				}
			}
		}
	}
	require.NotEmpty(t, scopes)
	for _, scope := range scopes {
		_, listed := service.AccessTokenVerificationScope(scope)
		assert.True(t, listed, "verification scope %s has no access token rule", scope)
	}
}
