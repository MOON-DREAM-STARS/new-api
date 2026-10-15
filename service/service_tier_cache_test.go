package service

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	redis "github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveServiceTierRouteFailsClosedBeforeFirstSuccessfulConfigLoad(t *testing.T) {
	previousDB := model.DB
	previousSnapshot := serviceTierSnapshotValue.Swap(nil)
	var closeDB func()
	t.Cleanup(func() {
		if closeDB != nil {
			closeDB()
		}
		model.DB = previousDB
		serviceTierSnapshotValue.Store(previousSnapshot)
	})
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "missing-tables.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	closeDB = func() { _ = sqlDB.Close() }
	model.DB = db

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	route, failure := ResolveServiceTierRoute(ctx, "deepseek-v4-pro")

	require.Nil(t, route)
	require.NotNil(t, failure)
	assert.Equal(t, 503, failure.StatusCode)
	assert.Nil(t, serviceTierSnapshotValue.Load(), "a failed load must not become a successful empty snapshot")
}

func TestServiceTierSettingsCacheDoesNotReplaceNewerVersion(t *testing.T) {
	server := miniredis.RunT(t)
	previousRDB, previousRedisEnabled, previousSyncFrequency := common.RDB, common.RedisEnabled, common.SyncFrequency
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	common.RDB, common.RedisEnabled, common.SyncFrequency = client, true, 60
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB, common.RedisEnabled, common.SyncFrequency = previousRDB, previousRedisEnabled, previousSyncFrequency
	})

	newer := ServiceTierSettings{EnabledTiers: []string{model.ServiceTierEconomy}, Preference: "lowest", Version: 2}
	older := ServiceTierSettings{EnabledTiers: []string{model.ServiceTierEconomy, model.ServiceTierPerformance}, Preference: "lowest", AllowEscalation: true, Version: 1}
	require.NoError(t, cacheServiceTierSettingsIfNewer(42, newer))
	require.NoError(t, cacheServiceTierSettingsIfNewer(42, older))

	cached, err := common.RedisGet(serviceTierSettingsCacheKey(42))
	require.NoError(t, err)
	var actual ServiceTierSettings
	require.NoError(t, common.UnmarshalJsonStr(cached, &actual))
	assert.Equal(t, newer.Version, actual.Version)
	assert.False(t, actual.AllowEscalation)
}
