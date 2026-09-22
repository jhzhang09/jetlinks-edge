package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/externalplugin"
)

// PluginHandler 管理进程外热插拔插件。
//
// @author jhzhang
// @date 2026-09-22
type PluginHandler struct {
	manager *externalplugin.Manager
	runner  *core.Runner
}

// NewPluginHandler 创建插件管理处理器。
func NewPluginHandler(manager *externalplugin.Manager, runner *core.Runner) *PluginHandler {
	return &PluginHandler{manager: manager, runner: runner}
}

// List 返回当前已加载的外部插件。
func (h *PluginHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"directory": h.manager.Directory(),
		"items":     h.manager.List(),
	})
}

// Reload 重新扫描插件目录，并把变化应用到现有运行时。
func (h *PluginHandler) Reload(c *gin.Context) {
	changes, err := h.manager.Reload()
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if err := h.runner.ReconcileExtensionTypes(
		c.Request.Context(),
		changes.DriverTypes,
		changes.NorthTypes,
		changes.RemovedDriverTypes,
		changes.RemovedNorthTypes,
	); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"changes": changes, "items": h.manager.List()})
}
