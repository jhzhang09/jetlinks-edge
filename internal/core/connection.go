// Package core 是边缘网关的运行时核心。
//
// @author jhzhang
// @date 2026-06-13
package core

import (
	"encoding/json"
	"time"
)

// Connection 物理连接通道（代表一个真实的物理链路，如 TCP 连接、串口线）。
//
// 物理通道维护了具体驱动类型的物理参数连接配置，而与具体的逻辑设备采集周期解耦。
type Connection struct {
	ID          string                 `json:"id" gorm:"primaryKey;type:varchar(64)"`
	Name        string                 `json:"name" gorm:"type:varchar(128)"`
	Description string                 `json:"description" gorm:"type:varchar(256)"`
	Driver      string                 `json:"driver" gorm:"type:varchar(64)"` // 关联的南向驱动类型，如 "modbus-tcp", "opc-ua"
	Enabled     bool                   `json:"enabled" gorm:"default:true"`
	Config      map[string]interface{} `json:"config" gorm:"-"` // 物理连接参数（Host, Port, Timeout 等）
	ConfigJSON  string                 `json:"-" gorm:"column:config;type:text"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
}

// MarshalConfig 把 Config 序列化为 JSON 字符串。
func (c *Connection) MarshalConfig() {
	if c.Config == nil {
		c.ConfigJSON = "{}"
		return
	}
	b, _ := json.Marshal(c.Config)
	c.ConfigJSON = string(b)
}

// UnmarshalConfig 从 ConfigJSON 反序列化为 Config。
func (c *Connection) UnmarshalConfig() {
	if c.ConfigJSON == "" {
		c.Config = map[string]interface{}{}
		return
	}
	_ = json.Unmarshal([]byte(c.ConfigJSON), &c.Config)
}
