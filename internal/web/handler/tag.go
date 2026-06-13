package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// TagHandler 点位管理。
type TagHandler struct {
	runner *core.Runner
	store  *store.Store
}

// NewTagHandler 构造器。
func NewTagHandler(r *core.Runner, s *store.Store) *TagHandler {
	return &TagHandler{runner: r, store: s}
}

// ListByGroup 列出某个点组的所有点位。
func (h *TagHandler) ListByGroup(c *gin.Context) {
	groupID := c.Param("id")
	tags, err := h.store.ListTagsByGroup(c.Request.Context(), groupID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": tags})
}

// Create 新建点位。
func (h *TagHandler) Create(c *gin.Context) {
	groupID := c.Param("id")
	var t core.Tag
	if err := c.ShouldBindJSON(&t); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	t.GroupID = groupID
	if t.Access == "" {
		t.Access = core.AccessRO
	}
	t.SyncLegacyToConfig()
	group, err := h.store.GetGroup(c.Request.Context(), groupID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if group == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	config, err := h.runner.DefaultTagConfig(group.Driver, t.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	t.Config = config
	if err := h.runner.ValidateTagConfig(group.Driver, t.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if err := h.store.SaveTag(c.Request.Context(), &t); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.runner.Reload(c.Request.Context(), groupID); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// Update 更新点位。
func (h *TagHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var t core.Tag
	if err := c.ShouldBindJSON(&t); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	t.ID = id
	t.SyncLegacyToConfig()
	existing, err := h.store.GetTag(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if existing == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	t.GroupID = existing.GroupID
	group, err := h.store.GetGroup(c.Request.Context(), t.GroupID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if group == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	config, err := h.runner.DefaultTagConfig(group.Driver, t.Config)
	if err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	t.Config = config
	if err := h.runner.ValidateTagConfig(group.Driver, t.Config); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	if err := h.store.SaveTag(c.Request.Context(), &t); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.runner.Reload(c.Request.Context(), t.GroupID); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// Delete 删除点位。
func (h *TagHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tag, err := h.store.GetTag(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if tag == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	if err := h.store.DeleteTag(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if err := h.runner.Reload(c.Request.Context(), tag.GroupID); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// Read 主动读点位。
func (h *TagHandler) Read(c *gin.Context) {
	id := c.Param("id")
	tag, err := h.store.GetTag(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if tag == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	v, err := h.runner.ReadTag(ctx, tag.GroupID, id)
	if err != nil {
		v.Quality = core.QualityBad
		v.Error = err.Error()
	}
	c.JSON(http.StatusOK, v)
}

// Write 主动写点位。
func (h *TagHandler) Write(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Value interface{} `json:"value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, err)
		return
	}
	tag, err := h.store.GetTag(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	if tag == nil {
		errResp(c, http.StatusNotFound, errNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	if err := h.runner.WriteTag(ctx, tag.GroupID, id, req.Value); err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "written": true, "value": req.Value})
}

// LastValues 返回点组最近一次采集值。
func (h *TagHandler) LastValues(c *gin.Context) {
	id := c.Param("id")
	c.JSON(http.StatusOK, h.runner.LastValues(id))
}
