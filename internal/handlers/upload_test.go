package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/great-magician-01/agent_note/internal/config"
)

// 构造 multipart 上传请求体，返回 body 与 Content-Type
func buildMultipart(t *testing.T, field, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("创建 multipart 文件字段失败: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("写入 multipart 内容失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 multipart writer 失败: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// 向 UploadFile 发起 POST 请求并返回响应记录器
func postUpload(t *testing.T, body *bytes.Buffer, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.POST("/api/uploads", UploadFile)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/uploads", body)
	req.Header.Set("Content-Type", contentType)
	r.ServeHTTP(w, req)
	return w
}

func uploadError(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
	}
	return resp["error"]
}

// 只覆盖校验失败路径；成功路径会触 database.DB（未初始化），不在单测范围内
func TestUploadFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.C = &config.Config{UploadDir: t.TempDir()}

	t.Run("请求无 file 字段返回 400 缺少文件", func(t *testing.T) {
		// multipart 表单里只有别的字段，没有 file
		body, ct := buildMultipart(t, "other", "x.png", []byte("data"))
		w := postUpload(t, body, ct)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望状态码 400，实际 %d, body=%s", w.Code, w.Body.String())
		}
		if got := uploadError(t, w); got != "缺少文件" {
			t.Errorf("期望 error=缺少文件，实际 %q", got)
		}
	})

	t.Run("非图片文件返回 400 仅支持图片文件", func(t *testing.T) {
		body, ct := buildMultipart(t, "file", "name.txt", []byte("hello"))
		w := postUpload(t, body, ct)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望状态码 400，实际 %d, body=%s", w.Code, w.Body.String())
		}
		if got := uploadError(t, w); !strings.Contains(got, "仅支持图片文件") {
			t.Errorf("期望 error 包含 仅支持图片文件，实际 %q", got)
		}
	})

	t.Run("超过 10MB 的文件返回 400 文件超过 10MB 限制", func(t *testing.T) {
		// 10<<20 + 1 字节，刚好超出上限
		big := make([]byte, 10<<20+1)
		body, ct := buildMultipart(t, "file", "x.png", big)
		w := postUpload(t, body, ct)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望状态码 400，实际 %d, body=%s", w.Code, w.Body.String())
		}
		if got := uploadError(t, w); got != "文件超过 10MB 限制" {
			t.Errorf("期望 error=文件超过 10MB 限制，实际 %q", got)
		}
	})
}
