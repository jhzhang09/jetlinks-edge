// Package opcua 实现 OPC UA 南向采集驱动。
//
// 本驱动包含：
//  1. 匿名或用户名密码安全登录。
//  2. 基于 NodeID 的批量点位（Tag）并行读取与单点位写入。
//  3. 高可用的连接生命周期管理。
//
// @author jhzhang
// @date 2026-06-07
package opcua

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

// DriverName 统一注册名称。
const DriverName = "opc-ua"

// driverImpl OPC UA 驱动实现。
type driverImpl struct {
	name   string
	cfg    OPCUAConfig
	mu     sync.Mutex
	client *opcua.Client
	stats  map[string]int64

	reconnectDelay time.Duration
	nextConnect    time.Time
	lastError      string
	lastTime       time.Time
}

// OPCUAConfig 驱动连接参数配置结构。
type OPCUAConfig struct {
	Endpoint string        `json:"endpoint"`
	Policy   string        `json:"policy"` // None, Basic256Sha256, Basic128Rsa15
	Mode     string        `json:"mode"`   // None, Sign, SignAndEncrypt
	Auth     string        `json:"auth"`   // Anonymous, Username
	Username string        `json:"username"`
	Password string        `json:"password"`
	Timeout  time.Duration `json:"timeout"`
}

// NewDriver 工厂创建函数。
func NewDriver(ctx context.Context, name string, cfg core.DriverConfig) (core.SouthDriver, error) {
	oc, err := parseConfig(cfg.Config)
	if err != nil {
		return nil, err
	}
	if _, configured := cfg.Config["timeout"]; !configured && cfg.ReadTimeout > 0 {
		oc.Timeout = cfg.ReadTimeout
	}
	reconnectDelay := cfg.ReconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = 5 * time.Second
	}
	return &driverImpl{
		name:           name,
		cfg:            oc,
		stats:          map[string]int64{},
		reconnectDelay: reconnectDelay,
	}, nil
}

// Register 挂载到边缘核心驱动管理器。
func Register(r *core.DriverRegistry) {
	r.RegisterExtension(Descriptor(), NewDriver)
}

// Descriptor 驱动 Schema 描述元信息。
func Descriptor() core.ExtensionDescriptor {
	return core.ExtensionDescriptor{
		Type:         DriverName,
		Name:         "OPC UA",
		Description:  "通过 OPC UA 协议从工业 PLC 周期采集或写入点位",
		Version:      "1.0.0",
		Capabilities: []string{"polling", "read", "write"},
		ConnectionSchema: []core.ConfigField{
			{Key: "endpoint", Label: "端点地址", Type: core.ConfigFieldText, Required: true, Placeholder: "opc.tcp://127.0.0.1:4840"},
			{Key: "policy", Label: "安全策略", Type: core.ConfigFieldSelect, Required: true, DefaultValue: "None", Options: []core.ConfigOption{
				{Label: "None", Value: "None"},
				{Label: "Basic256Sha256", Value: "Basic256Sha256"},
				{Label: "Basic128Rsa15", Value: "Basic128Rsa15"},
			}},
			{Key: "mode", Label: "安全模式", Type: core.ConfigFieldSelect, Required: true, DefaultValue: "None", Options: []core.ConfigOption{
				{Label: "None", Value: "None"},
				{Label: "Sign", Value: "Sign"},
				{Label: "SignAndEncrypt", Value: "SignAndEncrypt"},
			}},
			{Key: "auth", Label: "认证方式", Type: core.ConfigFieldSelect, Required: true, DefaultValue: "Anonymous", Options: []core.ConfigOption{
				{Label: "Anonymous", Value: "Anonymous"},
				{Label: "Username", Value: "Username"},
			}},
			{Key: "username", Label: "用户名", Type: core.ConfigFieldText, Required: false},
			{Key: "password", Label: "密码", Type: core.ConfigFieldPassword, Required: false},
			{Key: "timeout", Label: "请求超时", Type: core.ConfigFieldText, DefaultValue: "10s", Placeholder: "10s"},
		},
		TagSchema: []core.ConfigField{
			{Key: "nodeId", Label: "节点 ID", Type: core.ConfigFieldText, Required: true, Placeholder: "ns=2;s=Device.Channel.Tag1"},
		},
	}
}

