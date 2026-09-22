package webworkspace

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
)

func TestWebWorkspaceEntitlementMatrix(t *testing.T) {
	settings := system_setting.GetWebWorkspaceSettings()
	original := *settings
	t.Cleanup(func() { *settings = original })

	enabledUser := &model.User{Id: 1, Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}
	groupUser := &model.User{Id: 2, Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "vip"}
	disabledUser := &model.User{Id: 3, Role: common.RoleCommonUser, Status: 2, Group: "default"}
	rootUser := &model.User{Id: 4, Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}

	tests := []struct {
		name     string
		settings system_setting.WebWorkspaceSettings
		user     *model.User
		allowed  bool
		reason   string
	}{
		{
			name:     "feature disabled globally",
			settings: system_setting.WebWorkspaceSettings{Enabled: false, MinimumRole: common.RoleCommonUser},
			user:     enabledUser,
			allowed:  false,
			reason:   ReasonGlobalDisabled,
		},
		{
			name:     "user status disabled",
			settings: system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleCommonUser},
			user:     disabledUser,
			allowed:  false,
			reason:   ReasonUserDisabled,
		},
		{
			name:     "role below minimum",
			settings: system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleAdminUser},
			user:     enabledUser,
			allowed:  false,
			reason:   ReasonRole,
		},
		{
			name:     "group not allowed",
			settings: system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleCommonUser, AllowedGroups: []string{"vip"}},
			user:     enabledUser,
			allowed:  false,
			reason:   ReasonGroup,
		},
		{
			name:     "admin is not entitled without a group match",
			settings: system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleCommonUser, AllowedGroups: []string{"vip"}},
			user:     rootUser,
			allowed:  false,
			reason:   ReasonGroup,
		},
		{
			name:     "group allowed",
			settings: system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleCommonUser, AllowedGroups: []string{"vip"}},
			user:     groupUser,
			allowed:  true,
			reason:   ReasonOK,
		},
		{
			name:     "empty group list means no group restriction",
			settings: system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleCommonUser, AllowedGroups: []string{}},
			user:     enabledUser,
			allowed:  true,
			reason:   ReasonOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			*settings = test.settings
			decision := CheckEntitlement(test.user)
			assert.Equal(t, test.allowed, decision.Allowed)
			assert.Equal(t, test.reason, decision.Reason)
		})
	}
}

func TestWebWorkspaceEntitlementNilUserDenied(t *testing.T) {
	settings := system_setting.GetWebWorkspaceSettings()
	original := *settings
	t.Cleanup(func() { *settings = original })
	*settings = system_setting.WebWorkspaceSettings{Enabled: true, MinimumRole: common.RoleCommonUser}

	decision := CheckEntitlement(nil)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonUnauthenticated, decision.Reason)
}
