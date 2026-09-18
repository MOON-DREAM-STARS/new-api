package dto

// WebWorkspaceConfig is the capability probe used before the entry is shown.
type WebWorkspaceConfig struct {
	Enabled  bool `json:"enabled"`
	Entitled bool `json:"entitled"`
}

// WebWorkspaceStatus reports the caller's own workspace state. It never
// includes the internal workspace id, provider credentials or storage paths.
type WebWorkspaceStatus struct {
	Entitled  bool                 `json:"entitled"`
	Reason    string               `json:"reason,omitempty"`
	Workspace *WebWorkspaceSummary `json:"workspace"`
}

type WebWorkspaceSummary struct {
	Provider     string `json:"provider"`
	Status       int    `json:"status"`
	CreatedAt    int64  `json:"created_at"`
	LastActiveAt int64  `json:"last_active_at"`
}

// WebProjectDto exposes only the internal mapping identity. The provider-side
// external project id is deliberately not part of this DTO.
type WebProjectDto struct {
	Id        int    `json:"id"`
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// WebConversationDto exposes only the internal mapping identity. The
// provider-side external conversation id is deliberately not part of this DTO.
type WebConversationDto struct {
	Id        int    `json:"id"`
	Title     string `json:"title"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type WebProjectListResponse struct {
	Items []WebProjectDto `json:"items"`
}

type WebConversationListResponse struct {
	Items []WebConversationDto `json:"items"`
}

// WebProjectRenameRequest renames the local project mapping.
type WebProjectRenameRequest struct {
	Name string `json:"name"`
}
