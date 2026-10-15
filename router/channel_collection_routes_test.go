package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelCollectionRoutesAcceptBothSlashForms(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMaster := common.RedisEnabled, common.IsMasterNode
	previousSecret, previousRateLimit := common.SessionSecret, common.GlobalApiRateLimitEnable
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSession{},
		&model.UserAccessToken{},
		&model.AuditLog{},
		&model.CasbinRule{},
		&model.AuthzRole{},
		&model.Channel{},
	))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.IsMasterNode = false, true
	common.SessionSecret, common.GlobalApiRateLimitEnable = "channel-route-test-secret", false
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

	admin := &model.User{
		Username:    "channel-route-admin",
		Password:    "test-password-hash",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AuthVersion: 1,
		AffCode:     "channel-route-admin",
	}
	require.NoError(t, db.Create(admin).Error)
	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{
		authz.ResourceChannel: {
			authz.ActionRead:           true,
			authz.ActionWrite:          true,
			authz.ActionSensitiveWrite: true,
		},
	}))
	adminSession, err := service.CreateLoginSession(admin.Id, "test", "127.0.0.1", "channel-route-test")
	require.NoError(t, err)

	secret := "channel-route-test-secret-value"
	channel := &model.Channel{
		Type:   int(constant.ChannelTypeOpenAI),
		Key:    secret,
		Name:   "route-test-channel",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-route-test",
		Group:  "default",
	}
	require.NoError(t, db.Create(channel).Error)

	request := func(method, path, credential, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	paths := []string{"/api/channel", "/api/channel/"}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		for _, path := range paths {
			t.Run("unauthenticated_"+method+"_"+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
				response := request(method, path, "", "")
				assert.Equal(t, http.StatusUnauthorized, response.Code)
				assert.NotContains(t, response.Body.String(), "Invalid URL")
			})
		}
	}

	t.Run("authorized_get_returns_same_secret_free_list", func(t *testing.T) {
		firstResponse := request(http.MethodGet, "/api/channel?page=1&page_size=10", adminSession.AccessToken, "")
		secondResponse := request(http.MethodGet, "/api/channel/?page=1&page_size=10", adminSession.AccessToken, "")
		require.Equal(t, http.StatusOK, firstResponse.Code)
		require.Equal(t, http.StatusOK, secondResponse.Code)
		assert.Equal(t, firstResponse.Body.String(), secondResponse.Body.String())
		assert.Contains(t, firstResponse.Body.String(), `"name":"route-test-channel"`)
		assert.NotContains(t, firstResponse.Body.String(), secret)
		assert.NotContains(t, secondResponse.Body.String(), secret)
	})

	t.Run("missing_permission_and_token_scope_still_forbidden", func(t *testing.T) {
		adminWithoutRead := &model.User{
			Username:    "channel-route-read-denied",
			Password:    "test-password-hash",
			Role:        common.RoleAdminUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AuthVersion: 1,
			AffCode:     "channel-route-read-denied",
		}
		require.NoError(t, db.Create(adminWithoutRead).Error)
		require.NoError(t, authz.SetUserPermissions(adminWithoutRead.Id, authz.PermissionsMap{
			authz.ResourceChannel: {authz.ActionRead: false},
		}))
		deniedSession, err := service.CreateLoginSession(adminWithoutRead.Id, "test", "127.0.0.1", "channel-route-test")
		require.NoError(t, err)

		patValue := model.AccessTokenPrefix + "channel-route-write-only"
		grant := &model.UserAccessToken{
			UserId:    admin.Id,
			Name:      "channel route write only",
			TokenHash: model.AccessTokenFingerprint(patValue),
			TokenHint: "route",
		}
		require.NoError(t, grant.SetScopes([]string{service.AccessTokenScopeOf(authz.ChannelWrite)}))
		require.NoError(t, db.Create(grant).Error)

		for _, path := range paths {
			assert.Equal(t, http.StatusForbidden, request(http.MethodGet, path, deniedSession.AccessToken, "").Code, path)
			patResponse := request(http.MethodGet, path, patValue, "")
			assert.Equal(t, http.StatusForbidden, patResponse.Code, path)
			assert.Contains(t, patResponse.Body.String(), "ACCESS_TOKEN_SCOPE_DENIED")
		}
	})

	t.Run("invalid_create_and_update_payloads_do_not_write", func(t *testing.T) {
		for _, methodAndBody := range []struct {
			method string
			body   string
		}{
			{method: http.MethodPost, body: `{"channel":null}`},
			{method: http.MethodPut, body: `{"id":1,"status":2}`},
		} {
			responses := make([]*httptest.ResponseRecorder, len(paths))
			for index, path := range paths {
				responses[index] = request(methodAndBody.method, path, adminSession.AccessToken, methodAndBody.body)
			}
			assert.Equal(t, responses[0].Code, responses[1].Code, methodAndBody.method)
			assert.Equal(t, responses[0].Body.String(), responses[1].Body.String(), methodAndBody.method)
			assert.NotEqual(t, http.StatusNotFound, responses[0].Code, methodAndBody.method)
		}
		var channelCount int64
		require.NoError(t, db.Model(&model.Channel{}).Count(&channelCount).Error)
		assert.EqualValues(t, 1, channelCount)
	})
}
