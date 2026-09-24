package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRequireRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("role", c.GetHeader("X-Test-Role"))
	})
	engine.POST("/admin", RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := httptest.NewRequest(http.MethodPost, "/admin", nil)
	request.Header.Set("X-Test-Role", "user")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("user status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodPost, "/admin", nil)
	request.Header.Set("X-Test-Role", "admin")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("admin status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestLoginRateLimitCountsFailuresAndClearsOnSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/login", LoginRateLimit(2, time.Minute), func(c *gin.Context) {
		if c.GetHeader("X-Test-Success") == "true" {
			c.Status(http.StatusOK)
			return
		}
		c.Status(http.StatusUnauthorized)
	})

	call := func(success bool) int {
		request := httptest.NewRequest(http.MethodPost, "/login", nil)
		request.RemoteAddr = "192.0.2.1:1234"
		if success {
			request.Header.Set("X-Test-Success", "true")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response.Code
	}
	if call(false) != http.StatusUnauthorized || call(true) != http.StatusOK {
		t.Fatal("successful login should clear the client failure counter")
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if got := call(false); got != http.StatusUnauthorized {
			t.Fatalf("failed login attempt %d status = %d, want %d", attempt, got, http.StatusUnauthorized)
		}
	}
	if got := call(false); got != http.StatusTooManyRequests {
		t.Fatalf("third failed attempt status = %d, want %d", got, http.StatusTooManyRequests)
	}

	other := httptest.NewRequest(http.MethodPost, "/login", nil)
	other.RemoteAddr = "192.0.2.2:1234"
	other.Header.Set("X-Test-Success", "true")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, other)
	if response.Code != http.StatusOK {
		t.Fatalf("other client status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(SecurityHeaders())
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, header := range []string{"Content-Security-Policy", "Referrer-Policy", "X-Content-Type-Options", "X-Frame-Options", "Permissions-Policy"} {
		if response.Header().Get(header) == "" {
			t.Fatalf("missing security header %s", header)
		}
	}
}
