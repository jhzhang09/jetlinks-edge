package handler

import (
	"net/http"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

// StatusHandler 运行状态。
type StatusHandler struct {
	runner *core.Runner
	store  *store.Store
}

// NewStatusHandler 构造器。
func NewStatusHandler(r *core.Runner, s *store.Store) *StatusHandler {
	return &StatusHandler{runner: r, store: s}
}

// Status 系统总览。
func (h *StatusHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"groups":       h.runner.GroupNames(),
		"groupsDetail": h.runner.GroupInfoList(),
		"drivers":      h.runner.Status(),
		"startTime":    h.runner.StartTime().UnixMilli(),
	})
}

// Operations 返回面向运维工作台的聚合视图。
//
// 该接口只组合现有配置与运行时状态，不持久化或推断历史事件。告警表示当前仍存在的故障，
// 前端可据此动态渲染概览、链路拓扑与故障处置页面，并保持对新增编译期插件的兼容。
func (h *StatusHandler) Operations(c *gin.Context) {
	ctx := c.Request.Context()
	conns, err := h.store.ListConnections(ctx)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	connStatus := h.runner.ConnectionStatus()
	operationConns := make([]operationConnection, 0, len(conns))
	for _, conn := range conns {
		status, running := connStatus[conn.ID]
		operationConns = append(operationConns, operationConnection{
			ID:        conn.ID,
			Name:      conn.Name,
			Driver:    conn.Driver,
			Enabled:   conn.Enabled,
			Running:   running,
			Connected: running && status.Connected,
			LastError: status.LastError,
			LastTime:  status.LastTime,
		})
	}

	var groups []core.Group
	if err := h.store.DB().WithContext(ctx).Order("name asc").Find(&groups).Error; err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}
	for i := range groups {
		groups[i].UnmarshalConfig()
		h.store.PopulateGroupDriver(&groups[i])
	}
	northApps, err := h.runner.ListNorthAppStatus(ctx)
	if err != nil {
		errResp(c, http.StatusInternalServerError, err)
		return
	}

	driverStatus := h.runner.Status()
	operationGroups := make([]operationGroup, 0, len(groups))
	alarms := make([]operationAlarm, 0)
	recentValues := make([]operationValue, 0)
	for _, group := range groups {
		status, running := driverStatus[group.ID]
		values := h.runner.LastValues(group.ID)
		operationGroups = append(operationGroups, operationGroup{
			ID:           group.ID,
			Name:         group.Name,
			Driver:       group.Driver,
			Enabled:      group.Enabled,
			ConnectionID: group.ConnectionID,
			NorthAppID:   group.NorthAppID,
			IntervalMs:   group.IntervalMs,
			Running:      running,
			Connected:    running && status.Connected,
			LastError:    status.LastError,
			LastTime:     status.LastTime,
			Stats:        status.Stats,
			ValueCount:   len(values),
			DeviceID:     group.Device.DeviceID,
			Description:  group.Description,
		})
		if group.Enabled && (!running || !status.Connected || status.LastError != "") {
			message := status.LastError
			if message == "" {
				if !running {
					message = "南向驱动未运行"
				} else {
					message = "南向设备连接中断"
				}
			}
			alarms = append(alarms, operationAlarm{
				ID:         "group:" + group.ID,
				SourceType: "group",
				SourceID:   group.ID,
				SourceName: group.Name,
				Severity:   "critical",
				Message:    message,
				Time:       status.LastTime,
				Route:      "/groups/" + group.ID,
			})
		}
		for _, value := range values {
			recentValues = append(recentValues, operationValue{
				GroupID:   group.ID,
				GroupName: group.Name,
				TagID:     value.TagID,
				Name:      value.Name,
				Value:     value.Value,
				Quality:   value.Quality,
				Time:      value.Time,
				Error:     value.Error,
			})
			if value.Quality == core.QualityBad {
				alarms = append(alarms, operationAlarm{
					ID:         "tag:" + group.ID + ":" + value.TagID,
					SourceType: "tag",
					SourceID:   group.ID,
					SourceName: group.Name + " / " + value.Name,
					Severity:   "warning",
					Message:    value.Error,
					Time:       value.Time,
					Route:      "/groups/" + group.ID,
				})
			}
		}
	}
	for _, app := range northApps {
		if app.Enabled && (!app.Running || !app.Connected) {
			message := "北向传输未连接"
			if !app.Running {
				message = "北向传输未运行"
			}
			alarms = append(alarms, operationAlarm{
				ID:         "north:" + app.ID,
				SourceType: "north",
				SourceID:   app.ID,
				SourceName: app.Name,
				Severity:   "critical",
				Message:    message,
				Route:      "/northbound",
			})
		}
	}
	sort.Slice(alarms, func(i, j int) bool {
		if alarms[i].Severity != alarms[j].Severity {
			return alarms[i].Severity < alarms[j].Severity
		}
		return alarms[i].Time.After(alarms[j].Time)
	})
	sort.Slice(recentValues, func(i, j int) bool {
		return recentValues[i].Time.After(recentValues[j].Time)
	})
	if len(recentValues) > 50 {
		recentValues = recentValues[:50]
	}

	c.JSON(http.StatusOK, operationView{
		GeneratedAt:   time.Now(),
		StartTime:     h.runner.StartTime(),
		Runtime:       currentOperationRuntime(h.runner.StartTime()),
		Connections:   operationConns,
		Groups:        operationGroups,
		NorthApps:     northApps,
		DriverPlugins: h.runner.DriverDescriptors(),
		NorthPlugins:  h.runner.NorthDescriptors(),
		Alarms:        alarms,
		RecentValues:  recentValues,
	})
}

