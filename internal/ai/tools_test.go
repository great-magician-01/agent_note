package ai

import (
	"context"
	"strings"
	"testing"
)

// TestRegistryCompleteness 验证注册表完整性：
// toolOrder 中的 9 个工具全部存在于 Registry，且 Registry 不多不少恰好 9 个。
func TestRegistryCompleteness(t *testing.T) {
	want := []string{
		"search_notes", "get_note", "list_all_notes", "list_categories", "run_subagent",
		"replace_note_section", "append_note_content", "update_note_title", "create_note",
	}
	if len(toolOrder) != len(want) {
		t.Fatalf("toolOrder 长度变化: got %d, want %d", len(toolOrder), len(want))
	}
	for _, name := range want {
		if _, ok := Registry[name]; !ok {
			t.Errorf("Registry 缺少工具 %q", name)
		}
	}
	if len(Registry) != len(want) {
		t.Errorf("Registry 大小错误: got %d, want %d", len(Registry), len(want))
	}
}

// TestToolsForScope 验证按会话范围过滤工具：
// 非笔记绑定会话不含任何 Writing 工具；笔记绑定会话含全部工具且顺序与 toolOrder 一致。
func TestToolsForScope(t *testing.T) {
	// 统计 Registry 中 Writing 工具数量，推算非绑定会话应有的工具数
	writing := 0
	for _, def := range Registry {
		if def.Writing {
			writing++
		}
	}
	// 当前源码中后 4 个（replace_note_section / append_note_content / update_note_title / create_note）为写作工具
	if writing != 4 {
		t.Fatalf("Writing 工具数量变化: got %d, want 4", writing)
	}

	// 首页全局助手：不含 Writing 工具
	global := ToolsForScope(false)
	if len(global) != len(toolOrder)-writing {
		t.Fatalf("ToolsForScope(false) 长度错误: got %d, want %d", len(global), len(toolOrder)-writing)
	}
	for _, tool := range global {
		if Registry[tool.Function.Name].Writing {
			t.Errorf("ToolsForScope(false) 不应包含 Writing 工具 %q", tool.Function.Name)
		}
	}
	// 非绑定会话应保留 toolOrder 中非写作工具的相对顺序
	var wantGlobal []string
	for _, name := range toolOrder {
		if !Registry[name].Writing {
			wantGlobal = append(wantGlobal, name)
		}
	}
	for i, tool := range global {
		if tool.Function.Name != wantGlobal[i] {
			t.Errorf("ToolsForScope(false)[%d] 名称错误: got %q, want %q", i, tool.Function.Name, wantGlobal[i])
		}
	}

	// 编辑页写作助手：全部工具，顺序与 toolOrder 一致
	bound := ToolsForScope(true)
	if len(bound) != len(toolOrder) {
		t.Fatalf("ToolsForScope(true) 长度错误: got %d, want %d", len(bound), len(toolOrder))
	}
	for i, tool := range bound {
		if tool.Function.Name != toolOrder[i] {
			t.Errorf("ToolsForScope(true)[%d] 名称错误: got %q, want %q", i, tool.Function.Name, toolOrder[i])
		}
	}
}

// TestSubAgentTools 验证子代理工具集：只含只读检索类，不含写作工具与 run_subagent 自身。
func TestSubAgentTools(t *testing.T) {
	tools := SubAgentTools()
	if len(tools) != len(subAgentToolOrder) {
		t.Fatalf("SubAgentTools 长度错误: got %d, want %d", len(tools), len(subAgentToolOrder))
	}
	for i, tool := range tools {
		if tool.Function.Name != subAgentToolOrder[i] {
			t.Errorf("SubAgentTools()[%d] 名称错误: got %q, want %q", i, tool.Function.Name, subAgentToolOrder[i])
		}
		if tool.Function.Name == "run_subagent" {
			t.Error("子代理工具集不应包含 run_subagent 自身（防递归）")
		}
		if Registry[tool.Function.Name].Writing {
			t.Errorf("子代理工具集不应包含 Writing 工具 %q", tool.Function.Name)
		}
	}
}

// TestExecuteUnknownTool 验证执行未注册工具时报"未知工具"。
func TestExecuteUnknownTool(t *testing.T) {
	_, err := Execute(context.Background(), "不存在的工具", "{}")
	if err == nil {
		t.Fatal("期望出错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "未知工具") {
		t.Errorf("错误信息应含\"未知工具\": %v", err)
	}
}

// TestExecutorValidation 验证各执行器在触库前的参数校验。
// 注意：这些用例全部在访问 database.DB / services 之前返回错误，因此 DB 为 nil 也安全；
// 刻意不写任何会走到数据库的成功路径用例（校验顺序以源码实际为准）。
func TestExecutorValidation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string // 用例名
		tool    string // 工具名
		args    string // 入参 JSON
		wantErr string // 错误信息应包含的子串
	}{
		// search_notes：JSON 解析失败即返回，不进检索逻辑
		{"search_notes 坏 JSON", "search_notes", "not-json", "参数解析失败"},

		// get_note：先解析 JSON，再校验 note_id 数字格式
		{"get_note 坏 JSON", "get_note", "not-json", "参数解析失败"},
		{"get_note 非法 note_id", "get_note", `{"note_id":"abc"}`, "note_id 格式错误"},

		// run_subagent：JSON → task 非空（均在读取 AI 配置 / 触库之前）
		{"run_subagent 坏 JSON", "run_subagent", "not-json", "参数解析失败"},
		{"run_subagent 空 task", "run_subagent", `{"task":""}`, "task 不能为空"},

		// replace_note_section：JSON → note_id → old_text 非空（校验顺序见源码）
		{"replace_note_section 坏 JSON", "replace_note_section", "not-json", "参数解析失败"},
		{"replace_note_section 非法 note_id", "replace_note_section", `{"note_id":"abc","old_text":"a","new_text":"b"}`, "note_id 格式错误"},
		{"replace_note_section 空 old_text", "replace_note_section", `{"note_id":"123","old_text":"","new_text":"b"}`, "old_text 不能为空"},

		// append_note_content：JSON → note_id → content 非空
		{"append_note_content 坏 JSON", "append_note_content", "not-json", "参数解析失败"},
		{"append_note_content 空 content", "append_note_content", `{"note_id":"123","content":""}`, "content 不能为空"},

		// update_note_title：title 空白（含空格）被拒绝
		{"update_note_title 空白 title", "update_note_title", `{"note_id":"123","title":"   "}`, "title 不能为空"},

		// create_note：title / content 任一为空即拒绝
		{"create_note 缺 title", "create_note", `{"title":"","content":"正文"}`, "title 和 content 均不能为空"},
		{"create_note 缺 content", "create_note", `{"title":"标题","content":""}`, "title 和 content 均不能为空"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Execute(ctx, tc.tool, tc.args)
			if err == nil {
				t.Fatalf("期望出错（含 %q），实际为 nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("错误信息应含 %q, got: %v", tc.wantErr, err)
			}
		})
	}
}
