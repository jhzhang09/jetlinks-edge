// Package mqtt 实现把边缘网关的数据上送到通用的 MQTT Broker，并接收下行控制指令。
// @author jhzhang
// @date 2026-06-13
package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// DriverName 通用 MQTT 北向应用驱动名。
const DriverName = "generic-mqtt"

// AppConfig 通用 MQTT 北向配置。
type AppConfig struct {
	Broker       string `json:"broker"`       // 示例: tcp://127.0.0.1:1883
	ClientID     string `json:"clientId"`     // 客户端标识符，如留空，将使用随机 ID
	Username     string `json:"username"`     // 用户名（可选）
	Password     string `json:"password"`     // 密码（可选）
	CleanSession bool   `json:"cleanSession"` // 默认 true
	KeepAlive    int    `json:"keepAlive"`    // keepalive 时间（秒），默认 30
	UploadTopic  string `json:"uploadTopic"`  // 属性上送主题，支持 {appId}, {groupId}, {deviceId} 占位符
	WriteTopic   string `json:"writeTopic"`   // 写指令订阅主题（可选）
}

type app struct {
	appID           string
	cfg             AppConfig
	client          mqtt.Client
	groups          map[string]*core.Group
	commandExecutor core.NorthCommandExecutor
	statusProvider  core.GroupStatusProvider
	ctx             context.Context
	startTime       time.Time

	reconnecting bool
	reconnectMu  sync.Mutex
	mu           sync.RWMutex
}

// NewApp 通用 MQTT 北向应用工厂方法。
func NewApp(ctx context.Context, appID string, cfg core.NorthAppConfig) (core.NorthHandler, error) {
	ac, err := parseAppConfig(cfg.Config)
	if err != nil {
		return nil, err
	}
	if ac.KeepAlive == 0 {
		ac.KeepAlive = 30
	}
	if ac.ClientID == "" {
		ac.ClientID = "edge-mqtt-" + appID
	}
	if ac.UploadTopic == "" {
		ac.UploadTopic = "/edge/" + appID + "/upload"
	}

	a := &app{
		appID:           appID,
		cfg:             ac,
		groups:          map[string]*core.Group{},
		commandExecutor: cfg.CommandExecutor,
		statusProvider:  cfg.GroupStatusProvider,
		ctx:             ctx,
		startTime:       time.Now(),
	}

	if !a.connectWithTimeout(3 * time.Second) {
		a.startReconnectLoop()
	}

	return a, nil
}

// Register 注册通用 MQTT 插件工厂。
func Register(r *core.NorthRegistry) {
	r.RegisterExtension(Descriptor(), NewApp)
}

// Descriptor 返回通用 MQTT 驱动描述符，用于前端动态生成配置界面。
func Descriptor() core.ExtensionDescriptor {
	return core.ExtensionDescriptor{
		Type:         DriverName,
		Name:         "Generic MQTT",
		Description:  "将采集数据上送到通用的 MQTT Broker（支持按点组/应用自定义主题，并支持下行控制）",
		Version:      "1.0.0",
		Capabilities: []string{"report", "write-command", "shared-connection"},
		ConfigSchema: []core.ConfigField{
			{Key: "broker", Label: "Broker 地址", Type: core.ConfigFieldText, Required: true, DefaultValue: "tcp://127.0.0.1:1883", Placeholder: "tcp://127.0.0.1:1883"},
			{Key: "clientId", Label: "Client ID", Type: core.ConfigFieldText, Required: false, Placeholder: "留空时默认按应用ID生成"},
			{Key: "username", Label: "用户名", Type: core.ConfigFieldText, Required: false},
			{Key: "password", Label: "密码", Type: core.ConfigFieldPassword, Required: false},
			{Key: "cleanSession", Label: "清理会话", Type: core.ConfigFieldBoolean, DefaultValue: true},
			{Key: "keepAlive", Label: "Keep Alive（秒）", Type: core.ConfigFieldNumber, DefaultValue: 30, Min: mqttNumberPointer(5), Max: mqttNumberPointer(3600)},
			{Key: "uploadTopic", Label: "上送主题", Type: core.ConfigFieldText, Required: true, DefaultValue: "/edge/{appId}/upload", Placeholder: "/edge/{appId}/upload"},
			{Key: "writeTopic", Label: "指令下发订阅主题", Type: core.ConfigFieldText, Required: false, Placeholder: "可选，如 /edge/{appId}/write"},
		},
	}
}

