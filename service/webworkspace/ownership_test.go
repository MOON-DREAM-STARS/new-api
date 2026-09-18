package webworkspace

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func useWebWorkspaceTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
}

func openWebWorkspaceSQLiteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.WebWorkspace{}, &model.WebProject{}, &model.WebConversation{}))
	return db
}

// prepareWebWorkspaceExternalTestDB migrates the Web Workspace tables in a
// dedicated external test database and clears the previous run's rows so the
// ownership checks can use stable user names and ids.
func prepareWebWorkspaceExternalTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.WebWorkspace{}, &model.WebProject{}, &model.WebConversation{}))
	for _, table := range []string{"web_conversations", "web_projects", "web_workspaces"} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}
	require.NoError(t, db.Unscoped().Where("username LIKE ?", "ws-owner-%").Delete(&model.User{}).Error)
}

func createWebWorkspaceTestUser(t *testing.T, db *gorm.DB, username string) *model.User {
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

func runWebWorkspaceOwnershipChecks(t *testing.T, db *gorm.DB) {
	t.Helper()

	userA := createWebWorkspaceTestUser(t, db, "ws-owner-a")
	userB := createWebWorkspaceTestUser(t, db, "ws-owner-b")

	workspaceA, err := EnsureWorkspace(userA.Id, "chatgpt")
	require.NoError(t, err)
	workspaceB, err := EnsureWorkspace(userB.Id, "chatgpt")
	require.NoError(t, err)
	require.NotEqual(t, workspaceA.Id, workspaceB.Id)

	again, err := EnsureWorkspace(userA.Id, "chatgpt")
	require.NoError(t, err)
	assert.Equal(t, workspaceA.Id, again.Id)
	var workspaceCount int64
	require.NoError(t, db.Model(&model.WebWorkspace{}).Where("user_id = ?", userA.Id).Count(&workspaceCount).Error)
	assert.EqualValues(t, 1, workspaceCount)

	projectA := &model.WebProject{WorkspaceId: workspaceA.Id, Provider: "chatgpt", ExternalProjectId: "ext-a", Name: "Project A"}
	projectB := &model.WebProject{WorkspaceId: workspaceB.Id, Provider: "chatgpt", ExternalProjectId: "ext-b", Name: "Project B"}
	require.NoError(t, db.Create(projectA).Error)
	require.NoError(t, db.Create(projectB).Error)
	conversationA := &model.WebConversation{ProjectId: projectA.Id, Provider: "chatgpt", ExternalConversationId: "conv-a", Title: "Conversation A"}
	conversationB := &model.WebConversation{ProjectId: projectB.Id, Provider: "chatgpt", ExternalConversationId: "conv-b", Title: "Conversation B"}
	require.NoError(t, db.Create(conversationA).Error)
	require.NoError(t, db.Create(conversationB).Error)

	owned, err := GetOwnedProject(userA.Id, projectA.Id)
	require.NoError(t, err)
	assert.Equal(t, projectA.Id, owned.Id)

	_, err = GetOwnedProject(userA.Id, projectB.Id)
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = GetOwnedProject(userA.Id, 987654321)
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = GetOwnedConversation(userA.Id, conversationB.Id)
	assert.ErrorIs(t, err, ErrResourceNotFound)

	projects, err := ListOwnedProjects(userA.Id)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, projectA.Id, projects[0].Id)

	conversations, err := ListOwnedConversations(userA.Id, projectA.Id)
	require.NoError(t, err)
	require.Len(t, conversations, 1)
	assert.Equal(t, conversationA.Id, conversations[0].Id)

	otherConversations, err := ListOwnedConversations(userA.Id, projectB.Id)
	require.NoError(t, err)
	assert.Empty(t, otherConversations)

	_, err = RenameOwnedProject(userA.Id, projectB.Id, "stolen")
	assert.ErrorIs(t, err, ErrResourceNotFound)
	assert.ErrorIs(t, DeleteOwnedProject(userA.Id, projectB.Id), ErrResourceNotFound)

	renamed, err := RenameOwnedProject(userA.Id, projectA.Id, "Project A renamed")
	require.NoError(t, err)
	assert.Equal(t, "Project A renamed", renamed.Name)

	require.NoError(t, DeleteOwnedProject(userA.Id, projectA.Id))
	_, err = GetOwnedProject(userA.Id, projectA.Id)
	assert.ErrorIs(t, err, ErrResourceNotFound)
	var conversationCount int64
	require.NoError(t, db.Model(&model.WebConversation{}).Where("project_id = ?", projectA.Id).Count(&conversationCount).Error)
	assert.EqualValues(t, 0, conversationCount)

	stillOwned, err := GetOwnedProject(userB.Id, projectB.Id)
	require.NoError(t, err)
	assert.Equal(t, "Project B", stillOwned.Name)
}

func TestWebWorkspaceOwnershipIsolationSQLite(t *testing.T) {
	db := openWebWorkspaceSQLiteTestDB(t)
	useWebWorkspaceTestDB(t, db)
	runWebWorkspaceOwnershipChecks(t, db)
}

func TestWebWorkspaceOwnershipIsolationMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	prepareWebWorkspaceExternalTestDB(t, db)
	useWebWorkspaceTestDB(t, db)
	runWebWorkspaceOwnershipChecks(t, db)
}

func TestWebWorkspaceOwnershipIsolationPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	prepareWebWorkspaceExternalTestDB(t, db)
	useWebWorkspaceTestDB(t, db)
	runWebWorkspaceOwnershipChecks(t, db)
}

func TestWebWorkspaceEnsureWorkspaceRejectsInvalidInput(t *testing.T) {
	db := openWebWorkspaceSQLiteTestDB(t)
	useWebWorkspaceTestDB(t, db)

	_, err := EnsureWorkspace(0, "chatgpt")
	assert.ErrorIs(t, err, ErrResourceNotFound)
	_, err = GetOwnedProject(0, 1)
	assert.ErrorIs(t, err, ErrResourceNotFound)
}
