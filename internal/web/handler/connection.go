package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// ConnectionHandler 物理通道管理。
type ConnectionHandler struct {
	runner *core.Runner
	store  *store.Store
}

// NewConnectionHandler 构造器。
func NewConnectionHandler(r *core.Runner, s *store.Store) *ConnectionHandler {
	return &ConnectionHandler{runner: r, store: s}
}

// List 列出所有物理通道。
func (h *ConnectionHandler) List(c *gin.Context) {
	conns, err := h.store.ListConnections(c.Request.Context())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": conns})
}

// Get 通道详情。
func (h *ConnectionHandler) Get(c *gin.Context) {
	id := c.Param("id")
	conn, err := h.store.GetConnection(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if conn == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	c.JSON(http.StatusOK, conn)
}

// Create 新建通道。
func (h *ConnectionHandler) Create(c *gin.Context) {
	var conn core.Connection
	if err := c.ShouldBindJSON(&conn); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if conn.ID == "" {
		conn.ID = uuid.NewString()
	}
	if conn.Driver == "" {
		errResp(c, http.StatusBadRequest, errMissingField("driver"))
		return
	}

	// 补全默认配置与参数校验
	config, err := h.runner.DefaultDriverConfig(conn.Driver, conn.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	conn.Config = config
	if err := h.runner.ValidateDriverConfig(conn.Driver, conn.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}

	conn.MarshalConfig()
	if err := h.store.SaveConnection(c.Request.Context(), &conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}

	// 立即热加载运行
	if conn.Enabled {
		if err := h.runner.ReloadConnection(c.Request.Context(), conn.ID); err != nil {
			errResp(c, http.StatusInternalServerError, err)
			return
		}
	}
	c.JSON(http.StatusOK, conn)
}

// Update 更新通道。
func (h *ConnectionHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var conn core.Connection
	if err := c.ShouldBindJSON(&conn); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	conn.ID = id
	if conn.Driver == "" {
		errResp(c, http.StatusBadRequest, errMissingField("driver"))
		return
	}

	// 补全默认配置与参数校验
	config, err := h.runner.DefaultDriverConfig(conn.Driver, conn.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	conn.Config = config
	if err := h.runner.ValidateDriverConfig(conn.Driver, conn.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}

	conn.MarshalConfig()
	if err := h.store.SaveConnection(c.Request.Context(), &conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}

	// 热重启物理连接，并重新唤起该连接下所有已启用的 Group
	if err := h.runner.ReloadConnection(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, conn)
}

// Delete 删除通道（会级联清空其下的所有点组和点位）。
func (h *ConnectionHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeleteConnection(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.runner.ReloadConnection(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// Drivers 获取可用的南向物理驱动插件元描述列表。
func (h *ConnectionHandler) Drivers(c *gin.Context) {
	descriptors := h.runner.DriverDescriptors()
	c.JSON(http.StatusOK, gin.H{"items": descriptors})
}
