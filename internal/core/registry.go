package core

import (
	"context"
	"fmt"
	"sync"
)

// DriverFactory 是创建 SouthDriver 实例的工厂函数。
//   - name: 驱动名（注册时使用的 key）
//   - cfg:  包含 GroupID 与 Config 字段
type DriverFactory func(ctx context.Context, name string, cfg DriverConfig) (SouthDriver, error)

// DriverLifecycleFactory 创建只要求最小生命周期的南向驱动实例。
type DriverLifecycleFactory func(ctx context.Context, name string, cfg DriverConfig) (DriverLifecycle, error)

// DriverRegistry 南向驱动工厂注册表。
//
// 通过 Register(name, factory) 注册工厂；通过 Create() 创建实例。
// 这是一个全局单例（进程内），由 main.go 在启动时注册所有内置驱动。
type DriverRegistry struct {
	mu          sync.RWMutex
	factories   map[string]DriverLifecycleFactory
	descriptors map[string]ExtensionDescriptor
}

// NewDriverRegistry 创建空注册表。
func NewDriverRegistry() *DriverRegistry {
	return &DriverRegistry{
		factories:   map[string]DriverLifecycleFactory{},
		descriptors: map[string]ExtensionDescriptor{},
	}
}

// Register 注册一个驱动工厂。
// name 必须全局唯一，重复注册会覆盖（仅用于测试）。
func (r *DriverRegistry) Register(name string, factory DriverFactory) {
	r.RegisterExtension(ExtensionDescriptor{Type: name, Name: name, Version: "unknown"}, factory)
}

// RegisterExtension 注册带元数据描述的南向驱动工厂。
func (r *DriverRegistry) RegisterExtension(descriptor ExtensionDescriptor, factory DriverFactory) {
	r.RegisterLifecycleExtension(descriptor, func(ctx context.Context, name string, cfg DriverConfig) (DriverLifecycle, error) {
		return factory(ctx, name, cfg)
	})
}

// RegisterLifecycleExtension 注册只要求最小生命周期的南向插件。
// 轮询读取、点位写入、节点浏览和功能调用由 Runner 在运行时按可选接口探测。
func (r *DriverRegistry) RegisterLifecycleExtension(descriptor ExtensionDescriptor, factory DriverLifecycleFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if descriptor.Runtime == "" {
		descriptor.Runtime = "builtin"
	}
	r.factories[descriptor.Type] = factory
	r.descriptors[descriptor.Type] = descriptor
}

// Unregister 删除指定类型的驱动工厂。已创建实例不受影响，由 Runner 负责生命周期收敛。
func (r *DriverRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.factories, name)
	delete(r.descriptors, name)
}

// Names 返回所有已注册驱动名。
func (r *DriverRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for n := range r.factories {
		out = append(out, n)
	}
	return out
}

// Descriptors 返回所有已注册南向驱动描述符。
func (r *DriverRegistry) Descriptors() []ExtensionDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ExtensionDescriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		out = append(out, descriptor)
	}
	sortDescriptors(out)
	return out
}

// Descriptor 返回指定南向驱动描述符。
func (r *DriverRegistry) Descriptor(name string) (ExtensionDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.descriptors[name]
	return descriptor, ok
}

// Has 判断驱动是否存在。
func (r *DriverRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// Create 通过工厂创建一个驱动实例。
func (r *DriverRegistry) Create(ctx context.Context, name string, cfg DriverConfig) (SouthDriver, error) {
	driver, err := r.CreateLifecycle(ctx, name, cfg)
	if err != nil {
		return nil, err
	}
	legacyDriver, ok := driver.(SouthDriver)
	if !ok {
		return nil, fmt.Errorf("driver %s does not implement legacy polling capabilities", name)
	}
	return legacyDriver, nil
}

// CreateLifecycle 创建南向驱动实例，不强制要求轮询读取和点位写入能力。
func (r *DriverRegistry) CreateLifecycle(ctx context.Context, name string, cfg DriverConfig) (DriverLifecycle, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("driver not registered: %s", name)
	}
	return factory(ctx, name, cfg)
}
