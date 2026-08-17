package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/great-magician-01/agent_note/internal/ai"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/services"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

const workerCount = 2

// errContentChanged 处理期间笔记内容被修改（完成写入的 hash 条件未命中），放弃本次结果
var errContentChanged = errors.New("内容在处理期间已修改")

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
		log.Printf("[worker] note %d enqueued", noteID)
	default:
		// 队列满：丢弃但清除标记，由下次修改重新触发；
		// 同时置 failed，避免笔记滞留 pending 状态（启动恢复超容量时同样走这里）
		pending.Delete(noteID)
		log.Printf("[worker] queue full, dropped note %d，置 meta_status=failed", noteID)
		// database.DB 为 nil 仅发生在无数据库的单元测试中
		if database.DB == nil {
			return
		}
		if err := database.DB.Model(&models.Note{}).
			Where("id = ? AND meta_status = 'pending' AND is_active = 1", noteID).
			Updates(map[string]any{
				"meta_status": "failed",
				"meta_error":  "元数据生成队列已满，请稍后手动重试",
			}).Error; err != nil {
			log.Printf("[worker] note %d 置 failed 失败: %v", noteID, err)
		}
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
	// 原子认领：仅当仍处于 pending 才置 processing，避免与其他路径并发重复处理
	res := database.DB.Model(&models.Note{}).
		Where("id = ? AND meta_status = 'pending' AND is_active = 1", noteID).
		Update("meta_status", "processing")
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil // 状态已被其他路径改变（已认领/已删除/已重置）
	}
	log.Printf("[worker] note %d claimed", noteID)

	// 认领成功后再读取笔记内容与处理起始的内容 hash（完成写入时据此检测处理期间的内容变更）
	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", noteID).First(&note).Error; err != nil {
		return nil // 认领后又被删除，静默跳过
	}
	contentHash := note.MetaContentHash

	// 取激活配置
	cfg, err := services.GetActiveAIConfig()
	if err != nil {
		return markFailed(noteID, err.Error())
	}

	// 调 AI 提取：模型通过 submit_note_metadata 工具返回结构化结果；
	// 模型未调用工具则重试（最多 3 次）
	client := ai.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	userContent := "笔记标题：" + note.Title + "\n\n笔记正文：\n" + truncate(note.ContentMD, 8000)

	var meta metaResult
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		start := time.Now()
		call, usage, err := client.ChatToolCall(context.Background(), ai.MetaExtractPrompt, userContent, ai.MetaExtractTool)
		// 每次调用（含重试）落一条调用记录；写库失败不影响主流程
		logCall(noteID, cfg.Model, attempt+1, usage, time.Since(start), err)
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

		// 更新笔记：带处理起始时的内容 hash 条件，若处理期间内容被修改则放弃写入
		// （保存接口已重新置 pending 并入队，等下一轮用新内容再生成）
		upd := tx.Model(&models.Note{}).
			Where("id = ? AND meta_content_hash = ?", noteID, contentHash).
			Updates(map[string]any{
				"summary":     strings.TrimSpace(meta.Summary),
				"meta_status": "done",
				"meta_error":  "",
			})
		if upd.Error != nil {
			return upd.Error
		}
		if upd.RowsAffected == 0 {
			return errContentChanged
		}
		return nil
	})
	if err == errContentChanged {
		log.Printf("[worker] note %d 内容在处理期间已修改，放弃本次元数据写入，等待下一轮", noteID)
		return nil
	}
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

// logCall 落一条 AI 调用记录到 ai_call_logs（尽力而为：写库失败仅打日志，不影响元数据主流程）
func logCall(noteID int64, model string, attempt int, usage *ai.Usage, dur time.Duration, callErr error) {
	rec := models.AICallLog{
		ID:         snowflake.Next(),
		Kind:       "meta_extract",
		NoteID:     &noteID,
		Model:      model,
		Attempt:    attempt,
		Success:    callErr == nil,
		DurationMs: dur.Milliseconds(),
	}
	if usage != nil {
		raw, _ := json.Marshal(usage)
		s := string(raw)
		rec.Usage = &s
	}
	if callErr != nil {
		rec.Error = callErr.Error()
	}
	if err := database.DB.Create(&rec).Error; err != nil {
		log.Printf("[worker] note %d 调用记录写库失败: %v", noteID, err)
	}
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
