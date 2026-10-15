package model

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// The aggregation tables must migrate idempotently and keep their uniqueness
// and optimistic-version guarantees on every supported database.
func TestServiceTierStorageDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "tier_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			initCol()
			tables := []any{&ModelFamily{}, &ModelFamilyMember{}, &AggregationPolicy{}, &UserServiceTierSetting{}}
			require.NoError(t, db.Migrator().DropTable(tables...))
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(tables...))
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				initCol()
				require.NoError(t, sqlDB.Close())
			})
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			// A fresh table set is seeded once; later startups keep what the
			// administrator changed.
			var ddl []string
			require.NoError(t, db.Callback().Raw().After("gorm:raw").Register("tier_test_ddl", func(tx *gorm.DB) {
				if sql := strings.ToUpper(tx.Statement.SQL.String()); strings.Contains(sql, "ALTER ") || strings.Contains(sql, "CREATE ") {
					ddl = append(ddl, sql)
				}
			}))
			require.NoError(t, migrateServiceTierTables(db))
			assert.NotEmpty(t, ddl, "the first startup creates the tables")
			ddl = nil
			require.NoError(t, migrateServiceTierTables(db))
			require.NoError(t, db.Callback().Raw().Remove("tier_test_ddl"))
			assert.Empty(t, ddl, "a second startup changes no schema")
			store, err := LoadServiceTierStore()
			require.NoError(t, err)
			require.Len(t, store.Families, 4)
			assert.Equal(t, []string{"deepseek", "qwen", "glm", "kimi"}, []string{store.Families[0].Key, store.Families[1].Key, store.Families[2].Key, store.Families[3].Key})
			require.Len(t, store.Policies, 1, "only the DeepSeek example policy is seeded")
			assert.False(t, store.Policies[0].Enabled, "the seeded example never routes until enabled")
			seeded, err := store.Policies[0].GetConfig()
			require.NoError(t, err)
			assert.Equal(t, 0.15, seeded.Tiers[ServiceTierEconomy].Ratio)
			assert.Equal(t, 0.40, seeded.Tiers[ServiceTierPerformance].Ratio)
			for _, index := range []struct {
				table any
				name  string
			}{
				{&ModelFamily{}, "Key"},
				{&ModelFamilyMember{}, "ModelName"},
				{&AggregationPolicy{}, "idx_aggregation_policy_scope"},
				{&UserServiceTierSetting{}, "idx_user_service_tier_scope"},
			} {
				assert.True(t, db.Migrator().HasIndex(index.table, index.name), index.name)
			}

			deepseek, qwen := store.Families[0], store.Families[1]
			require.ErrorIs(t, CreateModelFamily(&ModelFamily{Key: "qwen", Name: "Duplicate"}), ErrModelFamilyKeyExists)

			_, err = ReplaceModelFamilyMembers(deepseek.Id, deepseek.Version, []string{"deepseek-v4-pro", "deepseek-v3"})
			require.NoError(t, err)
			_, err = ReplaceModelFamilyMembers(deepseek.Id, deepseek.Version, []string{"deepseek-v4-pro"})
			require.ErrorIs(t, err, ErrServiceTierVersionConflict, "a stale member edit is rejected")
			_, err = ReplaceModelFamilyMembers(qwen.Id, qwen.Version, []string{"qwen-plus", "deepseek-v3"})
			var message *common.Message
			require.True(t, errors.As(err, &message), "a model belongs to at most one family")
			assert.Equal(t, "deepseek-v3", message.Params["models"])

			override := &AggregationPolicy{FamilyId: deepseek.Id, ModelName: "deepseek-v3", Enabled: true}
			require.NoError(t, override.SetConfig(DefaultAggregationPolicyConfig(nil)))
			require.NoError(t, SaveAggregationPolicy(override, 0))
			assert.Equal(t, int64(1), override.Version)
			duplicate := &AggregationPolicy{FamilyId: deepseek.Id, ModelName: "deepseek-v3", Config: override.Config}
			require.ErrorIs(t, SaveAggregationPolicy(duplicate, 0), ErrServiceTierVersionConflict, "a concurrent first save loses")
			override.Enabled = false
			overrideID := override.Id
			override.Id = 0
			require.NoError(t, SaveAggregationPolicy(override, 1))
			assert.Equal(t, int64(2), override.Version)
			assert.Equal(t, overrideID, override.Id, "an update reports the stored row")
			require.ErrorIs(t, SaveAggregationPolicy(override, 1), ErrServiceTierVersionConflict)
			require.True(t, errors.As(DeleteAggregationPolicy(deepseek.Id, "", 1), &message), "family defaults are disabled, not deleted")

			removed, err := ReplaceModelFamilyMembers(deepseek.Id, deepseek.Version+1, []string{"deepseek-v4-pro"})
			require.NoError(t, err)
			assert.Equal(t, int64(1), removed, "an override leaves with its model")

			deepseek.Name = "DeepSeek models"
			require.ErrorIs(t, UpdateModelFamily(&deepseek, deepseek.Version), ErrServiceTierVersionConflict)
			require.NoError(t, UpdateModelFamily(&deepseek, deepseek.Version+2))

			setting := &UserServiceTierSetting{UserId: 7, Preference: ServiceTierPreferenceLowest, AllowEscalation: true, MaxEscalationTier: ServiceTierPerformance}
			setting.SetEnabledTiers([]string{ServiceTierPerformance, ServiceTierEconomy})
			require.NoError(t, SaveUserServiceTierSetting(setting, 0))
			require.ErrorIs(t, SaveUserServiceTierSetting(setting, 0), ErrServiceTierVersionConflict)
			setting.AllowEscalation, setting.MaxEscalationTier = false, ""
			require.NoError(t, SaveUserServiceTierSetting(setting, 1))
			stored, found, err := GetUserServiceTierSetting(7, 0)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, []string{ServiceTierEconomy, ServiceTierPerformance}, stored.GetEnabledTiers())
			assert.False(t, stored.AllowEscalation)
			assert.Equal(t, int64(2), stored.Version)

			require.NoError(t, DeleteModelFamily(deepseek.Id, deepseek.Version))
			store, err = LoadServiceTierStore()
			require.NoError(t, err)
			assert.Len(t, store.Families, 3)
			for _, member := range store.Members {
				assert.NotEqual(t, deepseek.Id, member.FamilyId)
			}
			assert.Empty(t, store.Policies, "a deleted family takes its policies")
			require.NoError(t, migrateServiceTierTables(db))
			store, err = LoadServiceTierStore()
			require.NoError(t, err)
			assert.Len(t, store.Families, 3, "a restart does not recreate deleted families")
		})
	}
}
