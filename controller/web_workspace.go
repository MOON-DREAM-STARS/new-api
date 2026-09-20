package controller

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/webworkspace"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	webWorkspaceCodeUnauthenticated   = "WEB_WORKSPACE_UNAUTHENTICATED"
	webWorkspaceCodeEntitlementDenied = "WEB_WORKSPACE_ENTITLEMENT_DENIED"
	webWorkspaceCodeResourceNotFound  = "WEB_WORKSPACE_RESOURCE_NOT_FOUND"
	webWorkspaceCodeInvalidRequest    = "WEB_WORKSPACE_INVALID_REQUEST"
	webWorkspaceCodeInternalError     = "WEB_WORKSPACE_INTERNAL_ERROR"

	// webWorkspaceModeLocked is the only runtime mode transition this API exposes:
	// leaving the operator-opened sign-in window and locking the provider session.
	webWorkspaceModeLocked = "LOCKED"

	// Accepted range of the optional remote screen size proposal. The Browser
	// Agent enforces the same range, so an out-of-range proposal is refused on
	// both sides instead of silently changing the remote display.
	webWorkspaceMinScreenWidth  = 640
	webWorkspaceMaxScreenWidth  = 3840
	webWorkspaceMinScreenHeight = 360
	webWorkspaceMaxScreenHeight = 2160
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
	maxScreenWidth, maxScreenHeight := system_setting.NormalizeWebWorkspaceScreenBounds(settings)
	common.ApiSuccess(c, dto.WebWorkspaceConfig{
		Enabled:         settings.Enabled,
		Entitled:        entitled,
		MaxScreenWidth:  maxScreenWidth,
		MaxScreenHeight: maxScreenHeight,
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

// GetWebWorkspaceProjects synchronises the guard observations first and then
// lists only the projects owned by the current user. A failed pull or apply is
// never reported as success and leaves the agent offset unacknowledged.
func GetWebWorkspaceProjects(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	workspace, err := webworkspace.GetWorkspaceByUserID(user.Id)
	if err != nil && !errors.Is(err, webworkspace.ErrResourceNotFound) {
		writeWebWorkspaceInternalError(c)
		return
	}
	// A user who never started a session has no runtime and no observations, so
	// there is nothing to synchronise yet.
	if err == nil {
		if err := webworkspace.SyncWorkspace(c.Request.Context(), workspace.Id); err != nil {
			writeWebWorkspaceSessionError(c, err)
			return
		}
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

// UpdateWebWorkspaceProject renames the local project mapping and republishes
// the ownership document so the guard and the control plane stay consistent.
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
	name, ok := webworkspace.NormalizeProjectDisplayName(request.Name)
	if !ok {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "name must use user-project format with 1-24 letters, digits or underscores per part", "")
		return
	}
	project, err := webworkspace.RenameOwnedProject(user.Id, projectID, name)
	if err != nil {
		writeWebWorkspaceResourceError(c, err)
		return
	}
	if err := webworkspace.PushOwnership(c.Request.Context(), project.WorkspaceId); err != nil {
		writeWebWorkspaceOwnershipError(c, err)
		return
	}
	common.ApiSuccess(c, toWebProjectDto(project))
}

// DeleteWebWorkspaceProject deletes the provider-side project through the
// runtime guard, then removes the local mapping and its conversations and
// revokes the project in the guard's ownership document.
func DeleteWebWorkspaceProject(c *gin.Context) {
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
	if err := webworkspace.DeleteOwnedProjectWithProvider(c.Request.Context(), user.Id, projectID); err != nil {
		writeWebWorkspaceProjectDeletionError(c, err)
		return
	}
	if err := webworkspace.PushOwnership(c.Request.Context(), project.WorkspaceId); err != nil {
		writeWebWorkspaceOwnershipError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": projectID})
}

// CreateWebWorkspaceProject issues a short-lived creation permit for one new
// provider-side project. The local row is created later from the guard's
// project_created observation, so a permit alone never registers a project the
// user did not actually open.
func CreateWebWorkspaceProject(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	var request dto.WebProjectCreateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid request body", "")
		return
	}
	settings := system_setting.GetWebWorkspaceSettings()
	permit, err := webworkspace.IssueProjectPermit(c.Request.Context(), user.Id, settings.MaxProjects, request.Name)
	switch {
	case errors.Is(err, webworkspace.ErrSessionRequired):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeSessionRequired, "a running web workspace session is required", "")
		return
	case errors.Is(err, webworkspace.ErrProjectLimitReached):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeProjectLimit, "web workspace project limit reached", "")
		return
	case errors.Is(err, webworkspace.ErrInvalidProjectName):
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "name must use user-project format with 1-24 letters, digits or underscores per part", "")
		return
	case errors.Is(err, webworkspace.ErrProjectCreationInProgress):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeProjectCreationInProgress, "a project creation is already running", "")
		return
	case err != nil:
		writeWebWorkspaceSessionError(c, err)
		return
	}
	common.ApiSuccess(c, dto.WebWorkspaceProjectPermitDto{PermitId: permit.PermitId, ExpiresAt: permit.ExpiresAt})
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

func writeWebWorkspaceProjectDeletionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, webworkspace.ErrLastProjectRequired):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeLastProjectRequired, "at least one project must remain", "")
	case errors.Is(err, webworkspace.ErrAgentProjectDeletionTimeout):
		writeWebWorkspaceError(c, http.StatusGatewayTimeout, webWorkspaceCodeProjectDeletionTimeout, "provider project deletion timed out", "")
	case errors.Is(err, webworkspace.ErrAgentProjectDeletionUnavailable), errors.Is(err, webworkspace.ErrAgentRuntimeNotFound):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeProjectDeletionUnavailable, "provider project deletion is unavailable", "")
	case errors.Is(err, webworkspace.ErrAgentProjectDeletionRejected):
		writeWebWorkspaceError(c, http.StatusUnprocessableEntity, webWorkspaceCodeProjectDeletionRejected, "provider project deletion failed", "")
	case errors.Is(err, webworkspace.ErrAgentUnavailable), errors.Is(err, webworkspace.ErrAgentRejected):
		writeWebWorkspaceError(c, http.StatusServiceUnavailable, webWorkspaceCodeAgentUnavailable, "web workspace agent unavailable", "")
	default:
		writeWebWorkspaceResourceError(c, err)
	}
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

const (
	webWorkspaceCodeAgentUnavailable      = "WEB_WORKSPACE_AGENT_UNAVAILABLE"
	webWorkspaceCodeSessionNotFound       = "WEB_WORKSPACE_SESSION_NOT_FOUND"
	webWorkspaceCodeSessionRequired       = "WEB_WORKSPACE_SESSION_REQUIRED"
	webWorkspaceCodeProjectLimit          = "WEB_WORKSPACE_PROJECT_LIMIT"
	webWorkspaceCodeLastProjectRequired   = "WEB_WORKSPACE_LAST_PROJECT_REQUIRED"
	webWorkspaceCodeTicketInvalid         = "WEB_WORKSPACE_TICKET_INVALID"
	webWorkspaceCodeNavigationTimeout     = "WEB_WORKSPACE_NAVIGATION_TIMEOUT"
	webWorkspaceCodeNavigationUnavailable = "WEB_WORKSPACE_NAVIGATION_UNAVAILABLE"

	// webWorkspaceCodeProjectCreationInProgress refuses a second creation while
	// the guard is still running one for the same workspace.
	webWorkspaceCodeProjectCreationInProgress  = "WEB_WORKSPACE_PROJECT_CREATION_IN_PROGRESS"
	webWorkspaceCodeProjectDeletionTimeout     = "WEB_WORKSPACE_PROJECT_DELETION_TIMEOUT"
	webWorkspaceCodeProjectDeletionUnavailable = "WEB_WORKSPACE_PROJECT_DELETION_UNAVAILABLE"
	webWorkspaceCodeProjectDeletionRejected    = "WEB_WORKSPACE_PROJECT_DELETION_REJECTED"
	// webWorkspaceCodeCapacityReached reports that the agent's global
	// active-runtime cap is full.
	webWorkspaceCodeCapacityReached = "WEB_WORKSPACE_CAPACITY_REACHED"
)

// StartWebWorkspaceSession starts (or reuses) the caller's browser runtime. The
// workspace is derived from the authenticated user; the runtime lives in the
// Browser Agent and is never reachable from the client directly.
func StartWebWorkspaceSession(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	// The body is optional: a start without one keeps the agent default size.
	var request dto.WebWorkspaceStartRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil && !errors.Is(err, io.EOF) {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid request body", "")
		return
	}
	width, height, ok := webWorkspaceScreenSize(request.ScreenWidth, request.ScreenHeight)
	if !ok {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid screen size", "")
		return
	}
	session, err := webworkspace.StartSession(c.Request.Context(), user.Id, width, height)
	if err != nil {
		writeWebWorkspaceSessionError(c, err)
		return
	}
	common.ApiSuccess(c, toWebWorkspaceSessionDto(session))
}

