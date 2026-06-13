package core

import (
	"context"
	"fmt"
	"sync"
)

// NorthAppFactory 创建 NorthHandler 的工厂。
//   - appID: 北向应用 ID（注册时使用的 key）
type NorthAppFactory func(ctx context.Context, appID string, cfg NorthAppConfig) (NorthHandler, error)

// GroupStatusProvider 按点组 ID 查询南向驱动的实时状态。
//
// 北向应用只读取状态用于上送或展示，不拥有南向驱动生命周期。
type GroupStatusProvider func(groupID string) (DriverStatus, bool)

// NorthAppConfig 北向应用实例化配置。
type NorthAppConfig struct {
	AppID               string
	Config              map[string]interface{}
	CommandExecutor     NorthCommandExecutor
	GroupStatusProvider GroupStatusProvider
}

// NorthRegistry 北向应用注册表。
type NorthRegistry struct {
	mu          sync.RWMutex
	factories   map[string]NorthAppFactory
	descriptors map[string]ExtensionDescriptor
}

// NewNorthRegistry 创建空注册表。
func NewNorthRegistry() *NorthRegistry {
	return &NorthRegistry{
		factories:   map[string]NorthAppFactory{},
		descriptors: map[string]ExtensionDescriptor{},
	}
}

// Register 注册一个北向工厂。
func (r *NorthRegistry) Register(name string, factory NorthAppFactory) {
	r.RegisterExtension(ExtensionDescriptor{Type: name, Name: name, Version: "unknown"}, factory)
}

// RegisterExtension 注册带元数据描述的北向应用工厂。
func (r *NorthRegistry) RegisterExtension(descriptor ExtensionDescriptor, factory NorthAppFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[descriptor.Type] = factory
	r.descriptors[descriptor.Type] = descriptor
}

// Names 返回所有已注册北向应用名。
func (r *NorthRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for n := range r.factories {
		out = append(out, n)
	}
	return out
}

// Descriptors 返回所有已注册北向应用描述符。
func (r *NorthRegistry) Descriptors() []ExtensionDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ExtensionDescriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		out = append(out, descriptor)
	}
	sortDescriptors(out)
	return out
}

// Descriptor 返回指定北向应用描述符。
func (r *NorthRegistry) Descriptor(name string) (ExtensionDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.descriptors[name]
	return descriptor, ok
}

// Create 通过工厂创建一个北向应用实例。
func (r *NorthRegistry) Create(ctx context.Context, name string, cfg NorthAppConfig) (NorthHandler, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("north app not registered: %s", name)
	}
	return factory(ctx, cfg.AppID, cfg)
}
