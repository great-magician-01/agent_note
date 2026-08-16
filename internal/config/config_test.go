package config

import "testing"

// loadEnvKeys 是 Load 读取的全部环境变量，测试中统一显式置空以隔离宿主机器环境
// （getEnv 视空串为未设置，走 fallback；注意 go test 工作目录是包目录，internal/config 下无 .env 干扰）
var loadEnvKeys = []string{
	"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SCHEMA",
	"JWT_SECRET", "ADMIN_USERNAME", "ADMIN_PASSWORD",
	"SERVER_PORT", "LOG_DIR", "UPLOAD_DIR", "WEB_DIST_DIR",
	"SNOWFLAKE_NODE", "DEBUG",
}

// setAllEnvEmpty 把 Load 依赖的环境变量全部置空
func setAllEnvEmpty(t *testing.T) {
	t.Helper()
	for _, k := range loadEnvKeys {
		t.Setenv(k, "")
	}
}

// TestGetEnv 环境变量存在时取值，不存在或为空串时取 fallback
func TestGetEnv(t *testing.T) {
	const key = "AGENT_NOTE_TEST_GETENV"

	t.Setenv(key, "自定义值")
	if got := getEnv(key, "fallback"); got != "自定义值" {
		t.Fatalf("getEnv(%q) = %q，期望 %q", key, got, "自定义值")
	}

	// 显式置空同样走 fallback
	t.Setenv(key, "")
	if got := getEnv(key, "fallback"); got != "fallback" {
		t.Fatalf("getEnv(%q 空串) = %q，期望 fallback", key, got)
	}

	// 不存在的变量取 fallback（使用不可能存在的长变量名）
	if got := getEnv("AGENT_NOTE_TEST_NOT_EXISTS_8f3a2c", "fallback"); got != "fallback" {
		t.Fatalf("getEnv(不存在) = %q，期望 fallback", got)
	}
}

// TestLoadDefaults 全部环境变量为空时，各项默认值生效
// （JWT_SECRET 为默认值时会打 WARNING 日志，属正常）
func TestLoadDefaults(t *testing.T) {
	setAllEnvEmpty(t)
	Load()
	if C == nil {
		t.Fatal("Load() 后 C 仍为 nil")
	}
	checks := []struct {
		name, got, want string
	}{
		{"DBHost", C.DBHost, "localhost"},
		{"DBPort", C.DBPort, "5432"},
		{"DBUser", C.DBUser, "postgres"},
		{"DBPassword", C.DBPassword, ""},
		{"DBName", C.DBName, "db"},
		{"DBSchema", C.DBSchema, "public"},
		{"JWTSecret", C.JWTSecret, "dev-secret-please-change"},
		{"AdminUsername", C.AdminUsername, "admin"},
		{"AdminPassword", C.AdminPassword, "admin"},
		{"ServerPort", C.ServerPort, "7562"},
		{"LogDir", C.LogDir, "logs"},
		{"UploadDir", C.UploadDir, "uploads"},
		{"WebDistDir", C.WebDistDir, "web/dist"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q，期望 %q", c.name, c.got, c.want)
		}
	}
	if C.SnowflakeNode != 1 {
		t.Errorf("SnowflakeNode = %d，期望默认 1", C.SnowflakeNode)
	}
	if C.Debug {
		t.Error("Debug 期望默认 false")
	}
}

// TestLoadSnowflakeNode SNOWFLAKE_NODE 非法（非数字 / 超出 0~1023 / 负数）时回退为 1，合法时取该值
func TestLoadSnowflakeNode(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  int64
	}{
		{"非数字", "abc", 1},
		{"超出上限", "2000", 1},
		{"负数", "-5", 1},
		{"合法值", "7", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setAllEnvEmpty(t)
			t.Setenv("SNOWFLAKE_NODE", tc.value)
			Load()
			if C.SnowflakeNode != tc.want {
				t.Fatalf("SNOWFLAKE_NODE=%q 时 SnowflakeNode = %d，期望 %d", tc.value, C.SnowflakeNode, tc.want)
			}
		})
	}
}

// TestLoadDebug DEBUG=true 时 Debug 为 true，否则为 false
func TestLoadDebug(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"true", "true", true},
		{"false", "false", false},
		{"空串走默认", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setAllEnvEmpty(t)
			t.Setenv("DEBUG", tc.value)
			Load()
			if C.Debug != tc.want {
				t.Fatalf("DEBUG=%q 时 Debug = %v，期望 %v", tc.value, C.Debug, tc.want)
			}
		})
	}
}
