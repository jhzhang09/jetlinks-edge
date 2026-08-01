package core

import (
	"context"
	"time"
)

// DriverLifecycle 是所有南向驱动必须实现的最小生命周期接口。
// 插件可通过 RegisterLifecycleExtension 注册，再按需实现读取、写入、浏览或功能调用能力。
//
// 实现方：
//   - modbus-tcp: 通过 TCP 连接 Modbus 设备
//   - modbus-rtu: 通过串口连接 Modbus 设备（预留）
//   - opc-ua: 通过 OPC-UA 协议连接
//   - siemens-s7: 连接西门子 PLC（预留）
type DriverLifecycle interface {
	// Name 驱动名（用于注册表查找）。
	Name() string
	// Connect 建立连接。配置变化时会被重新调用。
	Connect(ctx context.Context) error
	// Disconnect 断开连接。
	Disconnect() error
	// Status 返回驱动状态。
	Status() DriverStatus
}

// SouthDriver 是旧版轮询型南向驱动的兼容接口。
// 已有插件和 DriverRegistry.Create 的调用约定保持不变；Runner 内部按可选能力使用驱动。
type SouthDriver interface {
	DriverLifecycle
	TagReader
	TagWriter
}

// TagReader 是支持批量读取点位的南向驱动可选能力。
type TagReader interface {
	ReadTags(ctx context.Context, tags []Tag) ([]TagValue, error)
}

// TagWriter 是支持写入单个点位的南向驱动可选能力。
type TagWriter interface {
	WriteTag(ctx context.Context, tag Tag, value interface{}) error
}

// FunctionInvoker 是支持设备功能调用的南向驱动可选能力。
type FunctionInvoker interface {
	InvokeFunction(ctx context.Context, functionID string, inputs interface{}) (interface{}, error)
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
