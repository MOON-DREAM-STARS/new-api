package model

// Web Workspace status values. Stopping a browser runtime never deletes the
// workspace row; only an explicit owner or admin action may suspend it in a
// later phase.
const (
	WebWorkspaceStatusActive    = 1
	WebWorkspaceStatusSuspended = 2
)

// WebWorkspace is the single server-side browser workspace owned by one New API
// user. Ownership is derived exclusively from UserId; the model deliberately
// has no ACL table, no project-user join table and no second owner column.
type WebWorkspace struct {
	Id           int    `json:"id" gorm:"primaryKey"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:uk_web_workspace_user"`
	Provider     string `json:"provider" gorm:"type:varchar(32);not null"`
	Status       int    `json:"status" gorm:"type:int;not null"`
	CreatedAt    int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt    int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
	LastActiveAt int64  `json:"last_active_at" gorm:"not null;default:0;column:last_active_at"`
}

func (WebWorkspace) TableName() string {
	return "web_workspaces"
}
