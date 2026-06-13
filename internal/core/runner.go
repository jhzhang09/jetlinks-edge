package core

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	// ErrTagWriteOnly 表示点位不允许读取。
	ErrTagWriteOnly = errors.New("tag is write-only")
	// ErrTagReadOnly 表示点位不允许写入。
	ErrTagReadOnly = errors.New("tag is read-only")
)

// Store 提供点组、点位、用户等数据的持久化访问。
//
// 解耦于 *gorm.DB 的具体实现，便于后续切换到 PostgreSQL/MySQL。
// 约定：
//   - ListEnabledGroups: 列出所有启用的点组
//   - SaveTagValue: 缓存点位最新值（可选落库）
type Store interface {
	ListEnabledGroups(ctx context.Context) ([]*Group, error)
	GetGroup(ctx context.Context, id string) (*Group, error)
	SaveGroup(ctx context.Context, g *Group) error
	DeleteGroup(ctx context.Context, id string) error
	SaveTag(ctx context.Context, t *Tag) error
	DeleteTag(ctx context.Context, id string) error
	ListTagsByGroup(ctx context.Context, groupID string) ([]*Tag, error)
	GetTag(ctx context.Context, id string) (*Tag, error)

	ListNorthApps(ctx context.Context) ([]*NorthApp, error)
	ListEnabledNorthApps(ctx context.Context) ([]*NorthApp, error)
	GetNorthApp(ctx context.Context, id string) (*NorthApp, error)
	SaveNorthApp(ctx context.Context, n *NorthApp) error
	DeleteNorthApp(ctx context.Context, id string) error

	ListEnabledConnections(ctx context.Context) ([]*Connection, error)
	GetConnection(ctx context.Context, id string) (*Connection, error)
	SaveConnection(ctx context.Context, conn *Connection) error
	DeleteConnection(ctx context.Context, id string) error
}

// Runner 是边缘网关运行时核心：
//   - 加载所有 enabled 的点组
//   - 为每个点组创建一个 goroutine，按 interval 周期采集
//   - 把采集结果路由到对应的北向应用
//   - 提供点组/点位的热加载（Reload）
//
// 架构：北向应用（NorthApp）和南向点组（Group）解耦。
//   - 启动时先初始化所有 enabled NorthApp（每个只创建一次）
//   - 多个 Group 可共享同一个 NorthApp（共享 MQTT 连接等资源）
//   - Group.northAppId 变化时只切换引用，不重建采集逻辑
//   - NorthApp 配置变化时只重启该实例，不影响其他 Group
type Runner struct {
	drivers *DriverRegistry
	north   *NorthRegistry
	store   Store

	mu       sync.RWMutex
	groups   map[string]*groupRuntime
	lastVals map[string]map[string]TagValue // groupID -> tagName -> last value

	// Connection 池：connID -> *connectionRuntime（物理通道实例）
	connections map[string]*connectionRuntime

	// NorthApp 池：appID -> *northAppRuntime（共享实例）
	northApps map[string]*northAppRuntime

	// eventBus 进程内消息事件总线
	eventBus *EventBus

	// bgCtx 是 Runner 内部的"长寿命"context，所有点组的 goroutine 都派生自它。
	// 关键：不能用 Web handler 的 request context，否则 handler 返回后点组会被立即 cancel。
	bgCtx    context.Context
	bgCancel context.CancelFunc

	readTimeout    time.Duration
	writeTimeout   time.Duration
	reconnectDelay time.Duration
	collectSem     chan struct{}
	startTime      time.Time
}

// EventBus 返回进程内事件总线实例。
func (r *Runner) EventBus() *EventBus {
	return r.eventBus
}

// RunnerOptions 配置采集调度器的并发和超时边界。
type RunnerOptions struct {
	MaxConcurrency int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	ReconnectDelay time.Duration
}

// northAppRuntime 单个北向应用的运行时。
type northAppRuntime struct {
	app     *NorthApp
	handler NorthHandler
	cancel  context.CancelFunc
}

// connectionRuntime 单个物理连接通道的运行时。
type connectionRuntime struct {
	conn   *Connection
	driver SouthDriver
	cancel context.CancelFunc
	mu     sync.Mutex // 连接级互斥排他锁，防止并发 TCP 乱序或半双工串口冲突
}

// groupRuntime 单个点组的运行时。
type groupRuntime struct {
	group   *Group
	connRt  *connectionRuntime // 关联的物理通道运行时
	norths  []NorthHandler     // 引用 runner.northApps 中的共享实例（可能为空）
	cancel  context.CancelFunc
	unsub   func()         // 进程内 EventBus 取消订阅闭包
	lastRun time.Time
	wg      sync.WaitGroup // 用于等待采集协程完全退出，防止 Reload 时的并发竞态
}

