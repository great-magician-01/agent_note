package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/great-magician-01/agent_note/internal/ai"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/services"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

const workerCount = 2

var (
	queue   chan int64
	pending sync.Map // note_id → struct{} 去重（已在队列中等待的不重复投递）
)

// Start 启动元数据生成 worker pool，并注入笔记变化钩子
func Start() {
	queue = make(chan int64, 256)

	// 注入钩子：保存笔记 / AI 写作后触发
	services.OnNoteContentChanged = Enqueue

	for i := 0; i < workerCount; i++ {
		go workLoop(i)
	}

	// 启动时恢复遗留任务：processing → pending，然后全部入队
	recoverStuck()

	log.Printf("[worker] started %d workers", workerCount)
}

// Enqueue 投递笔记到生成队列（按 note_id 去重）
func Enqueue(noteID int64) {
	if _, loaded := pending.LoadOrStore(noteID, struct{}{}); loaded {
		return
	}
	select {
	case queue <- noteID:
	default:
		// 队列满：丢弃但清除标记，由下次修改重新触发
		pending.Delete(noteID)
		log.Printf("[worker] queue full, dropped note %d", noteID)
	}
}

func recoverStuck() {
	database.DB.Model(&models.Note{}).
		Where("meta_status = 'processing'").
		Update("meta_status", "pending")

	var ids []int64
	database.DB.Model(&models.Note{}).
		Where("is_active = 1 AND meta_status = 'pending'").
		Pluck("id", &ids)
	for _, id := range ids {
		Enqueue(id)
	}
	if len(ids) > 0 {
		log.Printf("[worker] recovered %d pending notes", len(ids))
	}
}

func workLoop(idx int) {
	for noteID := range queue {
		pending.Delete(noteID)
		if err := process(noteID); err != nil {
			log.Printf("[worker-%d] note %d failed: %v", idx, noteID, err)
		}
	}
}

// metaResult AI 返回的元数据结构
type metaResult struct {
	Summary  string   `json:"summary"`
	Tags     []string `json:"tags"`
	Entities []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"entities"`
}

func process(noteID int64) error {
	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", noteID).First(&note).Error; err != nil {
		return nil // 笔记已删除，静默跳过
	}
	if note.MetaStatus != "pending" {
		return nil // 状态已被其他路径改变
	}

	// 取激活配置
	cfg, err := services.GetActiveAIConfig()
	if err != nil {
		return markFailed(noteID, err.Error())
	}

	// 标记处理中
	if err := database.DB.Model(&models.Note{}).
		Where("id = ?", noteID).
		Update("meta_status", "processing").Error; err != nil {
		return err
	}

	// 调 AI 提取：模型通过 submit_note_metadata 工具返回结构化结果；
	// 模型未调用工具则重试（最多 3 次）
	client := ai.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	userContent := "笔记标题：" + note.Title + "\n\n笔记正文：\n" + truncate(note.ContentMD, 8000)

	var meta metaResult
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		call, err := client.ChatToolCall(context.Background(), ai.MetaExtractPrompt, userContent, ai.MetaExtractTool)
		if err != nil {
			lastErr = err
			if err == ai.ErrNoToolCall {
				log.Printf("[worker] note %d attempt %d: 模型未调用工具，重试", noteID, attempt+1)
			}
			continue
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &meta); err != nil {
			lastErr = fmt.Errorf("工具参数解析失败: %w", err)
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		return markFailed(noteID, "AI 元数据提取失败: "+lastErr.Error())
	}

	// 校验实体 type 合法值
	validTypes := map[string]bool{
		"person": true, "organization": true, "location": true,
		"technology": true, "product": true, "event": true, "other": true,
	}

	// 写库事务：upsert tags/entities + 重建关联 + 更新 summary
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		// 软删旧关联
		if err := tx.Model(&models.NoteTag{}).
			Where("note_id = ? AND is_active = 1", noteID).
			Update("is_active", 0).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.NoteEntity{}).
			Where("note_id = ? AND is_active = 1", noteID).
			Update("is_active", 0).Error; err != nil {
			return err
		}

		// tags：按 name 查复用 / 新建
		seenTags := map[string]bool{}
		for _, name := range meta.Tags {
			name = strings.TrimSpace(name)
			if name == "" || seenTags[name] || len([]rune(name)) > 64 {
				continue
			}
			seenTags[name] = true
			tagID, err := upsertTag(tx, name)
			if err != nil {
				return err
			}
			if err := tx.Create(&models.NoteTag{
				ID: snowflake.Next(), NoteID: noteID, TagID: tagID,
			}).Error; err != nil {
				return err
			}
		}

		// entities
		seenEntities := map[string]bool{}
		for _, e := range meta.Entities {
			name := strings.TrimSpace(e.Name)
			if name == "" || seenEntities[name] || len([]rune(name)) > 128 {
				continue
			}
			seenEntities[name] = true
			etype := e.Type
			if !validTypes[etype] {
				etype = "other"
			}
			entityID, err := upsertEntity(tx, name, etype)
			if err != nil {
				return err
			}
			if err := tx.Create(&models.NoteEntity{
				ID: snowflake.Next(), NoteID: noteID, EntityID: entityID,
			}).Error; err != nil {
				return err
			}
		}

		// 更新笔记
		return tx.Model(&models.Note{}).
			Where("id = ?", noteID).
			Updates(map[string]any{
				"summary":     strings.TrimSpace(meta.Summary),
				"meta_status": "done",
				"meta_error":  "",
			}).Error
	})
	if err != nil {
		return markFailed(noteID, "写入元数据失败: "+err.Error())
	}

	log.Printf("[worker] note %d meta done (tags=%d entities=%d)", noteID, len(meta.Tags), len(meta.Entities))
	return nil
}

// upsertTag 按 name 查询复用；不存在则新建（应用层去重）
func upsertTag(tx *gorm.DB, name string) (int64, error) {
	var tag models.Tag
	err := tx.Where("name = ? AND is_active = 1", name).First(&tag).Error
	if err == nil {
		return tag.ID, nil
	}
	if err != gorm.ErrRecordNotFound {
		return 0, err
	}
	tag = models.Tag{ID: snowflake.Next(), Name: name}
	if err := tx.Create(&tag).Error; err != nil {
		return 0, err
	}
	return tag.ID, nil
}

func upsertEntity(tx *gorm.DB, name, etype string) (int64, error) {
	var ent models.Entity
	err := tx.Where("name = ? AND is_active = 1", name).First(&ent).Error
	if err == nil {
		return ent.ID, nil
	}
	if err != gorm.ErrRecordNotFound {
		return 0, err
	}
	ent = models.Entity{ID: snowflake.Next(), Name: name, Type: etype}
	if err := tx.Create(&ent).Error; err != nil {
		return 0, err
	}
	return ent.ID, nil
}

func markFailed(noteID int64, reason string) error {
	log.Printf("[worker] note %d meta failed: %s", noteID, reason)
	return database.DB.Model(&models.Note{}).
		Where("id = ?", noteID).
		Updates(map[string]any{
			"meta_status": "failed",
			"meta_error":  reason,
		}).Error
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "\n…(内容过长已截断)"
}
