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
	Model         string         `json:"model"`
	Messages      []Message      `json:"messages"`
	Tools         []Tool         `json:"tools,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
}

// streamOptions 流式附加选项：include_usage 要求服务商在流末尾返回 token 用量
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ---- 流式响应解析 ----

type streamChoice struct {
	Delta struct {
		Content string `json:"content"`
		// 推理模型的思考增量（DeepSeek reasoner 等）
		ReasoningContent string          `json:"reasoning_content"`
		ToolCalls        []toolCallChunk `json:"tool_calls"`
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
	Usage   *usageRaw      `json:"usage"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// usageRaw 接口返回的原始 token 用量（各家服务商字段并存，归一化为 Usage 后使用）
type usageRaw struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	// DeepSeek 系：缓存命中/未命中
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
	// OpenAI 系：输入/输出细分
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// Usage 归一化后的 token 用量：随 assistant 消息落库，并作为 agent 循环上下文压缩的触发依据
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`              // 输入
	CompletionTokens int `json:"completion_tokens"`          // 输出
	TotalTokens      int `json:"total_tokens"`               // 合计
	CachedTokens     int `json:"cached_tokens,omitempty"`    // 输入中命中缓存的部分
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"` // 输出中的思考部分（推理模型）
}

// normalize 归一各家字段差异（缓存命中：OpenAI 的 cached_tokens 与 DeepSeek 的 prompt_cache_hit_tokens）
func (r *usageRaw) normalize() *Usage {
	u := &Usage{
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		TotalTokens:      r.TotalTokens,
		CachedTokens:     r.PromptCacheHitTokens,
	}
	if r.PromptTokensDetails != nil && r.PromptTokensDetails.CachedTokens > 0 {
		u.CachedTokens = r.PromptTokensDetails.CachedTokens
	}
	if r.CompletionTokensDetails != nil {
		u.ReasoningTokens = r.CompletionTokensDetails.ReasoningTokens
	}
	return u
}

// StreamResult 一轮流式调用的聚合结果
type StreamResult struct {
	Content   string
	Reasoning string     // 思考内容（推理模型，可能为空）
	ToolCalls []ToolCall
	Usage     *Usage     // token 用量（请求带 include_usage；服务商不返回时为空）
}

// ChatStream 发起流式 chat 请求；onDelta 回调正文增量，onThink 回调思考增量（均可为 nil）。
// 返回聚合后的完整内容与工具调用（若有）。
// 注意：流中途出错（含 ctx 取消）时返回的 result 携带已聚合的部分内容，err 非 nil。
func (c *Client) ChatStream(ctx context.Context, messages []Message, tools []Tool, onDelta func(string), onThink func(string)) (*StreamResult, error) {
	body := chatRequest{
		Model:         c.Model,
		Messages:      messages,
		Tools:         tools,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
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
			return result, fmt.Errorf("AI 服务错误: %s", chunk.Error.Message)
		}
		// usage 通常在末尾的独立空 choices 块中返回（stream_options.include_usage）
		if chunk.Usage != nil {
			result.Usage = chunk.Usage.normalize()
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.ReasoningContent != "" {
			result.Reasoning += delta.ReasoningContent
			if onThink != nil {
				onThink(delta.ReasoningContent)
			}
		}
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
		// 返回已聚合的部分结果（调用方对 context.Canceled 等中断场景可能想保留部分输出）
		return result, fmt.Errorf("读取 AI 流失败: %w", err)
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
// 同时返回归一化后的 token 用量（服务商不返回时为 nil；ErrNoToolCall 时也尽量带回用量供记录）
func (c *Client) ChatToolCall(ctx context.Context, systemPrompt, userContent string, tool Tool) (*ToolCall, *Usage, error) {
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
		return nil, nil, fmt.Errorf("请求 AI 服务失败: %w", err)
	}
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		status, raw, err = do(buildBody(false))
		if err != nil {
			return nil, nil, fmt.Errorf("请求 AI 服务失败: %w", err)
		}
	}
	if status != http.StatusOK {
		return nil, nil, fmt.Errorf("AI 服务返回 %d: %s", status, string(raw))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage *usageRaw `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, fmt.Errorf("响应解析失败: %w", err)
	}
	var usage *Usage
	if parsed.Usage != nil {
		usage = parsed.Usage.normalize()
	}
	if len(parsed.Choices) == 0 {
		return nil, usage, fmt.Errorf("响应无 choices")
	}

	// 找到目标工具调用
	for _, tc := range parsed.Choices[0].Message.ToolCalls {
		if tc.Function.Name == tool.Function.Name && tc.Function.Arguments != "" {
			return &tc, usage, nil
		}
	}
	return nil, usage, ErrNoToolCall
}
