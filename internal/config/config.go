package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DBHost        string
	DBPort        string
	DBUser        string
	DBPassword    string
	DBName        string
	DBSchema      string
	JWTSecret     string
	AdminUsername string
	AdminPassword string
	ServerPort    string
	LogDir        string
	UploadDir     string
	WebDistDir    string
	SnowflakeNode int64
	Debug         bool
}

var C *Config

func Load() {
	_ = godotenv.Load()

	node, err := strconv.ParseInt(getEnv("SNOWFLAKE_NODE", "1"), 10, 64)
	if err != nil || node < 0 || node > 1023 {
		node = 1
	}

	debug, _ := strconv.ParseBool(getEnv("DEBUG", "false"))

	C = &Config{
		DBHost:        getEnv("DB_HOST", "localhost"),
		DBPort:        getEnv("DB_PORT", "5432"),
		DBUser:        getEnv("DB_USER", "postgres"),
		DBPassword:    getEnv("DB_PASSWORD", ""),
		DBName:        getEnv("DB_NAME", "db"),
		DBSchema:      getEnv("DB_SCHEMA", "public"),
		JWTSecret:     getEnv("JWT_SECRET", "dev-secret-please-change"),
		AdminUsername: getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin"),
		ServerPort:    getEnv("SERVER_PORT", "7562"),
		LogDir:        getEnv("LOG_DIR", "logs"),
		UploadDir:     getEnv("UPLOAD_DIR", "uploads"),
		WebDistDir:    getEnv("WEB_DIST_DIR", "web/dist"),
		SnowflakeNode: node,
		Debug:         debug,
	}

	if C.JWTSecret == "dev-secret-please-change" {
		log.Println("[config] WARNING: JWT_SECRET 仍为默认值，生产环境请修改")
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
