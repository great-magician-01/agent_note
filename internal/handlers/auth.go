package handlers

import (
	"crypto/subtle"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/great-magician-01/agent_note/internal/config"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// 登录防爆破：内存记录连续失败次数，按次数递增响应延迟（500ms/次，上限 5s）
var (
	loginFailMu    sync.Mutex
	loginFailCount = map[string]int{}
)

func recordLoginFail(key string) time.Duration {
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	loginFailCount[key]++
	d := time.Duration(loginFailCount[key]) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

func resetLoginFail(key string) {
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	delete(loginFailCount, key)
}

// Login POST /api/auth/login
// 校验成功签发 JWT（不含 exp，永不过期）
func Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	uok := subtle.ConstantTimeCompare([]byte(req.Username), []byte(config.C.AdminUsername)) == 1
	pok := subtle.ConstantTimeCompare([]byte(req.Password), []byte(config.C.AdminPassword)) == 1
	if !uok || !pok {
		delay := recordLoginFail(c.ClientIP())
		log.Printf("[auth] 登录失败：username=%q ip=%s，延迟 %s 响应", req.Username, c.ClientIP(), delay)
		time.Sleep(delay)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}
	resetLoginFail(c.ClientIP())

	claims := jwt.MapClaims{
		"sub": req.Username,
		"iat": time.Now().Unix(),
		// 不设 exp —— 永不过期
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(config.C.JWTSecret))
	if err != nil {
		log.Printf("[auth] 签发 token 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "登录失败，请稍后重试"})
		return
	}

	log.Printf("[auth] 登录成功：username=%q ip=%s", req.Username, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"token": signed, "username": req.Username})
}

// Me GET /api/auth/me
func Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"username": config.C.AdminUsername})
}