func parseConfig(cfg map[string]interface{}) (OPCUAConfig, error) {
	endpoint, _ := cfg["endpoint"].(string)
	if endpoint == "" {
		return OPCUAConfig{}, errors.New("endpoint required")
	}
	policy, _ := cfg["policy"].(string)
	if policy == "" {
		policy = "None"
	}
	mode, _ := cfg["mode"].(string)
	if mode == "" {
		mode = "None"
	}
	auth, _ := cfg["auth"].(string)
	if auth == "" {
		auth = "Anonymous"
	}
	username, _ := cfg["username"].(string)
	password, _ := cfg["password"].(string)

	timeout := 10 * time.Second
	if tStr, ok := cfg["timeout"].(string); ok && tStr != "" {
		if d, err := time.ParseDuration(tStr); err == nil {
			timeout = d
		}
	}
	if timeout < 5*time.Second {
		timeout = 5 * time.Second
	}

	return OPCUAConfig{
		Endpoint: endpoint,
		Policy:   policy,
		Mode:     mode,
		Auth:     auth,
		Username: username,
		Password: password,
		Timeout:  timeout,
	}, nil
}

func getSecurityPolicy(policy string) string {
	switch policy {
	case "Basic256Sha256":
		return "http://opcfoundation.org/UA/SecurityPolicy#Basic256Sha256"
	case "Basic128Rsa15":
		return "http://opcfoundation.org/UA/SecurityPolicy#Basic128Rsa15"
	default:
		return "http://opcfoundation.org/UA/SecurityPolicy#None"
	}
}

func getMessageSecurityMode(mode string) ua.MessageSecurityMode {
	switch mode {
	case "Sign":
		return ua.MessageSecurityModeSign
	case "SignAndEncrypt":
		return ua.MessageSecurityModeSignAndEncrypt
	default:
		return ua.MessageSecurityModeNone
	}
}

// Name 驱动名称。
func (d *driverImpl) Name() string {
	return d.name
}

// Connect 建立连接并开启会话。
func (d *driverImpl) Connect(ctx context.Context) error {
	d.mu.Lock()
	if d.client != nil {
		_ = d.client.Close(ctx)
		d.client = nil
	}
	if !d.nextConnect.IsZero() && time.Now().Before(d.nextConnect) {
		next := d.nextConnect
		d.mu.Unlock()
		return fmt.Errorf("opcua reconnect delayed until %s", next.Format(time.RFC3339))
	}
	d.mu.Unlock()

	opts := []opcua.Option{
		opcua.SecurityPolicy(getSecurityPolicy(d.cfg.Policy)),
		opcua.SecurityMode(getMessageSecurityMode(d.cfg.Mode)),
	}
	if d.cfg.Auth == "Username" {
		opts = append(opts, opcua.AuthUsername(d.cfg.Username, d.cfg.Password))
	} else {
		opts = append(opts, opcua.AuthAnonymous())
	}
	if d.cfg.Timeout > 0 {
		opts = append(opts, opcua.RequestTimeout(d.cfg.Timeout))
	}

	c, err := opcua.NewClient(d.cfg.Endpoint, opts...)
	if err != nil {
		d.mu.Lock()
		d.nextConnect = time.Now().Add(d.reconnectDelay)
		d.lastError = err.Error()
		d.mu.Unlock()
		return fmt.Errorf("new opcua client failed: %w", err)
	}

	connectCtx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
	defer cancel()

	if err := c.Connect(connectCtx); err != nil {
		d.mu.Lock()
		d.nextConnect = time.Now().Add(d.reconnectDelay)
		d.lastError = err.Error()
		d.mu.Unlock()
		return fmt.Errorf("opcua client connect failed: %w", err)
	}

	d.mu.Lock()
	d.client = c
	d.nextConnect = time.Time{}
	d.lastError = ""
	d.lastTime = time.Now()
	d.mu.Unlock()
	return nil
}

