package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// GroupHandler 点组管理。
type GroupHandler struct {
	runner *core.Runner
	store  *store.Store
}

// NewGroupHandler 构造器。
func NewGroupHandler(r *core.Runner, s *store.Store) *GroupHandler {
	return &GroupHandler{runner: r, store: s}
}

// List 列出所有点组。
func (h *GroupHandler) List(c *gin.Context) {
	gs, err := h.store.ListGroups(c.Request.Context())
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": gs})
}

// Get 点组详情。
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
	c.JSON(http.StatusOK, g)
}

// Create 新建点组。
func (h *GroupHandler) Create(c *gin.Context) {
	var g core.Group
	if err := c.ShouldBindJSON(&g); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if g.ID == "" {
		g.ID = uuid.NewString()
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
	g.MarshalConfig()
	if err := h.store.DB().Create(&g).Error; err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	// 立即加载运行
	if g.Enabled {
		if err := h.runner.Reload(c.Request.Context(), g.ID); err != nil {
			errResp(c, http.StatusInternalServerError, err)
			return
		}
	}
	h.store.PopulateGroupDriver(&g)
	c.JSON(http.StatusOK, g)
}

// Update 更新点组。
func (h *GroupHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var g core.Group
	if err := c.ShouldBindJSON(&g); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	g.ID = id
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
	isRequired, err := h.isJetLinksGatewayRequired(c.Request.Context(), g.NorthAppID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if isRequired && (g.Device.ProductID == "" || g.Device.DeviceID == "") {
		errResp(c, http.StatusBadRequest, errMissingField("device.productId/device.deviceId (when JetLinks Gateway is bound)"))
		return
	}
	g.MarshalConfig()
	if err := h.store.DB().Save(&g).Error; err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	// 热重启
	if err := h.runner.Reload(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	h.store.PopulateGroupDriver(&g)
	c.JSON(http.StatusOK, g)
}

// Delete 删除点组。
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

// Reload 手动触发点组热重启。
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
