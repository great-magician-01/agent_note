package ai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/services"
	"github.com/great-magician-01/agent_note/internal/snowflake"
	"gorm.io/gorm"
)

// ToolResult 工具执行结果
type ToolResult struct {
	Content     string // 回传给模型的 JSON 文本
	NoteUpdated *int64 // 写作工具修改的笔记 id（触发 SSE note_updated）
}

// ToolExecutor 工具执行函数
type ToolExecutor func(argsJSON string) (*ToolResult, error)

// ToolDef 工具定义 + 执行器
type ToolDef struct {
	Def      Tool
	Executor ToolExecutor
	// Writing 标记是否为写作工具（仅编辑页会话可用）
	Writing bool
}

// Registry 全部工具注册表
var Registry = map[string]*ToolDef{}

func register(t *ToolDef) {
	Registry[t.Def.Function.Name] = t
}

// ToolsForScope 按会话范围返回工具列表（note 范围含写作工具）
func ToolsForScope(noteBound bool) []Tool {
	out := make([]Tool, 0, len(Registry))
	for _, name := range toolOrder {
		t := Registry[name]
		if t.Writing && !noteBound {
			continue
		}
		out = append(out, t.Def)
	}
	return out
}

var toolOrder = []string{
	"search_notes", "get_note", "list_categories",
	"replace_note_section", "append_note_content", "update_note_title", "create_note",
}

// Execute 执行工具
func Execute(name, argsJSON string) (*ToolResult, error) {
	t, ok := Registry[name]
	if !ok {
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	return t.Executor(argsJSON)
}

func init() {
	// ---------- search_notes ----------
	register(&ToolDef{
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_notes",
				Description: "在笔记库中搜索笔记。按关键词、标签、实体名检索，返回匹配笔记的简介列表。用户想查找、回忆过往笔记时必须先调用本工具。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"keywords": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "搜索关键词，可多个"},
						"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "按标签过滤，可为空"},
						"entities": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "按实体名过滤，可为空"},
						"limit":    map[string]any{"type": "integer", "default": 10, "description": "返回条数上限"},
					},
					"required": []string{"keywords"},
				},
			},
		},
		Executor: execSearchNotes,
	})

	// ---------- get_note ----------
	register(&ToolDef{
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_note",
				Description: "获取指定笔记的完整正文。search_notes 只返回简介，确认相关后调用本工具读取全文。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"note_id": map[string]any{"type": "string"},
					},
					"required": []string{"note_id"},
				},
			},
		},
		Executor: execGetNote,
	})

	// ---------- list_categories ----------
	register(&ToolDef{
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "list_categories",
				Description: "列出全部笔记分类，用于新建笔记时选择分类。",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		Executor: execListCategories,
	})

	// ---------- replace_note_section ----------
	register(&ToolDef{
		Writing: true,
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "replace_note_section",
				Description: "用 new_text 精确替换笔记中的 old_text 片段。old_text 必须与原文一字不差（从 get_note 结果中逐字复制），否则替换失败。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"note_id":  map[string]any{"type": "string"},
						"old_text": map[string]any{"type": "string"},
						"new_text": map[string]any{"type": "string"},
					},
					"required": []string{"note_id", "old_text", "new_text"},
				},
			},
		},
		Executor: execReplaceNoteSection,
	})

	// ---------- append_note_content ----------
	register(&ToolDef{
		Writing: true,
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "append_note_content",
				Description: "在笔记末尾追加内容。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"note_id": map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
					"required": []string{"note_id", "content"},
				},
			},
		},
		Executor: execAppendNoteContent,
	})

	// ---------- update_note_title ----------
	register(&ToolDef{
		Writing: true,
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "update_note_title",
				Description: "修改笔记标题。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"note_id": map[string]any{"type": "string"},
						"title":   map[string]any{"type": "string"},
					},
					"required": []string{"note_id", "title"},
				},
			},
		},
		Executor: execUpdateNoteTitle,
	})

	// ---------- create_note ----------
	register(&ToolDef{
		Writing: true,
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "create_note",
				Description: "新建一篇笔记（如 AI 产出一篇完整文章时）。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string"},
						"content":     map[string]any{"type": "string"},
						"category_id": map[string]any{"type": "string", "description": "可选，用 list_categories 查询"},
					},
					"required": []string{"title", "content"},
				},
			},
		},
		Executor: execCreateNote,
	})
}

// ==================== 检索类执行器 ====================

func execSearchNotes(argsJSON string) (*ToolResult, error) {
	var args struct {
		Keywords []string `json:"keywords"`
		Tags     []string `json:"tags"`
		Entities []string `json:"entities"`
		Limit    int      `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}

	notes, err := services.SearchNotesForAI(args.Keywords, args.Tags, args.Entities, args.Limit)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, n.ID)
	}
	tagMap, entityMap, err := services.LoadNoteMeta(ids)
	if err != nil {
		return nil, err
	}

	type item struct {
		ID       string              `json:"id"`
		Title    string              `json:"title"`
		Summary  string              `json:"summary"`
		Tags     []string            `json:"tags"`
		Entities []services.EntityVO `json:"entities"`
	}
	items := make([]item, 0, len(notes))
	for _, n := range notes {
		tags := tagMap[n.ID]
		if tags == nil {
			tags = []string{}
		}
		ents := entityMap[n.ID]
		if ents == nil {
			ents = []services.EntityVO{}
		}
		summary := n.Summary
		if summary == "" {
			summary = "(简介尚未生成)"
		}
		items = append(items, item{
			ID: strconv.FormatInt(n.ID, 10), Title: n.Title,
			Summary: summary, Tags: tags, Entities: ents,
		})
	}

	raw, _ := json.Marshal(map[string]any{"total": len(items), "notes": items})
	return &ToolResult{Content: string(raw)}, nil
}

func execGetNote(argsJSON string) (*ToolResult, error) {
	var args struct {
		NoteID string `json:"note_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	id, err := strconv.ParseInt(args.NoteID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("note_id 格式错误")
	}
	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", id).First(&note).Error; err != nil {
		return nil, fmt.Errorf("笔记不存在 (id=%s)", args.NoteID)
	}
	raw, _ := json.Marshal(map[string]any{
		"id": args.NoteID, "title": note.Title, "content": note.ContentMD,
	})
	return &ToolResult{Content: string(raw)}, nil
}

