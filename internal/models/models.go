package models

import "time"

// Category 分类（单层）
type Category struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Sort      int       `gorm:"not null;default:0" json:"sort"`
	IsActive  int       `gorm:"not null;default:1;index" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Note 笔记
type Note struct {
	ID              int64     `gorm:"primaryKey" json:"id,string"`
	Title           string    `gorm:"type:text;not null;default:''" json:"title"`
	ContentMD       string    `gorm:"type:text;not null;default:''" json:"content_md"`
	CategoryID      *int64    `json:"category_id,string"`
	Summary         string    `gorm:"type:text;not null;default:''" json:"summary"`
	MetaStatus      string    `gorm:"size:16;not null;default:'none'" json:"meta_status"` // none|pending|processing|done|failed
	MetaError       string    `gorm:"type:text;not null;default:''" json:"meta_error"`
	MetaContentHash string    `gorm:"size:64;not null;default:''" json:"-"`
	IsActive        int       `gorm:"not null;default:1;index" json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Tag 标签（AI 生成）
type Tag struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	Name      string    `gorm:"size:64;not null;index" json:"name"`
	IsActive  int       `gorm:"not null;default:1;index" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// NoteTag 笔记-标签关联
type NoteTag struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	NoteID    int64     `gorm:"not null;index" json:"note_id,string"`
	TagID     int64     `gorm:"not null;index" json:"tag_id,string"`
	IsActive  int       `gorm:"not null;default:1" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// Entity 实体（AI 生成）
type Entity struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	Name      string    `gorm:"size:128;not null;index" json:"name"`
	Type      string    `gorm:"size:32;not null;default:'other'" json:"type"`
	IsActive  int       `gorm:"not null;default:1;index" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// NoteEntity 笔记-实体关联
type NoteEntity struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	NoteID    int64     `gorm:"not null;index" json:"note_id,string"`
	EntityID  int64     `gorm:"not null;index" json:"entity_id,string"`
	IsActive  int       `gorm:"not null;default:1" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// AIConfig AI 配置（多配置，仅一个激活）
// is_active = 软删除标记；active = 激活标记（同一时间仅一个 active=1，应用层保证）
type AIConfig struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	BaseURL   string    `gorm:"size:256;not null" json:"base_url"`
	APIKey    string    `gorm:"type:text;not null" json:"api_key,omitempty"`
	Model     string    `gorm:"size:128;not null" json:"model"`
	Active    int       `gorm:"not null;default:0;index" json:"active"`
	IsActive  int       `gorm:"not null;default:1;index" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MaskedKey 脱敏后的 api_key（前3后4）
func (c *AIConfig) MaskedKey() string {
	k := c.APIKey
	if len(k) <= 7 {
		return "***"
	}
	return k[:3] + "***" + k[len(k)-4:]
}

// Conversation AI 会话（note_id 非空=绑定笔记，NULL=首页全局）
type Conversation struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	NoteID    *int64    `json:"note_id,string"`
	Title     string    `gorm:"size:128;not null;default:'新对话'" json:"title"`
	IsActive  int       `gorm:"not null;default:1;index" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message 消息（含工具消息）
type Message struct {
	ID             int64     `gorm:"primaryKey" json:"id,string"`
	ConversationID int64     `gorm:"not null;index" json:"conversation_id,string"`
	Role           string    `gorm:"size:16;not null" json:"role"` // user|assistant|tool
	Content        string    `gorm:"type:text;not null;default:''" json:"content"`
	ToolCalls      *string   `gorm:"type:jsonb" json:"tool_calls,omitempty"` // assistant 工具调用（原样回放）
	ToolCallID     string    `gorm:"size:64" json:"tool_call_id,omitempty"`  // tool 消息归属
	Name           string    `gorm:"size:64" json:"name,omitempty"`          // tool 消息工具名
	IsActive       int       `gorm:"not null;default:1" json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

// Upload 上传文件
type Upload struct {
	ID        int64     `gorm:"primaryKey" json:"id,string"`
	Filename  string    `gorm:"size:256;not null" json:"filename"`
	Path      string    `gorm:"size:512;not null" json:"path"`
	Size      int64     `gorm:"not null;default:0" json:"size"`
	Mime      string    `gorm:"size:64;not null;default:''" json:"mime"`
	IsActive  int       `gorm:"not null;default:1" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
