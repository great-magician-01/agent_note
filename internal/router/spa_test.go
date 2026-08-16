package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const (
	testIndexHTML = "<html>index</html>"
	testAppJS     = "console.log(1)"
)

// 在临时目录搭建最小 dist：index.html + assets/app.js，返回 dist 路径
func setupDist(t *testing.T) string {
	t.Helper()
	dist := t.TempDir()
	writeFile(t, filepath.Join(dist, "index.html"), testIndexHTML)
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatalf("创建 assets 目录失败: %v", err)
	}
	writeFile(t, filepath.Join(dist, "assets", "app.js"), testAppJS)
	return dist
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件 %s 失败: %v", path, err)
	}
}

func doRequest(r *gin.Engine, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestRegisterSPA(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dist := setupDist(t)

	newEngine := func() *gin.Engine {
		r := gin.New()
		registerSPA(r, dist)
		return r
	}

	t.Run("命中 assets 静态文件返回 200 且长缓存", func(t *testing.T) {
		w := doRequest(newEngine(), http.MethodGet, "/assets/app.js")
		if w.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d", w.Code)
		}
		if got := w.Body.String(); got != testAppJS {
			t.Errorf("期望 body=%q，实际 %q", testAppJS, got)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("assets 文件 Cache-Control 应含 immutable，实际 %q", cc)
		}
	})

	t.Run("不存在的 GET 路径回退 index.html 且 no-cache", func(t *testing.T) {
		w := doRequest(newEngine(), http.MethodGet, "/some/page")
		if w.Code != http.StatusOK {
			t.Fatalf("期望状态码 200，实际 %d", w.Code)
		}
		if got := w.Body.String(); got != testIndexHTML {
			t.Errorf("期望回退到 index.html（%q），实际 %q", testIndexHTML, got)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("index.html 回退的 Cache-Control 应为 no-cache，实际 %q", cc)
		}
	})

	t.Run("/api 未匹配路径返回 404 JSON 而不回退 index.html", func(t *testing.T) {
		w := doRequest(newEngine(), http.MethodGet, "/api/unknown")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望状态码 404，实际 %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "接口不存在") {
			t.Errorf("期望错误消息 接口不存在，实际 %q", w.Body.String())
		}
		if strings.Contains(w.Body.String(), testIndexHTML) {
			t.Error("/api 路径不应回退到 index.html")
		}
	})

	t.Run("POST 未匹配路径返回 404", func(t *testing.T) {
		w := doRequest(newEngine(), http.MethodPost, "/anything")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望状态码 404，实际 %d", w.Code)
		}
	})

	t.Run("路径穿越不能读到 dist 外的文件", func(t *testing.T) {
		// dist 平级放一个 secret.txt，确认 %2f 解码出的 ../ 无法逃逸
		base := t.TempDir()
		distDir := filepath.Join(base, "dist")
		if err := os.MkdirAll(filepath.Join(distDir, "assets"), 0o755); err != nil {
			t.Fatalf("创建 dist 失败: %v", err)
		}
		writeFile(t, filepath.Join(distDir, "index.html"), testIndexHTML)
		writeFile(t, filepath.Join(base, "secret.txt"), "SECRET-CONTENT")

		r := gin.New()
		registerSPA(r, distDir)

		// %2f 会被 URL 解码，URL.Path 变为 /../secret.txt
		w := doRequest(r, http.MethodGet, "/..%2fsecret.txt")
		if strings.Contains(w.Body.String(), "SECRET-CONTENT") {
			t.Fatalf("路径穿越读到了 dist 外的文件: %q", w.Body.String())
		}
		// 实际行为（以现有实现为准）：Clean("/"+p) 去掉 .. 后落在 dist/secret.txt（不存在），
		// 走到 c.File(index) 回退；但 c.File 底层 http.ServeFile 发现 r.URL.Path 含 ".."
		// 会直接 400，这是标准库对 ServeFile 路径穿越的内置防护
		if w.Code != http.StatusBadRequest {
			t.Errorf("期望 400（http.ServeFile 拒绝含 .. 的路径），实际 %d, body=%s", w.Code, w.Body.String())
		}
	})
}

// dist 不存在时 registerSPA 直接返回、不注册 NoRoute，任意路径都是 gin 默认 404
func TestRegisterSPADistNotExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerSPA(r, filepath.Join(t.TempDir(), "not-exist"))

	w := doRequest(r, http.MethodGet, "/anything")
	if w.Code != http.StatusNotFound {
		t.Errorf("dist 不存在时期望 gin 默认 404，实际 %d, body=%s", w.Code, w.Body.String())
	}
}
