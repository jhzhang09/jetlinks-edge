package core

import (
	"context"
	"time"
)

// SouthDriver 是南向驱动接口（设备侧）。
//
// 实现方：
//   - modbus-tcp: 通过 TCP 连接 Modbus 设备
//   - modbus-rtu: 通过串口连接 Modbus 设备（预留）
//   - opc-ua: 通过 OPC-UA 协议连接（预留）
//   - siemens-s7: 连接西门子 PLC（预留）
type SouthDriver interface {
	// Name 驱动名（用于注册表查找）。
	Name() string
	// Connect 建立连接。配置变化时会被重新调用。
	Connect(ctx context.Context) error
	// ReadTags 读取一组点位。返回的值按传入顺序一一对应。
	ReadTags(ctx context.Context, tags []Tag) ([]TagValue, error)
	// WriteTag 写入单个点位。
	WriteTag(ctx context.Context, tag Tag, value interface{}) error
	// Disconnect 断开连接。
	Disconnect() error
	// Status 返回驱动状态。
	Status() DriverStatus
}

// NodeItem 代表 OPC UA 节点浏览器中的单个节点项。
type NodeItem struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Folder        bool                   `json:"folder"`
	Type          string                 `json:"type"`
	AccessModes   []string               `json:"accessModes"`
	Configuration map[string]interface{} `json:"configuration"`
}

// NodeBrowser 是支持节点浏览的驱动可选实现接口。
type NodeBrowser interface {
	Browse(ctx context.Context, nodeId string) ([]NodeItem, error)
}

// DriverStatus 驱动状态。
type DriverStatus struct {
	Connected bool             `json:"connected"`
	LastError string           `json:"lastError"`
	LastTime  time.Time        `json:"lastTime"`
	Stats     map[string]int64 `json:"stats"`
}