// NewRunner 创建 Runner。
func NewRunner(drivers *DriverRegistry, north *NorthRegistry, store Store, options ...RunnerOptions) *Runner {
	opts := RunnerOptions{
		MaxConcurrency: 100,
		ReadTimeout:    3 * time.Second,
		WriteTimeout:   3 * time.Second,
		ReconnectDelay: 5 * time.Second,
	}
	if len(options) > 0 {
		if options[0].MaxConcurrency > 0 {
			opts.MaxConcurrency = options[0].MaxConcurrency
		}
		if options[0].ReadTimeout > 0 {
			opts.ReadTimeout = options[0].ReadTimeout
		}
		if options[0].WriteTimeout > 0 {
			opts.WriteTimeout = options[0].WriteTimeout
		}
		if options[0].ReconnectDelay > 0 {
			opts.ReconnectDelay = options[0].ReconnectDelay
		}
	}
	return &Runner{
		drivers:        drivers,
		north:          north,
		store:          store,
		groups:         map[string]*groupRuntime{},
		connections:    map[string]*connectionRuntime{},
		lastVals:       map[string]map[string]TagValue{},
		northApps:      map[string]*northAppRuntime{},
		eventBus:       NewEventBus(),
		readTimeout:    opts.ReadTimeout,
		writeTimeout:   opts.WriteTimeout,
		reconnectDelay: opts.ReconnectDelay,
		collectSem:     make(chan struct{}, opts.MaxConcurrency),
		startTime:      time.Now(),
	}
}

// Start 初始化北向应用池 + 初始化物理通道连接 + 加载点组并启动所有协程。
func (r *Runner) Start(ctx context.Context) error {
	bgCtx, cancel := context.WithCancel(context.Background())
	r.bgCtx = bgCtx
	r.bgCancel = cancel

	// 1. 初始化所有启用的北向应用（共享实例）
	if err := r.initNorthApps(bgCtx); err != nil {
		return fmt.Errorf("init north apps: %w", err)
	}

	// 2. 初始化所有启用的物理连接通道
	if err := r.initConnections(bgCtx); err != nil {
		r.Stop()
		return fmt.Errorf("init connections: %w", err)
	}

	// 3. 加载并启动点组
	groups, err := r.store.ListEnabledGroups(ctx)
	if err != nil {
		r.Stop()
		return fmt.Errorf("list groups: %w", err)
	}
	for _, g := range groups {
		if err := r.startGroup(bgCtx, g); err != nil {
			zap.L().Error("start group failed",
				zap.String("groupId", g.ID),
				zap.String("connectionId", g.ConnectionID),
				zap.Error(err))
		}
	}
	zap.L().Info("runner started",
		zap.Int("groups", len(groups)),
		zap.Int("connections", len(r.connections)),
		zap.Int("northApps", len(r.northApps)))
	return nil
}

// Stop 停止所有协程。
func (r *Runner) Stop() {
	if r.bgCancel != nil {
		r.bgCancel()
	}
	r.mu.Lock()
	groups := make([]*groupRuntime, 0, len(r.groups))
	for id, gr := range r.groups {
		groups = append(groups, gr)
		delete(r.groups, id)
	}
	northApps := make([]*northAppRuntime, 0, len(r.northApps))
	for _, app := range r.northApps {
		northApps = append(northApps, app)
	}
	r.northApps = map[string]*northAppRuntime{}

	connections := make([]*connectionRuntime, 0, len(r.connections))
	for id, connRt := range r.connections {
		connections = append(connections, connRt)
		delete(r.connections, id)
	}
	r.mu.Unlock()

	for _, gr := range groups {
		gr.cancel()
		if gr.unsub != nil {
			gr.unsub()
		}
		for _, north := range gr.norths {
			if reg, ok := north.(northRegister); ok {
				reg.DeregisterGroup(gr.group)
			}
		}
	}
	for _, connRt := range connections {
		closeConnection(connRt)
	}
	for _, app := range northApps {
		closeNorthApp(app)
	}
}

// initConnections 启动所有 enabled 的物理通道连接。
func (r *Runner) initConnections(ctx context.Context) error {
	conns, err := r.store.ListEnabledConnections(ctx)
	if err != nil {
		return err
	}
	for _, conn := range conns {
		if err := r.startConnection(ctx, conn); err != nil {
			zap.L().Error("start connection failed",
				zap.String("connectionId", conn.ID),
				zap.String("driver", conn.Driver),
				zap.Error(err))
		}
	}
	return nil
}

