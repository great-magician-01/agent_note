package services

import (
	"errors"

	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
)

var ErrNoActiveAIConfig = errors.New("未设置激活的 AI 配置，请先到设置页配置")

// GetActiveAIConfig 取当前激活的 AI 配置
// （ai_configs.active = 激活标记；is_active = 软删标记）
func GetActiveAIConfig() (*models.AIConfig, error) {
	var cfg models.AIConfig
	err := database.DB.Where("is_active = 1 AND active = 1").First(&cfg).Error
	if err != nil {
		return nil, ErrNoActiveAIConfig
	}
	return &cfg, nil
}
