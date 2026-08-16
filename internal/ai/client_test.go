package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseChatHandler 返回一个按 SSE 格式逐行输出 lines 的 mock chat/completions 处理器。
// extraCheck 用于在响应前校验请求（如请求头 / 请求体）。
func sseChatHandler(t *testing.T, lines []string, extraCheck func(r *http.Request, body []byte)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径错误: got %q, want /chat/completions", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("读取请求体失败: %v", err)
		}
		if extraCheck != nil {
			extraCheck(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, line := range lines {
			// SSE 每条消息以空行结尾
			io.WriteString(w, line+"\n\n")
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// TestNewClientTrimsTrailingSlash 验证 NewClient 裁掉 baseURL 尾部斜杠，
// 拼接后请求路径恰好是 /chat/completions 而不是 //chat/completions。
func TestNewClientTrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	// 故意带尾部斜杠构造客户端
	c := NewClient(srv.URL+"/", "test-key", "test-model")
	_, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ChatStream 出错: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("请求路径错误: got %q, want /chat/completions", gotPath)
	}
}

// TestChatStream 验证流式调用的正常聚合：
// content / reasoning_content 增量聚合、tool_calls 按 index 分片聚合与乱序恢复、
// 回调触发、[DONE] 结束、非 data 行与坏 JSON 行被跳过、请求头与请求体正确。
func TestChatStream(t *testing.T) {
	lines := []string{
		// 非 data 前缀行，应被跳过
		"event: message",
		// 坏 JSON 行，应被跳过且不报错
		`data: {oops`,
		// 思考增量
		`data: {"choices":[{"delta":{"reasoning_content":"思考"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"一下"}}]}`,
		// 正文增量
		`data: {"choices":[{"delta":{"content":"你好"}}]}`,
		// 工具调用：index=1 先乱序到达（id/type/name 在首个分片，arguments 分两片）
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"get_note","arguments":"{\"note_id\":\"123"}}]}}]}`,
		// index=0：name 整体到达，arguments 分两片
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_notes","arguments":"{\"keywords\":[\"go\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"]}"}}]}}]}`,
		// index=1 的 arguments 第二片
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"content":"，世界"}}]}`,
		`data: [DONE]`,
		// [DONE] 之后的行不应再被处理
		`data: {"choices":[{"delta":{"content":"不应出现"}}]}`,
	}

	srv := httptest.NewServer(sseChatHandler(t, lines, func(r *http.Request, body []byte) {
		// 校验请求头
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization 头错误: got %q, want %q", got, "Bearer test-key")
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept 头错误: got %q, want text/event-stream", got)
		}
		// 校验请求体
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("请求体 JSON 解析失败: %v", err)
			return
		}
		if !req.Stream {
			t.Errorf("请求体 stream 字段应为 true")
		}
		if req.Model != "test-model" {
			t.Errorf("请求体 model 错误: got %q, want test-model", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hi" {
			t.Errorf("请求体 messages 未原样传递: %+v", req.Messages)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")

	var deltaBuf, thinkBuf strings.Builder
	result, err := c.ChatStream(
		context.Background(),
		[]Message{{Role: "user", Content: "hi"}},
		nil,
		func(s string) { deltaBuf.WriteString(s) },
		func(s string) { thinkBuf.WriteString(s) },
	)
	if err != nil {
		t.Fatalf("ChatStream 出错: %v", err)
	}

	// 正文与思考聚合
	if result.Content != "你好，世界" {
		t.Errorf("Content 聚合错误: got %q, want %q", result.Content, "你好，世界")
	}
	if result.Reasoning != "思考一下" {
		t.Errorf("Reasoning 聚合错误: got %q, want %q", result.Reasoning, "思考一下")
	}
	// 回调累计文本与聚合结果一致
	if deltaBuf.String() != result.Content {
		t.Errorf("onDelta 累计 %q 与聚合结果 %q 不一致", deltaBuf.String(), result.Content)
	}
	if thinkBuf.String() != result.Reasoning {
		t.Errorf("onThink 累计 %q 与聚合结果 %q 不一致", thinkBuf.String(), result.Reasoning)
	}

	// 工具调用：乱序到达后按 index 顺序输出
	if len(result.ToolCalls) != 2 {
		t.Fatalf("ToolCalls 数量错误: got %d, want 2", len(result.ToolCalls))
	}
	tc0 := result.ToolCalls[0]
	if tc0.ID != "call_1" || tc0.Type != "function" ||
		tc0.Function.Name != "search_notes" || tc0.Function.Arguments != `{"keywords":["go"]}` {
		t.Errorf("ToolCalls[0] 聚合错误: %+v", tc0)
	}
	tc1 := result.ToolCalls[1]
	if tc1.ID != "call_2" || tc1.Type != "function" ||
		tc1.Function.Name != "get_note" || tc1.Function.Arguments != `{"note_id":"123"}` {
		t.Errorf("ToolCalls[1] 聚合错误: %+v", tc1)
	}
	// 工具参数应为合法 JSON（拼接完整）
	var args map[string]any
	if err := json.Unmarshal([]byte(tc0.Function.Arguments), &args); err != nil {
		t.Errorf("ToolCalls[0] arguments 不是合法 JSON: %v", err)
	}
}

// TestChatStreamHTTPError 验证非 200 响应：err 含状态码，result 为 nil。
func TestChatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "internal error")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	result, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil, nil)
	if err == nil {
		t.Fatal("期望出错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("错误信息应含 500: %v", err)
	}
	if result != nil {
		t.Errorf("非 200 时 result 应为 nil, got %+v", result)
	}
}

// TestChatStreamMidStreamError 验证流中途出现 error 消息：
// 返回的 result 携带已聚合的部分内容，err 含服务端错误消息。
func TestChatStreamMidStreamError(t *testing.T) {
	lines := []string{
		`data: {"choices":[{"delta":{"content":"部分"}}]}`,
		`data: {"error":{"message":"boom"}}`,
	}
	srv := httptest.NewServer(sseChatHandler(t, lines, nil))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	result, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil, nil)
	if err == nil {
		t.Fatal("期望出错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("错误信息应含 boom: %v", err)
	}
	if result == nil || result.Content != "部分" {
		t.Errorf("result 应携带已聚合的部分内容 \"部分\", got %+v", result)
	}
}

// metaTool 测试用的工具定义（与 worker 使用的元数据提交工具同名）
func metaTool() Tool {
	return Tool{
		Type: "function",
		Function: ToolFunction{
			Name:        "submit_note_metadata",
			Description: "提交元数据",
			Parameters:  map[string]any{"type": "object"},
		},
	}
}

// toolCallResponse 构造一个带指定工具调用的非流式响应体（附带 usage）。
func toolCallResponse(name, arguments string) string {
	return `{"choices":[{"message":{"tool_calls":[{"id":"call_1","type":"function","function":{"name":"` + name + `","arguments":` + arguments + `}}]}}],"usage":{"prompt_tokens":300,"completion_tokens":12,"total_tokens":312,"prompt_cache_hit_tokens":256,"prompt_cache_miss_tokens":44}}`
}

// TestChatToolCallSuccess 验证正常的工具调用返回，且 usage 被解析归一。
func TestChatToolCallSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, toolCallResponse("submit_note_metadata", `"{\"summary\":\"简介\"}"`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	tc, usage, err := c.ChatToolCall(context.Background(), "sys", "内容", metaTool())
	if err != nil {
		t.Fatalf("ChatToolCall 出错: %v", err)
	}
	if tc.Function.Name != "submit_note_metadata" {
		t.Errorf("工具名错误: got %q", tc.Function.Name)
	}
	if tc.Function.Arguments == "" {
		t.Error("arguments 不应为空")
	}
	if usage == nil {
		t.Fatal("应解析到 usage")
	}
	if usage.PromptTokens != 300 || usage.CompletionTokens != 12 || usage.TotalTokens != 312 {
		t.Errorf("usage 基本字段错误: %+v", usage)
	}
	if usage.CachedTokens != 256 {
		t.Errorf("DeepSeek 缓存命中归一错误: got %d, want 256", usage.CachedTokens)
	}
}

// TestChatToolCallFallback 验证 tool_choice 降级：
// 第一次请求（带 tool_choice）返回 400 后，应自动用不带 tool_choice 的请求重试并成功。
func TestChatToolCallFallback(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			// 第一次：模拟不支持 tool_choice 的服务商
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"tool_choice not supported"}}`)
			return
		}
		io.WriteString(w, toolCallResponse("submit_note_metadata", `"{\"summary\":\"降级成功\"}"`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	tc, _, err := c.ChatToolCall(context.Background(), "sys", "内容", metaTool())
	if err != nil {
		t.Fatalf("降级后应成功: %v", err)
	}
	if tc.Function.Name != "submit_note_metadata" {
		t.Errorf("工具名错误: got %q", tc.Function.Name)
	}
	if len(bodies) != 2 {
		t.Fatalf("应发起 2 次请求, got %d", len(bodies))
	}
	// 第一次请求体含 tool_choice，第二次不含
	if !strings.Contains(string(bodies[0]), "tool_choice") {
		t.Error("第一次请求体应包含 tool_choice")
	}
	if strings.Contains(string(bodies[1]), "tool_choice") {
		t.Error("第二次请求体不应包含 tool_choice")
	}
}

// TestChatToolCallNoToolCall 验证模型 200 但未发起目标工具调用时返回 ErrNoToolCall。
func TestChatToolCallNoToolCall(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		// tool_calls 为空
		{"空工具调用", `{"choices":[{"message":{"content":"没有调用工具"}}]}`},
		// 工具名不匹配
		{"工具名不匹配", toolCallResponse("other_tool", `"{\"x\":1}"`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			c := NewClient(srv.URL, "k", "m")
			_, _, err := c.ChatToolCall(context.Background(), "sys", "内容", metaTool())
			if !errors.Is(err, ErrNoToolCall) {
				t.Fatalf("期望 ErrNoToolCall, got %v", err)
			}
		})
	}
}

