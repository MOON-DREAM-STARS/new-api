package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/webworkspace"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const (
	webWorkspaceCodeUnauthenticated   = "WEB_WORKSPACE_UNAUTHENTICATED"
	webWorkspaceCodeEntitlementDenied = "WEB_WORKSPACE_ENTITLEMENT_DENIED"
	webWorkspaceCodeResourceNotFound  = "WEB_WORKSPACE_RESOURCE_NOT_FOUND"
	webWorkspaceCodeInvalidRequest    = "WEB_WORKSPACE_INVALID_REQUEST"
	webWorkspaceCodeInternalError     = "WEB_WORKSPACE_INTERNAL_ERROR"
)

// GetWebWorkspaceConfig is the capability probe. It answers whether the feature
// exists and whether the current user may use it, without touching any
// workspace resource, so it stays callable while the feature is disabled.
func GetWebWorkspaceConfig(c *gin.Context) {
	settings := system_setting.GetWebWorkspaceSettings()
	entitled := false
	if userID := c.GetInt("id"); userID > 0 {
		if user, err := model.GetUserById(userID, false); err == nil && user != nil {
			entitled = webworkspace.CheckEntitlement(user).Allowed
		}
	}
	common.ApiSuccess(c, dto.WebWorkspaceConfig{
		Enabled:  settings.Enabled,
		Entitled: entitled,
	})
}

// GetWebWorkspaceStatus reports the caller's own workspace state. A workspace
// that has not been created yet is not an error.
func GetWebWorkspaceStatus(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	status := dto.WebWorkspaceStatus{
		Entitled: true,
		Reason:   webworkspace.ReasonOK,
	}
	workspace, err := webworkspace.GetWorkspaceByUserID(user.Id)
	if err != nil && !errors.Is(err, webworkspace.ErrResourceNotFound) {
		writeWebWorkspaceInternalError(c)
		return
	}
	if err == nil {
		status.Workspace = &dto.WebWorkspaceSummary{
			Provider:     workspace.Provider,
			Status:       workspace.Status,
			CreatedAt:    workspace.CreatedAt,
			LastActiveAt: workspace.LastActiveAt,
		}
	}
	common.ApiSuccess(c, status)
}

// GetWebWorkspaceProjects lists only the projects owned by the current user.
func GetWebWorkspaceProjects(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	projects, err := webworkspace.ListOwnedProjects(user.Id)
	if err != nil {
		writeWebWorkspaceInternalError(c)
		return
	}
	items := make([]dto.WebProjectDto, 0, len(projects))
	for i := range projects {
		items = append(items, toWebProjectDto(&projects[i]))
	}
	common.ApiSuccess(c, dto.WebProjectListResponse{Items: items})
}

// GetWebWorkspaceProject returns one owned project.
func GetWebWorkspaceProject(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	projectID, ok := parseWebWorkspaceProjectID(c)
	if !ok {
		return
	}
	project, err := webworkspace.GetOwnedProject(user.Id, projectID)
	if err != nil {
		writeWebWorkspaceResourceError(c, err)
		return
	}
	common.ApiSuccess(c, toWebProjectDto(project))
}

// UpdateWebWorkspaceProject renames the local project mapping. Provider-side
// rename lands with the Phase 4 adapter.
func UpdateWebWorkspaceProject(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	projectID, ok := parseWebWorkspaceProjectID(c)
	if !ok {
		return
	}
	var request dto.WebProjectRenameRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid request body", "")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > 255 {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "name must be 1-255 characters", "")
		return
	}
	project, err := webworkspace.RenameOwnedProject(user.Id, projectID, name)
	if err != nil {
		writeWebWorkspaceResourceError(c, err)
		return
	}
	common.ApiSuccess(c, toWebProjectDto(project))
}

