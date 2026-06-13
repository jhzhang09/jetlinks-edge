// Package jetlinksmqtt 实现把边缘网关的数据上送到 JetLinks 物联网平台（私有 MQTT 协议）。
//
// 接入模型：**JetLinks 网关 + 子设备**（按官方协议 jetlinks-official-protocol V1.3.1）
//
// 我们把整个 JetLinks Edge 当作一个"网关设备"接入 JetLinks 平台。
//   - 1 个 NorthApp = 1 个 JetLinks 网关 = 1 个 MQTT 连接
//   - 多个 Group = 多个子设备 = 共享同一条 MQTT 连接
//
// JetLinks 平台 MQTT 认证规范：
//   - clientId = 平台设备实例 ID（网关的 deviceId）
//   - username = secureId + "|" + timestamp
//   - password = SM3(secureId + "|" + timestamp + "|" + secureKey)
//   - timestamp 为当前系统时间戳（毫秒），与平台时间差 < 5 分钟
//
// 主题格式（按官方协议 V1.3.1，**不是**内部 EventBus 的 /device/...）：
//
//	上行（网关→平台）：
//	  - 子设备属性上报：/{gwPid}/{gwDid}/child/{childDid}/properties/report
//	  - 子设备事件上报：/{gwPid}/{gwDid}/child/{childDid}/event/{eventId}
//	  - 子设备注册：    /{gwPid}/{gwDid}/child/{childDid}/register
//	  - 子设备上线：    /{gwPid}/{gwDid}/child/{childDid}/online
//	  - 子设备下线：    /{gwPid}/{gwDid}/child/{childDid}/offline
//	下行（平台→网关）：
//	  - 读子设备属性：  /{gwPid}/{gwDid}/child/{childDid}/properties/read
//	  - 写子设备属性：  /{gwPid}/{gwDid}/child/{childDid}/properties/write
//	  - 调用子设备功能：/{gwPid}/{gwDid}/child/{childDid}/function/invoke
package jetlinksmqtt

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	gmsm "github.com/piligo/gmsm/sm3"
	"go.uber.org/zap"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// DriverName 北向应用类型名。
const DriverName = "jetlinks-mqtt"

// AppConfig 北向应用配置（v0.4 - 符合 JetLinks 平台 MQTT 认证规范）。
//
// 字段说明：
//   - Broker:      broker 地址，例如 tcp://127.0.0.1:1883
//   - ProductID:   网关在 JetLinks 平台所属的 productId（用于 register 主题与子设备归属）
//   - DeviceID:    网关在 JetLinks 平台的 deviceId（**作为 MQTT clientId**）
//   - SecureID:    网关的 secureId（来自 JetLinks 平台）
//   - SecureKey:   网关的 secureKey（来自 JetLinks 平台）
//   - Username/Password: 留空，**程序运行时按 JetLinks 规范动态计算**：
//     username = SecureID + "|" + timestamp
//     password = SM3(SecureID + "|" + timestamp + "|" + SecureKey)
//   - TimestampDelta: timestamp 与平台时间的容差（秒），默认 300 (5 分钟)
//
// 重要：ProductID 是**网关**自己的产品 ID（在 JetLinks 平台的产品列表里），
// 不是子设备的 productId（子设备的在 Group.DeviceConfig 中）。
type AppConfig struct {
	Broker         string `json:"broker"`         // tcp://host:1883
	ProductID      string `json:"productId"`      // 网关在 JetLinks 平台的产品 ID
	DeviceID       string `json:"deviceId"`       // 网关在 JetLinks 平台的 deviceId（= clientId）
	SecureID       string `json:"secureId"`       // 网关的 secureId
	SecureKey      string `json:"secureKey"`      // 网关的 secureKey
	Username       string `json:"username"`       // 可选：留空时自动按 SM3 规则计算
	Password       string `json:"password"`       // 可选：留空时自动按 SM3 规则计算
	CleanSession   bool   `json:"cleanSession"`   // 默认 true
	KeepAlive      int    `json:"keepAlive"`      // 秒，默认 30
	TimestampDelta int    `json:"timestampDelta"` // timestamp 容差（秒），默认 300
}

