// Package core 是边缘网关的运行时核心。
//
// @author jhzhang
// @date 2026-06-13
package core

import (
	"sync"
	"time"
)

// Event 消息总线中的核心事件包。
type Event struct {
	Topic     string      `json:"topic"`
	Type      string      `json:"type"` // 例如 "values", "status"
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// EventBus 定义进程内发布订阅事件总线。
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
}

// NewEventBus 创建一个新的事件总线实例。
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

// Publish 发布事件到指定的 Topic。
func (eb *EventBus) Publish(topic string, ev Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for subTopic, chans := range eb.subscribers {
		if matchTopic(subTopic, topic) {
			for ch := range chans {
				select {
				case ch <- ev:
				default:
					// 缓冲区满时丢弃，防止单个订阅者缓慢拖垮总线
				}
			}
		}
	}
}

// Subscribe 订阅指定的主题。返回一个 Channel 和取消订阅的闭包函数。
func (eb *EventBus) Subscribe(topic string, bufSize int) (chan Event, func()) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan Event, bufSize)
	if eb.subscribers[topic] == nil {
		eb.subscribers[topic] = make(map[chan Event]struct{})
	}
	eb.subscribers[topic][ch] = struct{}{}

	unsubscribe := func() {
		eb.mu.Lock()
		defer eb.mu.Unlock()
		if chans, ok := eb.subscribers[topic]; ok {
			delete(chans, ch)
			if len(chans) == 0 {
				delete(eb.subscribers, topic)
			}
		}
	}
	return ch, unsubscribe
}

// matchTopic 简易的 MQTT 式 Topic 匹配器。支持 "+" (单层) 和 "#" (多层通配符)。
func matchTopic(pattern, topic string) bool {
	if pattern == "#" || pattern == topic {
		return true
	}
	pParts := splitTopic(pattern)
	tParts := splitTopic(topic)

	for i := 0; i < len(pParts); i++ {
		if pParts[i] == "#" {
			return true
		}
		if i >= len(tParts) {
			return false
		}
		if pParts[i] != "+" && pParts[i] != tParts[i] {
			return false
		}
	}
	return len(pParts) == len(tParts)
}

func splitTopic(t string) []string {
	if t == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(t); i++ {
		if t[i] == '/' {
			parts = append(parts, t[start:i])
			start = i + 1
		}
	}
	parts = append(parts, t[start:])
	return parts
}
