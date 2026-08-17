package database

import (
	"fmt"
	"log"

	"github.com/great-magician-01/agent_note/internal/config"
	"github.com/great-magician-01/agent_note/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init() {
	c := config.C
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s TimeZone=Asia/Shanghai",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
	)

	logLevel := logger.Warn
	if c.Debug {
		logLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		// 默认复数表名（categories/notes/note_tags...），与设计文档一致
		// 关键：禁用外键约束自动创建
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		log.Fatalf("[database] connect failed: %v", err)
	}

	DB = db

	if err := AutoMigrate(); err != nil {
		log.Fatalf("[database] migrate failed: %v", err)
	}
	ensureSearchIndexes()
	log.Println("[database] connected & migrated")
}

// ensureSearchIndexes 容错安装 pg_trgm 扩展并为 notes.title / content_md 建 GIN 索引
// （加速 ILIKE 关键词检索）。任何一步失败仅警告，不中断启动。
func ensureSearchIndexes() {
	schema := config.C.DBSchema
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_notes_title_trgm ON "%s".notes USING gin (title gin_trgm_ops)`, schema),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_notes_content_trgm ON "%s".notes USING gin (content_md gin_trgm_ops)`, schema),
	}
	for _, stmt := range stmts {
		if err := DB.Exec(stmt).Error; err != nil {
			log.Printf("[database] 检索索引初始化跳过（不影响启动）: %v; sql=%s", err, stmt)
			return
		}
	}
}

func AutoMigrate() error {
	return DB.AutoMigrate(
		&models.Category{},
		&models.Note{},
		&models.Tag{},
		&models.NoteTag{},
		&models.Entity{},
		&models.NoteEntity{},
		&models.AIConfig{},
		&models.Conversation{},
		&models.Message{},
		&models.AICallLog{},
		&models.Upload{},
	)
}