// NewApp 北向应用工厂。
func NewApp(ctx context.Context, appID string, cfg core.NorthAppConfig) (core.NorthHandler, error) {
	ac, err := parseAppConfig(cfg.Config)
	if err != nil {
		return nil, err
	}
	if ac.KeepAlive == 0 {
		ac.KeepAlive = 30
	}
	if ac.TimestampDelta == 0 {
		ac.TimestampDelta = 300
	}
	a := &app{
		appID:           appID,
		cfg:             ac,
		groups:          map[string]*core.Group{},
		deviceSubs:      map[string]int{},
		pendCh:          make(chan pending, 256),
		commandExecutor: cfg.CommandExecutor,
		statusProvider:  cfg.GroupStatusProvider,
		ctx:             ctx,
		startTime:       time.Now(),
	}
	if !a.connectWithTimeout(3 * time.Second) {
		a.startReconnectLoop()
	}
	go a.dispatch(ctx)
	go a.gatewayPropertiesReportLoop(ctx)
	return a, nil
}

func (a *app) startReconnectLoop() {
	select {
	case <-a.ctx.Done():
		return
	default:
	}

	a.reconnectMu.Lock()
	if a.reconnecting {
		a.reconnectMu.Unlock()
		return
	}
	a.reconnecting = true
	a.reconnectMu.Unlock()

	go a.reconnectLoop(a.ctx)
}

// reconnectLoop 自定义断网后台重连逻辑。
// 当网络意外中断时，通过 client.OnConnectionLost 触发，并在每次重连前重新构建 auth 信息，
// 以刷新含有最新毫秒时间戳的用户名/密码，解决超过 5 分钟后因时间戳过期被平台拒签的问题。
func (a *app) reconnectLoop(ctx context.Context) {
	defer func() {
		a.reconnectMu.Lock()
		a.reconnecting = false
		a.reconnectMu.Unlock()
	}()

	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.mu.RLock()
			client := a.client
			a.mu.RUnlock()
			if client != nil && client.IsConnected() {
				return
			}
			zap.L().Info("jetlinks mqtt reconnecting with fresh SM3 timestamp...")
			if a.connectWithTimeout(12 * time.Second) {
				return
			}
		}
	}
}

// Register 注册到 north registry。
func Register(r *core.NorthRegistry) {
	r.RegisterExtension(Descriptor(), NewApp)
}

// Descriptor 返回 JetLinks MQTT 编译期插件描述符。
func Descriptor() core.ExtensionDescriptor {
	return core.ExtensionDescriptor{
		Type:         DriverName,
		Name:         "JetLinks MQTT",
		Description:  "通过官方 MQTT 协议接入 JetLinks 平台",
		Version:      "1.0.0",
		Capabilities: []string{"report", "read-command", "write-command", "shared-connection"},
		ConfigSchema: []core.ConfigField{
			{Key: "broker", Label: "Broker", Type: core.ConfigFieldText, Required: true, DefaultValue: "tcp://127.0.0.1:1883", Placeholder: "tcp://127.0.0.1:1883"},
			{Key: "productId", Label: "网关产品 ID", Type: core.ConfigFieldText, Required: true},
			{Key: "deviceId", Label: "网关设备 ID", Type: core.ConfigFieldText, Required: true},
			{Key: "secureId", Label: "Secure ID", Type: core.ConfigFieldText, Required: true},
			{Key: "secureKey", Label: "Secure Key", Type: core.ConfigFieldPassword, Required: true},
			{Key: "cleanSession", Label: "清理会话", Type: core.ConfigFieldBoolean, DefaultValue: true},
			{Key: "keepAlive", Label: "Keep Alive（秒）", Type: core.ConfigFieldNumber, DefaultValue: 30, Min: jetlinksNumberPointer(5), Max: jetlinksNumberPointer(3600)},
			{Key: "timestampDelta", Label: "时间戳容差（秒）", Type: core.ConfigFieldNumber, DefaultValue: 300, Min: jetlinksNumberPointer(60), Max: jetlinksNumberPointer(600)},
		},
	}
}

func jetlinksNumberPointer(value float64) *float64 {
	return &value
}