// startConnection 实例化底层驱动并开启物理通道连接。
func (r *Runner) startConnection(ctx context.Context, conn *Connection) error {
	conn.UnmarshalConfig()
	connCtx, cancel := context.WithCancel(ctx)

	driver, err := r.drivers.Create(connCtx, conn.Driver, DriverConfig{
		GroupID:        conn.ID, // 使用通道 ID 作为驱动组标识
		Config:         conn.Config,
		ReadTimeout:    r.readTimeout,
		WriteTimeout:   r.writeTimeout,
		ReconnectDelay: r.reconnectDelay,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("create southbound driver for connection %s: %w", conn.ID, err)
	}

	if err := driver.Connect(connCtx); err != nil {
		zap.L().Warn("initial connection connect failed, it will automatically reconnect inside driver",
			zap.String("connectionId", conn.ID),
			zap.Error(err))
	}

	r.mu.Lock()
	old := r.connections[conn.ID]
	r.connections[conn.ID] = &connectionRuntime{
		conn:   conn,
		driver: driver,
		cancel: cancel,
	}
	// 更新当前可能已有的引用该 Connection 的 Group 运行时
	for _, gr := range r.groups {
		if gr.group.ConnectionID == conn.ID {
			gr.connRt = r.connections[conn.ID]
		}
	}
	r.mu.Unlock()

	if old != nil {
		closeConnection(old)
	}

	zap.L().Info("physical connection started",
		zap.String("connectionId", conn.ID),
		zap.String("driver", conn.Driver),
		zap.String("name", conn.Name))
	return nil
}

// closeConnection 关闭物理连接。
func closeConnection(connRt *connectionRuntime) {
	if connRt == nil {
		return
	}
	if connRt.cancel != nil {
		connRt.cancel()
	}
	if connRt.driver != nil {
		_ = connRt.driver.Disconnect()
	}
}

// initNorthApps 启动所有 enabled 的北向应用（每个实例只创建一次）。
func (r *Runner) initNorthApps(ctx context.Context) error {
	apps, err := r.store.ListEnabledNorthApps(ctx)
	if err != nil {
		return err
	}
	for _, app := range apps {
		if err := r.startNorthApp(ctx, app); err != nil {
			zap.L().Error("start north app failed",
				zap.String("northAppId", app.ID),
				zap.String("type", app.Type),
				zap.Error(err))
		}
	}
	return nil
}

// startNorthApp 启动/重新启动单个北向应用实例。
// 已有同 ID 实例会被替换。
func (r *Runner) startNorthApp(ctx context.Context, app *NorthApp) error {
	app.UnmarshalConfig()
	appCtx, cancel := context.WithCancel(ctx)
	handler, err := r.north.Create(appCtx, app.Type, NorthAppConfig{
		AppID:               app.ID,
		Config:              app.Config,
		CommandExecutor:     r.executeNorthCommand,
		GroupStatusProvider: r.groupStatus,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("create north app: %w", err)
	}

	r.mu.Lock()
	old := r.northApps[app.ID]
	groups := make([]*Group, 0)
	r.northApps[app.ID] = &northAppRuntime{
		app:     app,
		handler: handler,
		cancel:  cancel,
	}
	for _, gr := range r.groups {
		if HasNorthAppID(gr.group.NorthAppID, app.ID) {
			groups = append(groups, gr.group)
			gr.norths = r.lookupNorthsLocked(gr.group.NorthAppID)
		}
	}
	r.mu.Unlock()

	if old != nil {
		for _, group := range groups {
			if reg, ok := old.handler.(northRegister); ok {
				reg.DeregisterGroup(group)
			}
		}
		closeNorthApp(old)
	}
	for _, group := range groups {
		if reg, ok := handler.(northRegister); ok {
			reg.RegisterGroup(group)
		}
	}
	zap.L().Info("north app started",
		zap.String("northAppId", app.ID),
		zap.String("type", app.Type),
		zap.String("name", app.Name))
	return nil
}

// ReloadNorthApp 重新加载单个北向应用（用于 API 触发）。
// 同时刷新所有引用此 NorthApp 的 Group 的引用（实例已替换）。
func (r *Runner) ReloadNorthApp(ctx context.Context, appID string) error {
	if r.bgCancel == nil {
		return fmt.Errorf("runner not started")
	}
	app, err := r.store.GetNorthApp(ctx, appID)
	if err != nil {
		return err
	}
	if app == nil {
		return r.deleteNorthApp(appID, true)
	}
	if !app.Enabled {
		return r.deleteNorthApp(appID, false)
	}
	// 重建实例
	if err := r.startNorthApp(context.Background(), app); err != nil {
		return err
	}
	return nil
}

// deleteNorthApp 停止并删除单个北向应用实例。
func (r *Runner) deleteNorthApp(appID string, detach bool) error {
	r.mu.Lock()
	old := r.northApps[appID]
	groups := make([]*Group, 0)
	delete(r.northApps, appID)
	for _, gr := range r.groups {
		if HasNorthAppID(gr.group.NorthAppID, appID) {
			groups = append(groups, gr.group)
			if detach {
				gr.group.NorthAppID = RemoveNorthAppID(gr.group.NorthAppID, appID)
			}
			gr.norths = r.lookupNorthsLocked(gr.group.NorthAppID)
		}
	}
	r.mu.Unlock()
	if old != nil {
		for _, group := range groups {
			if reg, ok := old.handler.(northRegister); ok {
				reg.DeregisterGroup(group)
			}
		}
		closeNorthApp(old)
	}
	return nil
}

func closeNorthApp(app *northAppRuntime) {
	if app == nil {
		return
	}
	if app.cancel != nil {
		app.cancel()
	}
	if lifecycle, ok := app.handler.(NorthLifecycle); ok {
		_ = lifecycle.Close()
	}
}

// lookupNorthLocked 内部：从池里查实例（不加锁，调用方需持锁）。
// 注意：返回的 handler 可能为 nil（应用被停用/不存在）。
func (r *Runner) lookupNorthLocked(appID string) NorthHandler {
	if appID == "" {
		return nil
	}
	if nr, ok := r.northApps[appID]; ok {
		return nr.handler
	}
	return nil
}

// lookupNorthsLocked 内部：从池里查一组实例，解析逗号分隔的北向应用 ID 列表（不加锁，调用方需持锁）。
func (r *Runner) lookupNorthsLocked(appIDs string) []NorthHandler {
	if appIDs == "" {
		return nil
	}
	var res []NorthHandler
	parts := strings.Split(appIDs, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if nr, ok := r.northApps[part]; ok && nr.handler != nil {
			res = append(res, nr.handler)
		}
	}
	return res
}

// Reload 重新加载某个点组（用于 Web API 触发的热更新）。
//
// 优化：当 productId/deviceId/northAppID 没有变化时，复用旧 group 在 north 的订阅
// （避免 reload 时不必要的 Unsubscribe → Subscribe 往返）。
func (r *Runner) Reload(ctx context.Context, groupID string) error {
	r.mu.Lock()
	old, hadOld := r.groups[groupID]
	if hadOld {
		old.cancel()
		if old.unsub != nil {
			old.unsub()
		}
	}
	r.mu.Unlock()

	if hadOld {
		// 释放全局锁之后，阻塞等待旧协程安全退出，以防并发读写底层物理接口
		old.wg.Wait()
	}

	g, err := r.store.GetGroup(ctx, groupID)
	if err != nil {
		return err
	}
	if g == nil {
		// 已删除：注销旧 group 的订阅
		if hadOld && len(old.norths) > 0 {
			for _, north := range old.norths {
				if dereg, ok := north.(northRegister); ok {
					dereg.DeregisterGroup(old.group)
				}
			}
		}
		if hadOld {
			r.mu.Lock()
			delete(r.groups, groupID)
			r.mu.Unlock()
		}
		return nil
	}
	if !g.Enabled {
		// 禁用：注销旧 group 的订阅
		if hadOld && len(old.norths) > 0 {
			for _, north := range old.norths {
				if dereg, ok := north.(northRegister); ok {
					dereg.DeregisterGroup(old.group)
				}
			}
		}
		if hadOld {
			r.mu.Lock()
			delete(r.groups, groupID)
			r.mu.Unlock()
		}
		return nil
	}
	if r.bgCancel == nil {
		return fmt.Errorf("runner not started")
	}

	// 优化：设备身份/北向引用没变 → 复用订阅
	if hadOld && len(old.norths) > 0 &&
		old.group.NorthAppID == g.NorthAppID &&
		old.group.Device.ProductID == g.Device.ProductID &&
		old.group.Device.DeviceID == g.Device.DeviceID {
		// 不注销
	} else {
		// 设备身份/北向应用变了：注销旧的
		if hadOld && len(old.norths) > 0 {
			for _, north := range old.norths {
				if dereg, ok := north.(northRegister); ok {
					dereg.DeregisterGroup(old.group)
				}
			}
		}
	}

	if hadOld {
		r.mu.Lock()
		delete(r.groups, groupID)
		delete(r.lastVals, groupID)
		r.mu.Unlock()
	}
	return r.startGroup(r.bgCtx, g)
}

// ReloadConnection 重新加载单个物理通道连接（用于 Web API 触发的热更新）。
func (r *Runner) ReloadConnection(ctx context.Context, connID string) error {
	if r.bgCancel == nil {
		return fmt.Errorf("runner not started")
	}
	conn, err := r.store.GetConnection(ctx, connID)
	if err != nil {
		return err
	}

	r.mu.Lock()
	old, exists := r.connections[connID]
	r.mu.Unlock()

	if conn == nil || !conn.Enabled {
		// 禁用或删除物理通道
		if exists {
			r.mu.Lock()
			delete(r.connections, connID)
			// 级联断开所有该通道下的 Group 引用
			for _, gr := range r.groups {
				if gr.group.ConnectionID == connID {
					gr.connRt = nil
				}
			}
			r.mu.Unlock()
			closeConnection(old)
		}
		return nil
	}

	// 重建物理通道连接
	return r.startConnection(r.bgCtx, conn)
}

// northRegister 可选接口：NorthApp 支持 RegisterGroup/DeregisterGroup 即可被多 Group 共享。
// 不实现该接口的 NorthApp 仍可工作（每个 group 独立管理订阅）。
type northRegister interface {
	RegisterGroup(g *Group) bool
	DeregisterGroup(g *Group)
}

// startGroup 内部：寻找关联物理通道、引用共享北向应用、注册到 north（用于下行消息路由）、启动采集 goroutine。
func (r *Runner) startGroup(ctx context.Context, g *Group) error {
	g.UnmarshalConfig()
	g.Interval = time.Duration(g.IntervalMs) * time.Millisecond
	if g.Interval <= 0 {
		g.Interval = time.Second
	}

	r.mu.RLock()
	connRt, connExists := r.connections[g.ConnectionID]
	r.mu.RUnlock()
	if !connExists || connRt == nil {
		return fmt.Errorf("group %s references a non-running connection: %s", g.ID, g.ConnectionID)
	}

	// 加载点位
	tags, err := r.store.ListTagsByGroup(ctx, g.ID)
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}
	g.Tags = toTagSlice(tags)

	grpCtx, cancel := context.WithCancel(ctx)
	gr := &groupRuntime{
		group:  g,
		connRt: connRt,
		cancel: cancel,
	}

	// 引用共享北向应用（如有）
	r.mu.Lock()
	gr.norths = r.lookupNorthsLocked(g.NorthAppID)
	r.mu.Unlock()

	// 注册 group 到 north（用于下行消息路由 + 按需订阅设备主题）
	if len(gr.norths) > 0 {
		for _, north := range gr.norths {
			if reg, ok := north.(northRegister); ok {
				if !reg.RegisterGroup(g) {
					zap.L().Warn("group register to north failed (missing productId/deviceId)",
						zap.String("groupId", g.ID))
				}
			}
		}
	}

	// 进程内事件桥接：当 EventBus 产生本 Group 采集的数据时，自动推送给关联的北向 Handler。
	if len(gr.norths) > 0 {
		topic := fmt.Sprintf("south/connection/%s/group/%s/values", g.ConnectionID, g.ID)
		ch, unsubscribe := r.eventBus.Subscribe(topic, 100)
		gr.unsub = unsubscribe

		gr.wg.Add(1)
		go func() {
			defer gr.wg.Done()
			for {
				select {
				case <-grpCtx.Done():
					return
				case ev, ok := <-ch:
					if !ok {
						return
					}
					if len(gr.norths) > 0 {
						if msg, ok := ev.Payload.(NorthMessage); ok {
							for _, north := range gr.norths {
								if err := north.OnMessage(grpCtx, msg); err != nil {
									zap.L().Warn("bridge north OnMessage failed",
										zap.String("groupId", g.ID),
										zap.Error(err))
								}
							}
						}
					}
				}
			}
		}()
	}

	gr.wg.Add(1)
	r.mu.Lock()
	r.groups[g.ID] = gr
	r.mu.Unlock()

	go func() {
		defer gr.wg.Done()
		r.runGroup(grpCtx, gr)
	}()
	return nil
}

// runGroup 单个点组的采集循环。
func (r *Runner) runGroup(ctx context.Context, gr *groupRuntime) {
	g := gr.group
	zap.L().Info("group collection started",
		zap.String("groupId", g.ID),
		zap.String("connectionId", g.ConnectionID),
		zap.Duration("interval", g.Interval))

	// 错峰启动，避免所有点组同时打设备
	jitter := time.Duration(rand.Int63n(int64(g.Interval / 4)))
	timer := time.NewTimer(jitter)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			zap.L().Info("group collection stopped", zap.String("groupId", g.ID))
			return
		case <-timer.C:
			r.collectOnce(ctx, gr)
			timer.Reset(g.Interval)
		}
	}
}

