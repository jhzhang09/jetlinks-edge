package externalplugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// ChangeSet 描述一次扫描中需要由 Runner 收敛的插件类型。
type ChangeSet struct {
	DriverTypes        []string `json:"driverTypes"`
	NorthTypes         []string `json:"northTypes"`
	RemovedDriverTypes []string `json:"removedDriverTypes"`
	RemovedNorthTypes  []string `json:"removedNorthTypes"`
}

// PluginInfo 是管理 API 返回的外部插件快照。
type PluginInfo struct {
	Kind       string                   `json:"kind"`
	Manifest   string                   `json:"manifest"`
	Command    string                   `json:"command"`
	Descriptor core.ExtensionDescriptor `json:"descriptor"`
}

// Manager 扫描插件目录，并原子更新南北向注册表。
type Manager struct {
	directory string
	drivers   *core.DriverRegistry
	north     *core.NorthRegistry

	mu       sync.RWMutex
	reloadMu sync.Mutex
	plugins  map[string]Manifest
}

// NewManager 创建外部插件管理器。
func NewManager(directory string, drivers *core.DriverRegistry, north *core.NorthRegistry) *Manager {
	return &Manager{directory: directory, drivers: drivers, north: north, plugins: make(map[string]Manifest)}
}

// Directory 返回当前插件扫描目录。
func (m *Manager) Directory() string { return m.directory }

// Reload 重新扫描清单；新增、替换和移除的工厂立即对新实例生效。
func (m *Manager) Reload() (ChangeSet, error) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	if err := os.MkdirAll(m.directory, 0750); err != nil {
		return ChangeSet{}, err
	}
	paths, err := filepath.Glob(filepath.Join(m.directory, "*.json"))
	if err != nil {
		return ChangeSet{}, err
	}
	next := make(map[string]Manifest, len(paths))
	for _, path := range paths {
		manifest, err := loadManifest(m.directory, path)
		if err != nil {
			return ChangeSet{}, err
		}
		key := manifestKey(manifest.Kind, manifest.Descriptor.Type)
		if _, exists := next[key]; exists {
			return ChangeSet{}, fmt.Errorf("duplicate external plugin type: %s", manifest.Descriptor.Type)
		}
		next[key] = manifest
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for key, manifest := range next {
		if _, owned := m.plugins[key]; owned {
			continue
		}
		if manifest.Kind == KindDriver {
			if _, exists := m.drivers.Descriptor(manifest.Descriptor.Type); exists {
				return ChangeSet{}, fmt.Errorf("external driver conflicts with registered type: %s", manifest.Descriptor.Type)
			}
		} else if _, exists := m.north.Descriptor(manifest.Descriptor.Type); exists {
			return ChangeSet{}, fmt.Errorf("external north plugin conflicts with registered type: %s", manifest.Descriptor.Type)
		}
	}

	changes := diffPlugins(m.plugins, next)
	for key, manifest := range m.plugins {
		if _, exists := next[key]; exists {
			continue
		}
		m.unregister(manifest)
	}
	for key, manifest := range next {
		if previous, exists := m.plugins[key]; exists && manifestsEqual(previous, manifest) {
			continue
		}
		manifest := manifest
		m.register(manifest)
	}
	m.plugins = next
	return changes, nil
}

func (m *Manager) register(manifest Manifest) {
	if manifest.Kind == KindDriver {
		m.drivers.RegisterLifecycleExtension(manifest.Descriptor, func(_ context.Context, name string, config core.DriverConfig) (core.DriverLifecycle, error) {
			return newDriverAdapter(manifest, name, config), nil
		})
		return
	}
	m.north.RegisterMessageExtension(manifest.Descriptor, func(ctx context.Context, appID string, config core.NorthAppConfig) (core.NorthMessageHandler, error) {
		return newNorthAdapter(ctx, manifest, appID, config)
	})
}

func (m *Manager) unregister(manifest Manifest) {
	if manifest.Kind == KindDriver {
		m.drivers.Unregister(manifest.Descriptor.Type)
		return
	}
	m.north.Unregister(manifest.Descriptor.Type)
}

// Watch 监听插件目录变化并在短暂防抖后自动热重载。
func (m *Manager) Watch(ctx context.Context, apply func(ChangeSet) error, onError func(error)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := watcher.Add(m.directory); err != nil {
		_ = watcher.Close()
		return err
	}
	go func() {
		defer func() {
			if err := watcher.Close(); err != nil && onError != nil {
				onError(fmt.Errorf("close plugin watcher: %w", err))
			}
		}()
		var timer *time.Timer
		var timerC <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				if timer != nil {
					timer.Stop()
				}
				return
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				if err != nil && onError != nil {
					onError(err)
				}
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Name == "" {
					continue
				}
				if timer == nil {
					timer = time.NewTimer(500 * time.Millisecond)
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(500 * time.Millisecond)
				}
				timerC = timer.C
			case <-timerC:
				timerC = nil
				changes, err := m.Reload()
				if err == nil && apply != nil {
					err = apply(changes)
				}
				if err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}()
	return nil
}

// List 返回当前已加载的外部插件。
func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]PluginInfo, 0, len(m.plugins))
	for _, manifest := range m.plugins {
		out = append(out, PluginInfo{
			Kind:       manifest.Kind,
			Manifest:   filepath.Base(manifest.manifestPath),
			Command:    manifest.commandPath,
			Descriptor: manifest.Descriptor,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Descriptor.Type < out[j].Descriptor.Type
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func diffPlugins(previous, next map[string]Manifest) ChangeSet {
	changes := ChangeSet{
		DriverTypes:        []string{},
		NorthTypes:         []string{},
		RemovedDriverTypes: []string{},
		RemovedNorthTypes:  []string{},
	}
	for key, manifest := range previous {
		nextManifest, exists := next[key]
		if exists && manifestsEqual(manifest, nextManifest) {
			continue
		}
		if manifest.Kind == KindDriver {
			changes.DriverTypes = append(changes.DriverTypes, manifest.Descriptor.Type)
			if !exists {
				changes.RemovedDriverTypes = append(changes.RemovedDriverTypes, manifest.Descriptor.Type)
			}
		} else {
			changes.NorthTypes = append(changes.NorthTypes, manifest.Descriptor.Type)
			if !exists {
				changes.RemovedNorthTypes = append(changes.RemovedNorthTypes, manifest.Descriptor.Type)
			}
		}
	}
	for key, manifest := range next {
		if old, exists := previous[key]; exists && manifestsEqual(old, manifest) {
			continue
		}
		if manifest.Kind == KindDriver {
			changes.DriverTypes = appendUnique(changes.DriverTypes, manifest.Descriptor.Type)
		} else {
			changes.NorthTypes = appendUnique(changes.NorthTypes, manifest.Descriptor.Type)
		}
	}
	sort.Strings(changes.DriverTypes)
	sort.Strings(changes.NorthTypes)
	sort.Strings(changes.RemovedDriverTypes)
	sort.Strings(changes.RemovedNorthTypes)
	return changes
}

func manifestsEqual(left, right Manifest) bool {
	return left.fingerprint == right.fingerprint
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