func parseAppConfig(m map[string]interface{}) (AppConfig, error) {
	cfg := AppConfig{
		CleanSession:   true,
		KeepAlive:      30,
		TimestampDelta: 300,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return cfg, fmt.Errorf("jetlinks: encode config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("jetlinks: decode config: %w", err)
	}
	if cfg.Broker == "" {
		return cfg, fmt.Errorf("jetlinks: broker is required")
	}
	if cfg.ProductID == "" || cfg.DeviceID == "" {
		return cfg, fmt.Errorf("jetlinks: productId and deviceId are required")
	}
	// Username/Password 都为空时启用 SM3 自动认证
	// 此时必须提供 productId + secureId + secureKey + deviceId
	if cfg.Username == "" && cfg.Password == "" {
		missing := []string{}
		if cfg.ProductID == "" {
			missing = append(missing, "productId")
		}
		if cfg.SecureID == "" {
			missing = append(missing, "secureId")
		}
		if cfg.SecureKey == "" {
			missing = append(missing, "secureKey")
		}
		if cfg.DeviceID == "" {
			missing = append(missing, "deviceId")
		}
		if len(missing) > 0 {
			return cfg, fmt.Errorf("jetlinks: missing required fields for auto SM3 auth: %v", missing)
		}
	}
	return cfg, nil
}

// md5Hex 返回 input 的 MD5 摘要（32 字符大写十六进制）。
func md5Hex(input string) string {
	h := md5.New()
	h.Write([]byte(input))
	return fmt.Sprintf("%X", h.Sum(nil))
}

// sm3Hex 返回 input 的 SM3 摘要（64 字符大写十六进制）。
func sm3Hex(input string) string {
	h := gmsm.New()
	h.Write([]byte(input))
	return fmt.Sprintf("%X", h.Sum(nil))
}

// buildAuth 根据 cfg 生成当前时刻的 (clientId, username, password)。
//
// 规则（JetLinks MQTT 接入规范）：
//   - clientId = DeviceID
//   - timestamp = 当前毫秒时间戳
//   - username = SecureID + "|" + timestamp
//   - password = SM3(SecureID + "|" + timestamp + "|" + SecureKey)（大写十六进制）
//
// 如果 Username/Password 已显式配置，则用配置值（兼容 token 模式）。
func (a *app) buildAuth() (clientID, username, password string) {
	clientID = a.cfg.DeviceID
	if a.cfg.Username != "" {
		username = a.cfg.Username
		password = a.cfg.Password
		return
	}
	ts := time.Now().UnixMilli()
	username = a.cfg.SecureID + "|" + strconv.FormatInt(ts, 10)
	password = md5Hex(a.cfg.SecureID + "|" + strconv.FormatInt(ts, 10) + "|" + a.cfg.SecureKey)
	return
}

// app 一次运行实例（一个 MQTT 客户端连接）。
type app struct {
	appID     string
	cfg       AppConfig
	client    mqtt.Client
	startTime time.Time
	ctx       context.Context // 北向应用 Context，用于重连感知退出

	reconnectMu  sync.Mutex
	reconnecting bool

	// groups: deviceKey (productId+"/"+deviceId) -> *core.Group
	// 注：Group 不属于 NorthApp 生命周期，仅用于下行消息路由。
	mu         sync.RWMutex
	groups     map[string]*core.Group
	deviceSubs map[string]int // deviceKey -> 引用计数
	subMu      sync.Mutex     // 串行化订阅副作用，避免并发 reload 时旧退订覆盖新订阅

	// 待处理指令：messageID -> chan reply
	pendCh chan pending

	commandExecutor core.NorthCommandExecutor
	statusProvider  core.GroupStatusProvider
}

type pending struct {
	cmd core.NorthCommand
	// 注：早期版本预留 rep 字段用于同步等待 reply，2026-06 升级
	// golangci-lint v2 后被 unused 规则标红。当前所有调用方走 pendCh
	// 异步收 reply，不需要此字段。删除以通过 lint。
}

type NorthReply = core.NorthCommandReply

// deviceKey 构造一个 Group 在 NorthApp 中的 key。
func deviceKey(productID, deviceID string) string {
	return productID + "/" + deviceID
}

// RegisterGroup 注册一个 Group 到本 NorthApp（多对一共享）。
// 内部维护 device 引用计数，refCount 0→1 时自动 Subscribe 该设备主题。
// 返回：true=成功注册；false=设备身份缺失。
func (a *app) RegisterGroup(g *core.Group) bool {
	if g.Device.ProductID == "" || g.Device.DeviceID == "" {
		return false
	}
	key := deviceKey(g.Device.ProductID, g.Device.DeviceID)
	a.mu.Lock()
	old, exists := a.groups[key]
	if exists && old.ID == g.ID {
		a.mu.Unlock()
		return true // 已注册过
	}
	a.groups[key] = g
	a.deviceSubs[key]++

	needSub := a.deviceSubs[key] == 1
	a.mu.Unlock() // 提前释放锁

	// refCount 0→1 时在临界区外订阅该设备主题，避免锁内网络操作引起死锁/卡顿
	if needSub {
		a.subscribeDevice(g.Device.ProductID, g.Device.DeviceID)
	}
	return true
}

// DeregisterGroup 注销一个 Group。
// refCount 1→0 时自动 Unsubscribe。
func (a *app) DeregisterGroup(g *core.Group) {
	if g.Device.ProductID == "" || g.Device.DeviceID == "" {
		return
	}
	key := deviceKey(g.Device.ProductID, g.Device.DeviceID)
	a.mu.Lock()
	if a.deviceSubs[key] <= 0 {
		a.mu.Unlock()
		return
	}
	a.deviceSubs[key]--
	needUnsub := false
	if a.deviceSubs[key] == 0 {
		delete(a.deviceSubs, key)
		delete(a.groups, key)
		needUnsub = true
	}
	a.mu.Unlock() // 提前释放锁

	// 临界区外部执行注销操作，规避网络 I/O 阻塞造成的死锁风险
	if needUnsub {
		a.unsubscribeDevice(g.Device.ProductID, g.Device.DeviceID)
	}
}

// subscribeDevice 订阅某子设备的全部下行主题。
// 传入子设备的 productId/deviceId；网关身份从 a.cfg 取。
// 在锁外调用，避免网络阻塞全局互斥锁。
func (a *app) subscribeDevice(productID, deviceID string) {
	key := deviceKey(productID, deviceID)
	a.subMu.Lock()
	defer a.subMu.Unlock()

	a.mu.RLock()
	if a.deviceSubs[key] <= 0 {
		a.mu.RUnlock()
		return
	}
	client := a.client
	a.mu.RUnlock()
	if client == nil || !client.IsConnected() {
		// OnConnect 会统一订阅，断线期间无需重复堆积请求。
		return
	}
	topics := deviceChildTopics(a.cfg.ProductID, a.cfg.DeviceID, deviceID)
	for t, q := range topics {
		if token := client.Subscribe(t, q, a.onMessage); token.Wait() && token.Error() != nil {
			zap.L().Error("jetlinks subscribe failed",
				zap.String("topic", t), zap.Error(token.Error()))
		}
	}
	zap.L().Info("jetlinks subscribed to device topics",
		zap.String("northAppId", a.appID),
		zap.String("productId", productID),
		zap.String("deviceId", deviceID))
}

func (a *app) unsubscribeDevice(productID, deviceID string) {
	key := deviceKey(productID, deviceID)
	a.subMu.Lock()
	defer a.subMu.Unlock()

	a.mu.RLock()
	if a.deviceSubs[key] > 0 {
		a.mu.RUnlock()
		return
	}
	client := a.client
	a.mu.RUnlock()
	if client == nil || !client.IsConnected() {
		return
	}
	topics := deviceChildTopics(a.cfg.ProductID, a.cfg.DeviceID, deviceID)
	for t := range topics {
		if token := client.Unsubscribe(t); token.Wait() && token.Error() != nil {
			zap.L().Warn("jetlinks unsubscribe failed",
				zap.String("topic", t), zap.Error(token.Error()))
		}
	}
	zap.L().Info("jetlinks unsubscribed from device topics",
		zap.String("northAppId", a.appID),
		zap.String("productId", productID),
		zap.String("deviceId", deviceID))
}

func (a *app) buildOptions() *mqtt.ClientOptions {
	clientID, username, password := a.buildAuth()
	opts := mqtt.NewClientOptions().
		AddBroker(a.cfg.Broker).
		SetClientID(clientID).
		SetUsername(username).
		SetPassword(password).
		SetCleanSession(a.cfg.CleanSession).
		SetKeepAlive(time.Duration(a.cfg.KeepAlive) * time.Second).
		SetAutoReconnect(false). // ！！！关闭 paho 自身的自动重连，改由 reconnectLoop 托管，实现重连时刷新时间戳
		SetConnectTimeout(10 * time.Second)

	opts.OnConnect = a.onConnect
	opts.OnConnectionLost = func(c mqtt.Client, err error) {
		zap.L().Warn("jetlinks mqtt disconnected, starting reconnect loop...", zap.Error(err))
		a.startReconnectLoop()
	}
	return opts
}

func (a *app) connectWithTimeout(timeout time.Duration) bool {
	opts := a.buildOptions()
	a.mu.Lock()
	a.client = mqtt.NewClient(opts)
	client := a.client
	a.mu.Unlock()

	token := client.Connect()
	if !token.WaitTimeout(timeout) {
		zap.L().Warn("jetlinks mqtt connect timeout, will retry in background",
			zap.String("broker", a.cfg.Broker),
			zap.Duration("timeout", timeout))
		return false
	}
	if err := token.Error(); err != nil {
		zap.L().Warn("jetlinks mqtt initial connect failed, will retry in background",
			zap.String("broker", a.cfg.Broker),
			zap.Error(err))
		return false
	}
	zap.L().Info("jetlinks mqtt connected",
		zap.String("broker", a.cfg.Broker),
		zap.String("clientId", a.cfg.DeviceID))
	return true
}

// onConnect 连接建立/重连后回调：重新订阅并为每个子设备 publish register/online。
//
// 按官方协议，**子设备**（不是网关）需要 publish：
//   - /{gw_pid}/{gw_did}/child/{child_did}/register  ← 让平台知道这个子设备属于本网关
//   - /{gw_pid}/{gw_did}/child/{child_did}/online     ← 标记子设备在线
//
// 网关本身**不需要** register（它已是平台上一台真实设备，由人工在平台创建）。
func (a *app) onConnect(c mqtt.Client) {
	a.mu.Lock()
	groups := make([]*core.Group, 0, len(a.groups))
	for _, g := range a.groups {
		groups = append(groups, g)
	}
	a.mu.Unlock()

	// 重新订阅所有已注册子设备
	for _, g := range groups {
		a.subscribeDevice(g.Device.ProductID, g.Device.DeviceID)
	}
	// 为每个子设备 publish register + online
	if a.cfg.ProductID != "" && a.cfg.DeviceID != "" {
		now := time.Now().UnixMilli()
		for _, g := range groups {
			if g.Device.ProductID == "" || g.Device.DeviceID == "" {
				continue
			}
			// register
			regTopic := topicChildRegister(a.cfg.ProductID, a.cfg.DeviceID, g.Device.DeviceID)
			regPayload, _ := json.Marshal(map[string]interface{}{
				"timestamp": now,
				"messageId": uuid.NewString(),
				"deviceId":  g.Device.DeviceID,
				"headers": map[string]interface{}{
					"productId":  g.Device.ProductID,
					"deviceName": g.Name,
					"configuration": map[string]interface{}{
						"selfManageState": false,
					},
				},
			})
			if token := c.Publish(regTopic, 0, false, regPayload); token.Wait() && token.Error() != nil {
				zap.L().Warn("jetlinks publish child register failed", zap.Error(token.Error()))
			}
			// online
			onlineTopic := topicChildOnline(a.cfg.ProductID, a.cfg.DeviceID, g.Device.DeviceID)
			onlinePayload, _ := json.Marshal(map[string]interface{}{
				"timestamp": now,
				"messageId": uuid.NewString(),
				"deviceId":  g.Device.DeviceID,
			})
			if token := c.Publish(onlineTopic, 0, false, onlinePayload); token.Wait() && token.Error() != nil {
				zap.L().Warn("jetlinks publish child online failed", zap.Error(token.Error()))
			}
			zap.L().Info("jetlinks child registered & online",
				zap.String("gwProductId", a.cfg.ProductID),
				zap.String("gwDeviceId", a.cfg.DeviceID),
				zap.String("childProductId", g.Device.ProductID),
				zap.String("childDeviceId", g.Device.DeviceID))
		}
	}
}

// onMessage 收到 broker 下推消息。
func (a *app) onMessage(c mqtt.Client, m mqtt.Message) {
	topic := m.Topic()
	payload := m.Payload()
	zap.L().Debug("jetlinks mqtt received",
		zap.String("topic", topic),
		zap.Int("payloadLen", len(payload)))

	// 网关+子设备模式：解析 /{gwPid}/{gwDid}/child/{childDid}/{action}[/reply]
	_, _, childDeviceID, action, ok := parseChildTopic(topic)
	if !ok {
		zap.L().Warn("jetlinks mqtt topic cannot be parsed", zap.String("topic", topic))
		return
	}

	if strings.HasSuffix(action, "/reply") {
		// 异步回复（如平台 -> 设备调用功能的回复），不需主动处理
		return
	}
	cmd, err := decodeCommand(childDeviceID, action, payload)
	if err != nil {
		zap.L().Warn("invalid child command",
			zap.String("topic", topic),
			zap.Error(err))
		return
	}

	// 异步处理
	select {
	case a.pendCh <- pending{cmd: cmd}:
	default:
		zap.L().Warn("pending channel full, dropping command")
	}
}

func decodeCommand(childDeviceID, action string, payload []byte) (core.NorthCommand, error) {
	var env struct {
		MessageID  string          `json:"messageId"`
		Properties json.RawMessage `json:"properties"`
		FunctionID string          `json:"functionId"`
		Inputs     json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return core.NorthCommand{}, fmt.Errorf("decode command payload: %w", err)
	}
	if env.MessageID == "" {
		env.MessageID = uuid.NewString()
	}
	cmd := core.NorthCommand{
		ID:       env.MessageID,
		DeviceID: childDeviceID,
		Payload:  map[string]interface{}{},
	}
	switch action {
	case "properties/read":
		cmd.Type = "read-property"
		var properties []string
		if err := json.Unmarshal(env.Properties, &properties); err != nil {
			return core.NorthCommand{}, fmt.Errorf("decode read properties: %w", err)
		}
		cmd.Payload["properties"] = properties
	case "properties/write":
		cmd.Type = "write-property"
		var properties map[string]interface{}
		if err := json.Unmarshal(env.Properties, &properties); err != nil {
			return core.NorthCommand{}, fmt.Errorf("decode write properties: %w", err)
		}
		cmd.Payload["properties"] = properties
	case "function/invoke":
		cmd.Type = "invoke-function"
		cmd.Payload["functionId"] = env.FunctionID
		var inputs interface{}
		if len(env.Inputs) > 0 {
			if err := json.Unmarshal(env.Inputs, &inputs); err != nil {
				return core.NorthCommand{}, fmt.Errorf("decode function inputs: %w", err)
			}
		}
		cmd.Payload["inputs"] = inputs
	default:
		return core.NorthCommand{}, fmt.Errorf("unsupported child topic action: %s", action)
	}
	return cmd, nil
}

// dispatch 处理下行指令：调用对应 Group 的 driver 后 publish reply。
// 启动 goroutine 并发处理，避免单一设备的采集通道超时时拖慢全局下行控制（HOL 阻塞）。
func (a *app) dispatch(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-a.pendCh:
			go a.handleCommand(ctx, p.cmd)
		}
	}
}

