package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// SetWebWorkspaceRouter registers the Web Workspace API. Every route requires
// an authenticated dashboard user; the workspace is always derived from that
// user, never from a client-supplied identifier.
func SetWebWorkspaceRouter(apiRouter *gin.RouterGroup) {
	webWorkspaceRoute := apiRouter.Group("/web-workspace")
	webWorkspaceRoute.Use(middleware.UserAuth())
	{
		webWorkspaceRoute.GET("/config", controller.GetWebWorkspaceConfig)
		webWorkspaceRoute.GET("/status", controller.GetWebWorkspaceStatus)
		webWorkspaceRoute.GET("/projects", controller.GetWebWorkspaceProjects)
		webWorkspaceRoute.GET("/projects/:id", controller.GetWebWorkspaceProject)
		webWorkspaceRoute.PATCH("/projects/:id", controller.UpdateWebWorkspaceProject)
		webWorkspaceRoute.DELETE("/projects/:id", controller.DeleteWebWorkspaceProject)
		webWorkspaceRoute.GET("/projects/:id/conversations", controller.GetWebWorkspaceProjectConversations)
	}
}
