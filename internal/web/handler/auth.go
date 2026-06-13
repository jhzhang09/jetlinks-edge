// Package handler 提供 Web API 处理器。
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jhzhang09/jetlinks-edge/internal/config"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// AuthHandler 鉴权相关。
type AuthHandler struct {
	store *store.Store
	cfg   *config.Config
}

// NewAuthHandler 构造器。
func NewAuthHandler(s *store.Store, cfg *config.Config) *AuthHandler {
	return &AuthHandler{store: s, cfg: cfg}
}

// Login 登录。
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tk, u, err := h.store.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"token":  tk,
		"user":   u,
		"ttlSec": int(h.cfg.Web.TokenTTL.Seconds()),
	})
}

// Me 当前登录用户信息。
func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"id":       c.GetString("userId"),
		"username": c.GetString("username"),
		"role":     c.GetString("role"),
	})
}

// ChangePassword 修改当前登录用户的密码。
// @author jhzhang
// @date 2026-06-13
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"oldPassword" binding:"required"`
		NewPassword string `json:"newPassword" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID := c.GetString("userId")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err := h.store.ChangePassword(userID, req.OldPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "password updated successfully"})
}

// errResp 统一错误响应。
func errResp(c *gin.Context, code int, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	c.JSON(code, gin.H{"error": err.Error()})
}