// handleCommand 通过 Runner 注入的执行器调用目标 Group 驱动并回复平台。
func (a *app) handleCommand(ctx context.Context, cmd core.NorthCommand) {
	if a.commandExecutor == nil {
		a.publishReply(cmd, core.NorthCommandReply{
			ID:      cmd.ID,
			Code:    503,
			Message: "command executor unavailable",
		})
		return
	}
	reply, err := a.commandExecutor(ctx, cmd)
	if err != nil {
		reply = core.NorthCommandReply{ID: cmd.ID, Code: 500, Message: err.Error()}
	}
	a.publishReply(cmd, reply)
}

func (a *app) publishReply(cmd core.NorthCommand, reply core.NorthCommandReply) {
	topic := topicChildWriteReply(a.cfg.ProductID, a.cfg.DeviceID, cmd.DeviceID)
	switch cmd.Type {
	case "read-property":
		topic = topicChildReadReply(a.cfg.ProductID, a.cfg.DeviceID, cmd.DeviceID)
	case "invoke-function":
		topic = topicChildInvokeReply(a.cfg.ProductID, a.cfg.DeviceID, cmd.DeviceID)
	}
	body := map[string]interface{}{
		"messageId":  reply.ID,
		"code":       reply.Code,
		"message":    reply.Message,
		"timestamp":  time.Now().UnixMilli(),
		"successful": reply.Code == 0,
	}
	switch cmd.Type {
	case "read-property", "write-property":
		body["properties"] = reply.Payload
	case "invoke-function":
		body["output"] = reply.Payload
	}
	payload, _ := json.Marshal(body)
	a.mu.RLock()
	c := a.client
	a.mu.RUnlock()
	if c == nil {
		return
	}
	if token := c.Publish(topic, 0, false, payload); token.Wait() && token.Error() != nil {
		zap.L().Error("publish reply failed", zap.Error(token.Error()))
	}
}