// GetWebWorkspaceSession returns the caller's current session, or null. A live
// session is re-read from the agent first, so the client sees the real runtime
// state, the real navigation capability and the real transferred bytes instead
// of a stale cache. A refresh that cannot reach the agent keeps the cached view
// instead of turning the poll into an error.
func GetWebWorkspaceSession(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	session, ok := webworkspace.CurrentSession(user.Id)
	if !ok {
		common.ApiSuccess(c, nil)
		return
	}
	if webworkspace.LiveRuntimeState(session.State) {
		refreshed, err := webworkspace.RefreshSession(c.Request.Context(), user.Id, session.Id)
		if err == nil {
			session = refreshed
		} else if !errors.Is(err, webworkspace.ErrAgentUnavailable) &&
			!errors.Is(err, webworkspace.ErrAgentRejected) &&
			!errors.Is(err, webworkspace.ErrSessionNotFound) {
			writeWebWorkspaceSessionError(c, err)
			return
		}
	}
	common.ApiSuccess(c, toWebWorkspaceSessionDto(session))
}

// StopWebWorkspaceSession stops the runtime and drops the session. The browser
// profile is retained.
func StopWebWorkspaceSession(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	sessionId := c.Param("id")
	if sessionId == "" {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid session id", "")
		return
	}
	if err := webworkspace.StopSession(c.Request.Context(), user.Id, sessionId); err != nil {
		writeWebWorkspaceSessionError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": sessionId})
}

// RestartWebWorkspaceSession restarts the runtime behind one session.
func RestartWebWorkspaceSession(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	sessionId := c.Param("id")
	if sessionId == "" {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid session id", "")
		return
	}
	// The body is optional: a restart without one keeps the current runtime
	// mode, and only the explicit "signed in, lock the session" transition is
	// reachable from this API. Opening the LOGIN window stays an operator action.
	var request dto.WebWorkspaceRestartRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil && !errors.Is(err, io.EOF) {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid request body", "")
		return
	}
	mode := strings.TrimSpace(request.Mode)
	if mode != "" && mode != webWorkspaceModeLocked {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid runtime mode", "")
		return
	}
	width, height, sizeOK := webWorkspaceScreenSize(request.ScreenWidth, request.ScreenHeight)
	if !sizeOK {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid screen size", "")
		return
	}
	session, err := webworkspace.RestartSession(c.Request.Context(), user.Id, sessionId, mode, width, height)
	if err != nil {
		writeWebWorkspaceSessionError(c, err)
		return
	}
	common.ApiSuccess(c, toWebWorkspaceSessionDto(session))
}

// NavigateWebWorkspaceSession applies one navigation command to the caller's
// runtime and returns the updated session DTO. The response never contains a
// browser URL, runtime address or agent credential.
func NavigateWebWorkspaceSession(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	sessionId := c.Param("id")
	if sessionId == "" {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid session id", "")
		return
	}
	var request dto.WebWorkspaceNavigationRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid request body", "")
		return
	}
	session, err := webworkspace.NavigateSession(c.Request.Context(), user.Id, sessionId, request.Action, request.ProjectID)
	if err != nil {
		writeWebWorkspaceNavigationError(c, err)
		return
	}
	common.ApiSuccess(c, toWebWorkspaceSessionDto(session))
}

// webWorkspaceScreenSize validates the optional remote screen size proposal.
// Both dimensions must be given together and inside the range the Browser Agent
// accepts, or both omitted so the agent keeps its current or default size.
func webWorkspaceScreenSize(width int, height int) (int, int, bool) {
	if width == 0 && height == 0 {
		return 0, 0, true
	}
	if width < webWorkspaceMinScreenWidth || width > webWorkspaceMaxScreenWidth {
		return 0, 0, false
	}
	if height < webWorkspaceMinScreenHeight || height > webWorkspaceMaxScreenHeight {
		return 0, 0, false
	}
	return width, height, true
}