// DeleteWebWorkspaceProject removes the local project mapping and its
// conversation mappings. Provider-side deletion lands with the Phase 4 adapter.
func DeleteWebWorkspaceProject(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	projectID, ok := parseWebWorkspaceProjectID(c)
	if !ok {
		return
	}
	if err := webworkspace.DeleteOwnedProject(user.Id, projectID); err != nil {
		writeWebWorkspaceResourceError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": projectID})
}

// GetWebWorkspaceProjectConversations lists the conversations of one owned
// project. Ownership is resolved before listing so an unknown or foreign
// project cannot be distinguished from any other unknown resource.
func GetWebWorkspaceProjectConversations(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	projectID, ok := parseWebWorkspaceProjectID(c)
	if !ok {
		return
	}
	if _, err := webworkspace.GetOwnedProject(user.Id, projectID); err != nil {
		writeWebWorkspaceResourceError(c, err)
		return
	}
	conversations, err := webworkspace.ListOwnedConversations(user.Id, projectID)
	if err != nil {
		writeWebWorkspaceInternalError(c)
		return
	}
	items := make([]dto.WebConversationDto, 0, len(conversations))
	for i := range conversations {
		items = append(items, toWebConversationDto(&conversations[i]))
	}
	common.ApiSuccess(c, dto.WebConversationListResponse{Items: items})
}

// requireWebWorkspaceEntitlement loads the authenticated user and applies the
// feature entitlement gate. It writes the denial response itself and returns
// nil when access must not continue.
func requireWebWorkspaceEntitlement(c *gin.Context) *model.User {
	userID := c.GetInt("id")
	if userID <= 0 {
		writeWebWorkspaceError(c, http.StatusUnauthorized, webWorkspaceCodeUnauthenticated, "authentication required", "")
		return nil
	}
	user, err := model.GetUserById(userID, false)
	if err != nil || user == nil {
		writeWebWorkspaceError(c, http.StatusUnauthorized, webWorkspaceCodeUnauthenticated, "authentication required", "")
		return nil
	}
	decision := webworkspace.CheckEntitlement(user)
	if !decision.Allowed {
		writeWebWorkspaceError(c, http.StatusForbidden, webWorkspaceCodeEntitlementDenied, "Web Workspace is not available for this account", decision.Reason)
		return nil
	}
	return user
}

func parseWebWorkspaceProjectID(c *gin.Context) (int, bool) {
	projectID, err := strconv.Atoi(c.Param("id"))
	if err != nil || projectID <= 0 {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid project id", "")
		return 0, false
	}
	return projectID, true
}

func writeWebWorkspaceResourceError(c *gin.Context, err error) {
	if errors.Is(err, webworkspace.ErrResourceNotFound) {
		writeWebWorkspaceError(c, http.StatusNotFound, webWorkspaceCodeResourceNotFound, "web workspace resource not found", "")
		return
	}
	writeWebWorkspaceInternalError(c)
}

func writeWebWorkspaceInternalError(c *gin.Context) {
	writeWebWorkspaceError(c, http.StatusInternalServerError, webWorkspaceCodeInternalError, "web workspace is temporarily unavailable", "")
}

func writeWebWorkspaceError(c *gin.Context, status int, code string, message string, reason string) {
	body := gin.H{
		"success": false,
		"code":    code,
		"message": message,
	}
	if reason != "" {
		body["reason"] = reason
	}
	c.AbortWithStatusJSON(status, body)
}

func toWebProjectDto(project *model.WebProject) dto.WebProjectDto {
	return dto.WebProjectDto{
		Id:        project.Id,
		Provider:  project.Provider,
		Name:      project.Name,
		CreatedAt: project.CreatedAt,
		UpdatedAt: project.UpdatedAt,
	}
}

func toWebConversationDto(conversation *model.WebConversation) dto.WebConversationDto {
	return dto.WebConversationDto{
		Id:        conversation.Id,
		Title:     conversation.Title,
		CreatedAt: conversation.CreatedAt,
		UpdatedAt: conversation.UpdatedAt,
	}
}
