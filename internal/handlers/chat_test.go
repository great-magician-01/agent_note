package handlers

import (
	"testing"

	"github.com/great-magician-01/agent_note/internal/ai"
)

// toolSummary 是纯函数，表驱动覆盖全部已知工具名与未知工具名；
// chat.go 其余部分触库 / 起 SSE，不在单测范围内
func TestToolSummary(t *testing.T) {
	cases := []struct {
		name    string // 用例名
		tool    string // 工具名
		content string // ToolResult.Content
		want    string // 期望摘要
	}{
		{"search_notes 正常 JSON", "search_notes", `{"total":3}`, "找到 3 条笔记"},
		{"search_notes 坏 JSON", "search_notes", "这不是 JSON", "搜索完成"},
		{"get_note", "get_note", "{}", "已读取笔记全文"},
		{"list_categories", "list_categories", "{}", "已获取分类列表"},
		{"replace_note_section", "replace_note_section", "{}", "已替换笔记内容"},
		{"append_note_content", "append_note_content", "{}", "已追加内容到笔记"},
		{"update_note_title", "update_note_title", "{}", "已修改笔记标题"},
		{"create_note", "create_note", "{}", "已创建新笔记"},
		{"未知工具", "some_unknown_tool", "{}", "工具执行完成"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolSummary(tc.tool, &ai.ToolResult{Content: tc.content})
			if got != tc.want {
				t.Errorf("toolSummary(%q, %q) = %q，期望 %q", tc.tool, tc.content, got, tc.want)
			}
		})
	}
}
