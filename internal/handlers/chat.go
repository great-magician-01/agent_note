package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/ai"
	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"github.com/great-magician-01/agent_note/internal/services"
	"github.com/great-magician-01/agent_note/internal/snowflake"
)

const (
	// maxHistoryRunes 会话历史上下文的总字数预算：从最新消息开始回溯，
	// 累计超出预算即停止（替代固定条数截断；至少保留最新一条）
	maxHistoryRunes = 60_000
)

// SSE 事件写出助手（心跳 goroutine 与主循环并发写同一 ResponseWriter，写路径统一加锁）
type sseWriter struct {
	c  *gin.Context
	mu sync.Mutex
}

func (w *sseWriter) event(name string, data any) {
	raw, _ := json.Marshal(data)
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", name, raw)
	w.c.Writer.Flush()
}

// ping 发送 SSE 注释心跳，防止 agent 循环长时间无事件导致连接被判定空闲断开
func (w *sseWriter) ping() {
	w.mu.Lock()
	defer w.mu.Unlock()
	fmt.Fprint(w.c.Writer, ": ping\n\n")
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
	sse := &sseWriter{c: c}

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

	// 4. 加载上下文：先限量取最近 200 条（防超长会话全量扫描），再按总字数预算回溯
	var history []models.Message
	database.DB.
		Where("conversation_id = ? AND is_active = 1", conv.ID).
		Order("id DESC").
		Limit(200).
		Find(&history)
	kept := len(history)
	acc := 0
	for i := range history {
		if i > 0 && acc > maxHistoryRunes {
			kept = i
			break
		}
		acc += utf8.RuneCountInString(history[i].Content)
	}
	history = history[:kept]
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

	// 5. agent 循环：不设固定轮数上限——超限报错是让用户为工程问题买单。
	// 收敛由工程手段保证：停滞检测（连续相同工具调用 → 无工具强制收尾）
	// + 上下文压缩（每轮开头压缩较早的工具结果，见 ai.CompactLoopMessages）。
	client := ai.NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)
	tools := ai.ToolsForScope(conv.NoteID != nil)
	ctx := c.Request.Context()
	// 会话绑定笔记时注入绑定 id，正文类写作工具强制校验目标笔记
	if conv.NoteID != nil {
		ctx = ai.WithBoundNoteID(ctx, *conv.NoteID)
	}

	// SSE 心跳：agent 循环可能长时间无事件产出，每 15s 发注释行防连接空闲断开
	pingDone := make(chan struct{})
	defer close(pingDone)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				sse.ping()
			}
		}
	}()

	var pendingMsgs []models.Message // 待落库消息（本轮产生的 assistant/tool）

	flushAndDone := func() {
		// 落库本轮消息
		for i := range pendingMsgs {
			database.DB.Create(&pendingMsgs[i])
		}
		sse.event("done", gin.H{"conversation_id": strconv.FormatInt(conv.ID, 10)})
	}

	var guard ai.LoopGuard
	forceFinal := false   // 判定停滞后，下一轮不带工具强制模型收尾
	lastPromptTokens := 0 // 上一轮接口返回的输入 token 数（驱动上下文压缩；0 = 尚无数据）

	for {
		messages = ai.CompactLoopMessages(messages, lastPromptTokens)

		callTools := tools
		if forceFinal {
			callTools = nil
		}

		result, err := client.ChatStream(ctx, messages, callTools,
			func(delta string) {
				sse.event("delta", gin.H{"content": delta})
			},
			func(think string) {
				sse.event("think", gin.H{"content": think})
			},
		)
		if err != nil {
			// 客户端中断（页面停止/关闭）：上游请求已随 ctx 取消断开。
			// 把本轮已流出的部分正文/思考落库（截断的工具调用参数不完整，直接丢弃不执行），
			// 连接已断，静默收尾不再发事件
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if result != nil && (result.Content != "" || result.Reasoning != "") {
					var rp *string
					if result.Reasoning != "" {
						rp = &result.Reasoning
					}
					pendingMsgs = append(pendingMsgs, models.Message{
						ID:             snowflake.Next(),
						ConversationID: conv.ID,
						Role:           "assistant",
						Content:        result.Content,
						Reasoning:      rp,
					})
				}
				for i := range pendingMsgs {
					database.DB.Create(&pendingMsgs[i])
				}
				return
			}
			fail(err.Error())
			// 已产生的消息也尽量落库
			for i := range pendingMsgs {
				database.DB.Create(&pendingMsgs[i])
			}
			return
		}

		// 记录本轮 token 用量：驱动下一轮上下文压缩，并随 assistant 消息落库
		if result.Usage != nil {
			lastPromptTokens = result.Usage.PromptTokens
		}
		var usagePtr *string
		if result.Usage != nil {
			raw, _ := json.Marshal(result.Usage)
			s := string(raw)
			usagePtr = &s
		}

		// 思考内容（推理模型）随 assistant 消息落库
		var reasoningPtr *string
		if result.Reasoning != "" {
			reasoningPtr = &result.Reasoning
		}

		// 无工具调用（或停滞收尾轮）→ 最终回复，落库 assistant 消息并结束
		if len(result.ToolCalls) == 0 || forceFinal {
			pendingMsgs = append(pendingMsgs, models.Message{
				ID:             snowflake.Next(),
				ConversationID: conv.ID,
				Role:           "assistant",
				Content:        result.Content,
				Reasoning:      reasoningPtr,
				Usage:          usagePtr,
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
			Reasoning:      reasoningPtr,
			Usage:          usagePtr,
		})
		messages = append(messages, ai.Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})

		// 停滞检测：连续完全相同的工具调用视为模型卡死，标记下一轮强制收尾
		if guard.Observe(result.ToolCalls) {
			forceFinal = true
		}

		for _, tc := range result.ToolCalls {
			sse.event("tool_start", gin.H{"id": tc.ID, "name": tc.Function.Name, "input": json.RawMessage(tc.Function.Arguments)})

			toolResult, execErr := ai.Execute(ctx, tc.Function.Name, tc.Function.Arguments)

			var toolContent string
			var summary string
			ok := execErr == nil
			if ok {
				toolContent = toolResult.Content
				summary = toolSummary(tc.Function.Name, toolResult)
				if toolResult.Proposal != nil {
					// 正文修改提案：推给前端审核，用户接受后才落库，不发 note_updated
					sse.event("note_proposal", gin.H{
						"note_id": strconv.FormatInt(toolResult.Proposal.NoteID, 10),
						"tool":    toolResult.Proposal.Tool,
						"content": toolResult.Proposal.NewContent,
					})
				} else if toolResult.NoteUpdated != nil {
					sse.event("note_updated", gin.H{"note_id": strconv.FormatInt(*toolResult.NoteUpdated, 10)})
				}
			} else {
				errRaw, _ := json.Marshal(map[string]string{"error": execErr.Error()})
				toolContent = string(errRaw)
				summary = execErr.Error()
			}

			// 完整结果随事件下发（前端展开面板自行折叠长内容）
			sse.event("tool_end", gin.H{"id": tc.ID, "name": tc.Function.Name, "ok": ok, "summary": summary, "result": toolContent})

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
	case "list_all_notes":
		var parsed struct {
			Total int `json:"total"`
		}
		if err := json.Unmarshal([]byte(r.Content), &parsed); err == nil {
			return fmt.Sprintf("共 %d 条笔记", parsed.Total)
		}
		return "已获取全部笔记"
	case "run_subagent":
		return "子代理已完成任务"
	case "list_categories":
		return "已获取分类列表"
	case "replace_note_section":
		return "已提交替换修改，待用户审核"
	case "append_note_content":
		return "已提交追加修改，待用户审核"
	case "update_note_title":
		return "已修改笔记标题"
	case "create_note":
		return "已创建新笔记"
	default:
		return "工具执行完成"
	}
}
