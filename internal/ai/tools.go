package ai

import (
	"context"
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
	Content     string        // 回传给模型的 JSON 文本
	NoteUpdated *int64        // 写作工具修改的笔记 id（触发 SSE note_updated）
	Proposal    *NoteProposal // 正文修改提案（触发 SSE note_proposal，用户审核后才落库）
}

// NoteProposal 正文修改提案：正文类写作工具产出，不落库，
// 经 SSE 推送由用户审核通过后走常规更新接口生效
type NoteProposal struct {
	NoteID     int64  // 目标笔记 id
	Tool       string // 产出提案的工具名
	NewContent string // 修改后的完整正文
}

// ctxKey context 键类型（防冲突）
type ctxKey string

// boundNoteKey 会话绑定笔记 id 的 context 键
const boundNoteKey ctxKey = "boundNoteID"

// WithBoundNoteID 将会话绑定的笔记 id 注入 ctx，写作工具据此强制校验目标笔记
func WithBoundNoteID(ctx context.Context, noteID int64) context.Context {
	return context.WithValue(ctx, boundNoteKey, noteID)
}

// checkBoundNote 校验写作工具的目标笔记是否为当前会话绑定的笔记；ctx 无绑定值时放行
func checkBoundNote(ctx context.Context, noteID int64) error {
	if bound, ok := ctx.Value(boundNoteKey).(int64); ok && bound != noteID {
		return fmt.Errorf("只能修改当前会话绑定的笔记 (id=%d)", bound)
	}
	return nil
}

// ToolExecutor 工具执行函数（ctx 用于取消长耗时操作，如子代理的 AI 调用）
type ToolExecutor func(ctx context.Context, argsJSON string) (*ToolResult, error)

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

// subAgentToolOrder 子代理可用工具：只读检索类，不含写作工具与 run_subagent 自身（防递归）
var subAgentToolOrder = []string{"search_notes", "get_note", "list_all_notes"}

// SubAgentTools 返回子代理可用的工具列表
func SubAgentTools() []Tool {
	out := make([]Tool, 0, len(subAgentToolOrder))
	for _, name := range subAgentToolOrder {
		out = append(out, Registry[name].Def)
	}
	return out
}

var toolOrder = []string{
	"search_notes", "get_note", "list_all_notes", "list_categories", "run_subagent",
	"replace_note_section", "append_note_content", "update_note_title", "create_note",
}

