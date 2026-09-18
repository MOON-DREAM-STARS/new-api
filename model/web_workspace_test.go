package model

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// runWebWorkspaceMigrationChecks verifies the Phase 1 schema contract on one
// dialect: repeated migration is idempotent, existing mapping rows survive and
// the ownership uniqueness guarantees hold.
func runWebWorkspaceMigrationChecks(t *testing.T, db *gorm.DB) {
	t.Helper()

	for range 2 {
		require.NoError(t, db.AutoMigrate(&WebWorkspace{}, &WebProject{}, &WebConversation{}))
	}

	assert.True(t, db.Migrator().HasTable(&WebWorkspace{}))
	assert.True(t, db.Migrator().HasTable(&WebProject{}))
	assert.True(t, db.Migrator().HasTable(&WebConversation{}))
	assert.True(t, db.Migrator().HasIndex(&WebWorkspace{}, "uk_web_workspace_user"))
	assert.True(t, db.Migrator().HasIndex(&WebProject{}, "uk_web_project_provider_external_id"))
	assert.True(t, db.Migrator().HasIndex(&WebConversation{}, "uk_web_conversation_provider_external_id"))

	workspace := WebWorkspace{UserId: 91001, Provider: "chatgpt", Status: WebWorkspaceStatusActive}
	require.NoError(t, db.Create(&workspace).Error)

	project := WebProject{WorkspaceId: workspace.Id, Provider: "chatgpt", ExternalProjectId: "proj-1", Name: "Project 1"}
	require.NoError(t, db.Create(&project).Error)
	conversation := WebConversation{ProjectId: project.Id, Provider: "chatgpt", ExternalConversationId: "conv-1", Title: "Conversation 1"}
	require.NoError(t, db.Create(&conversation).Error)

	require.NoError(t, db.AutoMigrate(&WebWorkspace{}, &WebProject{}, &WebConversation{}))
	var stored WebProject
	require.NoError(t, db.First(&stored, project.Id).Error)
	assert.Equal(t, "Project 1", stored.Name)
	assert.Equal(t, workspace.Id, stored.WorkspaceId)

	// One user may own at most one workspace.
	require.Error(t, db.Create(&WebWorkspace{UserId: 91001, Provider: "chatgpt", Status: WebWorkspaceStatusActive}).Error)

	// Provider-side identifiers are unique inside one provider namespace only.
	require.Error(t, db.Create(&WebProject{WorkspaceId: workspace.Id, Provider: "chatgpt", ExternalProjectId: "proj-1", Name: "duplicate"}).Error)
	require.NoError(t, db.Create(&WebProject{WorkspaceId: workspace.Id, Provider: "gemini", ExternalProjectId: "proj-1", Name: "other provider"}).Error)
	require.Error(t, db.Create(&WebConversation{ProjectId: project.Id, Provider: "chatgpt", ExternalConversationId: "conv-1", Title: "duplicate"}).Error)
}

func TestWebWorkspaceMigrationSQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	runWebWorkspaceMigrationChecks(t, db)
}

func TestWebWorkspaceMigrationMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Migrator().DropTable(&WebConversation{}, &WebProject{}, &WebWorkspace{}))
	runWebWorkspaceMigrationChecks(t, db)
}

func TestWebWorkspaceMigrationPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Migrator().DropTable(&WebConversation{}, &WebProject{}, &WebWorkspace{}))
	runWebWorkspaceMigrationChecks(t, db)
}