func mqttNumberPointer(value float64) *float64 {
	return &value
}

func parseAppConfig(m map[string]interface{}) (AppConfig, error) {
	cfg := AppConfig{
		CleanSession: true,
		KeepAlive:    30,
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return cfg, fmt.Errorf("generic-mqtt: encode config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("generic-mqtt: decode config: %w", err)
	}
	if cfg.Broker == "" {
		return cfg, fmt.Errorf("generic-mqtt: broker is required")
	}
	return cfg, nil
}

// connectWithTimeout 尝试同步连接。
func (a *app) connectWithTimeout(timeout time.Duration) bool {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(a.cfg.Broker)
	opts.SetClientID(a.cfg.ClientID)
	opts.SetCleanSession(a.cfg.CleanSession)
	opts.SetKeepAlive(time.Duration(a.cfg.KeepAlive) * time.Second)
	if a.cfg.Username != "" {
		opts.SetUsername(a.cfg.Username)
	}
	if a.cfg.Password != "" {
		opts.SetPassword(a.cfg.Password)
	}
	opts.SetConnectTimeout(timeout)
	opts.SetOnConnectHandler(a.onConnect)
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		zap.L().Warn("generic-mqtt: connection lost", zap.String("appId", a.appID), zap.Error(err))
		a.startReconnectLoop()
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.WaitTimeout(timeout) && token.Error() == nil {
		a.mu.Lock()
		a.client = client
		a.mu.Unlock()
		zap.L().Info("generic-mqtt: connected successfully", zap.String("appId", a.appID), zap.String("broker", a.cfg.Broker))
		return true
	}
	err := token.Error()
	if err == nil {
		err = errors.New("connection timeout")
	}
	zap.L().Warn("generic-mqtt: failed to connect", zap.String("appId", a.appID), zap.Error(err))
	return false
}

func (a *app) startReconnectLoop() {
	a.reconnectMu.Lock()
	if a.reconnecting {
		a.reconnectMu.Unlock()
		return
	}
	a.reconnecting = true
	a.reconnectMu.Unlock()

	go func() {
		defer func() {
			a.reconnectMu.Lock()
			a.reconnecting = false
			a.reconnectMu.Unlock()
		}()

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				a.mu.RLock()
				c := a.client
				a.mu.RUnlock()
				if c != nil && c.IsConnected() {
					return
				}
				zap.L().Info("generic-mqtt: reconnecting...", zap.String("appId", a.appID))
				if a.connectWithTimeout(5 * time.Second) {
					return
				}
			}
		}
	}()
}

// onConnect 成功连接后的处理（例如自动订阅下行控制主题）。
func (a *app) onConnect(c mqtt.Client) {
	if a.cfg.WriteTopic == "" {
		return
	}
	topic := strings.ReplaceAll(a.cfg.WriteTopic, "{appId}", a.appID)
	// 订阅命令下发主题
	if token := c.Subscribe(topic, 0, a.onMessageReceived); token.Wait() && token.Error() != nil {
		zap.L().Error("generic-mqtt: failed to subscribe to write topic", zap.String("topic", topic), zap.Error(token.Error()))
	} else {
		zap.L().Info("generic-mqtt: subscribed to write topic", zap.String("topic", topic))
	}
}

