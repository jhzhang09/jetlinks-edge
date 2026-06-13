// Package core 是边缘网关的运行时核心。
//
// 核心职责：
//  1. 维护驱动注册表（南向），按 driver 名查找 SouthDriver 实例
//  2. 维护北向应用注册表，按 northapp 名查找 NorthApp 实例
//  3. 加载点组配置（Group + Tag），为每个点组创建采集协程
//  4. 把采集结果路由到对应的北向应用
//
// 设计：
//   - 点组（Group）= 一台逻辑设备，对应一个南向驱动实例
//   - 点位（Tag）= 一个属性，对应一个 Modbus 寄存器 / OPC-UA 节点 / ...
//   - 调度器（Runner）= 一个点组对应一个 goroutine，按 interval 周期采集
package core

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Address 数据地址。不同协议的 address 表示不同：
//   - Modbus: "40001"（保持寄存器）、"10001"（离散输入）、"30001"（输入寄存器）、"00001"（线圈）
//   - OPC-UA: "ns=2;s=Channel1.Device1.Tag1"
type Address string

// DataType 点位数据类型。
type DataType string

const (
	TypeBool    DataType = "bool"
	TypeInt16   DataType = "int16"
	TypeUInt16  DataType = "uint16"
	TypeInt32   DataType = "int32"
	TypeUInt32  DataType = "uint32"
	TypeInt64   DataType = "int64"
	TypeUInt64  DataType = "uint64"
	TypeFloat32 DataType = "float32"
	TypeFloat64 DataType = "float64"
	TypeString  DataType = "string"
	TypeBytes   DataType = "bytes"
)

// Access 读写权限。
type Access string

const (
	AccessRO Access = "ro" // 只读
	AccessWO Access = "wo" // 只写
	AccessRW Access = "rw" // 读写
)

// Tag 点位定义。
type Tag struct {
	ID          string                 `json:"id" gorm:"primaryKey;type:varchar(64)"`
	GroupID     string                 `json:"groupId" gorm:"index;type:varchar(64)"`
	Name        string                 `json:"name" gorm:"type:varchar(128)"`
	Address     Address                `json:"address" gorm:"type:varchar(256)"`
	Type        DataType               `json:"type" gorm:"type:varchar(32)"`
	ByteOrder   string                 `json:"byteOrder" gorm:"type:varchar(16)"` // AB / BA / ABCD / BADC ...
	Bit         int                    `json:"bit"`                               // bit 位（用于 bool 寄存器位提取）
	Decimal     float64                `json:"decimal"`                           // 缩放系数：value * Decimal
	Precision   int                    `json:"precision"`                         // 保留小数位
	Access      Access                 `json:"access" gorm:"type:varchar(8)"`
	Description string                 `json:"description" gorm:"type:varchar(256)"`
	Config      map[string]interface{} `json:"config" gorm:"-"`
	ConfigJSON  string                 `json:"-" gorm:"column:config;type:text"`
}

// MarshalConfig 把 Tag.Config 序列化为 JSON，并同步当前 Modbus 兼容字段。
func (t *Tag) MarshalConfig() {
	t.SyncLegacyToConfig()
	t.ApplyConfig()
	if t.Config == nil {
		t.ConfigJSON = "{}"
		return
	}
	b, _ := json.Marshal(t.Config)
	t.ConfigJSON = string(b)
}

// UnmarshalConfig 从 ConfigJSON 反序列化，并用现有字段补齐旧数据。
func (t *Tag) UnmarshalConfig() {
	if t.ConfigJSON == "" {
		t.Config = map[string]interface{}{}
	} else {
		_ = json.Unmarshal([]byte(t.ConfigJSON), &t.Config)
	}
	if t.Config == nil {
		t.Config = map[string]interface{}{}
	}
	t.SyncLegacyToConfig()
	t.ApplyConfig()
}

// SyncLegacyToConfig 使用旧版点位字段补齐动态配置，兼容旧 API 请求和已有数据。
func (t *Tag) SyncLegacyToConfig() {
	if t.Config == nil {
		t.Config = map[string]interface{}{}
	}
	if _, exists := t.Config["address"]; !exists && t.Address != "" {
		t.Config["address"] = string(t.Address)
	}
	if _, exists := t.Config["byteOrder"]; !exists && t.ByteOrder != "" {
		t.Config["byteOrder"] = t.ByteOrder
	}
	if _, exists := t.Config["bit"]; !exists {
		t.Config["bit"] = t.Bit
	}
}