// collectOnce 单次采集。
func (r *Runner) collectOnce(ctx context.Context, gr *groupRuntime) {
	zap.L().Debug("collectOnce entered", zap.String("groupId", gr.group.ID), zap.Int("tags", len(gr.group.Tags)))
	g := gr.group
	connRt := gr.connRt
	if connRt == nil {
		zap.L().Warn("group collect failed: physical connection runtime is nil", zap.String("groupId", g.ID))
		return
	}

	if len(g.Tags) == 0 {
		connRt.mu.Lock()
		status := connRt.driver.Status()
		if !status.Connected {
			connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
			err := connRt.driver.Connect(connCtx)
			connCancel()
			if err == nil {
				zap.L().Info("driver connected on tags-empty retry", zap.String("connectionId", connRt.conn.ID))
			}
		}
		connRt.mu.Unlock()
		return
	}

	select {
	case r.collectSem <- struct{}{}:
		defer func() { <-r.collectSem }()
	case <-ctx.Done():
		return
	}

	// 合并注入逻辑组配置到点位中（如 Modbus unitId）
	for i := range g.Tags {
		if g.Tags[i].Config == nil {
			g.Tags[i].Config = map[string]interface{}{}
		}
		for k, v := range g.Config {
			if _, exists := g.Tags[i].Config[k]; !exists {
				g.Tags[i].Config[k] = v
			}
		}
	}

	readCtx, cancel := context.WithTimeout(ctx, r.readTimeout)
	
	// 在物理通道排他锁保护下读取
	connRt.mu.Lock()
	values, err := connRt.driver.ReadTags(readCtx, g.Tags)
	connRt.mu.Unlock()
	
	cancel()
	if len(values) > len(g.Tags) {
		values = values[:len(g.Tags)]
	}
	for len(values) < len(g.Tags) {
		tag := g.Tags[len(values)]
		values = append(values, TagValue{
			TagID:   tag.ID,
			Name:    tag.Name,
			Quality: QualityBad,
			Error:   "driver returned incomplete result",
		})
	}

	now := time.Now()
	changes := make([]TagValue, 0, len(values))
	for i, v := range values {
		// 注入时间与点位 ID
		if v.TagID == "" {
			v.TagID = g.Tags[i].ID
		}
		if v.Name == "" {
			v.Name = g.Tags[i].Name
		}
		v.Time = now
		if err != nil {
			v.Quality = QualityBad
			v.Error = err.Error()
		} else if v.Quality == "" {
			v.Quality = QualityGood
		}
		values[i] = v
		// 比较旧值后再更新缓存，保证变化检测使用的是上一次采集结果。
		if r.updateCachedValue(g.ID, v) {
			changes = append(changes, v)
		}
	}

	gr.lastRun = now

	reportValues := reportableValues(values)
	if len(reportValues) == 0 {
		zap.L().Debug("skip north property report because no good quality tag value",
			zap.String("groupId", g.ID))
		return
	}
	reportChanges := reportableValues(changes)

	// 将数据统一发布到进程内 EventBus
	topic := fmt.Sprintf("south/connection/%s/group/%s/values", g.ConnectionID, g.ID)
	r.eventBus.Publish(topic, Event{
		Topic:     topic,
		Type:      "values",
		Timestamp: now,
		Payload: NorthMessage{
			GroupID:   g.ID,
			ProductID: g.Device.ProductID,
			DeviceID:  g.Device.DeviceID,
			Type:      "property-report",
			Timestamp: now,
			Payload: map[string]interface{}{
				"properties": valuesToMap(reportValues),
				"changes":    valuesToMap(reportChanges),
			},
		},
	})
}

