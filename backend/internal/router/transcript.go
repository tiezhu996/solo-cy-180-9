package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/oralhistory/oralhistory/internal/config"
	"github.com/oralhistory/oralhistory/internal/constants"
	"github.com/oralhistory/oralhistory/internal/handler"
	"github.com/oralhistory/oralhistory/internal/middleware"
)

// RegisterTranscriptRoutes 注册转写校对路由。
func RegisterTranscriptRoutes(g *gin.RouterGroup, h *handler.TranscriptHandler, cfg *config.Config, logger *slog.Logger) {
	group := g.Group("/transcripts", middleware.Auth(cfg.JWTSecret, logger))
	{
		// 查询类：登录即可。
		group.GET("", h.List)
		group.GET("/search", h.Search)
		group.GET("/:id", h.Get)
		group.GET("/:id/export", h.Export)

		// 采访员：创建草稿、编辑分段、提交审核。
		editor := group.Group("", middleware.RBAC(logger, constants.RoleInterviewer, constants.RoleAdmin))
		{
			editor.POST("", h.Create)
			editor.PUT("/:id/segments", h.SaveSegments)
			editor.POST("/:id/submit", h.Submit)
		}

		// 档案员：逐段确认、整篇通过、整篇退回。
		reviewer := group.Group("", middleware.RBAC(logger, constants.RoleArchivist, constants.RoleAdmin))
		{
			reviewer.POST("/:id/segments/:segmentId/confirm", h.ConfirmSegment)
			reviewer.POST("/:id/approve", h.Approve)
			reviewer.POST("/:id/reject", h.Reject)
		}
	}
}
