package router

import (
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/config"
	"github.com/great-magician-01/agent_note/internal/handlers"
	"github.com/great-magician-01/agent_note/internal/middleware"
)

func Setup() *gin.Engine {
	if !config.C.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestLogger(), middleware.CORS())

	// 静态文件：上传的图片（带安全头，防内容嗅探与脚本执行；路径 Clean 防目录逃逸）
	serveUpload := func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "script-src 'none'; sandbox")
		fp := filepath.Join(config.C.UploadDir, filepath.Clean("/"+c.Param("filepath")))
		c.File(fp)
	}
	r.GET("/uploads/*filepath", serveUpload)
	r.HEAD("/uploads/*filepath", serveUpload)

	api := r.Group("/api")
	{
		// 登录不需要鉴权
		api.POST("/auth/login", handlers.Login)

		// 以下全部需要 JWT
		auth := api.Group("")
		auth.Use(middleware.JWTAuth())
		{
			auth.GET("/auth/me", handlers.Me)

			// 分类
			auth.GET("/categories", handlers.ListCategories)
			auth.POST("/categories/create", handlers.CreateCategory)
			auth.POST("/categories/update", handlers.UpdateCategory)
			auth.POST("/categories/delete", handlers.DeleteCategory)

			// 笔记
			auth.GET("/notes", handlers.ListNotes)
			auth.POST("/notes/create", handlers.CreateNote)
			auth.GET("/notes/:id", handlers.GetNote)
			auth.POST("/notes/update", handlers.UpdateNote)
			auth.POST("/notes/delete", handlers.DeleteNote)
			auth.POST("/notes/batch/delete", handlers.BatchDeleteNotes)
			auth.POST("/notes/batch/move", handlers.BatchMoveNotes)
			auth.POST("/notes/meta/regenerate", handlers.RegenerateMeta)

			// 上传
			auth.POST("/uploads", handlers.UploadFile)

			// AI 配置
			auth.GET("/ai-configs", handlers.ListAIConfigs)
			auth.POST("/ai-configs/create", handlers.CreateAIConfig)
			auth.POST("/ai-configs/update", handlers.UpdateAIConfig)
			auth.POST("/ai-configs/delete", handlers.DeleteAIConfig)
			auth.POST("/ai-configs/activate", handlers.ActivateAIConfig)
			auth.POST("/ai-configs/test", handlers.TestAIConfig)

			// 会话与聊天
			auth.GET("/conversations", handlers.ListConversations)
			auth.POST("/conversations/create", handlers.CreateConversation)
			auth.POST("/conversations/delete", handlers.DeleteConversation)
			auth.GET("/conversations/:id/messages", handlers.ListMessages)
			auth.POST("/chat", handlers.Chat)
		}
	}

	// 生产模式：后端托管前端构建产物（web/dist 存在时生效）
	registerSPA(r, config.C.WebDistDir)

	return r
}