// onMessageReceived 处理接收到的下行 MQTT 消息。
// 支持格式：{"groupId": "...", "tag": "...", "value": 12.3} 或 {"properties": {"tag1": 100}}
func (a *app) onMessageReceived(c mqtt.Client, m mqtt.Message) {
	var body struct {
		GroupID    string                 `json:"groupId"`
		Tag        string                 `json:"tag"`
		Value      interface{}            `json:"value"`
		Properties map[string]interface{} `json:"properties"`
	}
	if err := json.Unmarshal(m.Payload(), &body); err != nil {
		zap.L().Warn("generic-mqtt: failed to unmarshal command payload", zap.String("payload", string(m.Payload())), zap.Error(err))
		return
	}

	// 构造 core.NorthCommand
	var cmd core.NorthCommand
	cmd.ID = uuid.NewString()
	cmd.GroupID = body.GroupID
	cmd.Type = "write-property"

	// 如果提供的是 values 点位，映射为 properties 结构
	if body.Properties != nil {
		cmd.Payload = map[string]interface{}{"properties": body.Properties}
	} else if body.Tag != "" {
		cmd.Payload = map[string]interface{}{"properties": map[string]interface{}{body.Tag: body.Value}}
	} else {
		zap.L().Warn("generic-mqtt: invalid command payload content", zap.String("payload", string(m.Payload())))
		return
	}

	// 执行指令
	go func() {
		reply, err := a.OnCommand(a.ctx, cmd)
		if err != nil {
			zap.L().Error("generic-mqtt: execute command failed", zap.Error(err))
			return
		}
		// 回复发布到 writeTopic + "/reply"
		replyTopic := m.Topic() + "/reply"
		replyPayload, _ := json.Marshal(reply)
		c.Publish(replyTopic, 0, false, replyPayload)
	}()
}

// OnMessage 将采集到的数据发送至通用的 MQTT uploadTopic。
func (a *app) OnMessage(ctx context.Context, msg core.NorthMessage) error {
	a.mu.RLock()
	c := a.client
	a.mu.RUnlock()

	if c == nil || !c.IsConnected() {
		return nil
	}

	// 动态替换占位符
	topic := a.cfg.UploadTopic
	topic = strings.ReplaceAll(topic, "{appId}", a.appID)
	topic = strings.ReplaceAll(topic, "{groupId}", msg.GroupID)
	topic = strings.ReplaceAll(topic, "{deviceId}", msg.DeviceID)

	// 统一输出 Payload 结构
	payload, _ := json.Marshal(map[string]interface{}{
		"appId":     a.appID,
		"groupId":   msg.GroupID,
		"timestamp": msg.Timestamp.UnixMilli(),
		"values":    msg.Payload["properties"],
	})

	if token := c.Publish(topic, 0, false, payload); token.Wait() && token.Error() != nil {
		return fmt.Errorf("generic-mqtt publish failed: %w", token.Error())
	}
	return nil
}

// OnCommand 执行平台下发的写指令。
func (a *app) OnCommand(ctx context.Context, cmd core.NorthCommand) (core.NorthCommandReply, error) {
	if a.commandExecutor == nil {
		return core.NorthCommandReply{ID: cmd.ID, Code: 503, Message: "command executor unavailable"}, nil
	}
	return a.commandExecutor(ctx, cmd)
}

// RegisterGroup 实现 northRegister 接口，支持被多个 Group 关联共享。
func (a *app) RegisterGroup(g *core.Group) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.groups[g.ID] = g
	zap.L().Info("generic-mqtt: registered group to northbound app", zap.String("groupId", g.ID), zap.String("appId", a.appID))
	return true
}

// DeregisterGroup 实现 northRegister 接口。
func (a *app) DeregisterGroup(g *core.Group) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.groups, g.ID)
	zap.L().Info("generic-mqtt: deregistered group from northbound app", zap.String("groupId", g.ID), zap.String("appId", a.appID))
}

// Close 优雅断开 MQTT 客户端。
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

// State 获取实时连通状态。
func (a *app) State() *core.NorthState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.client == nil {
		return &core.NorthState{Connected: false, LastError: "client not initialized"}
	}
	return &core.NorthState{Connected: a.client.IsConnected()}
}