// ReadTags 并发批量读取。
func (d *driverImpl) ReadTags(ctx context.Context, tags []core.Tag) ([]core.TagValue, error) {
	d.mu.Lock()
	client := d.client
	d.mu.Unlock()

	if client == nil {
		if err := d.Connect(ctx); err != nil {
			out := make([]core.TagValue, len(tags))
			for i, t := range tags {
				out[i] = core.TagValue{
					TagID:   t.ID,
					Name:    t.Name,
					Quality: core.QualityBad,
					Error:   err.Error(),
					Time:    time.Now(),
				}
			}
			return out, nil
		}
		d.mu.Lock()
		client = d.client
		d.mu.Unlock()
	}

	out := make([]core.TagValue, len(tags))
	var nodesToRead []*ua.ReadValueID
	for _, t := range tags {
		nodeStr, _ := t.Config["nodeId"].(string)
		nodeID, err := ua.ParseNodeID(nodeStr)
		if err != nil {
			nodesToRead = append(nodesToRead, nil)
			continue
		}
		nodesToRead = append(nodesToRead, &ua.ReadValueID{
			NodeID:      nodeID,
			AttributeID: ua.AttributeIDValue,
		})
	}

	var readNodes []*ua.ReadValueID
	var indices []int
	for i, rNode := range nodesToRead {
		if rNode != nil {
			readNodes = append(readNodes, rNode)
			indices = append(indices, i)
		} else {
			out[i] = core.TagValue{
				TagID:   tags[i].ID,
				Name:    tags[i].Name,
				Quality: core.QualityBad,
				Error:   "invalid nodeId configuration",
				Time:    time.Now(),
			}
		}
	}

	if len(readNodes) == 0 {
		return out, nil
	}

	req := &ua.ReadRequest{
		NodesToRead: readNodes,
	}

	resp, err := client.Read(ctx, req)
	if err != nil {
		d.mu.Lock()
		if d.client == client {
			_ = client.Close(ctx)
			d.client = nil
		}
		d.lastError = err.Error()
		d.stats["err"]++
		d.nextConnect = time.Now().Add(d.reconnectDelay)
		d.mu.Unlock()
		return nil, err
	}

	if resp.Results == nil || len(resp.Results) != len(readNodes) {
		return nil, errors.New("invalid read response from server")
	}

	d.mu.Lock()
	d.lastTime = time.Now()
	d.mu.Unlock()

	for idx, res := range resp.Results {
		originalIdx := indices[idx]
		tag := tags[originalIdx]

		if res.Status != ua.StatusOK {
			out[originalIdx] = core.TagValue{
				TagID:   tag.ID,
				Name:    tag.Name,
				Quality: core.QualityBad,
				Error:   fmt.Sprintf("status: %v", res.Status),
				Time:    time.Now(),
			}
			continue
		}

		val := res.Value.Value()
		out[originalIdx] = core.TagValue{
			TagID:   tag.ID,
			Name:    tag.Name,
			Value:   val,
			Quality: core.QualityGood,
			Time:    time.Now(),
		}
		d.mu.Lock()
		d.stats["ok.4"]++
		d.mu.Unlock()
	}

	return out, nil
}

