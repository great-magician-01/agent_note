package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/services"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

// ListNotes GET /api/notes?category_id&keyword&tag&entity&page&page_size
func ListNotes(c *gin.Context) {
	q := services.NoteListQuery{
		Keyword:  c.Query("keyword"),
		Tag:      c.Query("tag"),
		Entity:   c.Query("entity"),
		Page:     atoiDefault(c.Query("page"), 1),
		PageSize: atoiDefault(c.Query("page_size"), 20),
	}
	if v := c.Query("category_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.CategoryID = &id
		}
	}

	notes, total, err := services.ListNotes(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ids := make([]int64, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	tagMap, entityMap, err := services.LoadNoteMeta(ids)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]services.NoteVO, 0, len(notes))
	for i := range notes {
		items = append(items, services.ToVO(&notes[i], tagMap, entityMap, false))
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "items": items})
}

type noteReq struct {
	ID         int64  `json:"id,string"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	CategoryID *int64 `json:"category_id,string"`
}

// CreateNote POST /api/notes/create
func CreateNote(c *gin.Context) {
	var req noteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	title := req.Title
	if title == "" {
		title = services.DeriveTitle(req.Content)
	}

	note := models.Note{
		ID:         snowflake.Next(),
		Title:      title,
		ContentMD:  req.Content,
		CategoryID: req.CategoryID,
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&note).Error; err != nil {
			return err
		}
		// 内容非空 → 标记待生成元数据
		if req.Content != "" {
			if err := services.TouchNoteForMeta(tx, note.ID, req.Content); err != nil {
				return err
			}
			note.MetaStatus = "pending"
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 通知 worker（阶段 4 实现；现在为空操作钩子）
	services.OnNoteContentChanged(note.ID)

	c.JSON(http.StatusOK, services.ToVO(&note, nil, nil, true))
}

// GetNote GET /api/notes/:id
func GetNote(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式错误"})
		return
	}
	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", id).First(&note).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "笔记不存在"})
		return
	}
	tagMap, entityMap, _ := services.LoadNoteMeta([]int64{note.ID})
	c.JSON(http.StatusOK, services.ToVO(&note, tagMap, entityMap, true))
}

// UpdateNote POST /api/notes/update
func UpdateNote(c *gin.Context) {
	var req noteReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", req.ID).First(&note).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "笔记不存在"})
		return
	}

	title := req.Title
	if title == "" {
		title = services.DeriveTitle(req.Content)
	}
	contentChanged := services.ContentHash(req.Content) != note.MetaContentHash

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"title":       title,
			"content_md":  req.Content,
			"category_id": req.CategoryID,
		}
		if err := tx.Model(&models.Note{}).Where("id = ?", note.ID).Updates(updates).Error; err != nil {
			return err
		}
		// 仅内容变化才重新生成元数据
		if contentChanged {
			return services.TouchNoteForMeta(tx, note.ID, req.Content)
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if contentChanged {
		services.OnNoteContentChanged(note.ID)
	}

	// 重新读取最新状态
	database.DB.Where("id = ?", note.ID).First(&note)
	tagMap, entityMap, _ := services.LoadNoteMeta([]int64{note.ID})
	c.JSON(http.StatusOK, services.ToVO(&note, tagMap, entityMap, true))
}

// DeleteNote POST /api/notes/delete
// 软删笔记；级联软删 note_tags / note_entities / 绑定会话及消息
func DeleteNote(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Note{}).
			Where("id = ? AND is_active = 1", req.ID).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.NoteTag{}).
			Where("note_id = ? AND is_active = 1", req.ID).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.NoteEntity{}).
			Where("note_id = ? AND is_active = 1", req.ID).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		// 绑定会话及消息
		var convIDs []int64
		if err := tx.Model(&models.Conversation{}).
			Where("note_id = ? AND is_active = 1", req.ID).
			Pluck("id", &convIDs).Error; err != nil {
			return err
		}
		if len(convIDs) > 0 {
			if err := tx.Model(&models.Conversation{}).
				Where("id IN ?", convIDs).
				Update("is_active", 0).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.Message{}).
				Where("conversation_id IN ? AND is_active = 1", convIDs).
				Update("is_active", 0).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BatchDeleteNotes POST /api/notes/batch/delete
// 批量软删；级联同 DeleteNote
func BatchDeleteNotes(c *gin.Context) {
	ids, ok := bindIDs(c)
	if !ok {
		return
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Note{}).
			Where("id IN ? AND is_active = 1", ids).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.NoteTag{}).
			Where("note_id IN ? AND is_active = 1", ids).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.NoteEntity{}).
			Where("note_id IN ? AND is_active = 1", ids).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		// 绑定会话及消息
		var convIDs []int64
		if err := tx.Model(&models.Conversation{}).
			Where("note_id IN ? AND is_active = 1", ids).
			Pluck("id", &convIDs).Error; err != nil {
			return err
		}
		if len(convIDs) > 0 {
			if err := tx.Model(&models.Conversation{}).
				Where("id IN ?", convIDs).
				Update("is_active", 0).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.Message{}).
				Where("conversation_id IN ? AND is_active = 1", convIDs).
				Update("is_active", 0).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// BatchMoveNotes POST /api/notes/batch/move
// 批量移动分类；category_id 传 null 表示移到未分类
func BatchMoveNotes(c *gin.Context) {
	var req struct {
		IDs        []string `json:"ids" binding:"required,min=1"`
		CategoryID *int64   `json:"category_id,string"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	ids, err := parseIDs(req.IDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式错误"})
		return
	}

	// 目标分类必须存在
	if req.CategoryID != nil {
		var cnt int64
		if err := database.DB.Model(&models.Category{}).
			Where("id = ? AND is_active = 1", *req.CategoryID).
			Count(&cnt).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if cnt == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "目标分类不存在"})
			return
		}
	}

	if err := database.DB.Model(&models.Note{}).
		Where("id IN ? AND is_active = 1", ids).
		Update("category_id", req.CategoryID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// bindIDs 解析 { ids: ["123", ...] }（雪花 ID 以字符串传递）
func bindIDs(c *gin.Context) ([]int64, bool) {
	var req struct {
		IDs []string `json:"ids" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return nil, false
	}
	ids, err := parseIDs(req.IDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式错误"})
		return nil, false
	}
	return ids, true
}

func parseIDs(ss []string) ([]int64, error) {
	ids := make([]int64, 0, len(ss))
	for _, s := range ss {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// RegenerateMeta POST /api/notes/meta/regenerate
func RegenerateMeta(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	res := database.DB.Model(&models.Note{}).
		Where("id = ? AND is_active = 1", req.ID).
		Updates(map[string]any{"meta_status": "pending", "meta_error": ""})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "笔记不存在"})
		return
	}
	services.OnNoteContentChanged(req.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func atoiDefault(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}