// TestChatToolCallBothFail 验证第一次 400（触发降级）后第二次仍非 200：err 非 nil 且含第二次状态码。
func TestChatToolCallBothFail(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "server down")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	_, _, err := c.ChatToolCall(context.Background(), "sys", "内容", metaTool())
	if err == nil {
		t.Fatal("期望出错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("错误信息应含第二次的状态码 500: %v", err)
	}
	if n != 2 {
		t.Errorf("应发起 2 次请求, got %d", n)
	}
}

// TestChatOnce 验证非流式最小调用：成功 / choices 为空 / 401。
func TestChatOnce(t *testing.T) {
	t.Run("成功", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
		}))
		defer srv.Close()
		c := NewClient(srv.URL, "k", "m")
		if err := c.ChatOnce(context.Background(), "ping"); err != nil {
			t.Fatalf("ChatOnce 应成功: %v", err)
		}
	})

	t.Run("choices 为空", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"choices":[]}`)
		}))
		defer srv.Close()
		c := NewClient(srv.URL, "k", "m")
		if err := c.ChatOnce(context.Background(), "ping"); err == nil {
			t.Fatal("choices 为空时应返回错误")
		}
	})

	t.Run("401", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"message":"invalid key"}}`)
		}))
		defer srv.Close()
		c := NewClient(srv.URL, "k", "m")
		err := c.ChatOnce(context.Background(), "ping")
		if err == nil {
			t.Fatal("401 时应返回错误")
		}
		if !strings.Contains(err.Error(), "401") {
			t.Errorf("错误信息应含 401: %v", err)
		}
	})
}

