package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

type categoryReq struct {
	ID   int64  `json:"id,string"`
	Name string `json:"name" binding:"required"`
}

// ListCategories GET /api/categories
func ListCategories(c *gin.Context) {
	var cats []models.Category
	if err := database.DB.Where("is_active = 1").Order("sort, id").Find(&cats).Error; err != nil {
		log.Printf("[handler] 查询分类列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询分类列表失败"})
		return
	}

	// 每个分类的笔记数
	type countRow struct {
		CategoryID int64
		Cnt        int64
	}
	var counts []countRow
	database.DB.Model(&models.Note{}).
		Select("category_id, COUNT(*) AS cnt").
		Where("is_active = 1 AND category_id IS NOT NULL").
		Group("category_id").Scan(&counts)
	countMap := map[int64]int64{}
	for _, r := range counts {
		countMap[r.CategoryID] = r.Cnt
	}

	type catVO struct {
		ID        int64  `json:"id,string"`
		Name      string `json:"name"`
		Sort      int    `json:"sort"`
		NoteCount int64  `json:"note_count"`
	}
	out := make([]catVO, 0, len(cats))
	for _, cat := range cats {
		out = append(out, catVO{ID: cat.ID, Name: cat.Name, Sort: cat.Sort, NoteCount: countMap[cat.ID]})
	}
	c.JSON(http.StatusOK, out)
}

// CreateCategory POST /api/categories/create
func CreateCategory(c *gin.Context) {
	var req categoryReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	cat := models.Category{ID: snowflake.Next(), Name: req.Name}
	if err := database.DB.Create(&cat).Error; err != nil {
		log.Printf("[handler] 创建分类失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建分类失败"})
		return
	}
	log.Printf("[handler] 创建分类 id=%d name=%q", cat.ID, cat.Name)
	c.JSON(http.StatusOK, cat)
}

// UpdateCategory POST /api/categories/update
func UpdateCategory(c *gin.Context) {
	var req categoryReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	res := database.DB.Model(&models.Category{}).
		Where("id = ? AND is_active = 1", req.ID).
		Update("name", req.Name)
	if res.Error != nil {
		log.Printf("[handler] 更新分类 id=%d 失败: %v", req.ID, res.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新分类失败"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "分类不存在"})
		return
	}
	log.Printf("[handler] 更新分类 id=%d name=%q", req.ID, req.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteCategory POST /api/categories/delete
// 软删分类；其下笔记 category_id 置 NULL（应用层级联，事务内）
func DeleteCategory(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Category{}).
			Where("id = ? AND is_active = 1", req.ID).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		return tx.Model(&models.Note{}).
			Where("category_id = ? AND is_active = 1", req.ID).
			Update("category_id", nil).Error
	})
	if err != nil {
		log.Printf("[handler] 删除分类 id=%d 失败: %v", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除分类失败"})
		return
	}
	log.Printf("[handler] 删除分类 id=%d", req.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
