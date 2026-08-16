package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client OpenAI 兼容客户端（任意 baseUrl 通用）
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	http    *http.Client
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		http:    &http.Client{Timeout: 0}, // 流式长连接不设总超时，由 ctx 控制
	}
}

// ---- OpenAI 消息结构 ----

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// ---- 流式响应解析 ----

type streamChoice struct {
	Delta struct {
		Content   string          `json:"content"`
		ToolCalls []toolCallChunk `json:"tool_calls"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type toolCallChunk struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type streamResponse struct {
	Choices []streamChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// StreamResult 一轮流式调用的聚合结果
type StreamResult struct {
	Content   string
	ToolCalls []ToolCall
}

// ChatStream 发起流式 chat 请求；onDelta 回调文本增量。
// 返回聚合后的完整内容与工具调用（若有）。
func (c *Client) ChatStream(ctx context.Context, messages []Message, tools []Tool, onDelta func(string)) (*StreamResult, error) {
	body := chatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 AI 服务失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("AI 服务返回 %d: %s", resp.StatusCode, string(raw))
	}

	result := &StreamResult{}
	// 工具调用聚合：按 index 累积
	toolAcc := map[int]*ToolCall{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk streamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("AI 服务错误: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			result.Content += delta.Content
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			acc, ok := toolAcc[tc.Index]
			if !ok {
				acc = &ToolCall{Type: "function"}
				toolAcc[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Function.Name != "" {
				acc.Function.Name = tc.Function.Name
			}
			acc.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取 AI 流失败: %w", err)
	}

	// 按 index 顺序输出工具调用
	for i := 0; i < len(toolAcc); i++ {
		if tc, ok := toolAcc[i]; ok && tc.Function.Name != "" {
			result.ToolCalls = append(result.ToolCalls, *tc)
		}
	}
	return result, nil
}

// ChatOnce 非流式最小调用（测试连接用）
func (c *Client) ChatOnce(ctx context.Context, prompt string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	body := chatRequest{
		Model:    c.Model,
		Messages: []Message{{Role: "user", Content: prompt}},
		Stream:   false,
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务返回 %d: %s", resp.StatusCode, string(raw))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("响应解析失败: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return fmt.Errorf("响应无 choices")
	}
	return nil
}

// ErrNoToolCall 模型未发起工具调用（调用方应重试）
var ErrNoToolCall = fmt.Errorf("模型未调用工具")

// ChatToolCall 非流式调用，期望模型通过**工具调用**返回结构化内容。
// 优先用 tool_choice 强制指定工具；服务商不支持 tool_choice（HTTP 4xx）时降级为仅靠提示词引导。
// 模型最终仍未调用工具时返回 ErrNoToolCall。
func (c *Client) ChatToolCall(ctx context.Context, systemPrompt, userContent string, tool Tool) (*ToolCall, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	type toolChoice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	type toolChatRequest struct {
		Model      string      `json:"model"`
		Messages   []Message   `json:"messages"`
		Tools      []Tool      `json:"tools"`
		ToolChoice *toolChoice `json:"tool_choice,omitempty"`
		Stream     bool        `json:"stream"`
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}

	buildBody := func(withChoice bool) []byte {
		req := toolChatRequest{
			Model:    c.Model,
			Messages: messages,
			Tools:    []Tool{tool},
			Stream:   false,
		}
		if withChoice {
			tc := &toolChoice{Type: "function"}
			tc.Function.Name = tool.Function.Name
			req.ToolChoice = tc
		}
		payload, _ := json.Marshal(req)
		return payload
	}

	do := func(payload []byte) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		resp, err := c.http.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return resp.StatusCode, raw, nil
	}

	// 第一次：带 tool_choice 强制；若 4xx（多半是不支持 tool_choice）→ 降级重试
	status, raw, err := do(buildBody(true))
	if err != nil {
		return nil, fmt.Errorf("请求 AI 服务失败: %w", err)
	}
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		status, raw, err = do(buildBody(false))
		if err != nil {
			return nil, fmt.Errorf("请求 AI 服务失败: %w", err)
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("AI 服务返回 %d: %s", status, string(raw))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("响应解析失败: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("响应无 choices")
	}

	// 找到目标工具调用
	for _, tc := range parsed.Choices[0].Message.ToolCalls {
		if tc.Function.Name == tool.Function.Name && tc.Function.Arguments != "" {
			return &tc, nil
		}
	}
	return nil, ErrNoToolCall
}