// OnMessage 处理上行消息（属性上报）。
// msg.ProductID 和 msg.DeviceID 由 runner 填入（子设备的身份）。
func (a *app) OnMessage(ctx context.Context, msg core.NorthMessage) error {
	if a.cfg.ProductID == "" || a.cfg.DeviceID == "" || msg.DeviceID == "" {
		return fmt.Errorf("jetlinks: missing gw productId/deviceId or child deviceId")
	}
	// 网关+子设备模式：用子设备 topic
	topic := topicChildPropertiesReport(a.cfg.ProductID, a.cfg.DeviceID, msg.DeviceID)
	payload, _ := json.Marshal(map[string]interface{}{
		"messageId":  uuid.NewString(),
		"properties": msg.Payload["properties"],
		"changes":    msg.Payload["changes"],
		"timestamp":  msg.Timestamp.UnixMilli(),
	})
	a.mu.RLock()
	c := a.client
	a.mu.RUnlock()
	if c == nil || !c.IsConnected() {
		// 采集链路不因北向离线失败；连接状态由 State() 对外暴露。
		return nil
	}
	if token := c.Publish(topic, 0, false, payload); token.Wait() && token.Error() != nil {
		return fmt.Errorf("publish report: %w", token.Error())
	}
	return nil
}

// OnCommand 处理下行指令（由 runner 调用）。
func (a *app) OnCommand(ctx context.Context, cmd core.NorthCommand) (core.NorthCommandReply, error) {
	if a.commandExecutor == nil {
		return core.NorthCommandReply{ID: cmd.ID, Code: 503, Message: "command executor unavailable"}, nil
	}
	reply, err := a.commandExecutor(ctx, cmd)
	if err != nil {
		return core.NorthCommandReply{}, err
	}
	a.publishReply(cmd, reply)
	return reply, nil
}

