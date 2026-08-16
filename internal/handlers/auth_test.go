package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/great-magician-01/agent_note/internal/config"
)

// 向 Login 发起 POST 请求并返回响应记录器
func postLogin(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.POST("/api/auth/login", Login)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.C = &config.Config{
		JWTSecret:     "test-secret",
		AdminUsername: "admin",
		AdminPassword: "pass123",
	}

	t.Run("缺少参数返回 400 参数错误", func(t *testing.T) {
		w := postLogin(t, `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望状态码 400，实际 %d, body=%s", w.Code, w.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if resp["error"] != "参数错误" {
			t.Errorf("期望 error=参数错误，实际 %q", resp["error"])
		}
	})

	t.Run("密码错误返回 401 用户名或密码错误", func(t *testing.T) {
		w := postLogin(t, `{"username":"admin","password":"wrong"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望状态码 401，实际 %d, body=%s", w.Code, w.Body.String())
		}
		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if resp["error"] != "用户名或密码错误" {
			t.Errorf("期望 error=用户名或密码错误，实际 %q", resp["error"])
		}
	})

	t.Run("用户名密码正确返回 200 及 token", func(t *testing.T) {
		w := postLogin(t, `{"username":"admin","password":"pass123"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d, body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Token    string `json:"token"`
			Username string `json:"username"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if resp.Token == "" {
			t.Fatal("响应缺少 token 字段")
		}
		if resp.Username != "admin" {
			t.Errorf("期望 username=admin，实际 %q", resp.Username)
		}

		// 用同一 secret 解析 token，校验 claims
		token, err := jwt.Parse(resp.Token, func(tk *jwt.Token) (interface{}, error) {
			return []byte(config.C.JWTSecret), nil
		})
		if err != nil || !token.Valid {
			t.Fatalf("返回的 token 无法通过校验: %v", err)
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			t.Fatal("claims 类型不是 MapClaims")
		}
		if claims["sub"] != "admin" {
			t.Errorf("期望 sub=admin，实际 %v", claims["sub"])
		}
		if _, ok := claims["iat"]; !ok {
			t.Error("claims 应包含 iat（签发时间）")
		}
		if _, ok := claims["exp"]; ok {
			t.Error("claims 不应包含 exp（JWT 永不过期是设计决策）")
		}
	})
}

func TestMe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.C = &config.Config{AdminUsername: "admin"}

	r := gin.New()
	r.GET("/api/auth/me", Me)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if resp["username"] != config.C.AdminUsername {
		t.Errorf("期望 username=%s，实际 %q", config.C.AdminUsername, resp["username"])
	}
}
