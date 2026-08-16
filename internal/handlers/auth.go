package handlers

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/great-magician-01/agent_note/internal/config"
)

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
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
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	claims := jwt.MapClaims{
		"sub": req.Username,
		"iat": time.Now().Unix(),
		// 不设 exp —— 永不过期
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(config.C.JWTSecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发 token 失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": signed, "username": req.Username})
}

// Me GET /api/auth/me
func Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"username": config.C.AdminUsername})
}