// Close 停止 MQTT 客户端并释放自动重连资源。
func (a *app) Close() error {
	a.mu.Lock()
	client := a.client
	a.client = nil
	a.mu.Unlock()
	if client != nil {
		client.Disconnect(1000)
	}
	return nil
}

// State 返回 MQTT 连接的实时连通状态。
func (a *app) State() *core.NorthState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.client == nil {
		return &core.NorthState{Connected: false, LastError: "client not initialized"}
	}
	return &core.NorthState{Connected: a.client.IsConnected()}
}

// ============ 主题工具函数（按官方协议 jetlinks-official-protocol V1.3.1）============
//
// 上行 topic（设备 → 平台）：
//   - 直连设备属性上报：   /{productId}/{deviceId}/properties/report
//   - 直连设备事件上报：   /{productId}/{deviceId}/event/{eventId}
//   - 直连设备读属性回复： /{productId}/{deviceId}/properties/read/reply
//   - 直连设备写属性回复： /{productId}/{deviceId}/properties/write/reply
//   - 直连设备调用功能回复：/{productId}/{deviceId}/function/invoke/reply
//   - 网关子设备属性上报：/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/report
//   - 网关子设备上线：    /{gwProductId}/{gwDeviceId}/child/{childDeviceId}/online
//   - 网关子设备离线：    /{gwProductId}/{gwDeviceId}/child/{childDeviceId}/offline
//   - 网关子设备注册：    /{gwProductId}/{gwDeviceId}/child/{childDeviceId}/register
//   - 网关子设备注销：    /{gwProductId}/{gwDeviceId}/child/{childDeviceId}/unregister
//
// 下行 topic（平台 → 设备）：
//   - 直连设备读属性：   /{productId}/{deviceId}/properties/read
//   - 直连设备写属性：   /{productId}/{deviceId}/properties/write
//   - 直连设备调用功能： /{productId}/{deviceId}/function/invoke
//   - 网关子设备读属性： /{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/read
//   - 网关子设备写属性： /{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/write
//   - 网关子设备调用功能：/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/function/invoke
//
// 注意：本实现采用"网关+子设备"模型。1 个 NorthApp = 1 个网关 = 1 个 MQTT 连接。
// 多个子设备（Group）共享同一条连接，上送用网关的子设备 topic（带 child/... 段）。

