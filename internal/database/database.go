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
	log.Println("[database] connected & migrated")
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
		&models.Upload{},
	)
}