func (r *Runner) executeNorthCommand(ctx context.Context, cmd NorthCommand) (NorthCommandReply, error) {
	r.mu.RLock()
	var target *groupRuntime
	if cmd.GroupID != "" {
		target = r.groups[cmd.GroupID]
	} else {
		for _, gr := range r.groups {
			if gr.group.Device.DeviceID == cmd.DeviceID &&
				(cmd.ProductID == "" || gr.group.Device.ProductID == cmd.ProductID) {
				target = gr
				break
			}
		}
	}
	r.mu.RUnlock()
	if target == nil {
		return NorthCommandReply{ID: cmd.ID, Code: 404, Message: "device group not running"}, nil
	}
	cmd.GroupID = target.group.ID
	cmd.ProductID = target.group.Device.ProductID

	switch cmd.Type {
	case "read-property":
		readCtx, cancel := context.WithTimeout(ctx, r.readTimeout)
		defer cancel()
		names := toStringSlice(cmd.Payload["properties"])
		properties := make(map[string]interface{}, len(names))
		for _, name := range names {
			tag, ok := findTagByName(target.group.Tags, name)
			if !ok {
				return NorthCommandReply{ID: cmd.ID, Code: 404, Message: "tag not found: " + name}, nil
			}
			value, err := r.ReadTag(readCtx, target.group.ID, tag.ID)
			if err != nil {
				return NorthCommandReply{ID: cmd.ID, Code: 500, Message: err.Error()}, nil
			}
			if value.Quality != QualityGood {
				message := value.Error
				if message == "" {
					message = "tag quality is not good: " + string(value.Quality)
				}
				return NorthCommandReply{ID: cmd.ID, Code: 500, Message: message}, nil
			}
			properties[name] = value.Value
		}
		return NorthCommandReply{ID: cmd.ID, Code: 0, Message: "success", Payload: properties}, nil
	case "write-property":
		writeCtx, cancel := context.WithTimeout(ctx, r.writeTimeout)
		defer cancel()
		properties, ok := cmd.Payload["properties"].(map[string]interface{})
		if !ok {
			return NorthCommandReply{ID: cmd.ID, Code: 400, Message: "invalid properties"}, nil
		}
		for name, value := range properties {
			tag, exists := findTagByName(target.group.Tags, name)
			if !exists {
				return NorthCommandReply{ID: cmd.ID, Code: 404, Message: "tag not found: " + name}, nil
			}
			if err := r.WriteTag(writeCtx, target.group.ID, tag.ID, value); err != nil {
				return NorthCommandReply{ID: cmd.ID, Code: 500, Message: err.Error()}, nil
			}
		}
		return NorthCommandReply{ID: cmd.ID, Code: 0, Message: "success", Payload: properties}, nil
	default:
		return NorthCommandReply{ID: cmd.ID, Code: 400, Message: "unsupported command: " + cmd.Type}, nil
	}
}

