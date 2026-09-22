package webworkspace

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/model"
)

// TestWebWorkspaceRenameDeleteRaceKeepsOwnershipConsistent drives rename and
// delete requests for the same project concurrently and pins the invariants the
// ownership join must keep: a conversation never survives its project, a
// deleted project is never resurrected by a racing rename, a foreign workspace
// stays untouched, and the ownership snapshot pushed to the guard never names a
// project row that no longer exists. (checklist §31: delete/rename race)
func TestWebWorkspaceRenameDeleteRaceKeepsOwnershipConsistent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ownership-race.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.WebWorkspace{}, &model.WebProject{}, &model.WebConversation{}))
	useWebWorkspaceTestDB(t, db)

	owner := createWebWorkspaceTestUser(t, db, "ws-race-owner")
	other := createWebWorkspaceTestUser(t, db, "ws-race-other")
	workspace := &model.WebWorkspace{UserId: owner.Id, Provider: "chatgpt", Status: model.WebWorkspaceStatusActive}
	require.NoError(t, db.Create(workspace).Error)
	foreignWorkspace := &model.WebWorkspace{UserId: other.Id, Provider: "chatgpt", Status: model.WebWorkspaceStatusActive}
	require.NoError(t, db.Create(foreignWorkspace).Error)

	project := &model.WebProject{WorkspaceId: workspace.Id, Provider: "chatgpt", ExternalProjectId: "ext-race", Name: "race"}
	require.NoError(t, db.Create(project).Error)
	foreignProject := &model.WebProject{WorkspaceId: foreignWorkspace.Id, Provider: "chatgpt", ExternalProjectId: "ext-race-foreign", Name: "foreign"}
	require.NoError(t, db.Create(foreignProject).Error)
	conversation := &model.WebConversation{ProjectId: project.Id, Provider: "chatgpt", ExternalConversationId: "conv-race", Title: "race"}
	require.NoError(t, db.Create(conversation).Error)
	foreignConversation := &model.WebConversation{ProjectId: foreignProject.Id, Provider: "chatgpt", ExternalConversationId: "conv-race-foreign", Title: "foreign"}
	require.NoError(t, db.Create(foreignConversation).Error)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 10; index++ {
			_, _ = RenameOwnedProject(owner.Id, project.Id, fmt.Sprintf("race-%d", index))
		}
	}()
	go func() {
		defer waitGroup.Done()
		_ = DeleteOwnedProject(owner.Id, project.Id)
	}()
	waitGroup.Wait()

	count := func(value any, query string, args ...any) int64 {
		t.Helper()
		var total int64
		require.NoError(t, db.Model(value).Where(query, args...).Count(&total).Error, query)
		return total
	}

	// A conversation must never outlive the project it belongs to.
	assert.Zero(t, count(&model.WebConversation{}, "project_id NOT IN (?)", db.Model(&model.WebProject{}).Select("id")))

	projects := count(&model.WebProject{}, "id = ?", project.Id)
	conversations := count(&model.WebConversation{}, "project_id = ?", project.Id)
	assert.Equal(t, projects == 1, conversations == 1, "project and conversation must disappear or survive together")

	// A racing rename must not touch or resurrect the foreign workspace.
	assert.Equal(t, int64(1), count(&model.WebProject{}, "id = ?", foreignProject.Id))
	assert.Equal(t, int64(1), count(&model.WebConversation{}, "project_id = ?", foreignProject.Id))
	assert.Equal(t, "foreign", func() string {
		var stored model.WebProject
		require.NoError(t, db.Where("id = ?", foreignProject.Id).First(&stored).Error)
		return stored.Name
	}())

	// The ownership snapshot the guard receives only names live project rows.
	snapshot, err := BuildOwnership(workspace.Id)
	require.NoError(t, err)
	if projects == 0 {
		assert.Empty(t, snapshot.Projects)
	} else {
		assert.Equal(t, []string{"ext-race"}, snapshot.Projects)
	}
}
