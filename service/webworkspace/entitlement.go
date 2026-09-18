package webworkspace

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// Entitlement reasons. They explain an API denial to the affected user without
// exposing anything about other users or resources.
const (
	ReasonOK              = "ok"
	ReasonUnauthenticated = "unauthenticated"
	ReasonUserDisabled    = "user_disabled"
	ReasonGlobalDisabled  = "global_disabled"
	ReasonRole            = "role"
	ReasonGroup           = "group"
)

// Decision is the feature entitlement result. Entitlement answers "may this
// user use Web Workspace at all"; resource ownership is never part of it.
type Decision struct {
	Allowed bool
	Reason  string
}

// CheckEntitlement applies the Phase 1 entitlement gate:
//
//	global switch AND enabled user AND role >= minimum_role AND group allowed
//
// The per-user override documented in the architecture is intentionally not
// implemented in the first version, so it is not an input here. The check fails
// closed: a missing user or disabled feature denies access.
func CheckEntitlement(user *model.User) Decision {
	if user == nil {
		return Decision{Allowed: false, Reason: ReasonUnauthenticated}
	}
	settings := system_setting.GetWebWorkspaceSettings()
	if !settings.Enabled {
		return Decision{Allowed: false, Reason: ReasonGlobalDisabled}
	}
	if user.Status != common.UserStatusEnabled {
		return Decision{Allowed: false, Reason: ReasonUserDisabled}
	}
	if user.Role < settings.MinimumRole {
		return Decision{Allowed: false, Reason: ReasonRole}
	}
	if len(settings.AllowedGroups) > 0 && !containsGroup(settings.AllowedGroups, user.Group) {
		return Decision{Allowed: false, Reason: ReasonGroup}
	}
	return Decision{Allowed: true, Reason: ReasonOK}
}

func containsGroup(groups []string, group string) bool {
	for _, candidate := range groups {
		if candidate == group {
			return true
		}
	}
	return false
}
