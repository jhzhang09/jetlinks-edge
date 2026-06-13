package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// DriverHandler 驱动/北向应用管理。
type DriverHandler struct {
	runner *core.Runner
}

// NewDriverHandler 构造器。
func NewDriverHandler(r *core.Runner) *DriverHandler {
	return &DriverHandler{runner: r}
}

// ListDrivers 列出所有已注册南向驱动。
func (h *DriverHandler) ListDrivers(c *gin.Context) {
	drivers := h.runner.Drivers()
	c.JSON(http.StatusOK, gin.H{"items": drivers})
}

// ListDriverExtensions 列出全部南向编译期插件描述符。
func (h *DriverHandler) ListDriverExtensions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": h.runner.DriverDescriptors()})
}

// ListNorthExtensions 列出全部北向编译期插件描述符。
func (h *DriverHandler) ListNorthExtensions(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": h.runner.NorthDescriptors()})
}