// TouchWebWorkspaceActivity keeps a live session alive without attaching a
// display stream. A hidden tab uses it so the runtime is not reclaimed while the
// user is away; it never touches the provider page itself.
func TouchWebWorkspaceActivity(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	sessionId := c.Param("id")
	if sessionId == "" {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid session id", "")
		return
	}
	session, err := webworkspace.TouchRuntimeActivity(c.Request.Context(), user.Id, sessionId)
	if err != nil {
		writeWebWorkspaceSessionError(c, err)
		return
	}
	common.ApiSuccess(c, toWebWorkspaceSessionDto(session))
}

// CreateWebWorkspaceStreamTicket issues a single-use ticket that the client
// redeems on the WSS stream endpoint.
func CreateWebWorkspaceStreamTicket(c *gin.Context) {
	user := requireWebWorkspaceEntitlement(c)
	if user == nil {
		return
	}
	sessionId := c.Param("id")
	if sessionId == "" {
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid session id", "")
		return
	}
	// Refresh first so a runtime stopped by the agent idle timeout cannot get a
	// fresh ticket from stale control-plane state.
	session, err := webworkspace.RefreshSession(c.Request.Context(), user.Id, sessionId)
	if err != nil {
		writeWebWorkspaceSessionError(c, err)
		return
	}
	if !webworkspace.LiveRuntimeState(session.State) {
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeAgentUnavailable, "web workspace runtime is not running", "")
		return
	}
	ticket, expiresAt, err := webworkspace.IssueStreamTicket(session)
	if err != nil {
		writeWebWorkspaceInternalError(c)
		return
	}
	common.ApiSuccess(c, dto.WebWorkspaceStreamTicketDto{
		Ticket:    ticket,
		ExpiresAt: expiresAt,
		StreamUrl: "/api/web-workspace/session/" + session.Id + "/stream",
	})
}

var webWorkspaceStreamUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" {
			return false
		}
		return strings.EqualFold(parsed.Host, r.Host)
	},
}

// WebWorkspaceStream is the WSS gateway: it authenticates the one-time ticket,
// connects to the Browser Agent over the private network and proxies the raw
// display stream. The client never learns the agent address or any credential.
func WebWorkspaceStream(c *gin.Context) {
	sessionId := c.Param("id")
	ticket, err := webworkspace.ConsumeStreamTicket(c.Query("ticket"), sessionId)
	if err != nil {
		writeWebWorkspaceError(c, http.StatusForbidden, webWorkspaceCodeTicketInvalid, "invalid stream ticket", "")
		return
	}
	session, err := webworkspace.GetSession(ticket.UserId, sessionId)
	if err != nil || session.WorkspaceId != ticket.WorkspaceId {
		writeWebWorkspaceError(c, http.StatusNotFound, webWorkspaceCodeSessionNotFound, "web workspace session not found", "")
		return
	}
	if !webworkspace.LiveRuntimeState(session.State) {
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeAgentUnavailable, "web workspace runtime is not running", "")
		return
	}

	agentConn, agentResponse, err := webworkspace.DialAgentStream(c.Request.Context(), session.WorkspaceId)
	if err != nil {
		if agentResponse != nil && agentResponse.Body != nil {
			_ = agentResponse.Body.Close()
		}
		writeWebWorkspaceSessionError(c, err)
		return
	}
	defer func() { _ = agentConn.Close() }()

	clientConn, err := webWorkspaceStreamUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = clientConn.Close() }()

	_ = webworkspace.TouchSession(ticket.UserId, sessionId)
	proxyWebWorkspaceStream(clientConn, agentConn)
	_ = webworkspace.MarkSessionIdle(ticket.UserId, sessionId)
}

// proxyWebWorkspaceStream pumps messages in both directions until either side
// closes, then closes the peer so the other pump can finish.
func proxyWebWorkspaceStream(client *websocket.Conn, agent *websocket.Conn) {
	done := make(chan struct{}, 2)
	pump := func(source *websocket.Conn, target *websocket.Conn) {
		defer func() { done <- struct{}{} }()
		for {
			messageType, payload, err := source.ReadMessage()
			if err != nil {
				return
			}
			if err := target.WriteMessage(messageType, payload); err != nil {
				return
			}
		}
	}
	go pump(client, agent)
	go pump(agent, client)
	<-done
	_ = client.Close()
	_ = agent.Close()
	<-done
}

