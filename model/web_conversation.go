package model

// WebConversation maps one provider-side conversation to the project that owns
// it. Ownership is derived through the project chain; the row carries no ACL.
type WebConversation struct {
	Id                     int    `json:"id" gorm:"primaryKey"`
	ProjectId              int    `json:"project_id" gorm:"not null;index:idx_web_conversation_project"`
	Provider               string `json:"provider" gorm:"type:varchar(32);not null;uniqueIndex:uk_web_conversation_provider_external_id,priority:1"`
	ExternalConversationId string `json:"external_conversation_id" gorm:"type:varchar(128);not null;uniqueIndex:uk_web_conversation_provider_external_id,priority:2"`
	Title                  string `json:"title" gorm:"type:varchar(255);not null"`
	CreatedAt              int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt              int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

func (WebConversation) TableName() string {
	return "web_conversations"
}
