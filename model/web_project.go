package model

// WebProject maps one provider-side project to the workspace that owns it.
// Owner is derived from WorkspaceId -> WebWorkspace.UserId; there is no
// redundant owner_user_id column and no per-project permission matrix.
type WebProject struct {
	Id                int    `json:"id" gorm:"primaryKey"`
	WorkspaceId       int    `json:"workspace_id" gorm:"not null;index:idx_web_project_workspace"`
	Provider          string `json:"provider" gorm:"type:varchar(32);not null;uniqueIndex:uk_web_project_provider_external_id,priority:1"`
	ExternalProjectId string `json:"external_project_id" gorm:"type:varchar(128);not null;uniqueIndex:uk_web_project_provider_external_id,priority:2"`
	Name              string `json:"name" gorm:"type:varchar(255);not null"`
	CreatedAt         int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt         int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

func (WebProject) TableName() string {
	return "web_projects"
}
