package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
}

// setupFlowControllerTestDB seeds two users' traffic. Only gpt-a belongs to a
// model family; the returned family is the one gpt-a is annotated with.
func setupFlowControllerTestDB(t *testing.T) model.ModelFamily {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}, &model.ModelFamily{}, &model.ModelFamilyMember{}, &model.AggregationPolicy{}, &model.UserServiceTierSetting{}))
	family := model.ModelFamily{Key: "gpt", Name: "GPT", SortOrder: 1, Version: 1}
	require.NoError(t, model.DB.Create(&family).Error)
	require.NoError(t, model.DB.Create(&model.ModelFamilyMember{FamilyId: family.Id, ModelName: "gpt-a"}).Error)
	require.NoError(t, service.RefreshServiceTierCache())
	t.Cleanup(func() {
		// Leave no model family behind for later tests in this package.
		require.NoError(t, model.DB.Where("1 = 1").Delete(&model.ModelFamilyMember{}).Error)
		require.NoError(t, model.DB.Where("1 = 1").Delete(&model.ModelFamily{}).Error)
		require.NoError(t, service.RefreshServiceTierCache())
	})
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		UseGroup:    "default",
		ChannelID:   1,
		ModelName:   "gpt-a",
		ServiceTier: model.ServiceTierEconomy,
		CreatedAt:   1100,
		Count:       2,
		Quota:       100,
		TokenUsed:   40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:      2,
		Username:    "bob",
		NodeName:    "node-b",
		TokenID:     22,
		UseGroup:    "vip",
		ChannelID:   1,
		ModelName:   "gpt-b",
		ServiceTier: model.ServiceTierBalanced,
		CreatedAt:   1200,
		Count:       1,
		Quota:       70,
		TokenUsed:   30,
	}).Error)
	return family
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "node-b", payload.Data[0].NodeName)
	require.Equal(t, "backup", payload.Data[0].TokenName)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Equal(t, model.ServiceTierBalanced, payload.Data[0].ServiceTier)
	require.Zero(t, payload.Data[0].ModelFamilyID, "gpt-b belongs to no family")
	require.Empty(t, payload.Data[0].ModelFamily)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	family := setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Equal(t, model.ServiceTierEconomy, payload.Data[0].ServiceTier)
	require.Equal(t, family.Id, payload.Data[0].ModelFamilyID)
	require.Equal(t, "GPT", payload.Data[0].ModelFamily)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	family := setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	raw := recorder.Body.String()
	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "gpt-a", payload.Data[0].ModelName)
	require.Equal(t, model.ServiceTierEconomy, payload.Data[0].ServiceTier)
	require.Equal(t, family.Id, payload.Data[0].ModelFamilyID)
	require.Equal(t, "GPT", payload.Data[0].ModelFamily)
	// The response carries no key for the nodes, groups or channels behind the
	// traffic, not even an empty one.
	for _, hidden := range []string{"node_name", "use_group", "channel_id", "channel_name"} {
		require.NotContains(t, raw, hidden)
	}
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}

// legacyQuotaData is quota_data as released before the serving tier was kept.
type legacyQuotaData struct {
	Id        int    `gorm:"primaryKey"`
	UserID    int    `gorm:"index"`
	Username  string `gorm:"index:idx_qdt_model_user_name,priority:2;size:64;default:''"`
	ModelName string `gorm:"index:idx_qdt_model_user_name,priority:1;size:64;default:''"`
	CreatedAt int64  `gorm:"bigint;index:idx_qdt_created_at,priority:2"`
	UseGroup  string `gorm:"index;size:64;default:''"`
	TokenID   int    `gorm:"index;default:0"`
	ChannelID int    `gorm:"index;default:0"`
	NodeName  string `gorm:"index;size:64;default:''"`
	TokenUsed int    `gorm:"default:0"`
	Count     int    `gorm:"default:0"`
	Quota     int    `gorm:"default:0"`
}

func (legacyQuotaData) TableName() string {
	return "quota_data"
}