// WriteTag 写入单点位。
func (d *driverImpl) WriteTag(ctx context.Context, tag core.Tag, value interface{}) error {
	d.mu.Lock()
	client := d.client
	d.mu.Unlock()

	if client == nil {
		if err := d.Connect(ctx); err != nil {
			return err
		}
		d.mu.Lock()
		client = d.client
		d.mu.Unlock()
	}

	nodeStr, _ := tag.Config["nodeId"].(string)
	nodeID, err := ua.ParseNodeID(nodeStr)
	if err != nil {
		return fmt.Errorf("invalid nodeId: %w", err)
	}

	variant, err := ua.NewVariant(value)
	if err != nil {
		return fmt.Errorf("invalid value for write: %w", err)
	}

	req := &ua.WriteRequest{
		NodesToWrite: []*ua.WriteValue{
			{
				NodeID:      nodeID,
				AttributeID: ua.AttributeIDValue,
				Value: &ua.DataValue{
					Value: variant,
				},
			},
		},
	}

	resp, err := client.Write(ctx, req)
	if err != nil {
		d.mu.Lock()
		if d.client == client {
			_ = client.Close(ctx)
			d.client = nil
		}
		d.lastError = err.Error()
		d.nextConnect = time.Now().Add(d.reconnectDelay)
		d.mu.Unlock()
		return err
	}

	if len(resp.Results) == 0 {
		return errors.New("no result from server")
	}

	if resp.Results[0] != ua.StatusOK {
		return fmt.Errorf("write failed with status: %v", resp.Results[0])
	}

	return nil
}

// Disconnect 优雅释放 Session 连接。
func (d *driverImpl) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nextConnect = time.Time{}
	if d.client != nil {
		err := d.client.Close(context.Background())
		d.client = nil
		return err
	}
	return nil
}

// Status 获取实时网路连接与异常指标。
func (d *driverImpl) Status() core.DriverStatus {
	d.mu.Lock()
	defer d.mu.Unlock()

	connected := false
	if d.client != nil {
		connected = d.lastError == ""
	}

	statsCopy := make(map[string]int64, len(d.stats))
	for k, v := range d.stats {
		statsCopy[k] = v
	}

	return core.DriverStatus{
		Connected: connected,
		LastError: d.lastError,
		LastTime:  d.lastTime,
		Stats:     statsCopy,
	}
}

func (d *driverImpl) getOrConnect(ctx context.Context) (*opcua.Client, error) {
	d.mu.Lock()
	client := d.client
	d.mu.Unlock()

	if client != nil {
		return client, nil
	}

	if err := d.Connect(ctx); err != nil {
		return nil, err
	}

	d.mu.Lock()
	client = d.client
	d.mu.Unlock()

	if client == nil {
		return nil, errors.New("opcua client not connected")
	}
	return client, nil
}

