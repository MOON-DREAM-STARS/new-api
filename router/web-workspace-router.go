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
		webWorkspaceRoute.GET("/session/:id/file-chooser", controller.GetWebWorkspaceFileChooser)
		webWorkspaceRoute.POST("/session/:id/file-chooser/:chooser_id/files", controller.UploadWebWorkspaceFileChooserFiles)
		webWorkspaceRoute.POST("/session/:id/file-chooser/:chooser_id/cancel", controller.CancelWebWorkspaceFileChooser)
		webWorkspaceRoute.POST("/session/:id/clipboard/copy", controller.CopyWebWorkspaceClipboard)
		webWorkspaceRoute.POST("/session/:id/clipboard/paste", controller.PasteWebWorkspaceClipboard)
		webWorkspaceRoute.POST("/session/:id/input/text", controller.InsertWebWorkspaceInputText)
		webWorkspaceRoute.POST("/session/:id/input/key", controller.DispatchWebWorkspaceInputKey)
		webWorkspaceRoute.GET("/session/:id/input/caret", controller.GetWebWorkspaceInputCaret)
	}

	// Stream and KasmVNC attaches authenticate with a short-lived ticket instead
	// of a bearer header, because browser WebSocket clients and iframe documents
	// cannot set headers. Each ticket is bound to the user, workspace and session;
	// the Kasm asset path also carries the ticket so relative assets are covered.
	webWorkspaceTicketRoute := apiRouter.Group("/web-workspace")
	{
		webWorkspaceTicketRoute.GET("/session/:id/stream", controller.WebWorkspaceStream)
		webWorkspaceTicketRoute.GET("/session/:id/kasm/*rest", controller.WebWorkspaceKasm)
		webWorkspaceTicketRoute.POST("/session/:id/kasm/*rest", controller.WebWorkspaceKasm)
	}
}
