package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// GroupHandler 采集组管理。
type GroupHandler struct {
	runner *core.Runner
	store  *store.Store
}

// NewGroupHandler 构造器。
func NewGroupHandler(r *core.Runner, s *store.Store) *GroupHandler {
	return &GroupHandler{runner: r, store: s}
}

// List 列出所有采集组。
func (h *GroupHandler) List(c *gin.Context) {
	gs, err := h.store.ListGroups(c.Request.Context())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	for _, group := range gs {
		redactGroupSecrets(group)
	}
	c.JSON(http.StatusOK, gin.H{"items": gs})
}

// Get 采集组详情。
func (h *GroupHandler) Get(c *gin.Context) {
	id := c.Param("id")
	g, err := h.store.GetGroup(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if g == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	redactGroupSecrets(g)
	c.JSON(http.StatusOK, g)
}

// Create 新建采集组。
func (h *GroupHandler) Create(c *gin.Context) {
	var g core.Group
	if err := c.ShouldBindJSON(&g); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	existing, err := h.store.GetGroup(c.Request.Context(), g.ID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if existing != nil {
		errResp(c, http.StatusConflict, &simpleErr{msg: "group id already exists"})
		return
	}
	if g.IntervalMs == 0 {
		g.IntervalMs = 1000
	}
	if g.ConnectionID == "" {
		errResp(c, http.StatusBadRequest, errMissingField("connectionId"))
		return
	}
	conn, err := h.store.GetConnection(c.Request.Context(), g.ConnectionID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if conn == nil {
		errResp(c, http.StatusBadRequest, &simpleErr{msg: "connection not found"})
		return
	}
	config, err := h.runner.DefaultGroupConfig(conn.Driver, g.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	g.Config = config
	if err := h.runner.ValidateGroupConfig(conn.Driver, g.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	// 校验：若绑定了 JetLinks 网关北向时，必须填设备身份
	isRequired, err := h.isJetLinksGatewayRequired(c.Request.Context(), g.NorthAppID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if isRequired && (g.Device.ProductID == "" || g.Device.DeviceID == "") {
		errResp(c, http.StatusBadRequest, errMissingField("device.productId/device.deviceId (when JetLinks Gateway is bound)"))
		return
	}
	if err := h.store.CreateGroup(c.Request.Context(), &g); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	// 立即加载运行
	if g.Enabled {
		if err := h.runner.Reload(c.Request.Context(), g.ID); err != nil {
			_ = h.store.DeleteGroup(c.Request.Context(), g.ID)
			errResp(c, http.StatusInternalServerError, err)
			return
		}
	}
	h.store.PopulateGroupDriver(&g)
	redactGroupSecrets(&g)
	c.JSON(http.StatusOK, g)
}

// Update 更新采集组。
func (h *GroupHandler) Update(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.store.GetGroup(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if existing == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	var g core.Group
	if err := c.ShouldBindJSON(&g); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	g.ID = id
	if g.Device.SecureKey == core.MaskedSecret {
		g.Device.SecureKey = existing.Device.SecureKey
	}
	if g.ConnectionID == "" {
		errResp(c, http.StatusBadRequest, errMissingField("connectionId"))
		return
	}
	conn, err := h.store.GetConnection(c.Request.Context(), g.ConnectionID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if conn == nil {
		errResp(c, http.StatusBadRequest, &simpleErr{msg: "connection not found"})
		return
	}
	config, err := h.runner.DefaultGroupConfig(conn.Driver, g.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	g.Config = config
	if err := h.runner.ValidateGroupConfig(conn.Driver, g.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	isRequired, err := h.isJetLinksGatewayRequired(c.Request.Context(), g.NorthAppID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if isRequired && (g.Device.ProductID == "" || g.Device.DeviceID == "") {
		errResp(c, http.StatusBadRequest, errMissingField("device.productId/device.deviceId (when JetLinks Gateway is bound)"))
		return
	}
	if err := h.store.SaveGroup(c.Request.Context(), &g); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	// 热重启
	if err := h.runner.Reload(c.Request.Context(), id); err != nil {
		rollbackErr := h.store.SaveGroup(c.Request.Context(), existing)
		if rollbackErr == nil {
			rollbackErr = h.runner.Reload(c.Request.Context(), id)
		}
		if rollbackErr != nil {
			err = fmt.Errorf("apply group update: %w; rollback failed: %v", err, rollbackErr)
		}
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	h.store.PopulateGroupDriver(&g)
	redactGroupSecrets(&g)
	c.JSON(http.StatusOK, g)
}

// Delete 删除采集组。
func (h *GroupHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.store.DeleteGroup(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.runner.Reload(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// Reload 手动触发采集组热重启。
func (h *GroupHandler) Reload(c *gin.Context) {
	id := c.Param("id")
	if err := h.runner.Reload(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "reloaded": true})
}

// BrowseOPCUA 浏览指定的 OPC UA 节点。
func (h *GroupHandler) BrowseOPCUA(c *gin.Context) {
	id := c.Param("id")
	nodeId := c.Query("nodeId")
	nodes, err := h.runner.BrowseOPCUA(c.Request.Context(), id, nodeId)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": nodes})
}

var errNotFound = &simpleErr{msg: "not found"}

type simpleErr struct{ msg string }

func (e *simpleErr) Error() string { return e.msg }

func redactGroupSecrets(group *core.Group) {
	if group != nil && group.Device.SecureKey != "" {
		group.Device.SecureKey = core.MaskedSecret
	}
}

func (h *GroupHandler) isJetLinksGatewayRequired(ctx context.Context, northAppIDs string) (bool, error) {
	if northAppIDs == "" {
		return false, nil
	}
	parts := strings.Split(northAppIDs, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		app, err := h.store.GetNorthApp(ctx, part)
		if err != nil {
			return false, err
		}
		if app != nil && app.Type == "jetlinks-mqtt" {
			return true, nil
		}
	}
	return false, nil
}
