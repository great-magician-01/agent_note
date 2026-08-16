package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/ai"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/services"
	"github.com/great-magician-01/agent_note/internal/snowflake"
)

const (
	maxAgentRounds  = 8  // agent 循环最大轮数
	contextMessages = 20 // 上下文携带的最近消息数
)

// SSE 事件写出助手
type sseWriter struct {
	c *gin.Context
}

func (w *sseWriter) event(name string, data any) {
	raw, _ := json.Marshal(data)
	fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", name, raw)
	w.c.Writer.Flush()
}

// Chat POST /api/chat（SSE）
// 请求：{conversation_id?: string, note_id?: string, content: string}
func Chat(c *gin.Context) {
	var req struct {
		ConversationID *int64 `json:"conversation_id,string"`
		NoteID         *int64 `json:"note_id,string"`
		Content        string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	// SSE 头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	sse := &sseWriter{c}

	fail := func(msg string) {
		sse.event("error", gin.H{"message": msg})
	}

	// 1. 取激活 AI 配置
	cfg, err := services.GetActiveAIConfig()
	if err != nil {
		fail(err.Error())
		return
	}

	// 2. 解析 / 创建会话
	var conv models.Conversation
	if req.ConversationID != nil && *req.ConversationID != 0 {
		if err := database.DB.Where("id = ? AND is_active = 1", *req.ConversationID).First(&conv).Error; err != nil {
			fail("会话不存在")
			return
		}
	} else {
		conv = models.Conversation{ID: snowflake.Next(), NoteID: req.NoteID}
		if err := database.DB.Create(&conv).Error; err != nil {
			fail("创建会话失败")
			return
		}
	}

	// 3. 保存用户消息
	userMsg := models.Message{
		ID:             snowflake.Next(),
		ConversationID: conv.ID,
		Role:           "user",
		Content:        req.Content,
	}
	if err := database.DB.Create(&userMsg).Error; err != nil {
		fail("保存消息失败")
		return
	}

	// 会话标题：首条用户消息时生成
	if conv.Title == "新对话" {
		title := []rune(req.Content)
		if len(title) > 20 {
			title = title[:20]
		}
		database.DB.Model(&models.Conversation{}).Where("id = ?", conv.ID).
			Updates(map[string]any{"title": string(title), "updated_at": time.Now()})
	} else {
		database.DB.Model(&models.Conversation{}).Where("id = ?", conv.ID).
			Update("updated_at", time.Now())
	}

	sse.event("meta", gin.H{
		"conversation_id": strconv.FormatInt(conv.ID, 10),
		"user_message_id": strconv.FormatInt(userMsg.ID, 10),
	})

	// 4. 加载上下文（最近 20 条）
	var history []models.Message
	database.DB.
		Where("conversation_id = ? AND is_active = 1", conv.ID).
		Order("id DESC").
		Limit(contextMessages).
		Find(&history)
	// 反转为时间正序
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	messages := make([]ai.Message, 0, len(history)+1)

	// 系统提示词：会话绑定笔记 → 写作助手；否则全局助手
	if conv.NoteID != nil {
		messages = append(messages, ai.Message{
			Role:    "system",
			Content: ai.WritingAssistantPrompt(strconv.FormatInt(*conv.NoteID, 10)),
		})
	} else {
		messages = append(messages, ai.Message{Role: "system", Content: ai.GlobalAssistantPrompt})
	}

	for _, m := range history {
		am := ai.Message{Role: m.Role, Content: m.Content}
		if m.Role == "assistant" && m.ToolCalls != nil && *m.ToolCalls != "" {
			var tcs []ai.ToolCall
			if err := json.Unmarshal([]byte(*m.ToolCalls), &tcs); err == nil {
				am.ToolCalls = tcs
			}
		}
		if m.Role == "tool" {
			am.ToolCallID = m.ToolCallID
			am.Name = m.Name
		}
		messages = append(messages, am)
	}

	// 5. agent 循环
	client := ai.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	tools := ai.ToolsForScope(conv.NoteID != nil)
	ctx := c.Request.Context()

	var pendingMsgs []models.Message // 待落库消息（本轮产生的 assistant/tool）

	flushAndDone := func() {
		// 落库本轮消息
		for i := range pendingMsgs {
			database.DB.Create(&pendingMsgs[i])
		}
		sse.event("done", gin.H{"conversation_id": strconv.FormatInt(conv.ID, 10)})
	}

	for round := 0; round < maxAgentRounds; round++ {
		result, err := client.ChatStream(ctx, messages, tools, func(delta string) {
			sse.event("delta", gin.H{"content": delta})
		})
		if err != nil {
			fail(err.Error())
			// 已产生的消息也尽量落库
			for i := range pendingMsgs {
				database.DB.Create(&pendingMsgs[i])
			}
			return
		}

		// 无工具调用 → 最终回复，落库 assistant 消息并结束
		if len(result.ToolCalls) == 0 {
			pendingMsgs = append(pendingMsgs, models.Message{
				ID:             snowflake.Next(),
				ConversationID: conv.ID,
				Role:           "assistant",
				Content:        result.Content,
			})
			flushAndDone()
			return
		}

		// 有工具调用：落库 assistant(tool_calls) 消息，逐个执行工具
		toolCallsRaw, _ := json.Marshal(result.ToolCalls)
		tcStr := string(toolCallsRaw)
		pendingMsgs = append(pendingMsgs, models.Message{
			ID:             snowflake.Next(),
			ConversationID: conv.ID,
			Role:           "assistant",
			Content:        result.Content,
			ToolCalls:      &tcStr,
		})
		messages = append(messages, ai.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})

		for _, tc := range result.ToolCalls {
			sse.event("tool_start", gin.H{"name": tc.Function.Name, "input": json.RawMessage(tc.Function.Arguments)})

			toolResult, execErr := ai.Execute(tc.Function.Name, tc.Function.Arguments)

			var toolContent string
			var summary string
			ok := execErr == nil
			if ok {
				toolContent = toolResult.Content
				summary = toolSummary(tc.Function.Name, toolResult)
				if toolResult.NoteUpdated != nil {
					sse.event("note_updated", gin.H{"note_id": strconv.FormatInt(*toolResult.NoteUpdated, 10)})
				}
			} else {
				errRaw, _ := json.Marshal(map[string]string{"error": execErr.Error()})
				toolContent = string(errRaw)
				summary = execErr.Error()
			}

			sse.event("tool_end", gin.H{"name": tc.Function.Name, "ok": ok, "summary": summary})

			pendingMsgs = append(pendingMsgs, models.Message{
				ID:             snowflake.Next(),
				ConversationID: conv.ID,
				Role:           "tool",
				Content:        toolContent,
				ToolCallID:     tc.ID,
				Name:           tc.Function.Name,
			})
			messages = append(messages, ai.Message{
				Role:       "tool",
				Content:    toolContent,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
	}

	fail(fmt.Sprintf("工具调用轮数超过上限（%d）", maxAgentRounds))
	for i := range pendingMsgs {
		database.DB.Create(&pendingMsgs[i])
	}
}

// toolSummary 生成工具执行的一行摘要（前端状态行展示）
func toolSummary(name string, r *ai.ToolResult) string {
	switch name {
	case "search_notes":
		var parsed struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal([]byte(r.Content), &parsed); err == nil {
			return fmt.Sprintf("找到 %d 条笔记", parsed.Total)
		}
		return "搜索完成"
	case "get_note":
		return "已读取笔记全文"
	case "list_categories":
		return "已获取分类列表"
	case "replace_note_section":
		return "已替换笔记内容"
	case "append_note_content":
		return "已追加内容到笔记"
	case "update_note_title":
		return "已修改笔记标题"
	case "create_note":
		return "已创建新笔记"
	default:
		return "工具执行完成"
	}
}
