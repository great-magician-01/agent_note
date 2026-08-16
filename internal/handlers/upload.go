package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/config"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/snowflake"
)

var allowedImageExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

// UploadFile POST /api/uploads（multipart file）
func UploadFile(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件"})
		return
	}
	if file.Size > 10<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件超过 10MB 限制"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	mime, ok := allowedImageExt[ext]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持图片文件（png/jpg/jpeg/gif/webp/svg）"})
		return
	}

	id := snowflake.Next()
	filename := fmt.Sprintf("%d%s", id, ext)
	dst := filepath.Join(config.C.UploadDir, filename)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}

	up := models.Upload{
		ID:       id,
		Filename: file.Filename,
		Path:     "/uploads/" + filename,
		Size:     file.Size,
		Mime:     mime,
	}
	database.DB.Create(&up)

	c.JSON(http.StatusOK, gin.H{"url": up.Path})
}