// ApplyConfig 把动态点位配置同步到现有驱动字段，保持旧驱动实现兼容。
func (t *Tag) ApplyConfig() {
	if t.Config == nil {
		t.Config = map[string]interface{}{}
	}
	if value, ok := t.Config["address"].(string); ok {
		t.Address = Address(value)
	}
	if value, ok := t.Config["byteOrder"].(string); ok {
		t.ByteOrder = value
	}
	if value, ok := asFloat64(t.Config["bit"]); ok {
		t.Bit = int(value)
	}
}

// TagValue 一个点位的当前值。
type TagValue struct {
	TagID   string      `json:"tagId"`
	Name    string      `json:"name"`
	Value   interface{} `json:"value"`
	Quality Quality     `json:"quality"`
	Time    time.Time   `json:"time"`
	Error   string      `json:"error,omitempty"`
}

// Quality 数值质量。
type Quality string

const (
	QualityGood      Quality = "good"
	QualityBad       Quality = "bad"
	QualityUncertain Quality = "uncertain"
)

// Group 点组 = 一台逻辑设备。
type Group struct {
	ID           string                 `json:"id" gorm:"primaryKey;type:varchar(64)"`
	Name         string                 `json:"name" gorm:"type:varchar(128)"`
	Description  string                 `json:"description" gorm:"type:varchar(256)"`
	ConnectionID string                 `json:"connectionId" gorm:"type:varchar(64);index"` // 关联的物理通道外键
	Driver       string                 `json:"driver" gorm:"-"`                            // 驱动类型，从关联物理连接动态填充
	Interval     time.Duration          `json:"interval" gorm:"-"`                          // 采集周期（不存 DB，启动时由 IntervalMs 转换）
	IntervalMs   int                    `json:"intervalMs" gorm:"column:interval_ms"`
	Config       map[string]interface{} `json:"config" gorm:"-"` // 逻辑协议组配置（如 Modbus 组下的从站号 "unitId"）
	ConfigJSON   string                 `json:"-" gorm:"column:config;type:text"`
	Enabled      bool                   `json:"enabled" gorm:"default:true"`
	// 与北向应用的关联：可空（纯本地采集）、可改（切换上送目标不影响采集）
	NorthAppID string `json:"northAppId" gorm:"type:varchar(64);index"`
	// 设备身份：每台设备一份，定义本 Group 对应 JetLinks 平台哪个 productId/deviceId
	Device DeviceConfig `json:"device" gorm:"embedded;embeddedPrefix:device_"`
	Tags   []Tag        `json:"tags" gorm:"foreignKey:GroupID;references:ID"`
}

