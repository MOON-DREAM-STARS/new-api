package webworkspace

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetWorkspaceByUserID returns the single workspace owned by the user. A
// missing workspace is reported as ErrResourceNotFound so callers never
// distinguish "not created yet" from "not yours".
func GetWorkspaceByUserID(userID int) (*model.WebWorkspace, error) {
	if userID <= 0 {
		return nil, ErrResourceNotFound
	}
	var workspace model.WebWorkspace
	err := model.DB.Where("user_id = ?", userID).First(&workspace).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrResourceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &workspace, nil
}

// EnsureWorkspace returns the user's workspace, creating it on first use. The
// unique index on user_id is the concurrency guard: competing callers may race,
// but only one row can exist, and every caller reads that row back instead of
// trusting RowsAffected, whose semantics differ between databases.
func EnsureWorkspace(userID int, provider string) (*model.WebWorkspace, error) {
	if userID <= 0 {
		return nil, ErrResourceNotFound
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, errors.New("web workspace provider is required")
	}
	workspace := model.WebWorkspace{
		UserId:   userID,
		Provider: provider,
		Status:   model.WebWorkspaceStatusActive,
	}
	if err := model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&workspace).Error; err != nil {
		return nil, err
	}
	return GetWorkspaceByUserID(userID)
}

// GetOwnedProject resolves a project through
// web_projects -> web_workspaces -> user_id. Unknown ids and projects owned by
// another user both return ErrResourceNotFound.
func GetOwnedProject(userID int, projectID int) (*model.WebProject, error) {
	if userID <= 0 || projectID <= 0 {
		return nil, ErrResourceNotFound
	}
	var project model.WebProject
	err := model.DB.Model(&model.WebProject{}).
		Select("web_projects.*").
		Joins("JOIN web_workspaces ON web_workspaces.id = web_projects.workspace_id").
		Where("web_projects.id = ? AND web_workspaces.user_id = ?", projectID, userID).
		First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrResourceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &project, nil
}

// GetOwnedConversation resolves a conversation through
// web_conversations -> web_projects -> web_workspaces -> user_id.
func GetOwnedConversation(userID int, conversationID int) (*model.WebConversation, error) {
	if userID <= 0 || conversationID <= 0 {
		return nil, ErrResourceNotFound
	}
	var conversation model.WebConversation
	err := model.DB.Model(&model.WebConversation{}).
		Select("web_conversations.*").
		Joins("JOIN web_projects ON web_projects.id = web_conversations.project_id").
		Joins("JOIN web_workspaces ON web_workspaces.id = web_projects.workspace_id").
		Where("web_conversations.id = ? AND web_workspaces.user_id = ?", conversationID, userID).
		First(&conversation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrResourceNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}

// ListOwnedProjects returns every project whose workspace belongs to the user.
func ListOwnedProjects(userID int) ([]model.WebProject, error) {
	projects := make([]model.WebProject, 0)
	if userID <= 0 {
		return projects, nil
	}
	err := model.DB.Model(&model.WebProject{}).
		Select("web_projects.*").
		Joins("JOIN web_workspaces ON web_workspaces.id = web_projects.workspace_id").
		Where("web_workspaces.user_id = ?", userID).
		Order("web_projects.id ASC").
		Find(&projects).Error
	if err != nil {
		return nil, err
	}
	return projects, nil
}

// ListOwnedConversations returns the conversations of one owned project.
func ListOwnedConversations(userID int, projectID int) ([]model.WebConversation, error) {
	conversations := make([]model.WebConversation, 0)
	if userID <= 0 || projectID <= 0 {
		return conversations, nil
	}
	err := model.DB.Model(&model.WebConversation{}).
		Select("web_conversations.*").
		Joins("JOIN web_projects ON web_projects.id = web_conversations.project_id").
		Joins("JOIN web_workspaces ON web_workspaces.id = web_projects.workspace_id").
		Where("web_conversations.project_id = ? AND web_workspaces.user_id = ?", projectID, userID).
		Order("web_conversations.id ASC").
		Find(&conversations).Error
	if err != nil {
		return nil, err
	}
	return conversations, nil
}

// RenameOwnedProject updates only the local mapping name in Phase 1. The
// provider-side rename and the fail-safe resynchronisation path arrive with the
// Phase 4 ChatGPT adapter; until then no provider call is attempted.
func RenameOwnedProject(userID int, projectID int, name string) (*model.WebProject, error) {
	project, err := GetOwnedProject(userID, projectID)
	if err != nil {
		return nil, err
	}
	// RowsAffected is intentionally not checked: MySQL reports zero changed
	// rows when the new name equals the previous one.
	if err := model.DB.Model(&model.WebProject{}).
		Where("id = ? AND workspace_id = ?", project.Id, project.WorkspaceId).
		Update("name", name).Error; err != nil {
		return nil, err
	}
	return GetOwnedProject(userID, projectID)
}

// DeleteOwnedProjectWithProvider deletes the provider-side project through the
// runtime guard first, then removes the local mapping and its conversations. If
// the provider deletion fails, the local row is deliberately retained.
func DeleteOwnedProjectWithProvider(ctx context.Context, userID int, projectID int) error {
	project, err := GetOwnedProject(userID, projectID)
	if err != nil {
		return err
	}
	client, err := newAgentClient()
	if err != nil {
		return err
	}
	if err := client.DeleteProject(ctx, project.WorkspaceId, project.ExternalProjectId, project.Name); err != nil {
		return err
	}
	if err := DeleteOwnedProject(userID, projectID); err != nil && !errors.Is(err, ErrResourceNotFound) {
		return err
	}
	return nil
}

// DeleteOwnedProject removes the local mapping and its conversation mappings in
// one transaction. Provider-side deletion is performed by callers before this
// local cleanup. The ownership join is repeated in every statement so a
// concurrent ownership change can never delete a foreign row.
func DeleteOwnedProject(userID int, projectID int) error {
	project, err := GetOwnedProject(userID, projectID)
	if err != nil {
		return err
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", project.Id).Delete(&model.WebConversation{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND workspace_id = ?", project.Id, project.WorkspaceId).
			Delete(&model.WebProject{}).Error
	})
}