func findTagByName(tags []Tag, name string) (Tag, bool) {
	for _, tag := range tags {
		if tag.Name == name {
			return tag, true
		}
	}
	return Tag{}, false
}

func toStringSlice(value interface{}) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []interface{}:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// ReadTag 主动读单个点位（供 Web API 调用）。
func (r *Runner) ReadTag(ctx context.Context, groupID, tagID string) (TagValue, error) {
	r.mu.RLock()
	gr, ok := r.groups[groupID]
	r.mu.RUnlock()
	if !ok {
		return TagValue{}, errors.New("group not running")
	}
	connRt := gr.connRt
	if connRt == nil {
		return TagValue{}, errors.New("connection not running")
	}

	for _, t := range gr.group.Tags {
		if t.ID == tagID {
			if t.Access == AccessWO {
				return TagValue{}, ErrTagWriteOnly
			}

			// 注入逻辑组配置到点位中
			if t.Config == nil {
				t.Config = map[string]interface{}{}
			}
			for k, v := range gr.group.Config {
				if _, exists := t.Config[k]; !exists {
					t.Config[k] = v
				}
			}

			connRt.mu.Lock()
			values, err := connRt.driver.ReadTags(ctx, []Tag{t})
			connRt.mu.Unlock()

			if err != nil {
				return TagValue{Quality: QualityBad, Error: err.Error()}, err
			}
			if len(values) == 0 {
				return TagValue{Quality: QualityBad, Error: "empty result"}, errors.New("empty result")
			}
			values[0].TagID = t.ID
			values[0].Name = t.Name
			r.cacheValue(groupID, values[0])
			return values[0], nil
		}
	}
	return TagValue{}, errors.New("tag not found")
}