// Browse 浏览指定的节点，返回其子节点。
func (d *driverImpl) Browse(ctx context.Context, nodeId string) ([]core.NodeItem, error) {
	client, err := d.getOrConnect(ctx)
	if err != nil {
		return nil, err
	}

	var id *ua.NodeID
	if nodeId == "" {
		id = ua.NewNumericNodeID(0, 85) // ObjectsFolder i=85
	} else {
		id, err = ua.ParseNodeID(nodeId)
		if err != nil {
			return nil, fmt.Errorf("invalid nodeId: %w", err)
		}
	}

	req := &ua.BrowseRequest{
		NodesToBrowse: []*ua.BrowseDescription{
			{
				NodeID:          id,
				BrowseDirection: ua.BrowseDirectionForward,
				ReferenceTypeID: ua.NewNumericNodeID(0, 33), // HierarchicalReferences i=33
				IncludeSubtypes: true,
				ResultMask:      uint32(ua.BrowseResultMaskAll),
			},
		},
	}

	resp, err := client.Browse(ctx, req)
	if err != nil {
		// 第一次失败，可能是连接空闲太久断开了，我们重连重试一次
		d.mu.Lock()
		if d.client != nil {
			_ = d.client.Close(ctx)
			d.client = nil
		}
		d.mu.Unlock()

		client, err = d.getOrConnect(ctx)
		if err != nil {
			return nil, fmt.Errorf("reconnect failed: %w", err)
		}
		resp, err = client.Browse(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("opcua browse retry failed: %w", err)
		}
	}

	if len(resp.Results) == 0 {
		return nil, nil
	}

	result := resp.Results[0]
	if result.StatusCode != ua.StatusOK {
		return nil, fmt.Errorf("browse failed with status: %v", result.StatusCode)
	}

	var items []core.NodeItem
	var varNodes []*ua.NodeID
	var varIdxs []int

	for _, ref := range result.References {
		refNodeID := ref.NodeID.NodeID
		if refNodeID == nil {
			continue
		}
		name := ""
		if ref.BrowseName != nil {
			name = ref.BrowseName.Name
		}
		if name == "" {
			name = refNodeID.String()
		}

		isFolder := ref.NodeClass != ua.NodeClassVariable
		nodeType := "string" // 默认值
		nodeIDStr := refNodeID.String()

		item := core.NodeItem{
			ID:          nodeIDStr,
			Name:        name,
			Folder:      isFolder,
			Type:        nodeType,
			AccessModes: []string{"read"},
			Configuration: map[string]interface{}{
				"nodeId":   nodeIDStr,
				"interval": 3000,
				"type":     nodeType,
			},
		}

		items = append(items, item)
		if !isFolder {
			// 必须通过 ParseNodeID 重新解析 NodeID，以清理从 ExpandedNodeID 中遗留的 mask 脏标志位（如 0x40 NamespaceURI 标志），
			// 否则在后续的 Read 请求中，脏 mask 会导致 OPC UA 服务端反序列化失败并直接断开连接 (EOF)。
			cleanNodeID, parseErr := ua.ParseNodeID(nodeIDStr)
			if parseErr == nil {
				varNodes = append(varNodes, cleanNodeID)
			} else {
				varNodes = append(varNodes, refNodeID)
			}
			varIdxs = append(varIdxs, len(items)-1)
		}
	}

	// 批量读取变量节点的数据类型
	if len(varNodes) > 0 {
		var readNodes []*ua.ReadValueID
		for _, n := range varNodes {
			readNodes = append(readNodes, &ua.ReadValueID{
				NodeID:      n,
				AttributeID: ua.AttributeIDDataType,
			})
		}
		readReq := &ua.ReadRequest{
			NodesToRead: readNodes,
		}
		readResp, err := client.Read(ctx, readReq)
		if err != nil {
			// 如果读取失败，也尝试重连重新读取一次
			d.mu.Lock()
			if d.client != nil {
				_ = d.client.Close(ctx)
				d.client = nil
			}
			d.mu.Unlock()

			client, err = d.getOrConnect(ctx)
			if err == nil {
				readResp, err = client.Read(ctx, readReq)
			}
		}

		if err == nil && len(readResp.Results) == len(varNodes) {
			for i, res := range readResp.Results {
				if res.Status == ua.StatusOK && res.Value != nil {
					var dtNodeID *ua.NodeID
					if id, ok := res.Value.Value().(*ua.NodeID); ok {
						dtNodeID = id
					} else if id, ok := res.Value.Value().(ua.NodeID); ok {
						dtNodeID = &id
					}

					if dtNodeID != nil {
						dataTypeStr := mapDataType(dtNodeID)
						idx := varIdxs[i]
						items[idx].Type = dataTypeStr
						items[idx].Configuration["type"] = dataTypeStr
					}
				}
			}
		}
	}

	return items, nil
}

func mapDataType(id *ua.NodeID) string {
	if id.Namespace() != 0 {
		return "string"
	}
	switch id.IntID() {
	case 1: // Boolean
		return "bool"
	case 2: // SByte
		return "int16"
	case 3: // Byte
		return "uint16"
	case 4: // Int16
		return "int16"
	case 5: // UInt16
		return "uint16"
	case 6: // Int32
		return "int32"
	case 7: // UInt32
		return "uint32"
	case 8: // Int64
		return "int64"
	case 9: // UInt64
		return "uint64"
	case 10: // Float
		return "float32"
	case 11: // Double
		return "float64"
	case 12: // String
		return "string"
	case 13: // DateTime
		return "string"
	case 15: // ByteString
		return "bytes"
	default:
		return "string"
	}
}
