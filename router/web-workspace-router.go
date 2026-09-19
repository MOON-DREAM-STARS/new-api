package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// SetWebWorkspaceRouter registers the Web Workspace API. Every control-plane
// route requires an authenticated dashboard user; the workspace is always
// derived from that user, never from a client-supplied identifier.
func SetWebWorkspaceRouter(apiRouter *gin.RouterGroup) {
	webWorkspaceRoute := apiRouter.Group("/web-workspace")
	webWorkspaceRoute.Use(middleware.UserAuth())
	{
		webWorkspaceRoute.GET("/config", controller.GetWebWorkspaceConfig)
		webWorkspaceRoute.GET("/status", controller.GetWebWorkspaceStatus)
		webWorkspaceRoute.GET("/projects", controller.GetWebWorkspaceProjects)
		webWorkspaceRoute.POST("/projects", controller.CreateWebWorkspaceProject)
		webWorkspaceRoute.GET("/projects/:id", controller.GetWebWorkspaceProject)
		webWorkspaceRoute.PATCH("/projects/:id", controller.UpdateWebWorkspaceProject)
		webWorkspaceRoute.DELETE("/projects/:id", controller.DeleteWebWorkspaceProject)
		webWorkspaceRoute.GET("/projects/:id/conversations", controller.GetWebWorkspaceProjectConversations)

		webWorkspaceRoute.GET("/session", controller.GetWebWorkspaceSession)
		webWorkspaceRoute.POST("/session", controller.StartWebWorkspaceSession)
		webWorkspaceRoute.DELETE("/session/:id", controller.StopWebWorkspaceSession)
		webWorkspaceRoute.POST("/session/:id/restart", controller.RestartWebWorkspaceSession)
		webWorkspaceRoute.POST("/session/:id/navigation", controller.NavigateWebWorkspaceSession)
		webWorkspaceRoute.POST("/session/:id/activity", controller.TouchWebWorkspaceActivity)
		webWorkspaceRoute.POST("/session/:id/stream-ticket", controller.CreateWebWorkspaceStreamTicket)
	}

	// The stream attach authenticates with a single-use ticket instead of a
	// bearer header, because browser WebSocket clients cannot set headers. The
	// ticket is bound to the user, workspace and session, and the gateway is
	// the only path to the Browser Agent.
	webWorkspaceStreamRoute := apiRouter.Group("/web-workspace")
	{
		webWorkspaceStreamRoute.GET("/session/:id/stream", controller.WebWorkspaceStream)
	}
}