// WriteTag 主动写单个点位（供 Web API 或北向指令调用）。
func (r *Runner) WriteTag(ctx context.Context, groupID, tagID string, value interface{}) error {
	r.mu.RLock()
	gr, ok := r.groups[groupID]
	r.mu.RUnlock()
	if !ok {
		return errors.New("group not running")
	}
	connRt := gr.connRt
	if connRt == nil {
		return errors.New("connection not running")
	}

	for _, t := range gr.group.Tags {
		if t.ID == tagID {
			if t.Access == AccessRO {
				return ErrTagReadOnly
			}

			// 注入逻辑组配置到点位中
			if t.Config == nil {
				t.Config = map[string]interface{}{}
			}
			for k, v := range gr.group.Config {
				if _, exists := t.Config[k]; !exists {
					t.Config[k] = v
				}
			}

			connRt.mu.Lock()
			err := connRt.driver.WriteTag(ctx, t, value)
			connRt.mu.Unlock()

			if err != nil {
				return err
			}
			return nil
		}
	}
	return errors.New("tag not found")
}

// Status 返回所有点组的运行状态。
func (r *Runner) Status() map[string]DriverStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]DriverStatus{}
	for id, gr := range r.groups {
		if gr.connRt != nil && gr.connRt.driver != nil {
			out[id] = gr.connRt.driver.Status()
		}
	}
	return out
}

// ConnectionStatus 返回所有物理通道的运行状态。
func (r *Runner) ConnectionStatus() map[string]DriverStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string]DriverStatus{}
	for id, connRt := range r.connections {
		if connRt.driver != nil {
			out[id] = connRt.driver.Status()
		}
	}
	return out
}

// groupStatus 返回单个点组对应南向驱动的实时状态。
func (r *Runner) groupStatus(groupID string) (DriverStatus, bool) {
	r.mu.RLock()
	gr := r.groups[groupID]
	r.mu.RUnlock()
	if gr == nil || gr.connRt == nil || gr.connRt.driver == nil {
		return DriverStatus{}, false
	}
	return gr.connRt.driver.Status(), true
}

// BrowseOPCUA 浏览指定 OPC UA 点组的节点树。
func (r *Runner) BrowseOPCUA(ctx context.Context, groupID string, nodeId string) ([]NodeItem, error) {
	r.mu.RLock()
	gr := r.groups[groupID]
	r.mu.RUnlock()
	if gr == nil {
		return nil, errors.New("device group not running")
	}
	if gr.connRt == nil || gr.connRt.driver == nil {
		return nil, errors.New("connection not running")
	}
	browser, ok := gr.connRt.driver.(NodeBrowser)
	if !ok {
		return nil, fmt.Errorf("driver %s does not support browse", gr.connRt.conn.Driver)
	}
	return browser.Browse(ctx, nodeId)
}

// Drivers 返回所有已注册南向驱动名。
func (r *Runner) Drivers() []string { return r.drivers.Names() }

// DriverDescriptors 返回全部编译期注册的南向插件描述符。
func (r *Runner) DriverDescriptors() []ExtensionDescriptor { return r.drivers.Descriptors() }

// NorthDescriptors 返回全部编译期注册的北向插件描述符。
func (r *Runner) NorthDescriptors() []ExtensionDescriptor { return r.north.Descriptors() }

// ValidateDriverConfig 校验南向驱动连接配置。
func (r *Runner) ValidateDriverConfig(name string, config map[string]interface{}) error {
	descriptor, ok := r.drivers.Descriptor(name)
	if !ok {
		return fmt.Errorf("driver not registered: %s", name)
	}
	return ValidateConfig(descriptor.ConnectionSchema, config)
}

