package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// ConnectionHandler 南向连接管理。
type ConnectionHandler struct {
	runner *core.Runner
	store  *store.Store
}

// NewConnectionHandler 构造器。
func NewConnectionHandler(r *core.Runner, s *store.Store) *ConnectionHandler {
	return &ConnectionHandler{runner: r, store: s}
}

// List 列出所有南向连接。
func (h *ConnectionHandler) List(c *gin.Context) {
	conns, err := h.store.ListConnections(c.Request.Context())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	for _, conn := range conns {
		if err := h.redactConfig(conn); err != nil {
			errResp(c, http.StatusInternalServerError, err)
			return
		}
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
	if err := h.redactConfig(conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
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
	existing, err := h.store.GetConnection(c.Request.Context(), conn.ID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if existing != nil {
		errResp(c, http.StatusConflict, &simpleErr{msg: "connection id already exists"})
		return
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

	if err := h.store.CreateConnection(c.Request.Context(), &conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}

	// 立即热加载运行
	if conn.Enabled {
		if err := h.runner.ReloadConnection(c.Request.Context(), conn.ID); err != nil {
			_ = h.store.DeleteConnection(c.Request.Context(), conn.ID)
			errResp(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := h.redactConfig(&conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, conn)
}

// Update 更新通道。
func (h *ConnectionHandler) Update(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.store.GetConnection(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if existing == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	var conn core.Connection
	if err := c.ShouldBindJSON(&conn); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	conn.ID = id
	conn.CreatedAt = existing.CreatedAt
	if conn.Driver == "" {
		errResp(c, http.StatusBadRequest, errMissingField("driver"))
		return
	}

	// 补全默认配置与参数校验；密码占位符表示保留原值。
	merged := conn.Config
	if conn.Driver == existing.Driver {
		merged, err = h.runner.MergeDriverSensitiveConfig(conn.Driver, conn.Config, existing.Config)
		if err != nil {
			errResp(c, http.StatusBadRequest, err)
			return
		}
	}
	config, err := h.runner.DefaultDriverConfig(conn.Driver, merged)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	conn.Config = config
	if err := h.runner.ValidateDriverConfig(conn.Driver, conn.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}

	if err := h.store.SaveConnection(c.Request.Context(), &conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}

	// 热重启物理连接，并重新唤起该连接下所有已启用的 Group
	if err := h.runner.ReloadConnection(c.Request.Context(), id); err != nil {
		rollbackErr := h.store.SaveConnection(c.Request.Context(), existing)
		if rollbackErr == nil {
			rollbackErr = h.runner.ReloadConnection(c.Request.Context(), id)
		}
		if rollbackErr != nil {
			err = fmt.Errorf("apply connection update: %w; rollback failed: %v", err, rollbackErr)
		}
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.redactConfig(&conn); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, conn)
}

func (h *ConnectionHandler) redactConfig(conn *core.Connection) error {
	redacted, err := h.runner.RedactDriverConfig(conn.Driver, conn.Config)
	if err != nil {
		return err
	}
	conn.Config = redacted
	return nil
}

// Delete 删除南向连接，并级联删除其采集组和点位。
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