// Execute 执行工具
func Execute(ctx context.Context, name, argsJSON string) (*ToolResult, error) {
	t, ok := Registry[name]
	if !ok {
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	return t.Executor(ctx, argsJSON)
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

	// ---------- list_all_notes ----------
	register(&ToolDef{
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "list_all_notes",
				Description: "获取笔记库中全部笔记的概览列表：标题、简介、标签、实体、创建与修改时间（不含正文）。用户想纵览全库、按时间或标签整体梳理时使用；需要正文再调 get_note。",
				Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
		Executor: execListAllNotes,
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

	// ---------- run_subagent ----------
	register(&ToolDef{
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "run_subagent",
				Description: "委派一个子代理处理需要阅读大量笔记的长上下文任务（如全库总结、多篇笔记对比归纳）。子代理拥有独立上下文，可自行检索并通读笔记全文，完成后返回精炼结论；它只读不写。当任务需要通读多篇笔记、直接处理会让对话上下文过长时，优先使用本工具。",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task": map[string]any{"type": "string", "description": "交给子代理的任务描述，需自包含（目标、范围、期望产出）"},
					},
					"required": []string{"task"},
				},
			},
		},
		Executor: execRunSubAgent,
	})

	// ---------- replace_note_section ----------
	register(&ToolDef{
		Writing: true,
		Def: Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        "replace_note_section",
				Description: "用 new_text 精确替换笔记中的 old_text 片段。old_text 必须与原文一字不差（从 get_note 结果中逐字复制），否则替换失败。修改不直接生效，而是作为提案提交给用户审核。",
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
				Description: "在笔记末尾追加内容。修改不直接生效，而是作为提案提交给用户审核。",
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

func execSearchNotes(_ context.Context, argsJSON string) (*ToolResult, error) {
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

func execGetNote(_ context.Context, argsJSON string) (*ToolResult, error) {
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

func execListAllNotes(_ context.Context, _ string) (*ToolResult, error) {
	var notes []models.Note
	if err := database.DB.
		Select("id", "title", "summary", "created_at", "updated_at").
		Where("is_active = 1").
		Order("updated_at DESC").
		Find(&notes).Error; err != nil {
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
		ID        string              `json:"id"`
		Title     string              `json:"title"`
		Summary   string              `json:"summary"`
		Tags      []string            `json:"tags"`
		Entities  []services.EntityVO `json:"entities"`
		CreatedAt string              `json:"created_at"`
		UpdatedAt string              `json:"updated_at"`
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
			CreatedAt: n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt: n.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	raw, _ := json.Marshal(map[string]any{"total": len(items), "notes": items})
	return &ToolResult{Content: string(raw)}, nil
}

func execListCategories(_ context.Context, _ string) (*ToolResult, error) {
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

// proposeNoteWrite 正文类写作工具公共流程：读取当前内容并应用修改，产出修改提案。
// 不写库、不触发元数据钩子：提案经 SSE 推给前端，用户审核通过后才由常规更新接口落库。
func proposeNoteWrite(noteID int64, toolName string, mutate func(content string) (string, error)) (*ToolResult, error) {
	var note models.Note
	if err := database.DB.Where("id = ? AND is_active = 1", noteID).First(&note).Error; err != nil {
		return nil, fmt.Errorf("笔记不存在 (id=%d)", noteID)
	}

	newContent, err := mutate(note.ContentMD)
	if err != nil {
		return nil, err
	}

	return &ToolResult{
		Content:  `{"ok":true,"pending_review":true,"message":"修改已提交用户审核，用户接受后才会生效"}`,
		Proposal: &NoteProposal{NoteID: noteID, Tool: toolName, NewContent: newContent},
	}, nil
}

// replaceSection 精确替换正文片段（proposeNoteWrite 的 mutate 逻辑，纯函数便于测试）
func replaceSection(content, oldText, newText string) (string, error) {
	if !strings.Contains(content, oldText) {
		return "", fmt.Errorf("替换失败：未在笔记中找到与 old_text 完全一致的片段。请先调用 get_note 获取原文，逐字复制后再试")
	}
	return strings.Replace(content, oldText, newText, 1), nil
}

// appendContent 在正文末尾追加内容（空正文不加前导分隔；纯函数便于测试）
func appendContent(content, add string) string {
	sep := "\n\n"
	if content == "" {
		sep = ""
	}
	return content + sep + add
}

func execReplaceNoteSection(ctx context.Context, argsJSON string) (*ToolResult, error) {
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
	if err := checkBoundNote(ctx, id); err != nil {
		return nil, err
	}
	if args.OldText == "" {
		return nil, fmt.Errorf("old_text 不能为空")
	}

	return proposeNoteWrite(id, "replace_note_section", func(content string) (string, error) {
		return replaceSection(content, args.OldText, args.NewText)
	})
}

func execAppendNoteContent(ctx context.Context, argsJSON string) (*ToolResult, error) {
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
	if err := checkBoundNote(ctx, id); err != nil {
		return nil, err
	}
	if args.Content == "" {
		return nil, fmt.Errorf("content 不能为空")
	}

	return proposeNoteWrite(id, "append_note_content", func(content string) (string, error) {
		return appendContent(content, args.Content), nil
	})
}

func execUpdateNoteTitle(_ context.Context, argsJSON string) (*ToolResult, error) {
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

func execCreateNote(_ context.Context, argsJSON string) (*ToolResult, error) {
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
