package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// NorthAppHandler 北向应用管理。
type NorthAppHandler struct {
	runner *core.Runner
}

// NewNorthAppHandler 构造器。
func NewNorthAppHandler(r *core.Runner) *NorthAppHandler {
	return &NorthAppHandler{runner: r}
}

// List 列出所有北向应用 + 状态。
func (h *NorthAppHandler) List(c *gin.Context) {
	items, err := h.runner.ListNorthAppStatus(c.Request.Context())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"types": h.runner.NorthApps(), // 注册表里可用的类型
	})
}

// Get 详情。
func (h *NorthAppHandler) Get(c *gin.Context) {
	id := c.Param("id")
	n, err := h.runner.Store().GetNorthApp(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if n == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	c.JSON(http.StatusOK, n)
}

// Create 新建。
func (h *NorthAppHandler) Create(c *gin.Context) {
	var n core.NorthApp
	if err := c.ShouldBindJSON(&n); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	if n.Name == "" || n.Type == "" {
		errResp(c, http.StatusBadRequest, errMissingField("name/type"))
		return
	}
	config, err := h.runner.DefaultNorthConfig(n.Type, n.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	n.Config = config
	if err := h.runner.ValidateNorthConfig(n.Type, n.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if err := h.runner.Store().SaveNorthApp(c.Request.Context(), &n); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if n.Enabled {
		if err := h.runner.ReloadNorthApp(c.Request.Context(), n.ID); err != nil {
			errResp(c, http.StatusInternalServerError, err)
			return
		}
	}
	c.JSON(http.StatusOK, n)
}

// Update 更新。
func (h *NorthAppHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var n core.NorthApp
	if err := c.ShouldBindJSON(&n); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	n.ID = id
	config, err := h.runner.DefaultNorthConfig(n.Type, n.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	n.Config = config
	if err := h.runner.ValidateNorthConfig(n.Type, n.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if err := h.runner.Store().SaveNorthApp(c.Request.Context(), &n); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	// 重建实例（已存在的 Group 会自动引用新实例）
	if err := h.runner.ReloadNorthApp(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, n)
}

// Delete 删除（会解除所有 Group 的绑定）。
func (h *NorthAppHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.runner.Store().DeleteNorthApp(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.runner.ReloadNorthApp(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// Reload 手动重启北向应用实例。
func (h *NorthAppHandler) Reload(c *gin.Context) {
	id := c.Param("id")
	if err := h.runner.ReloadNorthApp(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "reloaded": true})
}
