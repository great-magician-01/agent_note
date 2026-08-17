package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/great-magician-01/agent_note/internal/services"
)

// execRunSubAgent run_subagent 工具：启动一个独立上下文的只读子代理处理长上下文任务，
// 子代理自行检索/通读笔记，最终结论作为工具结果回传主代理。
// 循环不设固定轮数上限，收敛保障与主循环一致（停滞检测 + 上下文压缩，见 agentloop.go）。
func execRunSubAgent(ctx context.Context, argsJSON string) (*ToolResult, error) {
	var args struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("参数解析失败: %w", err)
	}
	if args.Task == "" {
		return nil, fmt.Errorf("task 不能为空")
	}

	cfg, err := services.GetActiveAIConfig()
	if err != nil {
		return nil, err
	}
	client := NewClient(cfg.BaseURL, cfg.APIKey, cfg.Model)

	messages := []Message{
		{Role: "system", Content: SubAgentPrompt},
		{Role: "user", Content: args.Task},
	}
	tools := SubAgentTools()

	// 子代理可用工具白名单：执行前校验，拦截模型越权调用（如写作类工具）
	allowed := make(map[string]bool, len(tools))
	for _, t := range tools {
		allowed[t.Function.Name] = true
	}

	var guard LoopGuard
	forceFinal := false   // 判定停滞后，下一轮不带工具强制模型收尾
	lastPromptTokens := 0 // 上一轮接口返回的输入 token 数（驱动上下文压缩；0 = 尚无数据）
	var usageSum Usage    // 各轮累计用量：随结果回传（经 tool 消息落库）并打日志
	rounds := 0

	for {
		messages = CompactLoopMessages(messages, lastPromptTokens)

		callTools := tools
		if forceFinal {
			callTools = nil
		}

		// 子代理输出只进工具结果，无需流式回调
		result, err := client.ChatStream(ctx, messages, callTools, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("子代理调用失败: %w", err)
		}
		rounds++

		if result.Usage != nil {
			lastPromptTokens = result.Usage.PromptTokens
			usageSum.PromptTokens += result.Usage.PromptTokens
			usageSum.CompletionTokens += result.Usage.CompletionTokens
			usageSum.TotalTokens += result.Usage.TotalTokens
			usageSum.CachedTokens += result.Usage.CachedTokens
			usageSum.ReasoningTokens += result.Usage.ReasoningTokens
		}

		// 无工具调用（或停滞收尾轮）→ 子代理完成，结论回传主代理
		if len(result.ToolCalls) == 0 || forceFinal {
			out := map[string]any{"ok": true, "result": result.Content}
			if usageSum.TotalTokens > 0 {
				out["usage"] = usageSum
				log.Printf("[subagent] done: rounds=%d prompt=%d completion=%d cached=%d reasoning=%d",
					rounds, usageSum.PromptTokens, usageSum.CompletionTokens, usageSum.CachedTokens, usageSum.ReasoningTokens)
			} else {
				log.Printf("[subagent] done: rounds=%d (服务商未返回 usage)", rounds)
			}
			raw, _ := json.Marshal(out)
			return &ToolResult{Content: string(raw)}, nil
		}

		// 停滞检测：连续完全相同的工具调用视为模型卡死
		if guard.Observe(result.ToolCalls) {
			forceFinal = true
		}

		messages = append(messages, Message{
			Role:      "assistant",
			Content:   result.Content,
			ToolCalls: result.ToolCalls,
		})
		for _, tc := range result.ToolCalls {
			var toolContent string
			if !allowed[tc.Function.Name] {
				// 白名单外的工具：不执行，直接返回错误工具结果让模型改用可用工具
				log.Printf("[subagent] 拦截白名单外工具调用: %s", tc.Function.Name)
				errRaw, _ := json.Marshal(map[string]string{"error": "该工具在子代理中不可用"})
				toolContent = string(errRaw)
			} else {
				toolResult, execErr := Execute(ctx, tc.Function.Name, tc.Function.Arguments)
				if execErr != nil {
					errRaw, _ := json.Marshal(map[string]string{"error": execErr.Error()})
					toolContent = string(errRaw)
				} else {
					toolContent = toolResult.Content
				}
			}
			messages = append(messages, Message{
				Role:       "tool",
				Content:    toolContent,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			})
		}
	}
}
