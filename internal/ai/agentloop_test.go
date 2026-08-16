package ai

import (
	"strings"
	"testing"
)

// TestCompactLoopMessagesUnderBudget 输入 token 未超预算时消息原样返回
func TestCompactLoopMessagesUnderBudget(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "问题"},
		{Role: "tool", Name: "get_note", ToolCallID: "c1", Content: strings.Repeat("字", 500_000)},
	}
	out := CompactLoopMessages(msgs, 300_000) // 30 万输入 token，未超 40 万预算
	if !strings.Contains(out[2].Content, "字") {
		t.Fatalf("未超预算不应改动: %.30q", out[2].Content)
	}
}

// TestCompactLoopMessagesCompressByTokens 输入 token 超预算后较早的 tool 结果被占位替换，最近 4 条保留原文
func TestCompactLoopMessagesCompressByTokens(t *testing.T) {
	msgs := []Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, Message{
			Role: "tool", Name: "get_note", ToolCallID: "c",
			Content: strings.Repeat("字", 1_000),
		})
	}
	// 上一轮接口返回 50 万输入 token，超过 40 万预算 → 触发压缩（尽管字符数很小）
	out := CompactLoopMessages(msgs, 500_000)

	// 前 6 条被压缩，占位说明带工具名
	for i := 1; i <= 6; i++ {
		if !strings.Contains(out[i].Content, "已省略") || !strings.Contains(out[i].Content, "get_note") {
			t.Errorf("第 %d 条 tool 消息应被压缩为占位说明, got: %.30q", i, out[i].Content)
		}
	}
	// 后 4 条保留原文
	for i := 7; i <= 10; i++ {
		if len([]rune(out[i].Content)) != 1_000 {
			t.Errorf("第 %d 条 tool 消息应保留原文, len=%d", i, len([]rune(out[i].Content)))
		}
	}
	// 非 tool 消息不受影响
	if out[0].Content != "sys" {
		t.Errorf("非 tool 消息不应改动: %q", out[0].Content)
	}
}

// TestCompactLoopMessagesFallback 服务商未返回 usage（token 数 <= 0）时按字符数兜底触发
func TestCompactLoopMessagesFallback(t *testing.T) {
	build := func() []Message {
		msgs := []Message{{Role: "system", Content: "sys"}}
		for i := 0; i < 10; i++ {
			msgs = append(msgs, Message{
				Role: "tool", Name: "get_note", ToolCallID: "c",
				Content: strings.Repeat("字", 50_000), // 总量 50 万字符，超 40 万兜底预算
			})
		}
		return msgs
	}

	// 无 usage：字符数兜底，应压缩
	out := CompactLoopMessages(build(), 0)
	if !strings.Contains(out[1].Content, "已省略") {
		t.Errorf("无 usage 且字符数超兜底预算时应压缩, got: %.30q", out[1].Content)
	}

	// 有 usage 且未超 token 预算：即使字符数超兜底也不压缩（token 为准）
	out = CompactLoopMessages(build(), 300_000)
	if strings.Contains(out[1].Content, "已省略") {
		t.Errorf("有 usage 且 token 未超预算时不应压缩")
	}
}

// TestCompactLoopMessagesAllProtected tool 消息全部在保留窗口内时不做压缩（宁可超限也不丢最近结果）
func TestCompactLoopMessagesAllProtected(t *testing.T) {
	msgs := []Message{
		{Role: "tool", Name: "get_note", ToolCallID: "c1", Content: strings.Repeat("甲", 1_000)},
		{Role: "tool", Name: "get_note", ToolCallID: "c2", Content: strings.Repeat("乙", 1_000)},
	}
	out := CompactLoopMessages(msgs, 900_000)
	for i, m := range out {
		if strings.Contains(m.Content, "已省略") {
			t.Errorf("第 %d 条在保留窗口内，不应被压缩", i)
		}
	}
}

// TestLoopGuard 连续相同的工具调用达到阈值判定停滞；调用变化则重新计数
func TestLoopGuard(t *testing.T) {
	callsA := []ToolCall{{Function: FunctionCall{Name: "get_note", Arguments: `{"note_id":"1"}`}}}
	callsB := []ToolCall{{Function: FunctionCall{Name: "get_note", Arguments: `{"note_id":"2"}`}}}

	var g LoopGuard
	if g.Observe(callsA) {
		t.Fatal("第 1 轮不应判定停滞")
	}
	if g.Observe(callsA) {
		t.Fatal("第 2 轮不应判定停滞")
	}
	if !g.Observe(callsA) {
		t.Fatal("连续 3 轮相同调用应判定停滞")
	}

	// 调用内容变化后重新计数
	var g2 LoopGuard
	g2.Observe(callsA)
	if g2.Observe(callsB) {
		t.Fatal("调用变化后第 1 轮不应判定停滞")
	}
	if g2.Observe(callsB) {
		t.Fatal("调用变化后第 2 轮不应判定停滞")
	}
	if !g2.Observe(callsB) {
		t.Fatal("变化后再连续 3 轮相同应判定停滞")
	}
}

// TestUsageNormalize 验证各家 usage 字段归一化：
// OpenAI 系 cached_tokens / reasoning_tokens 细分、DeepSeek 系 prompt_cache_hit_tokens
func TestUsageNormalize(t *testing.T) {
	// OpenAI 风格
	openaiStyle := &usageRaw{
		PromptTokens:     1234,
		CompletionTokens: 56,
		TotalTokens:      1290,
	}
	openaiStyle.PromptTokensDetails = &struct {
		CachedTokens int `json:"cached_tokens"`
	}{CachedTokens: 1024}
	openaiStyle.CompletionTokensDetails = &struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	}{ReasoningTokens: 20}
	u := openaiStyle.normalize()
	if u.PromptTokens != 1234 || u.CompletionTokens != 56 || u.TotalTokens != 1290 {
		t.Errorf("基本字段归一错误: %+v", u)
	}
	if u.CachedTokens != 1024 {
		t.Errorf("OpenAI cached_tokens 归一错误: got %d, want 1024", u.CachedTokens)
	}
	if u.ReasoningTokens != 20 {
		t.Errorf("reasoning_tokens 归一错误: got %d, want 20", u.ReasoningTokens)
	}

	// DeepSeek 风格
	deepseekStyle := &usageRaw{
		PromptTokens:          100,
		CompletionTokens:      10,
		TotalTokens:           110,
		PromptCacheHitTokens:  80,
		PromptCacheMissTokens: 20,
	}
	u = deepseekStyle.normalize()
	if u.CachedTokens != 80 {
		t.Errorf("DeepSeek 缓存命中归一错误: got %d, want 80", u.CachedTokens)
	}
	if u.ReasoningTokens != 0 {
		t.Errorf("无输出细分时 reasoning_tokens 应为 0, got %d", u.ReasoningTokens)
	}
}
