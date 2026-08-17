package handlers

import (
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/config"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/snowflake"
)

// 上传白名单（svg 可携带脚本，已移除；静态服务侧另有 nosniff + CSP 兜底）
var allowedImageExt = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
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
		log.Printf("[handler] 上传被拒：不支持的扩展名 %q（原始文件名 %q）", ext, file.Filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持图片文件（png/jpg/jpeg/gif/webp）"})
		return
	}

	id := snowflake.Next()
	filename := fmt.Sprintf("%d%s", id, ext)
	dst := filepath.Join(config.C.UploadDir, filename)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		log.Printf("[handler] 保存上传文件失败: %v", err)
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
	if err := database.DB.Create(&up).Error; err != nil {
		// 文件已落盘，记录失败但照常返回 URL（uploads 表仅作审计）
		log.Printf("[handler] 上传记录写库失败 id=%d: %v", id, err)
	}

	log.Printf("[handler] 上传成功 %s（%d 字节）", up.Path, up.Size)
	c.JSON(http.StatusOK, gin.H{"url": up.Path})
}