// 直连设备 topic（直连模式暂未启用，保留供 v0.5+）
func topicPropertiesReport(productID, deviceID string) string {
	return fmt.Sprintf("/%s/%s/properties/report", productID, deviceID)
}

// 网关子设备 topic（当前模式）
func topicChildPropertiesReport(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/properties/report", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildPropertiesRead(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/properties/read", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildPropertiesWrite(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/properties/write", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildFunctionInvoke(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/function/invoke", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildReadReply(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/properties/read/reply", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildWriteReply(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/properties/write/reply", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildInvokeReply(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/function/invoke/reply", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildRegister(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/register", gwProductID, gwDeviceID, childDeviceID)
}
func topicChildOnline(gwProductID, gwDeviceID, childDeviceID string) string {
	return fmt.Sprintf("/%s/%s/child/%s/online", gwProductID, gwDeviceID, childDeviceID)
}

// deviceChildTopics 返回某子设备需要订阅的全部下行主题（按官方协议）。
func deviceChildTopics(gwProductID, gwDeviceID, childDeviceID string) map[string]byte {
	return map[string]byte{
		topicChildPropertiesRead(gwProductID, gwDeviceID, childDeviceID):  0,
		topicChildPropertiesWrite(gwProductID, gwDeviceID, childDeviceID): 0,
		topicChildFunctionInvoke(gwProductID, gwDeviceID, childDeviceID):  0,
	}
}

// parseChildTopic 解析 "/{gwPid}/{gwDid}/child/{childDid}/{action}[/reply]"。
// 返回 (gwProductID, gwDeviceID, childDeviceID, action)。
// action 形如 "properties/read" / "properties/read/reply" / "function/invoke" / "register" 等。
func parseChildTopic(topic string) (gwProductID, gwDeviceID, childDeviceID, action string, ok bool) {
	parts := strings.Split(topic, "/")
	// 期望: ["", "{gwPid}", "{gwDid}", "child", "{childDid}", "{action...}"]  len >= 6
	if len(parts) < 6 || parts[1] == "" || parts[3] != "child" || parts[4] == "" {
		return
	}
	gwProductID = parts[1]
	gwDeviceID = parts[2]
	childDeviceID = parts[4]
	action = strings.Join(parts[5:], "/")
	ok = true
	return
}

func (a *app) gatewayPropertiesReportLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// 启动后稍微延迟 5 秒进行首次属性上报
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second):
		a.reportGatewayProperties()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.reportGatewayProperties()
		}
	}
}

