package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/great-magician-01/agent_note/internal/database"
	"github.com/great-magician-01/agent_note/internal/models"
	"gorm.io/gorm"
)

// ContentHash 计算笔记内容 hash
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// DeriveTitle 标题为空时取正文首个非空行（去掉 markdown 符号），最长 50 字符
func DeriveTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#>*-` ")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 50 {
			return string(runes[:50])
		}
		return line
	}
	return "无标题"
}

// NoteVO 笔记列表/详情返回结构（id 以字符串序列化）
type NoteVO struct {
	ID         int64      `json:"id,string"`
	Title      string     `json:"title"`
	ContentMD  string     `json:"content_md,omitempty"`
	CategoryID *int64     `json:"category_id,string"`
	Summary    string     `json:"summary"`
	MetaStatus string     `json:"meta_status"`
	MetaError  string     `json:"meta_error,omitempty"`
	Tags       []string   `json:"tags"`
	Entities   []EntityVO `json:"entities"`
	CreatedAt  string     `json:"created_at"`
	UpdatedAt  string     `json:"updated_at"`
}

type EntityVO struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// LoadNoteMeta 加载笔记的 tags / entities（用于 VO 组装）
func LoadNoteMeta(noteIDs []int64) (map[int64][]string, map[int64][]EntityVO, error) {
	tagMap := map[int64][]string{}
	entityMap := map[int64][]EntityVO{}
	if len(noteIDs) == 0 {
		return tagMap, entityMap, nil
	}

	type tagRow struct {
		NoteID int64
		Name   string
	}
	var tagRows []tagRow
	if err := database.DB.Table("note_tags AS nt").
		Select("nt.note_id, t.name").
		Joins("JOIN tags t ON t.id = nt.tag_id AND t.is_active = 1").
		Where("nt.is_active = 1 AND nt.note_id IN ?", noteIDs).
		Scan(&tagRows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range tagRows {
		tagMap[r.NoteID] = append(tagMap[r.NoteID], r.Name)
	}

	type entityRow struct {
		NoteID int64
		Name   string
		Type   string
	}
	var entityRows []entityRow
	if err := database.DB.Table("note_entities AS ne").
		Select("ne.note_id, e.name, e.type").
		Joins("JOIN entities e ON e.id = ne.entity_id AND e.is_active = 1").
		Where("ne.is_active = 1 AND ne.note_id IN ?", noteIDs).
		Scan(&entityRows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range entityRows {
		entityMap[r.NoteID] = append(entityMap[r.NoteID], EntityVO{Name: r.Name, Type: r.Type})
	}
	return tagMap, entityMap, nil
}

// ToVO 组装单个笔记 VO
func ToVO(n *models.Note, tagMap map[int64][]string, entityMap map[int64][]EntityVO, withContent bool) NoteVO {
	vo := NoteVO{
		ID:         n.ID,
		Title:      n.Title,
		CategoryID: n.CategoryID,
		Summary:    n.Summary,
		MetaStatus: n.MetaStatus,
		MetaError:  n.MetaError,
		Tags:       tagMap[n.ID],
		Entities:   entityMap[n.ID],
		CreatedAt:  n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:  n.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if withContent {
		vo.ContentMD = n.ContentMD
	}
	if vo.Tags == nil {
		vo.Tags = []string{}
	}
	if vo.Entities == nil {
		vo.Entities = []EntityVO{}
	}
	return vo
}

// NoteListQuery 列表查询参数
type NoteListQuery struct {
	CategoryID *int64 // nil=全部；传 0 表示"未分类"
	Keyword    string // 标题/正文关键词
	Tag        string // 按标签名过滤
	Entity     string // 按实体名过滤
	Page       int
	PageSize   int
}

// ListNotes 笔记列表查询（关键词 ILIKE + tag/实体命中）
func ListNotes(q NoteListQuery) ([]models.Note, int64, error) {
	db := database.DB.Model(&models.Note{}).Where("is_active = 1")

	if q.CategoryID != nil {
		if *q.CategoryID == 0 {
			db = db.Where("category_id IS NULL")
		} else {
			db = db.Where("category_id = ?", *q.CategoryID)
		}
	}

	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		db = db.Where("title ILIKE ? OR content_md ILIKE ?", like, like)
	}
	if q.Tag != "" {
		db = db.Where(`EXISTS (
			SELECT 1 FROM note_tags nt JOIN tags t ON t.id = nt.tag_id AND t.is_active = 1
			WHERE nt.note_id = notes.id AND nt.is_active = 1 AND t.name = ?)`, q.Tag)
	}
	if q.Entity != "" {
		db = db.Where(`EXISTS (
			SELECT 1 FROM note_entities ne JOIN entities e ON e.id = ne.entity_id AND e.is_active = 1
			WHERE ne.note_id = notes.id AND ne.is_active = 1 AND e.name = ?)`, q.Entity)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.PageSize
	if size < 1 || size > 100 {
		size = 20
	}

	var notes []models.Note
	if err := db.Order("updated_at DESC").Offset((page - 1) * size).Limit(size).Find(&notes).Error; err != nil {
		return nil, 0, err
	}
	return notes, total, nil
}

// SearchNotesForAI 供 AI search_notes 工具使用：关键词/tags/实体 命中 + 权重排序
func SearchNotesForAI(keywords, tags, entities []string, limit int) ([]models.Note, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	var conds []string
	var condArgs []any
	var weightParts []string
	var weightArgs []any

	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		like := "%" + kw + "%"
		conds = append(conds, "(title ILIKE ? OR content_md ILIKE ?)")
		condArgs = append(condArgs, like, like)
		// 权重：标题 2 分，正文 1 分
		weightParts = append(weightParts,
			"(CASE WHEN title ILIKE ? THEN 2 ELSE 0 END + CASE WHEN content_md ILIKE ? THEN 1 ELSE 0 END)")
		weightArgs = append(weightArgs, like, like)
	}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		like := "%" + t + "%"
		// 权重：标签命中 3 分
		conds = append(conds, `EXISTS (
			SELECT 1 FROM note_tags nt JOIN tags tg ON tg.id = nt.tag_id AND tg.is_active = 1
			WHERE nt.note_id = notes.id AND nt.is_active = 1 AND tg.name ILIKE ?)`)
		condArgs = append(condArgs, like)
		weightParts = append(weightParts, `(CASE WHEN EXISTS (
			SELECT 1 FROM note_tags nt2 JOIN tags tg2 ON tg2.id = nt2.tag_id AND tg2.is_active = 1
			WHERE nt2.note_id = notes.id AND nt2.is_active = 1 AND tg2.name ILIKE ?) THEN 3 ELSE 0 END)`)
		weightArgs = append(weightArgs, like)
	}
	for _, e := range entities {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		like := "%" + e + "%"
		// 权重：实体命中 3 分
		conds = append(conds, `EXISTS (
			SELECT 1 FROM note_entities ne JOIN entities en ON en.id = ne.entity_id AND en.is_active = 1
			WHERE ne.note_id = notes.id AND ne.is_active = 1 AND en.name ILIKE ?)`)
		condArgs = append(condArgs, like)
		weightParts = append(weightParts, `(CASE WHEN EXISTS (
			SELECT 1 FROM note_entities ne2 JOIN entities en2 ON en2.id = ne2.entity_id AND en2.is_active = 1
			WHERE ne2.note_id = notes.id AND ne2.is_active = 1 AND en2.name ILIKE ?) THEN 3 ELSE 0 END)`)
		weightArgs = append(weightArgs, like)
	}

	if len(conds) == 0 {
		return []models.Note{}, nil
	}

	where := strings.Join(conds, " OR ")
	weightExpr := strings.Join(weightParts, " + ")

	// Select 的占位参数在前，Where 的占位参数在后
	args := append(append([]any{}, weightArgs...), condArgs...)

	var notes []models.Note
	err := database.DB.Model(&models.Note{}).
		Select("notes.*, ("+weightExpr+") AS relevance").
		Where("is_active = 1 AND ("+where+")", args...).
		Order("relevance DESC, updated_at DESC").
		Limit(limit).
		Find(&notes).Error
	if err != nil {
		return nil, fmt.Errorf("search notes: %w", err)
	}
	return notes, nil
}

// TouchNoteForMeta 内容变化后标记 pending（worker 入队在 worker 包完成，这里只改状态）
func TouchNoteForMeta(tx *gorm.DB, noteID int64, content string) error {
	return tx.Model(&models.Note{}).
		Where("id = ?", noteID).
		Updates(map[string]any{
			"meta_content_hash": ContentHash(content),
			"meta_status":       "pending",
			"meta_error":        "",
		}).Error
}
