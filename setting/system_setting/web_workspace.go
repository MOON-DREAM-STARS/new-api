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
}

var defaultWebWorkspaceSettings = WebWorkspaceSettings{
	Enabled:       false,
	MinimumRole:   common.RoleCommonUser,
	AllowedGroups: []string{},
}

func init() {
	config.GlobalConfig.Register("web_workspace", &defaultWebWorkspaceSettings)
}

func GetWebWorkspaceSettings() *WebWorkspaceSettings {
	return &defaultWebWorkspaceSettings
}