type operationView struct {
	GeneratedAt   time.Time                  `json:"generatedAt"`
	StartTime     time.Time                  `json:"startTime"`
	Runtime       operationRuntime           `json:"runtime"`
	Connections   []operationConnection      `json:"connections"`
	Groups        []operationGroup           `json:"groups"`
	NorthApps     []core.NorthAppStatus      `json:"northApps"`
	DriverPlugins []core.ExtensionDescriptor `json:"driverPlugins"`
	NorthPlugins  []core.ExtensionDescriptor `json:"northPlugins"`
	Alarms        []operationAlarm           `json:"alarms"`
	RecentValues  []operationValue           `json:"recentValues"`
}

type operationConnection struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Driver    string    `json:"driver"`
	Enabled   bool      `json:"enabled"`
	Running   bool      `json:"running"`
	Connected bool      `json:"connected"`
	LastError string    `json:"lastError,omitempty"`
	LastTime  time.Time `json:"lastTime"`
}

type operationRuntime struct {
	NodeID            string  `json:"nodeId"`
	Goroutines        int     `json:"goroutines"`
	MemoryAllocBytes  uint64  `json:"memoryAllocBytes"`
	MemorySysBytes    uint64  `json:"memorySysBytes"`
	MemoryUsedPercent float64 `json:"memoryUsedPercent"`
	UptimeSeconds     int64   `json:"uptimeSeconds"`
}

type operationGroup struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Driver       string           `json:"driver"`
	Enabled      bool             `json:"enabled"`
	ConnectionID string           `json:"connectionId"`
	NorthAppID   string           `json:"northAppId,omitempty"`
	IntervalMs   int              `json:"intervalMs"`
	Running      bool             `json:"running"`
	Connected    bool             `json:"connected"`
	LastError    string           `json:"lastError,omitempty"`
	LastTime     time.Time        `json:"lastTime"`
	Stats        map[string]int64 `json:"stats,omitempty"`
	ValueCount   int              `json:"valueCount"`
	DeviceID     string           `json:"deviceId,omitempty"`
	Description  string           `json:"description,omitempty"`
}

type operationAlarm struct {
	ID         string    `json:"id"`
	SourceType string    `json:"sourceType"`
	SourceID   string    `json:"sourceId"`
	SourceName string    `json:"sourceName"`
	Severity   string    `json:"severity"`
	Message    string    `json:"message"`
	Time       time.Time `json:"time"`
	Route      string    `json:"route"`
}

type operationValue struct {
	GroupID   string       `json:"groupId"`
	GroupName string       `json:"groupName"`
	TagID     string       `json:"tagId"`
	Name      string       `json:"name"`
	Value     interface{}  `json:"value"`
	Quality   core.Quality `json:"quality"`
	Time      time.Time    `json:"time"`
	Error     string       `json:"error,omitempty"`
}

func currentOperationRuntime(startTime time.Time) operationRuntime {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	nodeID, err := os.Hostname()
	if err != nil || nodeID == "" {
		nodeID = "jetlinks-edge"
	}
	usedPercent := 0.0
	if mem.Sys > 0 {
		usedPercent = float64(mem.Alloc) / float64(mem.Sys) * 100
	}
	return operationRuntime{
		NodeID:            nodeID,
		Goroutines:        runtime.NumGoroutine(),
		MemoryAllocBytes:  mem.Alloc,
		MemorySysBytes:    mem.Sys,
		MemoryUsedPercent: usedPercent,
		UptimeSeconds:     int64(time.Since(startTime).Seconds()),
	}
}
