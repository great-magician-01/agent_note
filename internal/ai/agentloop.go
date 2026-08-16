package ai

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Agent 循环收敛保障（替代固定轮数上限——超限报错是让用户为工程问题买单）：
//  1. 停滞检测：连续多轮完全相同的工具调用视为模型卡死，由调用方改为无工具调用强制收尾；
//  2. 上下文压缩：上一轮接口返回的输入 token 数超过预算时，较早的 tool 结果替换为占位说明
//     （保留最近若干条原文），被省略的内容模型可随时重新调用工具获取，不影响正确性。
const (
	// promptTokensBudget 输入 token 预算：超过即触发压缩。
	// 当前主流模型上下文已达 1M，400K 以内的输入没有压力
	promptTokensBudget = 400_000
	// fallbackRunesBudget 服务商未返回 usage 时的兜底预算：按字符数保守估算
	//（字符数一般多于实际 token 数，只会偏早触发而不会漏触发）
	fallbackRunesBudget   = 400_000
	keepRecentToolContent = 4 // 压缩时保留原文的最近 tool 消息条数
	stuckRepeatThreshold  = 2 // 工具调用指纹连续重复次数达到该值判定为停滞
)

// LoopGuard agent 循环停滞检测器（每个循环实例一个，逐轮 Observe）
type LoopGuard struct {
	prevKey      string
	repeatRounds int
	hasPrev      bool
}

// Observe 记录一轮工具调用；连续相同调用达到阈值时返回 true，
// 调用方应把后续模型调用改为不带工具，强制模型基于已有信息收尾
func (g *LoopGuard) Observe(calls []ToolCall) bool {
	key := toolCallsKey(calls)
	if g.hasPrev && key == g.prevKey {
		g.repeatRounds++
	} else {
		g.repeatRounds = 0
	}
	g.prevKey = key
	g.hasPrev = true
	return g.repeatRounds >= stuckRepeatThreshold
}

// toolCallsKey 生成一轮工具调用的指纹（名称 + 参数原文，按顺序拼接）
func toolCallsKey(calls []ToolCall) string {
	var b strings.Builder
	for _, c := range calls {
		b.WriteString(c.Function.Name)
		b.WriteByte(0)
		b.WriteString(c.Function.Arguments)
		b.WriteByte(1)
	}
	return b.String()
}

// CompactLoopMessages 压缩 agent 循环消息，控制上下文长度。
// 触发依据是上一轮接口返回的真实输入 token 数（lastPromptTokens），超过 400K 预算时：
// 把较早的 tool 消息内容替换为占位说明（保留最近 keepRecentToolContent 条原文）。
// 服务商未返回 usage（lastPromptTokens <= 0）时退化为字符数兜底判断。
// 就地修改并返回原切片。
func CompactLoopMessages(messages []Message, lastPromptTokens int) []Message {
	if !overBudget(messages, lastPromptTokens) {
		return messages
	}

	// 仅较早的 tool 消息参与压缩：保留最后 keepRecentToolContent 条原文
	toolCount := 0
	for i := range messages {
		if messages[i].Role == "tool" {
			toolCount++
		}
	}
	compressible := toolCount - keepRecentToolContent

	compressed := 0
	for i := range messages {
		if messages[i].Role != "tool" || compressed >= compressible {
			continue
		}
		compressed++
		messages[i].Content = fmt.Sprintf("[较早的 %s 结果已省略以控制上下文长度，需要时可重新调用该工具获取]", messages[i].Name)
	}
	return messages
}

// overBudget 判断上下文是否超预算：优先按接口返回的输入 token 数，无 usage 数据时按字符数兜底
func overBudget(messages []Message, lastPromptTokens int) bool {
	if lastPromptTokens > 0 {
		return lastPromptTokens > promptTokensBudget
	}
	total := 0
	for i := range messages {
		total += utf8.RuneCountInString(messages[i].Content)
	}
	return total > fallbackRunesBudget
}
