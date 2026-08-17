package handlers

import (
	"log"
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
		log.Printf("[handler] 查询会话列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话列表失败"})
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

	// 绑定笔记的会话：校验笔记存在且未删除
	if req.NoteID != nil {
		var cnt int64
		if err := database.DB.Model(&models.Note{}).
			Where("id = ? AND is_active = 1", *req.NoteID).
			Count(&cnt).Error; err != nil {
			log.Printf("[handler] 校验会话绑定笔记 id=%d 失败: %v", *req.NoteID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
			return
		}
		if cnt == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "绑定的笔记不存在"})
			return
		}
	}

	conv := models.Conversation{ID: snowflake.Next(), NoteID: req.NoteID}
	if err := database.DB.Create(&conv).Error; err != nil {
		log.Printf("[handler] 创建会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}
	log.Printf("[handler] 创建会话 id=%d note_id=%v", conv.ID, req.NoteID)
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
		log.Printf("[handler] 删除会话 id=%d 失败: %v", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除会话失败"})
		return
	}
	log.Printf("[handler] 删除会话 id=%d", req.ID)
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
		log.Printf("[handler] 查询会话消息 conv=%d 失败: %v", convID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询消息失败"})
		return
	}
	c.JSON(http.StatusOK, msgs)
}
