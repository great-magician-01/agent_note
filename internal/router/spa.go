package router

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// registerSPA 当 WEB_DIST_DIR 存在时，由后端托管前端构建产物；
// 未命中静态文件的 GET 路径回退到 index.html（前端路由 history 模式）。
func registerSPA(r *gin.Engine, dist string) {
	index := filepath.Join(dist, "index.html")
	info, err := os.Stat(index)
	if err != nil || info.IsDir() {
		log.Printf("[router] 前端目录 %s 不存在，跳过 SPA 托管（开发模式由 vite 提供页面）", dist)
		return
	}
	log.Printf("[router] 托管前端静态目录：%s", dist)

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path

		// API / 上传文件未匹配 → 404，不回退到前端页面
		if p == "/api" || strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/uploads/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}

		// 命中真实静态文件（js/css/favicon 等）则直接返回；
		// 前导 "/" 保证 Clean 后的路径不会逃逸出 dist
		fp := filepath.Join(dist, filepath.Clean("/"+p))
		if st, err := os.Stat(fp); err == nil && !st.IsDir() {
			// vite 产物带内容哈希，可长缓存
			if strings.HasPrefix(p, "/assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			c.File(fp)
			return
		}

		// 其余 GET 路径交给前端路由；不缓存，保证发版即生效
		c.Header("Cache-Control", "no-cache")
		c.File(index)
	})
}