func execListCategories(_ string) (*ToolResult, error) {
	var cats []models.Category
	if err := database.DB.Where("is_active = 1").Order("sort, id").Find(&cats).Error; err != nil {
		return nil, err
	}
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	items := make([]item, 0, len(cats))
	for _, cat := range cats {
		items = append(items, item{ID: strconv.FormatInt(cat.ID, 10), Name: cat.Name})
	}
	raw, _ := json.Marshal(items)
	return &ToolResult{Content: string(raw)}, nil
}

// ==================== 写作类执行器 ====================

// applyNoteWrite 写作工具公共流程：更新内容（同事务标记元数据 pending）→ 通知 worker
func applyNoteWrite(noteID int64, mutate func(content string) (string, error)) (*ToolResult, error) {
	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", noteID).First(&note).Error; err != nil {
		return nil, fmt.Errorf("笔记不存在 (id=%d)", noteID)
	}

	newContent, err := mutate(note.ContentMD)
	if err != nil {
		return nil, err
	}

	err = database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Note{}).
			Where("id = ?", noteID).
			Update("content_md", newContent).Error; err != nil {
			return err
		}
		return services.TouchNoteForMeta(tx, noteID, newContent)
	})
	if err != nil {
		return nil, err
	}

	services.OnNoteContentChanged(noteID)
	return &ToolResult{
		Content:     `{"ok":true}`,
		NoteUpdated: &noteID,
	}, nil
}

func execReplaceNoteSection(argsJSON string) (*ToolResult, error) {
	var args struct {
		NoteID  string `json:"note_id"`
		OldText string `json:"old_text"`
		NewText string `json:"new_text"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	id, err := strconv.ParseInt(args.NoteID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("note_id 格式错误")
	}
	if args.OldText == "" {
		return nil, fmt.Errorf("old_text 不能为空")
	}

	return applyNoteWrite(id, func(content string) (string, error) {
		if !strings.Contains(content, args.OldText) {
			return "", fmt.Errorf("替换失败：未在笔记中找到与 old_text 完全一致的片段。请先调用 get_note 获取原文，逐字复制后再试")
		}
		return strings.Replace(content, args.OldText, args.NewText, 1), nil
	})
}

func execAppendNoteContent(argsJSON string) (*ToolResult, error) {
	var args struct {
		NoteID  string `json:"note_id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	id, err := strconv.ParseInt(args.NoteID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("note_id 格式错误")
	}
	if args.Content == "" {
		return nil, fmt.Errorf("content 不能为空")
	}

	return applyNoteWrite(id, func(content string) (string, error) {
		sep := "\n\n"
		if content == "" {
			sep = ""
		}
		return content + sep + args.Content, nil
	})
}

func execUpdateNoteTitle(argsJSON string) (*ToolResult, error) {
	var args struct {
		NoteID string `json:"note_id"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	id, err := strconv.ParseInt(args.NoteID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("note_id 格式错误")
	}
	if strings.TrimSpace(args.Title) == "" {
		return nil, fmt.Errorf("title 不能为空")
	}

	res := database.DB.Model(&models.Note{}).
		Where("id = ? AND is_active = 1", id).
		Update("title", args.Title)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("笔记不存在 (id=%s)", args.NoteID)
	}
	return &ToolResult{Content: `{"ok":true}`, NoteUpdated: &id}, nil
}

func execCreateNote(argsJSON string) (*ToolResult, error) {
	var args struct {
		Title      string `json:"title"`
		Content    string `json:"content"`
		CategoryID string `json:"category_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	if args.Title == "" || args.Content == "" {
		return nil, fmt.Errorf("title 和 content 均不能为空")
	}

	var catID *int64
	if args.CategoryID != "" {
		if v, err := strconv.ParseInt(args.CategoryID, 10, 64); err == nil {
			catID = &v
		}
	}

	note := models.Note{
		ID:         snowflake.Next(),
		Title:      args.Title,
		ContentMD:  args.Content,
		CategoryID: catID,
	}
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&note).Error; err != nil {
			return err
		}
		return services.TouchNoteForMeta(tx, note.ID, note.ContentMD)
	})
	if err != nil {
		return nil, err
	}

	services.OnNoteContentChanged(note.ID)
	raw, _ := json.Marshal(map[string]any{
		"ok": true, "note_id": strconv.FormatInt(note.ID, 10), "title": note.Title,
	})
	return &ToolResult{Content: string(raw)}, nil
}