// writeWebWorkspaceOwnershipError reports an ownership push that failed after a
// local change was committed. The client must not see success while the guard
// still enforces the previous ownership document.
func writeWebWorkspaceOwnershipError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, webworkspace.ErrAgentUnavailable),
		errors.Is(err, webworkspace.ErrAgentRejected),
		errors.Is(err, webworkspace.ErrAgentRuntimeNotFound):
		writeWebWorkspaceError(c, http.StatusServiceUnavailable, webWorkspaceCodeAgentUnavailable, "web workspace agent unavailable", "")
	default:
		writeWebWorkspaceInternalError(c)
	}
}

func writeWebWorkspaceSessionError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, webworkspace.ErrSessionNotFound), errors.Is(err, webworkspace.ErrAgentRuntimeNotFound):
		writeWebWorkspaceError(c, http.StatusNotFound, webWorkspaceCodeSessionNotFound, "web workspace session not found", "")
	case errors.Is(err, webworkspace.ErrCapacityReached):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeCapacityReached, "web workspace capacity reached", "")
	case errors.Is(err, webworkspace.ErrAgentUnavailable), errors.Is(err, webworkspace.ErrAgentRejected):
		writeWebWorkspaceError(c, http.StatusServiceUnavailable, webWorkspaceCodeAgentUnavailable, "web workspace agent unavailable", "")
	case errors.Is(err, webworkspace.ErrResourceNotFound):
		writeWebWorkspaceError(c, http.StatusNotFound, webWorkspaceCodeResourceNotFound, "web workspace resource not found", "")
	default:
		writeWebWorkspaceInternalError(c)
	}
}

func writeWebWorkspaceNavigationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, webworkspace.ErrInvalidNavigationAction):
		writeWebWorkspaceError(c, http.StatusBadRequest, webWorkspaceCodeInvalidRequest, "invalid navigation action", "")
	case errors.Is(err, webworkspace.ErrAgentNavigationTimeout):
		writeWebWorkspaceError(c, http.StatusGatewayTimeout, webWorkspaceCodeNavigationTimeout, "web workspace navigation timed out", "")
	case errors.Is(err, webworkspace.ErrAgentNavigationUnavailable):
		writeWebWorkspaceError(c, http.StatusConflict, webWorkspaceCodeNavigationUnavailable, "web workspace navigation unavailable", "")
	case errors.Is(err, webworkspace.ErrSessionNotFound), errors.Is(err, webworkspace.ErrAgentRuntimeNotFound):
		writeWebWorkspaceError(c, http.StatusNotFound, webWorkspaceCodeSessionNotFound, "web workspace session not found", "")
	case errors.Is(err, webworkspace.ErrResourceNotFound):
		writeWebWorkspaceError(c, http.StatusNotFound, webWorkspaceCodeResourceNotFound, "web workspace resource not found", "")
	case errors.Is(err, webworkspace.ErrAgentUnavailable), errors.Is(err, webworkspace.ErrAgentRejected):
		writeWebWorkspaceError(c, http.StatusServiceUnavailable, webWorkspaceCodeAgentUnavailable, "web workspace agent unavailable", "")
	default:
		writeWebWorkspaceInternalError(c)
	}
}

func toWebWorkspaceSessionDto(session *webworkspace.Session) dto.WebWorkspaceSessionDto {
	result := dto.WebWorkspaceSessionDto{
		SessionId:      session.Id,
		State:          session.State,
		Mode:           session.Mode,
		CreatedAt:      session.CreatedAt,
		LastSeenAt:     session.LastSeenAt,
		IdleDeadlineAt: session.IdleDeadlineAt,
		StreamBytesOut: session.StreamBytesOut,
		StreamBytesIn:  session.StreamBytesIn,
	}
	if session.Navigation != nil {
		result.Navigation = &dto.WebWorkspaceNavigationDto{
			CanGoBack:    session.Navigation.CanGoBack,
			CanGoForward: session.Navigation.CanGoForward,
			UpdatedAt:    session.Navigation.UpdatedAt,
		}
	}
	if session.Page != nil {
		result.Page = &dto.WebWorkspacePageDto{
			State:     session.Page.State,
			Error:     session.Page.Error,
			Attempts:  session.Page.Attempts,
			UpdatedAt: session.Page.UpdatedAt,
		}
	}
	if session.ProjectCreation != nil {
		result.ProjectCreation = &dto.WebWorkspaceProjectCreationDto{
			PermitId:  session.ProjectCreation.PermitId,
			State:     session.ProjectCreation.State,
			Error:     session.ProjectCreation.Error,
			UpdatedAt: session.ProjectCreation.UpdatedAt,
		}
	}
	return result
}