// openQuotaDataExternalDB opens a MySQL or PostgreSQL database for the quota
// data migration test. It refuses a database that already holds one of the
// tables the test creates, and drops them afterwards.
func openQuotaDataExternalDB(t *testing.T, dialect string, dsn string) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	var (
		db     *gorm.DB
		dbType common.DatabaseType
		err    error
	)
	switch dialect {
	case "mysql":
		dbType = common.DatabaseTypeMySQL
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	case "postgres":
		dbType = common.DatabaseTypePostgreSQL
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	default:
		t.Fatalf("unsupported dialect %q", dialect)
	}
	require.NoError(t, err)
	tables := []string{"quota_data", "tokens", "channels"}
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Skipf("refusing to run the %s quota data migration test against external database because %s already exists", dialect, table)
		}
	}

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	common.SetDatabaseTypes(dbType, dbType)
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		for _, table := range tables {
			_ = db.Migrator().DropTable(table)
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
	})
	return db
}

// Traffic recorded before the serving tier was kept stays visible as untiered
// after the column is added, and new traffic is counted per tier next to it.
func runQuotaDataServiceTierMigrationTest(t *testing.T, db *gorm.DB) {
	t.Helper()

	require.NoError(t, db.AutoMigrate(&legacyQuotaData{}, &model.Token{}, &model.Channel{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, db.Create(&legacyQuotaData{
		UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: 3600, UseGroup: "default",
		TokenID: 11, ChannelID: 1, NodeName: "node-a", TokenUsed: 40, Count: 2, Quota: 100,
	}).Error)
	require.False(t, db.Migrator().HasColumn(&model.QuotaData{}, "service_tier"))

	require.NoError(t, db.AutoMigrate(&model.QuotaData{}))
	require.True(t, db.Migrator().HasColumn(&model.QuotaData{}, "service_tier"))
	var released model.QuotaData
	require.NoError(t, db.First(&released, "username = ?", "alice").Error)
	assert.Empty(t, released.ServiceTier, "released rows are untiered")
	assert.Equal(t, 100, released.Quota)

	// A second startup finds nothing left to change.
	finishCapture := captureSchemaChanges(t, db)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}))
	assert.Empty(t, finishCapture(), "a second migration changes no schema")

	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	base := model.QuotaDataLogParams{
		UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: 3700, UseGroup: "default",
		TokenID: 11, ChannelID: 1, NodeName: "node-a",
	}
	untiered, economy := base, base
	untiered.Quota, untiered.TokenUsed = 7, 3
	economy.Quota, economy.TokenUsed, economy.ServiceTier = 10, 5, model.ServiceTierEconomy
	model.LogQuotaData(untiered)
	model.LogQuotaData(economy)
	model.SaveQuotaDataCache()
	model.SaveQuotaDataCache()

	var stored []model.QuotaData
	require.NoError(t, db.Order("service_tier").Find(&stored).Error)
	require.Len(t, stored, 2, "untiered traffic joins the released row and tiered traffic gets its own")
	assert.Equal(t, 3, stored[0].Count)
	assert.Equal(t, 107, stored[0].Quota)
	assert.Equal(t, model.ServiceTierEconomy, stored[1].ServiceTier)
	assert.Equal(t, 10, stored[1].Quota)

	operatorRows, err := model.GetFlowQuotaData(0, 7200, "", 0, common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, operatorRows, 2)
	assert.Equal(t, "node-a", operatorRows[0].NodeName)
	assert.Equal(t, "east", operatorRows[0].ChannelName)
	assert.Equal(t, "primary", operatorRows[0].TokenName)
	assert.Empty(t, operatorRows[0].ServiceTier)
	assert.Equal(t, model.ServiceTierEconomy, operatorRows[1].ServiceTier)

	userRows, err := model.GetFlowQuotaData(0, 7200, "", 1, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, userRows, 2)
	for _, row := range userRows {
		assert.Empty(t, row.NodeName)
		assert.Empty(t, row.UseGroup)
		assert.Zero(t, row.ChannelID)
		assert.Equal(t, "gpt-a", row.ModelName)
	}
	assert.Equal(t, 107, userRows[0].Quota)
	assert.Equal(t, model.ServiceTierEconomy, userRows[1].ServiceTier)
}

func TestQuotaDataServiceTierMigration(t *testing.T) {
	runQuotaDataServiceTierMigrationTest(t, openTokenControllerTestDB(t))
}

func TestQuotaDataServiceTierMigrationMySQL(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to run the mysql quota data migration test")
	}
	runQuotaDataServiceTierMigrationTest(t, openQuotaDataExternalDB(t, "mysql", dsn))
}

func TestQuotaDataServiceTierMigrationPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to run the postgres quota data migration test")
	}
	runQuotaDataServiceTierMigrationTest(t, openQuotaDataExternalDB(t, "postgres", dsn))
}