// NorthApp 北向应用（独立实体，与 Group 解耦）。
//
// 语义：
//   - 一个 NorthApp 是一种"上送通道"（如 jetlinks-mqtt），可被多个 Group 共享。
//   - 改 NorthApp 配置不会影响点组的 Modbus 连接/采集逻辑。
//   - NorthApp 可独立启停（Enabled=false 时不上送，但 Group 仍在采集）。
//
// 关键：NorthApp 内部**不包含设备身份**（productId/deviceId）。它只描述
// "用什么 broker 通道上送"，设备身份在 Group.DeviceConfig 中。
// 多个 Group 共享一个 NorthApp = 多个设备共享一条 MQTT 连接。
type NorthApp struct {
	ID          string                 `json:"id" gorm:"primaryKey;type:varchar(64)"`
	Name        string                 `json:"name" gorm:"type:varchar(128)"`
	Description string                 `json:"description" gorm:"type:varchar(256)"`
	Type        string                 `json:"type" gorm:"type:varchar(64)"` // north app type (registered name)
	Enabled     bool                   `json:"enabled" gorm:"default:true"`
	Config      map[string]interface{} `json:"config" gorm:"-"` // app 私有配置（broker 地址/网关账号等）
	ConfigJSON  string                 `json:"-" gorm:"column:config;type:text"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

// DeviceConfig 设备身份（每台 JetLinks 设备一份）。
//
// 放在 Group 里而不是 NorthApp 里，因为：
//   - NorthApp 是通道（broker 连接），不关心具体设备
//   - 同一 NorthApp 上送的多个 Group 对应多台设备
//   - productId/deviceId 用于构造 MQTT topic
//   - secureId/secureKey 用于 JetLinks 规范的 SM3 认证
//     （clientId=deviceId、username=secureId+'|'+timestamp、password=SM3(...)）
//
// 注意：secureId 与 secureKey 是 JetLinks 平台对设备/产品的安全凭据，
// 与"北向应用"中的 broker 网关账号不同。
type DeviceConfig struct {
	ProductID string `json:"productId" gorm:"type:varchar(128)"`
	DeviceID  string `json:"deviceId"  gorm:"type:varchar(128)"`
	SecureID  string `json:"secureId"  gorm:"type:varchar(128)"`           // 平台设备 secureId
	SecureKey string `json:"secureKey,omitempty" gorm:"type:varchar(256)"` // 平台设备 secureKey
}

// MarshalConfig 把 Config 序列化为 JSON 字符串。
func (g *Group) MarshalConfig() {
	if g.Config == nil {
		g.ConfigJSON = "{}"
		return
	}
	b, _ := json.Marshal(g.Config)
	g.ConfigJSON = string(b)
}

// UnmarshalConfig 从 ConfigJSON 反序列化。
func (g *Group) UnmarshalConfig() {
	if g.ConfigJSON == "" {
		g.Config = map[string]interface{}{}
		return
	}
	_ = json.Unmarshal([]byte(g.ConfigJSON), &g.Config)
}

// MarshalConfig 把 NorthApp.Config 序列化为 JSON 字符串。
func (n *NorthApp) MarshalConfig() {
	if n.Config == nil {
		n.ConfigJSON = "{}"
		return
	}
	b, _ := json.Marshal(n.Config)
	n.ConfigJSON = string(b)
}

// UnmarshalConfig 从 ConfigJSON 反序列化。
func (n *NorthApp) UnmarshalConfig() {
	if n.ConfigJSON == "" {
		n.Config = map[string]interface{}{}
		return
	}
	_ = json.Unmarshal([]byte(n.ConfigJSON), &n.Config)
}

// ValueChange 数据变化回调。
type ValueChange func(groupID string, values []TagValue)

// DriverConfig 驱动实例化配置。
type DriverConfig struct {
	GroupID        string
	Config         map[string]interface{}
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	ReconnectDelay time.Duration
}

// NorthMessage 北向消息。
type NorthMessage struct {
	GroupID   string                 `json:"groupId"`
	ProductID string                 `json:"productId"`
	DeviceID  string                 `json:"deviceId"`
	Type      string                 `json:"type"` // property/event/register/log
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// NorthHandler 北向消息处理接口。
type NorthHandler interface {
	// OnMessage 处理从边缘到平台的消息（属性上报、事件、日志等）。
	// msg 中带 ProductID/DeviceID，handler 自行根据这些字段路由 topic。
	OnMessage(ctx context.Context, msg NorthMessage) error
	// OnCommand 接收从平台到边缘的指令（读属性/写属性/调用功能），返回响应。
	// cmd 中带 ProductID/DeviceID，handler 根据这些字段定位目标 Group 调用驱动。
	OnCommand(ctx context.Context, cmd NorthCommand) (NorthCommandReply, error)
}

// NorthCommandExecutor 执行平台下发的南向指令。
type NorthCommandExecutor func(ctx context.Context, cmd NorthCommand) (NorthCommandReply, error)

// NorthLifecycle 是北向应用可选的关闭生命周期。
type NorthLifecycle interface {
	Close() error
}

// NorthState 北向应用实时状态。
type NorthState struct {
	Connected bool   `json:"connected"`
	LastError string `json:"lastError"`
}

// NorthStateReporter 可选接口：NorthHandler 可实现，用于探活。
type NorthStateReporter interface {
	State() *NorthState
}

// NorthCommand 北向指令。
type NorthCommand struct {
	ID        string                 `json:"id"`
	GroupID   string                 `json:"groupId"`
	ProductID string                 `json:"productId"`
	DeviceID  string                 `json:"deviceId"`
	Type      string                 `json:"type"` // read-property / write-property / invoke-function
	Payload   map[string]interface{} `json:"payload"`
}

// NorthCommandReply 北向指令响应。
type NorthCommandReply struct {
	ID      string                 `json:"id"`
	Code    int                    `json:"code"`    // 0 = 成功，其他 = 失败
	Message string                 `json:"message"` // 错误信息
	Payload map[string]interface{} `json:"payload"`
}

// HasNorthAppID 判断逗号分隔的北向应用 ID 列表中是否包含指定 ID
func HasNorthAppID(idsStr, id string) bool {
	parts := strings.Split(idsStr, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == id {
			return true
		}
	}
	return false
}

// RemoveNorthAppID 从逗号分隔的北向应用 ID 列表中移除指定 ID
func RemoveNorthAppID(idsStr, id string) string {
	parts := strings.Split(idsStr, ",")
	var res []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != id {
			res = append(res, part)
		}
	}
	return strings.Join(res, ",")
}
