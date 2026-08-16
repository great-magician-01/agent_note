package services

import (
	"strings"
	"testing"
	"time"

	"github.com/great-magician-01/agent_note/internal/models"
)

// TestContentHash 返回 64 位小写 hex；同输入同输出，不同输入不同输出
func TestContentHash(t *testing.T) {
	h1 := ContentHash("你好，世界")
	if len(h1) != 64 {
		t.Fatalf("ContentHash 长度 = %d，期望 64", len(h1))
	}
	for _, c := range h1 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("ContentHash 含非小写 hex 字符 %q: %s", c, h1)
		}
	}
	if got := ContentHash("你好，世界"); got != h1 {
		t.Fatalf("同输入结果不同: %s != %s", got, h1)
	}
	if got := ContentHash("别的内容"); got == h1 {
		t.Fatal("不同输入得到相同 hash")
	}
}

// TestDeriveTitle 空内容回退「无标题」；剥掉 markdown 前导符号（TrimLeft 字符集 "#>*-` "）；跳过空行取首个非空行
func TestDeriveTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空字符串", "", "无标题"},
		{"全空行", "\n\n   \n\t\n", "无标题"},
		{"一级标题", "# 标题", "标题"},
		{"多级标题", "### 三级标题", "三级标题"},
		{"引用", "> 引用内容", "引用内容"},
		{"无序列表", "- 列表项", "列表项"},
		{"代码块围栏行被跳过", "```\n代码行\n```", "代码行"},
		{"跳过前导空行", "\n\n  \n# 真正标题", "真正标题"},
		// TrimLeft 字符集含 '*'，仅剥左侧
		{"星号加粗", "**加粗**内容", "加粗**内容"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveTitle(tc.in); got != tc.want {
				t.Fatalf("DeriveTitle(%q) = %q，期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDeriveTitleRuneTruncate 超过 50 rune 截断为前 50 rune（按 rune 而非字节，中文不断码）
func TestDeriveTitleRuneTruncate(t *testing.T) {
	s := strings.Repeat("汉", 60) // 60 个汉字 = 180 字节
	got := DeriveTitle(s)
	want := strings.Repeat("汉", 50)
	if got != want {
		t.Fatalf("截断结果长度 = %d rune，期望 50 rune 且内容一致", len([]rune(got)))
	}
	if len([]rune(got)) != 50 {
		t.Fatalf("截断结果 = %d rune，期望 50", len([]rune(got)))
	}
}

// TestToVO nil 的 tagMap/entityMap 输出空切片而非 nil；withContent 控制是否带正文；时间格式化
func TestToVO(t *testing.T) {
	created := time.Date(2024, 3, 5, 12, 30, 45, 0, time.UTC)
	updated := time.Date(2024, 3, 6, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
	n := &models.Note{
		ID:         123,
		Title:      "标题",
		ContentMD:  "正文内容",
		Summary:    "摘要",
		MetaStatus: "done",
		CreatedAt:  created,
		UpdatedAt:  updated,
	}

	// nil map + 不带正文
	vo := ToVO(n, nil, nil, false)
	if vo.Tags == nil || len(vo.Tags) != 0 {
		t.Errorf("Tags = %#v，期望非 nil 空切片", vo.Tags)
	}
	if vo.Entities == nil || len(vo.Entities) != 0 {
		t.Errorf("Entities = %#v，期望非 nil 空切片", vo.Entities)
	}
	if vo.ContentMD != "" {
		t.Errorf("withContent=false 时 ContentMD = %q，期望空", vo.ContentMD)
	}
	if vo.CreatedAt != "2024-03-05T12:30:45Z" {
		t.Errorf("CreatedAt = %q，期望 2024-03-05T12:30:45Z", vo.CreatedAt)
	}
	if vo.UpdatedAt != "2024-03-06T08:00:00+08:00" {
		t.Errorf("UpdatedAt = %q，期望 2024-03-06T08:00:00+08:00", vo.UpdatedAt)
	}
	if vo.ID != 123 || vo.Title != "标题" || vo.Summary != "摘要" || vo.MetaStatus != "done" {
		t.Errorf("基础字段透传有误: %+v", vo)
	}

	// 带正文 + 命中元数据 map
	tagMap := map[int64][]string{123: {"go", "笔记"}}
	entityMap := map[int64][]EntityVO{123: {{Name: "Golang", Type: "technology"}}}
	vo2 := ToVO(n, tagMap, entityMap, true)
	if vo2.ContentMD != "正文内容" {
		t.Errorf("withContent=true 时 ContentMD = %q，期望正文", vo2.ContentMD)
	}
	if len(vo2.Tags) != 2 || vo2.Tags[0] != "go" || vo2.Tags[1] != "笔记" {
		t.Errorf("Tags = %#v，期望 [go 笔记]", vo2.Tags)
	}
	if len(vo2.Entities) != 1 || vo2.Entities[0].Name != "Golang" || vo2.Entities[0].Type != "technology" {
		t.Errorf("Entities = %#v，期望 [{Golang technology}]", vo2.Entities)
	}
}

// TestLoadNoteMetaEmpty 空切片直接早返回两个空 map 和 nil error，不触 database.DB
func TestLoadNoteMetaEmpty(t *testing.T) {
	for _, ids := range [][]int64{nil, {}} {
		tagMap, entityMap, err := LoadNoteMeta(ids)
		if err != nil {
			t.Fatalf("LoadNoteMeta(%v) err = %v，期望 nil", ids, err)
		}
		if tagMap == nil || len(tagMap) != 0 {
			t.Fatalf("tagMap = %#v，期望非 nil 空 map", tagMap)
		}
		if entityMap == nil || len(entityMap) != 0 {
			t.Fatalf("entityMap = %#v，期望非 nil 空 map", entityMap)
		}
	}
}
