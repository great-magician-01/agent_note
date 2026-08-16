package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

// ListConversations GET /api/conversations?note_id=
// note_id 缺省 = 全局会话；"null" 也视为全局；具体 id = 绑定该笔记的会话
func ListConversations(c *gin.Context) {
	db := database.DB.Where("is_active = 1")
	if v := c.Query("note_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			db = db.Where("note_id = ?", id)
		}
	} else {
		db = db.Where("note_id IS NULL")
	}

	var convs []models.Conversation
	if err := db.Order("updated_at DESC").Find(&convs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, convs)
}

// CreateConversation POST /api/conversations/create
func CreateConversation(c *gin.Context) {
	var req struct {
		NoteID *int64 `json:"note_id,string"`
	}
	_ = c.ShouldBindJSON(&req)

	conv := models.Conversation{ID: snowflake.Next(), NoteID: req.NoteID}
	if err := database.DB.Create(&conv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, conv)
}

// DeleteConversation POST /api/conversations/delete
// 软删会话并级联软删消息
func DeleteConversation(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Conversation{}).
			Where("id = ? AND is_active = 1", req.ID).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		return tx.Model(&models.Message{}).
			Where("conversation_id = ? AND is_active = 1", req.ID).
			Update("is_active", 0).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ListMessages GET /api/conversations/:id/messages
func ListMessages(c *gin.Context) {
	convID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式错误"})
		return
	}
	var msgs []models.Message
	if err := database.DB.
		Where("conversation_id = ? AND is_active = 1", convID).
		Order("id").
		Find(&msgs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, msgs)
}
