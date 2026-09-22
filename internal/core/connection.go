// Package core 是边缘网关的运行时核心。
//
// @author jhzhang
// @date 2026-06-13
package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// Connection 南向连接（代表一个真实的物理链路，如 TCP 连接、串口线）。
//
// 南向连接维护具体插件的物理参数，与采集组的采集周期解耦。
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
func (c *Connection) MarshalConfig() error {
	if c.Config == nil {
		c.ConfigJSON = "{}"
		return nil
	}
	b, err := json.Marshal(c.Config)
	if err != nil {
		return fmt.Errorf("marshal connection config: %w", err)
	}
	c.ConfigJSON = string(b)
	return nil
}

// UnmarshalConfig 从 ConfigJSON 反序列化为 Config。
func (c *Connection) UnmarshalConfig() error {
	if c.ConfigJSON == "" {
		c.Config = map[string]interface{}{}
		return nil
	}
	if err := json.Unmarshal([]byte(c.ConfigJSON), &c.Config); err != nil {
		c.Config = map[string]interface{}{}
		return fmt.Errorf("unmarshal connection config: %w", err)
	}
	return nil
}