const (
	southDeviceStatusOnline  = "online"
	southDeviceStatusOffline = "offline"
	southDeviceStatusUnknown = "unknown"
)

type boundDeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Driver    string `json:"driver"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"`
	TagsCount int    `json:"tagsCount"`
	ProductID string `json:"productId"`
}

// southDeviceStatus 把南向驱动连接状态转换为平台属性中稳定的设备状态文本。
func southDeviceStatus(status core.DriverStatus, ok bool) string {
	if !ok {
		return southDeviceStatusUnknown
	}
	if status.Connected {
		return southDeviceStatusOnline
	}
	return southDeviceStatusOffline
}

// boundDevicesSnapshot 生成网关属性上报中的子设备清单。
//
// 状态查询在 app 锁外执行，避免北向应用锁与 Runner/驱动锁相互嵌套。
func (a *app) boundDevicesSnapshot() ([]string, []boundDeviceInfo) {
	a.mu.RLock()
	groups := make([]*core.Group, 0, len(a.groups))
	for _, g := range a.groups {
		groups = append(groups, g)
	}
	statusProvider := a.statusProvider
	a.mu.RUnlock()

	boundDevices := make([]string, 0, len(groups))
	boundDevicesInfo := make([]boundDeviceInfo, 0, len(groups))
	for _, g := range groups {
		if g.Device.DeviceID == "" {
			continue
		}
		status, ok := core.DriverStatus{}, false
		if statusProvider != nil {
			status, ok = statusProvider(g.ID)
		}
		boundDevices = append(boundDevices, g.Device.DeviceID)
		boundDevicesInfo = append(boundDevicesInfo, boundDeviceInfo{
			ID:        g.Device.DeviceID,
			Name:      g.Name,
			Driver:    g.Driver,
			Enabled:   g.Enabled,
			Status:    southDeviceStatus(status, ok),
			TagsCount: len(g.Tags),
			ProductID: g.Device.ProductID,
		})
	}
	return boundDevices, boundDevicesInfo
}

func (a *app) reportGatewayProperties() {
	a.mu.RLock()
	client := a.client
	a.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return
	}
	if a.cfg.ProductID == "" || a.cfg.DeviceID == "" {
		return
	}
	boundDevices, boundDevicesInfo := a.boundDevicesSnapshot()

	// 采集网关自身的系统/程序资源数据
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(a.startTime).Seconds()
	goroutines := runtime.NumGoroutine()
	memAlloc := float64(m.Alloc) / 1024 / 1024 // MB
	memSys := float64(m.Sys) / 1024 / 1024     // MB
	cpuCoreCount := runtime.NumCPU()
	goVersion := runtime.Version()
	osType := runtime.GOOS
	archType := runtime.GOARCH

	properties := map[string]interface{}{
		"upTime":            int64(uptime),
		"goroutineCount":    goroutines,
		"memoryAlloc":       memAlloc,
		"memorySys":         memSys,
		"cpuCoreCount":      cpuCoreCount,
		"goVersion":         goVersion,
		"os":                osType,
		"arch":              archType,
		"ipAddress":         getLocalIP(),
		"macAddress":        getMACAddress(),
		"boundDevicesCount": len(boundDevices),
		"boundDevices":      boundDevices,
		"boundDevicesInfo":  boundDevicesInfo,
	}

	topic := topicPropertiesReport(a.cfg.ProductID, a.cfg.DeviceID)
	payload, err := json.Marshal(map[string]interface{}{
		"messageId":  uuid.NewString(),
		"properties": properties,
		"timestamp":  time.Now().UnixMilli(),
	})
	if err != nil {
		return
	}

	if token := client.Publish(topic, 0, false, payload); token.Wait() && token.Error() != nil {
		zap.L().Warn("jetlinks publish gateway properties failed", zap.Error(token.Error()))
	} else {
		zap.L().Debug("jetlinks publish gateway properties success", zap.Any("properties", properties))
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func getMACAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, i := range interfaces {
		if i.Flags&net.FlagLoopback == 0 && i.HardwareAddr != nil {
			mac := i.HardwareAddr.String()
			if mac != "" {
				return mac
			}
		}
	}
	return ""
}
