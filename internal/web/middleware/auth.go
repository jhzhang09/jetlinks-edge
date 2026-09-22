// Package middleware 提供 gin 中间件。
package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

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

// RequireRole 限制只有指定角色可以访问管理操作。
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(c *gin.Context) {
		if _, ok := allowed[c.GetString("role")]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}

// LoginRateLimit 按客户端地址限制登录尝试，降低默认入口遭受暴力破解的风险。
func LoginRateLimit(maxAttempts int, window time.Duration) gin.HandlerFunc {
	type attempt struct {
		count int
		reset time.Time
	}
	var mu sync.Mutex
	attempts := make(map[string]attempt)
	return func(c *gin.Context) {
		now := time.Now()
		key := c.ClientIP()
		mu.Lock()
		if len(attempts) >= 4096 {
			for client, current := range attempts {
				if !now.Before(current.reset) {
					delete(attempts, client)
				}
			}
		}
		entry := attempts[key]
		if entry.reset.IsZero() || !now.Before(entry.reset) {
			entry = attempt{reset: now.Add(window)}
		}
		_, knownClient := attempts[key]
		blocked := entry.count >= maxAttempts || !knownClient && len(attempts) >= 4096
		mu.Unlock()
		if blocked {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
			return
		}
		c.Next()
		mu.Lock()
		defer mu.Unlock()
		if c.Writer.Status() == http.StatusUnauthorized {
			entry.count++
			attempts[key] = entry
		} else if c.Writer.Status() < http.StatusBadRequest {
			delete(attempts, key)
		}
	}
}

// SecurityHeaders 为管理界面和 API 设置同源安全策略。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
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