// TestChatStreamUsage 验证流式请求带 stream_options.include_usage，
// 且流末尾的 usage 块（空 choices）被解析并归一化。
func TestChatStreamUsage(t *testing.T) {
	lines := []string{
		`data: {"choices":[{"delta":{"content":"你好"}}]}`,
		// 末尾 usage 块：choices 为空
		`data: {"choices":[],"usage":{"prompt_tokens":1234,"completion_tokens":56,"total_tokens":1290,"prompt_tokens_details":{"cached_tokens":1024},"completion_tokens_details":{"reasoning_tokens":20}}}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(sseChatHandler(t, lines, func(r *http.Request, body []byte) {
		if !strings.Contains(string(body), `"include_usage":true`) {
			t.Errorf("请求体应带 stream_options.include_usage: %s", body)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	result, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ChatStream 出错: %v", err)
	}
	if result.Content != "你好" {
		t.Errorf("Content 聚合错误: got %q", result.Content)
	}
	if result.Usage == nil {
		t.Fatal("应解析到 usage")
	}
	u := result.Usage
	if u.PromptTokens != 1234 || u.CompletionTokens != 56 || u.TotalTokens != 1290 {
		t.Errorf("usage 基本字段错误: %+v", u)
	}
	if u.CachedTokens != 1024 {
		t.Errorf("cached_tokens 归一错误: got %d, want 1024", u.CachedTokens)
	}
	if u.ReasoningTokens != 20 {
		t.Errorf("reasoning_tokens 归一错误: got %d, want 20", u.ReasoningTokens)
	}
}

// TestChatStreamNoUsage 服务商不返回 usage 时 Usage 为 nil，不影响正常聚合。
func TestChatStreamNoUsage(t *testing.T) {
	lines := []string{
		`data: {"choices":[{"delta":{"content":"hi"}}]}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(sseChatHandler(t, lines, nil))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "m")
	result, err := c.ChatStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ChatStream 出错: %v", err)
	}
	if result.Usage != nil {
		t.Errorf("无 usage 块时 Usage 应为 nil, got %+v", result.Usage)
	}
	if result.Content != "hi" {
		t.Errorf("Content 聚合错误: got %q", result.Content)
	}
}
