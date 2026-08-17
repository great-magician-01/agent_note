package handlers

import (
	"context"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/ai"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

type aiConfigVO struct {
	ID      int64  `json:"id,string"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"` // 脱敏
	Model   string `json:"model"`
	Active  int    `json:"active"`
}

func toAIConfigVO(c *models.AIConfig) aiConfigVO {
	return aiConfigVO{
		ID:      c.ID,
		Name:    c.Name,
		BaseURL: c.BaseURL,
		APIKey:  c.MaskedKey(),
		Model:   c.Model,
		Active:  c.Active,
	}
}

// ListAIConfigs GET /api/ai-configs
func ListAIConfigs(c *gin.Context) {
	var cfgs []models.AIConfig
	if err := database.DB.Where("is_active = 1").Order("id").Find(&cfgs).Error; err != nil {
		log.Printf("[handler] 查询 AI 配置列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询 AI 配置列表失败"})
		return
	}
	out := make([]aiConfigVO, 0, len(cfgs))
	for i := range cfgs {
		out = append(out, toAIConfigVO(&cfgs[i]))
	}
	c.JSON(http.StatusOK, out)
}

type aiConfigReq struct {
	ID      int64  `json:"id,string"`
	Name    string `json:"name" binding:"required"`
	BaseURL string `json:"base_url" binding:"required"`
	APIKey  string `json:"api_key" binding:"required"`
	Model   string `json:"model" binding:"required"`
}

// CreateAIConfig POST /api/ai-configs/create
func CreateAIConfig(c *gin.Context) {
	var req aiConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误：name/base_url/api_key/model 均必填"})
		return
	}
	cfg := models.AIConfig{
		ID:      snowflake.Next(),
		Name:    req.Name,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Model:   req.Model,
	}
	if err := database.DB.Create(&cfg).Error; err != nil {
		log.Printf("[handler] 创建 AI 配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建 AI 配置失败"})
		return
	}
	log.Printf("[handler] 创建 AI 配置 id=%d name=%q", cfg.ID, cfg.Name)
	c.JSON(http.StatusOK, toAIConfigVO(&cfg))
}

// UpdateAIConfig POST /api/ai-configs/update
func UpdateAIConfig(c *gin.Context) {
	var req aiConfigReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	res := database.DB.Model(&models.AIConfig{}).
		Where("id = ? AND is_active = 1", req.ID).
		Updates(map[string]any{
			"name":     req.Name,
			"base_url": req.BaseURL,
			"api_key":  req.APIKey,
			"model":    req.Model,
		})
	if res.Error != nil {
		log.Printf("[handler] 更新 AI 配置 id=%d 失败: %v", req.ID, res.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 AI 配置失败"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}
	log.Printf("[handler] 更新 AI 配置 id=%d name=%q", req.ID, req.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteAIConfig POST /api/ai-configs/delete
// 软删；若为激活配置，删除后系统处于无激活状态
func DeleteAIConfig(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	res := database.DB.Model(&models.AIConfig{}).
		Where("id = ? AND is_active = 1", req.ID).
		Update("is_active", 0)
	if res.Error != nil {
		log.Printf("[handler] 删除 AI 配置 id=%d 失败: %v", req.ID, res.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除 AI 配置失败"})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}
	log.Printf("[handler] 删除 AI 配置 id=%d", req.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ActivateAIConfig POST /api/ai-configs/activate
// 同一事务内：先全部置 0，再置当前为 1（应用层保证至多一个激活）
func ActivateAIConfig(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var cnt int64
		if err := tx.Model(&models.AIConfig{}).
			Where("id = ? AND is_active = 1", req.ID).
			Count(&cnt).Error; err != nil {
			return err
		}
		if cnt == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&models.AIConfig{}).
			Where("active = 1 AND id != ?", req.ID).
			Update("active", 0).Error; err != nil {
			return err
		}
		return tx.Model(&models.AIConfig{}).
			Where("id = ?", req.ID).
			Update("active", 1).Error
	})
	if err == gorm.ErrRecordNotFound {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}
	if err != nil {
		log.Printf("[handler] 激活 AI 配置 id=%d 失败: %v", req.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "激活 AI 配置失败"})
		return
	}
	log.Printf("[handler] 激活 AI 配置 id=%d", req.ID)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// TestAIConfig POST /api/ai-configs/test
func TestAIConfig(c *gin.Context) {
	var req struct {
		ID int64 `json:"id,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var cfg models.AIConfig
	if err := database.DB.Where("id = ? AND is_active = 1", req.ID).First(&cfg).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	client := ai.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	if err := client.ChatOnce(context.Background(), "ping"); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