// DefaultDriverConfig 使用南向描述符默认值补齐连接配置。
func (r *Runner) DefaultDriverConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	descriptor, ok := r.drivers.Descriptor(name)
	if !ok {
		return nil, fmt.Errorf("driver not registered: %s", name)
	}
	return ApplyConfigDefaults(descriptor.ConnectionSchema, config), nil
}

// ValidateTagConfig 校验指定南向驱动的点位私有配置。
func (r *Runner) ValidateTagConfig(name string, config map[string]interface{}) error {
	descriptor, ok := r.drivers.Descriptor(name)
	if !ok {
		return fmt.Errorf("driver not registered: %s", name)
	}
	return ValidateConfig(descriptor.TagSchema, config)
}

// DefaultTagConfig 使用指定南向描述符默认值补齐点位私有配置。
func (r *Runner) DefaultTagConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	descriptor, ok := r.drivers.Descriptor(name)
	if !ok {
		return nil, fmt.Errorf("driver not registered: %s", name)
	}
	return ApplyConfigDefaults(descriptor.TagSchema, config), nil
}

// ValidateNorthConfig 校验北向应用配置。
func (r *Runner) ValidateNorthConfig(name string, config map[string]interface{}) error {
	descriptor, ok := r.north.Descriptor(name)
	if !ok {
		return fmt.Errorf("north app not registered: %s", name)
	}
	return ValidateConfig(descriptor.ConfigSchema, config)
}

// DefaultNorthConfig 使用北向描述符默认值补齐应用配置。
func (r *Runner) DefaultNorthConfig(name string, config map[string]interface{}) (map[string]interface{}, error) {
	descriptor, ok := r.north.Descriptor(name)
	if !ok {
		return nil, fmt.Errorf("north app not registered: %s", name)
	}
	return ApplyConfigDefaults(descriptor.ConfigSchema, config), nil
}

// Store 返回 Runner 使用的 Store（供 Web handler 直接操作 NorthApp）。
func (r *Runner) Store() Store { return r.store }

// NorthApps 返回所有已注册北向应用类型（注册表里的）。
func (r *Runner) NorthApps() []string { return r.north.Names() }

// NorthAppStatus 返回所有运行中的北向应用（实例 ID + 名称）。
type NorthAppStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	Connected bool   `json:"connected"` // MQTT 连接是否真实可用
	LastError string `json:"lastError,omitempty"`
}

// ListNorthAppStatus 返回北向应用运行状态列表。
func (r *Runner) ListNorthAppStatus(ctx context.Context) ([]NorthAppStatus, error) {
	apps, err := r.store.ListNorthApps(ctx)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NorthAppStatus, 0, len(apps))
	for _, a := range apps {
		nr, running := r.northApps[a.ID]
		st := NorthAppStatus{
			ID:      a.ID,
			Name:    a.Name,
			Type:    a.Type,
			Enabled: a.Enabled,
			Running: running && nr != nil && nr.handler != nil,
		}
		if st.Running {
			if stater, ok := nr.handler.(NorthStateReporter); ok {
				s := stater.State()
				if s != nil {
					st.Connected = s.Connected
					st.LastError = s.LastError
				}
			}
		}
		out = append(out, st)
	}
	return out, nil
}

// GroupNames 返回当前在运行的所有点组 ID。
func (r *Runner) GroupNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.groups))
	for id := range r.groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// GroupInfo 返回在运行的点组 ID 与名称。
type GroupInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GroupInfoList 返回当前所有运行中的点组 ID + 名称。
func (r *Runner) GroupInfoList() []GroupInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]GroupInfo, 0, len(r.groups))
	for _, gr := range r.groups {
		out = append(out, GroupInfo{ID: gr.group.ID, Name: gr.group.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LastValues 返回某个点组的最近一次采集值。
func (r *Runner) LastValues(groupID string) map[string]TagValue {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.lastVals[groupID]
	if !ok {
		return map[string]TagValue{}
	}
	out := make(map[string]TagValue, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (r *Runner) cacheValue(groupID string, v TagValue) {
	r.updateCachedValue(groupID, v)
}

func (r *Runner) updateCachedValue(groupID string, v TagValue) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastVals[groupID] == nil {
		r.lastVals[groupID] = map[string]TagValue{}
	}
	old, exists := r.lastVals[groupID][v.TagID]
	r.lastVals[groupID][v.TagID] = v
	return !exists || !equalValue(old.Value, v.Value)
}

func equalValue(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func valuesToMap(values []TagValue) map[string]interface{} {
	out := map[string]interface{}{}
	for _, v := range values {
		out[v.Name] = v.Value
	}
	return out
}

func reportableValues(values []TagValue) []TagValue {
	out := make([]TagValue, 0, len(values))
	for _, v := range values {
		if v.Quality == QualityGood {
			out = append(out, v)
		}
	}
	return out
}

func toTagSlice(tags []*Tag) []Tag {
	out := make([]Tag, len(tags))
	for i, t := range tags {
		out[i] = *t
	}
	return out
}

// StartTime 返回边缘网关服务启动时间。
func (r *Runner) StartTime() time.Time {
	return r.startTime
}
