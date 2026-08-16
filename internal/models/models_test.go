package models

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestMaskedKey api_key 脱敏：长度 ≤7 整体打码，否则前 3 后 4
func TestMaskedKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"空字符串", "", "***"},
		{"长度7", "1234567", "***"},
		{"长度8", "12345678", "123***5678"},
		{"典型 sk 密钥", "sk-abcdefghijklmnop", "sk-***mnop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &AIConfig{APIKey: tc.key}
			if got := c.MaskedKey(); got != tc.want {
				t.Fatalf("MaskedKey(%q) = %q，期望 %q", tc.key, got, tc.want)
			}
		})
	}
}

// TestNoteIDJSONString Note 的 id,string 标签：超过 2^53 的雪花 ID 序列化为字符串，往返无损
func TestNoteIDJSONString(t *testing.T) {
	// 2^53 + 1，超出 JS Number.MAX_SAFE_INTEGER，浮点表示不下
	const id = int64(9007199254740993)

	n := Note{ID: id, Title: "测试"}
	data, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	if !strings.Contains(string(data), `"id":"9007199254740993"`) {
		t.Fatalf("序列化结果 %s 中 id 不是字符串形式", data)
	}

	var back Note
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if back.ID != id {
		t.Fatalf("往返后 ID = %d，期望 %d（精度丢失）", back.ID, id)
	}
}
