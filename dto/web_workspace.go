package dto

// WebWorkspaceConfig is the capability probe used before the entry is shown.
type WebWorkspaceConfig struct {
	Enabled         bool `json:"enabled"`
	Entitled        bool `json:"entitled"`
	MaxScreenWidth  int  `json:"max_screen_width"`
	MaxScreenHeight int  `json:"max_screen_height"`
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

// WebProjectCreateRequest carries the operator-facing name of one provider-side
// project creation. The provider URL and the provider project id are never
// accepted from the client: only the guard can observe them.
type WebProjectCreateRequest struct {
	Name string `json:"name"`
}

// WebWorkspaceProjectPermitDto carries the short-lived permit the client may
// redeem for exactly one provider-side project creation. It never includes the
// runtime address or workspace identity.
type WebWorkspaceProjectPermitDto struct {
	PermitId  string `json:"permit_id"`
	ExpiresAt int64  `json:"expires_at"`
}

// WebWorkspaceNavigationDto is the control-plane view of browser navigation
// state. It intentionally contains no URL or runtime address.
type WebWorkspaceNavigationDto struct {
	CanGoBack    bool  `json:"can_go_back"`
	CanGoForward bool  `json:"can_go_forward"`
	UpdatedAt    int64 `json:"updated_at"`
}

// WebWorkspaceNavigationRequest is the only client input for a navigation
// command. The action is validated by the service before reaching the agent.
type WebWorkspaceNavigationRequest struct {
	Action    string `json:"action"`
	ProjectID int    `json:"project_id,omitempty"`
}

// WebWorkspaceStartRequest carries the optional remote screen size proposal of
// a start. Both dimensions zero mean the agent default.
type WebWorkspaceStartRequest struct {
	ScreenWidth  int `json:"screen_width"`
	ScreenHeight int `json:"screen_height"`
}

// WebWorkspaceRestartRequest carries the optional restart overrides. An empty
// mode keeps the runtime mode the running session already had, and a zero size
// keeps the size the running runtime already has.
type WebWorkspaceRestartRequest struct {
	Mode         string `json:"mode"`
	ScreenWidth  int    `json:"screen_width"`
	ScreenHeight int    `json:"screen_height"`
}

// WebWorkspacePageDto is the URL-free health state of the remote page.
type WebWorkspacePageDto struct {
	State     string `json:"state"`
	Error     string `json:"error"`
	Attempts  int    `json:"attempts"`
	UpdatedAt int64  `json:"updated_at"`
}

// WebWorkspaceProjectCreationDto is the URL-free creation state of the guard.
// It carries no provider identifier, address or credential.
type WebWorkspaceProjectCreationDto struct {
	PermitId  string `json:"permit_id"`
	State     string `json:"state"`
	Error     string `json:"error"`
	UpdatedAt int64  `json:"updated_at"`
}

// WebWorkspaceSessionDto is the control-plane view of one browser session.
// Runtime ids, container addresses and ports never leave the control plane.
type WebWorkspaceSessionDto struct {
	SessionId       string                          `json:"session_id"`
	State           string                          `json:"state"`
	Mode            string                          `json:"mode"`
	CreatedAt       int64                           `json:"created_at"`
	LastSeenAt      int64                           `json:"last_seen_at"`
	IdleDeadlineAt  int64                           `json:"idle_deadline_at"`
	StreamBytesOut  int64                           `json:"stream_bytes_out"`
	StreamBytesIn   int64                           `json:"stream_bytes_in"`
	Navigation      *WebWorkspaceNavigationDto      `json:"navigation"`
	Page            *WebWorkspacePageDto            `json:"page"`
	ProjectCreation *WebWorkspaceProjectCreationDto `json:"project_creation"`
}

// WebWorkspaceStreamTicketDto carries the one-time ticket for a stream attach.
// StreamUrl is a same-origin API path; the client derives the ws/wss URL from
// the page origin and never receives an internal address.
type WebWorkspaceStreamTicketDto struct {
	Ticket    string `json:"ticket"`
	ExpiresAt int64  `json:"expires_at"`
	StreamUrl string `json:"stream_url"`
}
