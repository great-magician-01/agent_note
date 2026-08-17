package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/great-magician-01/agent_note/internal/config"
)

// 读取错误响应体中的 error 字段
func respError(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
	}
	return body.Error
}

// 用指定 secret 现场签发 HS256 token（与登录接口一致，不含 exp —— 永不过期是设计决策）
func signHS256(t *testing.T, secret string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "admin"})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("签发 HS256 token 失败: %v", err)
	}
	return signed
}

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("GET 请求带跨域响应头", func(t *testing.T) {
		r := gin.New()
		r.Use(CORS())
		r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d", w.Code)
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("Access-Control-Allow-Origin 期望 *，实际 %q", got)
		}
		methods := w.Header().Get("Access-Control-Allow-Methods")
		if !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") {
			t.Errorf("Access-Control-Allow-Methods 应包含 GET/POST，实际 %q", methods)
		}
	})

	t.Run("OPTIONS 预检返回 204 且不执行下游 handler", func(t *testing.T) {
		r := gin.New()
		r.Use(CORS())
		called := false
		r.OPTIONS("/ping", func(c *gin.Context) {
			called = true
			c.String(http.StatusOK, "pong")
		})

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("期望状态码 204，实际 %d", w.Code)
		}
		if called {
			t.Error("OPTIONS 预检请求不应执行下游 handler")
		}
	})
}

func TestJWTAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.C = &config.Config{JWTSecret: "test-secret"}

	// 构造挂了 JWTAuth 的最小路由，下游 handler 仅记录是否被执行
	setup := func() (*gin.Engine, *bool) {
		r := gin.New()
		r.Use(JWTAuth())
		called := new(bool)
		r.GET("/protected", func(c *gin.Context) {
			*called = true
			c.String(http.StatusOK, "ok")
		})
		return r, called
	}
	doGet := func(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("无 Authorization 头返回 401 未提供登录凭证", func(t *testing.T) {
		r, called := setup()
		w := doGet(r, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望状态码 401，实际 %d", w.Code)
		}
		if got := respError(t, w); got != "未提供登录凭证" {
			t.Errorf("期望 error=未提供登录凭证，实际 %q", got)
		}
		if *called {
			t.Error("鉴权失败不应执行下游 handler")
		}
	})

	t.Run("非 Bearer 前缀返回 401 未提供登录凭证", func(t *testing.T) {
		r, called := setup()
		w := doGet(r, "Token abc.def.ghi")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望状态码 401，实际 %d", w.Code)
		}
		if got := respError(t, w); got != "未提供登录凭证" {
			t.Errorf("期望 error=未提供登录凭证，实际 %q", got)
		}
		if *called {
			t.Error("鉴权失败不应执行下游 handler")
		}
	})

	t.Run("错误 secret 签发的 token 返回 401 登录凭证无效", func(t *testing.T) {
		r, called := setup()
		w := doGet(r, "Bearer "+signHS256(t, "wrong-secret"))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望状态码 401，实际 %d", w.Code)
		}
		if got := respError(t, w); got != "登录凭证无效" {
			t.Errorf("期望 error=登录凭证无效，实际 %q", got)
		}
		if *called {
			t.Error("鉴权失败不应执行下游 handler")
		}
	})

	t.Run("alg 为 none 的 token 返回 401", func(t *testing.T) {
		r, called := setup()
		token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"sub": "admin"})
		signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("构造 none token 失败: %v", err)
		}
		w := doGet(r, "Bearer "+signed)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望状态码 401，实际 %d", w.Code)
		}
		if got := respError(t, w); got != "登录凭证无效" {
			t.Errorf("期望 error=登录凭证无效，实际 %q", got)
		}
		if *called {
			t.Error("鉴权失败不应执行下游 handler")
		}
	})

	t.Run("非 HMAC 算法（RS256）签发的 token 返回 401", func(t *testing.T) {
		r, called := setup()
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成 RSA 密钥失败: %v", err)
		}
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"sub": "admin"})
		signed, err := token.SignedString(key)
		if err != nil {
			t.Fatalf("签发 RS256 token 失败: %v", err)
		}
		w := doGet(r, "Bearer "+signed)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("期望状态码 401，实际 %d", w.Code)
		}
		if got := respError(t, w); got != "登录凭证无效" {
			t.Errorf("期望 error=登录凭证无效，实际 %q", got)
		}
		if *called {
			t.Error("鉴权失败不应执行下游 handler")
		}
	})

	t.Run("合法 token 放行并执行下游 handler", func(t *testing.T) {
		r, called := setup()
		w := doGet(r, "Bearer "+signHS256(t, config.C.JWTSecret))
		if w.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d, body=%s", w.Code, w.Body.String())
		}
		if !*called {
			t.Error("合法 token 应放行并执行下游 handler")
		}
	})
}
