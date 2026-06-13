// Package middleware 提供 gin 中间件。
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// JWTAuth 校验请求头中的 JWT。
func JWTAuth(s *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.AbortWithStatusJSON(401, gin.H{"error": "missing bearer token"})
			return
		}
		tk := strings.TrimPrefix(auth, "Bearer ")
		claims, err := s.ParseToken(tk)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
			return
		}
		c.Set("claims", claims)
		c.Set("userId", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// ZapLogger 简易访问日志。
func ZapLogger(l *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := zap.L()
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/healthz") {
			c.Next()
			return
		}
		c.Next()
		start.Info("http",
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.String("client", c.ClientIP()),
		)
	}
}
