package system_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// WebWorkspaceSettings answers one question only: may this user use the Web
// Workspace feature at all. Resource ownership is never decided here; it is
// always resolved from the database relationship chain.
//
// An empty AllowedGroups slice means the group check is not restricted; when it
// is non-empty, the user's group must match one entry exactly.
type WebWorkspaceSettings struct {
	Enabled       bool     `json:"enabled"`
	MinimumRole   int      `json:"minimum_role"`
	AllowedGroups []string `json:"allowed_groups"`
	// MaxProjects caps the number of projects one workspace may register. Zero
	// means no limit.
	MaxProjects int `json:"max_projects"`
	// MaxScreenWidth and MaxScreenHeight bound the remote framebuffer a client
	// may propose. They are a presentation budget, not a security boundary.
	MaxScreenWidth  int `json:"max_screen_width"`
	MaxScreenHeight int `json:"max_screen_height"`
	// AgentBaseURL is the private Browser Agent endpoint, for example
	// http://browser-agent:8730. The service token is never stored here; it is
	// read from the WEB_WORKSPACE_AGENT_TOKEN environment variable.
	AgentBaseURL string `json:"agent_base_url"`
}

const (
	WebWorkspaceMinScreenWidth  = 640
	WebWorkspaceMaxScreenWidth  = 3840
	WebWorkspaceMinScreenHeight = 720
	WebWorkspaceMaxScreenHeight = 1440
)

// NormalizeWebWorkspaceScreenBounds returns the configured presentation bounds
// after validating them against the same hard limits the agent enforces. An
// invalid or legacy setting falls back to the local default rather than
// silently disabling the workspace.
func NormalizeWebWorkspaceScreenBounds(settings *WebWorkspaceSettings) (int, int) {
	if settings == nil {
		return WebWorkspaceMaxScreenWidth, WebWorkspaceMaxScreenHeight
	}
	width := settings.MaxScreenWidth
	if width < WebWorkspaceMinScreenWidth || width > WebWorkspaceMaxScreenWidth {
		width = WebWorkspaceMaxScreenWidth
	}
	height := settings.MaxScreenHeight
	if height < WebWorkspaceMinScreenHeight || height > WebWorkspaceMaxScreenHeight {
		height = WebWorkspaceMaxScreenHeight
	}
	return width, height
}

var defaultWebWorkspaceSettings = WebWorkspaceSettings{
	Enabled:         false,
	MinimumRole:     common.RoleCommonUser,
	AllowedGroups:   []string{},
	MaxProjects:     0,
	MaxScreenWidth:  WebWorkspaceMaxScreenWidth,
	MaxScreenHeight: WebWorkspaceMaxScreenHeight,
	AgentBaseURL:    "",
}

func init() {
	config.GlobalConfig.Register("web_workspace", &defaultWebWorkspaceSettings)
}

func GetWebWorkspaceSettings() *WebWorkspaceSettings {
	return &defaultWebWorkspaceSettings
}
