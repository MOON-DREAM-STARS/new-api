package service

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

func TestCheckTokenModelFamilyFailsClosedWhenConfigUnavailable(t *testing.T) {
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

	limited, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(limited, constant.ContextKeyTokenModelFamilies, []int{1})
	failure := CheckTokenModelFamily(limited, "deepseek-v4-pro")
	require.NotNil(t, failure, "a limited key never becomes unlimited because the configuration cannot be read")
	assert.Equal(t, 503, failure.StatusCode)

	unlimited, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Nil(t, CheckTokenModelFamily(unlimited, "deepseek-v4-pro"), "an unlimited key does not need the configuration")
}

func TestServiceTierSnapshotFamilyIDOf(t *testing.T) {
	snapshot := &serviceTierSnapshot{memberFamily: map[string]int{"qwen-plus": 2, "gpt-4o-gizmo-*": 5}}
	assert.Equal(t, 2, snapshot.familyIDOf("qwen-plus"))
	assert.Equal(t, 5, snapshot.familyIDOf("gpt-4o-gizmo-g-123"), "a model matches the way routing matches it")
	assert.Zero(t, snapshot.familyIDOf("unlisted-model"), "models without a family are id 0")
}

func TestTokenServiceTierOverrideKeepsVersionsWhenFollowingAccount(t *testing.T) {
	server := miniredis.RunT(t)
	previousRDB, previousRedisEnabled, previousSyncFrequency, previousDB := common.RDB, common.RedisEnabled, common.SyncFrequency, model.DB
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "token-tier.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.UserServiceTierSetting{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	common.RDB, common.RedisEnabled, common.SyncFrequency, model.DB = client, true, 60, db
	t.Cleanup(func() {
		_ = client.Close()
		_ = sqlDB.Close()
		common.RDB, common.RedisEnabled, common.SyncFrequency, model.DB = previousRDB, previousRedisEnabled, previousSyncFrequency, previousDB
	})

	own := ServiceTierSettings{EnabledTiers: []string{model.ServiceTierPerformance}, Preference: "performance"}
	require.NoError(t, SaveTokenServiceTierSettings(7, 42, &own))
	override, err := GetTokenServiceTierOverride(7, 42)
	require.NoError(t, err)
	require.NotNil(t, override)
	assert.Equal(t, "performance", override.Preference)
	settings, err := GetTokenServiceTierSettings(7, 42)
	require.NoError(t, err)
	assert.Equal(t, "performance", settings.Preference, "the key's own settings route its requests")

	// Following the account default keeps the row as a marker, so versions
	// only grow and an older cached value can never come back.
	require.NoError(t, SaveTokenServiceTierSettings(7, 42, nil))
	override, err = GetTokenServiceTierOverride(7, 42)
	require.NoError(t, err)
	assert.Nil(t, override)
	settings, err = GetTokenServiceTierSettings(7, 42)
	require.NoError(t, err)
	assert.Equal(t, DefaultServiceTierSettings().Preference, settings.Preference, "a key without settings routes by the account default")

	stale := serviceTierTokenCacheEntry{Version: 1, Settings: own}
	require.NoError(t, cacheServiceTierValueIfNewer(serviceTierTokenCacheKey(42), stale.Version, stale))
	override, err = GetTokenServiceTierOverride(7, 42)
	require.NoError(t, err)
	assert.Nil(t, override, "a stale write does not replace the newer marker")

	require.NoError(t, SaveTokenServiceTierSettings(7, 42, &own))
	var row model.UserServiceTierSetting
	require.NoError(t, db.Where("user_id = ? AND token_id = ?", 7, 42).First(&row).Error)
	assert.EqualValues(t, 3, row.Version)
	override, err = GetTokenServiceTierOverride(7, 42)
	require.NoError(t, err)
	require.NotNil(t, override)
	assert.Equal(t, "performance", override.Preference)

	// A key that never had settings needs no row to follow the account.
	require.NoError(t, SaveTokenServiceTierSettings(7, 43, nil))
	var count int64
	require.NoError(t, db.Model(&model.UserServiceTierSetting{}).Where("token_id = ?", 43).Count(&count).Error)
	assert.Zero(t, count)
}
