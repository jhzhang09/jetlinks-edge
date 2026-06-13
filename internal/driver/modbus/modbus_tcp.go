// Package modbus 实现 Modbus TCP 南向驱动（RTU 预留）。
//
// 寄存器类型与功能码映射：
//   - 线圈 (0xxxxx):     读 FC01 / 写单 FC05 / 写多 FC15
//   - 离散输入 (1xxxxx): 读 FC02
//   - 输入寄存器 (3xxxxx): 读 FC04
//   - 保持寄存器 (4xxxxx): 读 FC03 / 写单 FC06 / 写多 FC16
//
// 性能优化（不同于按点轮询的"天真的 Modbus"实现）：
//   - 按 (Area, UnitId) 把点位分组
//   - 同区域连续地址合并为单次多寄存器读/写
//   - 离散读取区间之间的 gap 通过多次请求补齐
package modbus

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/pkg/modbuslib"
)

// DriverName 驱动名。
const DriverName = "modbus-tcp"

// driverImpl Modbus TCP 驱动实例。
type driverImpl struct {
	name   string
	cfg    ModbusConfig
	mu     sync.Mutex
	client *modbuslib.TCPClient
	stats  map[string]int64

	reconnectDelay time.Duration
	nextConnect    time.Time
	lastError      string
	lastTime       time.Time
}

// ModbusConfig Modbus TCP 连接配置。
type ModbusConfig struct {
	Host        string        `json:"host"`        // 设备 IP
	Port        int           `json:"port"`        // 端口（默认 502）
	UnitID      byte          `json:"unitId"`      // 从站号（默认 1）
	Timeout     time.Duration `json:"timeout"`     // 单次请求超时（默认 3s）
	IdleTimeout time.Duration `json:"idleTimeout"` // 连接空闲超时（默认 60s）
}

// NewDriver 通过注册表工厂函数创建驱动。
func NewDriver(ctx context.Context, name string, cfg core.DriverConfig) (core.SouthDriver, error) {
	mc, err := parseConfig(cfg.Config)
	if err != nil {
		return nil, err
	}
	if _, configured := cfg.Config["timeout"]; !configured && cfg.ReadTimeout > 0 {
		mc.Timeout = cfg.ReadTimeout
	}
	reconnectDelay := cfg.ReconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = 5 * time.Second
	}
	return &driverImpl{
		name:           name,
		cfg:            mc,
		stats:          map[string]int64{},
		reconnectDelay: reconnectDelay,
	}, nil
}

// Register 把驱动工厂注册到 registry。
func Register(r *core.DriverRegistry) {
	r.RegisterExtension(Descriptor(), NewDriver)
}

// Descriptor 返回 Modbus TCP 编译期插件描述符。
func Descriptor() core.ExtensionDescriptor {
	return core.ExtensionDescriptor{
		Type:         DriverName,
		Name:         "Modbus TCP",
		Description:  "通过 Modbus TCP 周期采集和写入设备点位",
		Version:      "1.0.0",
		Capabilities: []string{"polling", "read", "write"},
		ConnectionSchema: []core.ConfigField{
			{Key: "host", Label: "主机", Type: core.ConfigFieldText, Required: true, Placeholder: "127.0.0.1"},
			{Key: "port", Label: "端口", Type: core.ConfigFieldNumber, Required: true, DefaultValue: 502, Min: numberPointer(1), Max: numberPointer(65535)},
			{Key: "timeout", Label: "请求超时", Type: core.ConfigFieldText, DefaultValue: "3s", Placeholder: "3s"},
			{Key: "idleTimeout", Label: "空闲超时", Type: core.ConfigFieldText, DefaultValue: "60s", Placeholder: "60s"},
		},
		ConfigSchema: []core.ConfigField{
			{Key: "unitId", Label: "从站号", Type: core.ConfigFieldNumber, Required: true, DefaultValue: 1, Min: numberPointer(1), Max: numberPointer(255)},
		},
		TagSchema: []core.ConfigField{
			{Key: "address", Label: "地址", Type: core.ConfigFieldText, Required: true, DefaultValue: "40001", Placeholder: "40001"},
			{Key: "byteOrder", Label: "字节序", Type: core.ConfigFieldSelect, Required: true, DefaultValue: "AB", Options: []core.ConfigOption{
				{Label: "AB", Value: "AB"}, {Label: "BA", Value: "BA"}, {Label: "ABCD", Value: "ABCD"},
				{Label: "BADC", Value: "BADC"}, {Label: "CDAB", Value: "CDAB"}, {Label: "DCBA", Value: "DCBA"},
			}},
			{Key: "bit", Label: "位", Type: core.ConfigFieldNumber, DefaultValue: 0, Min: numberPointer(0), Max: numberPointer(15)},
			{Key: "length", Label: "长度(寄存器数)", Type: core.ConfigFieldNumber, DefaultValue: 1, Min: numberPointer(1), Max: numberPointer(100), Description: "读取 String/Bytes 类型时的寄存器个数（每个寄存器占2字节）"},
		},
	}
}

func numberPointer(value float64) *float64 {
	return &value
}

func parseConfig(m map[string]interface{}) (ModbusConfig, error) {
	get := func(k string) string {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
	host := get("host")
	if host == "" {
		return ModbusConfig{}, errors.New("modbus: host is required")
	}
	port := 502
	if p, ok := m["port"]; ok {
		switch v := p.(type) {
		case int:
			port = v
		case int32:
			port = int(v)
		case int64:
			port = int(v)
		case float64:
			port = int(v)
		}
	}
	unitID := byte(1)
	if u, ok := m["unitId"]; ok {
		switch v := u.(type) {
		case int:
			unitID = byte(v)
		case int32:
			unitID = byte(v)
		case int64:
			unitID = byte(v)
		case float64:
			unitID = byte(v)
		}
	}
	timeout := 3 * time.Second
	if t, ok := m["timeout"]; ok {
		if d, ok := parseDuration(t); ok {
			timeout = d
		}
	}
	idle := 60 * time.Second
	if t, ok := m["idleTimeout"]; ok {
		if d, ok := parseDuration(t); ok {
			idle = d
		}
	}
	return ModbusConfig{
		Host: host, Port: port, UnitID: unitID,
		Timeout: timeout, IdleTimeout: idle,
	}, nil
}

func parseDuration(v interface{}) (time.Duration, bool) {
	switch x := v.(type) {
	case string:
		d, err := time.ParseDuration(x)
		return d, err == nil
	case int:
		return time.Duration(x) * time.Millisecond, true
	case int32:
		return time.Duration(x) * time.Millisecond, true
	case int64:
		return time.Duration(x) * time.Millisecond, true
	case float64:
		return time.Duration(x) * time.Millisecond, true
	}
	return 0, false
}

// Name 驱动名。
func (d *driverImpl) Name() string { return d.name }

// Connect 建立 TCP 连接。
func (d *driverImpl) Connect(ctx context.Context) error {
	d.mu.Lock()
	if d.client != nil && d.client.Connected() {
		d.mu.Unlock()
		return nil
	}
	if !d.nextConnect.IsZero() && time.Now().Before(d.nextConnect) {
		next := d.nextConnect
		d.mu.Unlock()
		return fmt.Errorf("modbus reconnect delayed until %s", next.Format(time.RFC3339))
	}
	if d.client == nil {
		d.client = modbuslib.NewTCPClient(d.cfg.Host, d.cfg.Port, d.cfg.UnitID, d.cfg.Timeout, d.cfg.IdleTimeout)
	}
	client := d.client
	d.mu.Unlock()

	if err := client.Connect(); err != nil {
		d.mu.Lock()
		d.nextConnect = time.Now().Add(d.reconnectDelay)
		d.lastError = err.Error()
		d.lastTime = time.Now()
		d.mu.Unlock()
		return fmt.Errorf("modbus tcp connect: %w", err)
	}
	d.mu.Lock()
	d.nextConnect = time.Time{}
	d.lastError = ""
	d.lastTime = time.Now()
	d.mu.Unlock()
	zap.L().Info("modbus tcp connected",
		zap.String("host", d.cfg.Host),
		zap.Int("port", d.cfg.Port),
		zap.Int("unitId", int(d.cfg.UnitID)))
	return nil
}

// Disconnect 断开连接。
func (d *driverImpl) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client != nil {
		_ = d.client.Close()
		d.client = nil
	}
	d.lastTime = time.Now()
	return nil
}

// Status 驱动状态。
func (d *driverImpl) Status() core.DriverStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	connected := d.client != nil && d.client.Connected()
	stats := make(map[string]int64, len(d.stats))
	for k, v := range d.stats {
		stats[k] = v
	}
	return core.DriverStatus{
		Connected: connected,
		LastError: d.lastError,
		LastTime:  d.lastTime,
		Stats:     stats,
	}
}

// ReadTags 读取一组点位。
// 把传入的 tags 按区域分组后合并为若干次批量请求，再把结果映射回原顺序。
func (d *driverImpl) ReadTags(ctx context.Context, tags []core.Tag) ([]core.TagValue, error) {
	out := make([]core.TagValue, len(tags))
	for i, tag := range tags {
		out[i] = core.TagValue{TagID: tag.ID, Name: tag.Name}
		if tag.Access == core.AccessWO {
			out[i].Quality = core.QualityBad
			out[i].Error = "tag is write-only"
		}
	}

	unitID := d.cfg.UnitID
	if len(tags) > 0 {
		for _, t := range tags {
			if uVal, ok := asInt(t.Config["unitId"]); ok {
				unitID = byte(uVal)
				break
			}
		}
	}

	groups := groupByArea(tags, out)

	d.mu.Lock()
	c := d.client
	d.mu.Unlock()
	if c == nil || !c.Connected() {
		if err := d.Connect(ctx); err != nil {
			for i := range out {
				if out[i].Quality == "" {
					out[i].Quality = core.QualityBad
					out[i].Error = err.Error()
				}
			}
			return out, nil
		}
		d.mu.Lock()
		c = d.client
		d.mu.Unlock()
	}
	if c == nil {
		for i := range out {
			if out[i].Quality == "" {
				out[i].Quality = core.QualityBad
				out[i].Error = "modbus: not connected"
			}
		}
		return out, nil
	}

	for _, g := range groups {
		d.readGroup(ctx, c, unitID, g, tags, out)
	}
	return out, nil
}

func (d *driverImpl) readGroup(_ context.Context, c *modbuslib.TCPClient, unitID byte, g readGroup, allTags []core.Tag, out []core.TagValue) {
	items := g.items
	sortByOffset(items)
	for _, span := range mergeSpans(items) {
		if err := d.readSpan(c, unitID, g.area, span, allTags, out); err != nil {
			for _, item := range span.items {
				out[item.tagIdx].Quality = core.QualityBad
				out[item.tagIdx].Error = err.Error()
			}
		}
	}
}

func (d *driverImpl) readSpan(c *modbuslib.TCPClient, unitID byte, area modbuslib.Area, span readSpan, allTags []core.Tag, out []core.TagValue) error {
	length := span.end - span.start + 1
	var (
		raw []byte
		err error
	)
	switch area {
	case modbuslib.AreaCoil:
		raw, err = c.ReadCoilsWithSlaveID(unitID, span.start, length)
	case modbuslib.AreaDiscreteInput:
		raw, err = c.ReadDiscreteInputsWithSlaveID(unitID, span.start, length)
	case modbuslib.AreaInputRegister:
		raw, err = c.ReadInputRegistersWithSlaveID(unitID, span.start, length)
	case modbuslib.AreaHolding:
		raw, err = c.ReadHoldingRegistersWithSlaveID(unitID, span.start, length)
	default:
		return fmt.Errorf("unsupported area: %d", area)
	}
	if err != nil {
		d.incErr(err)
		return err
	}
	d.incOK(area)

	for _, it := range span.items {
		t := allTags[it.tagIdx]
		out[it.tagIdx] = decodeOne(t, area, raw, it.startInSpan, it.length)
	}
	return nil
}

// decodeOne 从 raw 缓冲区中按 tag 类型解码一个值。
func decodeOne(t core.Tag, area modbuslib.Area, raw []byte, startInSpan, length uint16) core.TagValue {
	out := core.TagValue{
		TagID:   t.ID,
		Name:    t.Name,
		Quality: core.QualityGood,
	}
	switch area {
	case modbuslib.AreaCoil, modbuslib.AreaDiscreteInput:
		// 纠正合并读取时的位偏移计算：线圈单位是 bit，直接使用偏移值，绝不能乘以 8
		bit := uint(startInSpan) + uint(t.Bit)
		if bit/8 >= uint(len(raw)) {
			out.Quality = core.QualityBad
			out.Error = "bit out of range"
			return out
		}
		v := (raw[bit/8] >> (bit % 8)) & 0x01
		out.Value = v == 1
	case modbuslib.AreaInputRegister, modbuslib.AreaHolding:
		regBytes := int(length) * 2
		off := int(startInSpan) * 2
		if off+regBytes > len(raw) {
			out.Quality = core.QualityBad
			out.Error = "register out of range"
			return out
		}
		slice := raw[off : off+regBytes]
		v, err := decodeByType(t.Type, slice, t.ByteOrder)
		if err != nil {
			out.Quality = core.QualityBad
			out.Error = err.Error()
			return out
		}
		if t.Decimal != 0 && t.Decimal != 1 {
			out.Value = applyDecimal(v, t.Decimal)
		} else {
			out.Value = v
		}
	}
	return out
}

func applyDecimal(v interface{}, decimal float64) interface{} {
	switch x := v.(type) {
	case int16:
		return float64(x) * decimal
	case uint16:
		return float64(x) * decimal
	case int32:
		return float64(x) * decimal
	case uint32:
		return float64(x) * decimal
	case int64:
		return float64(x) * decimal
	case uint64:
		return float64(x) * decimal
	case float32:
		return float64(x) * decimal
	case float64:
		return x * decimal
	}
	return v
}

func decodeByType(typ core.DataType, data []byte, order string) (interface{}, error) {
	switch typ {
	case core.TypeBool:
		if len(data) < 2 {
			return false, nil
		}
		return data[1] != 0, nil
	case core.TypeInt16:
		return modbuslib.DecodeBytes(data, order, false, false)
	case core.TypeUInt16:
		return modbuslib.DecodeBytes(data, order, true, false)
	case core.TypeInt32:
		return modbuslib.DecodeBytes(data, order, false, false)
	case core.TypeUInt32:
		return modbuslib.DecodeBytes(data, order, true, false)
	case core.TypeInt64:
		return modbuslib.DecodeBytes(data, order, false, false)
	case core.TypeUInt64:
		return modbuslib.DecodeBytes(data, order, true, false)
	case core.TypeFloat32:
		return modbuslib.DecodeBytes(data, order, false, true)
	case core.TypeFloat64:
		return modbuslib.DecodeBytes(data, order, false, true)
	case core.TypeString:
		return string(data), nil
	case core.TypeBytes:
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	}
	return nil, fmt.Errorf("unsupported type: %s", typ)
}

// WriteTag 写单个点位。
func (d *driverImpl) WriteTag(ctx context.Context, t core.Tag, value interface{}) error {
	if t.Access == core.AccessRO {
		return core.ErrTagReadOnly
	}
	d.mu.Lock()
	c := d.client
	d.mu.Unlock()
	if c == nil || !c.Connected() {
		if err := d.Connect(ctx); err != nil {
			return err
		}
		d.mu.Lock()
		c = d.client
		d.mu.Unlock()
	}
	addr, err := modbuslib.ParseAddress(string(t.Address))
	if err != nil {
		return err
	}
	if !addr.IsWrite() {
		return fmt.Errorf("address %s is read-only", t.Address)
	}

	unitID := d.cfg.UnitID
	if uVal, ok := asInt(t.Config["unitId"]); ok {
		unitID = byte(uVal)
	}

	return d.writeOne(c, unitID, addr, t, value)
}

func (d *driverImpl) writeOne(c *modbuslib.TCPClient, unitID byte, addr modbuslib.Address, t core.Tag, value interface{}) error {
	switch addr.Area {
	case modbuslib.AreaCoil:
		b, _ := toBool(value)
		var u uint16
		if b {
			u = 0xFF00
		}
		if err := c.WriteSingleCoilWithSlaveID(unitID, addr.Offset, u); err != nil {
			d.incErr(err)
			return err
		}
	case modbuslib.AreaHolding:
		size, isFloat, isUnsigned, err := typeMeta(t.Type)
		if err != nil {
			return err
		}
		bs, err := modbuslib.EncodeBytes(value, t.ByteOrder, isUnsigned, isFloat, size)
		if err != nil {
			return err
		}
		if size <= 2 {
			if err := c.WriteSingleRegisterWithSlaveID(unitID, addr.Offset, binaryBigEndianU16(bs)); err != nil {
				d.incErr(err)
				return err
			}
		} else {
			regs := bytesToU16(bs)
			if err := c.WriteMultipleRegistersWithSlaveID(unitID, addr.Offset, regs); err != nil {
				d.incErr(err)
				return err
			}
		}
	default:
		return fmt.Errorf("area %d is not writable", addr.Area)
	}
	d.incOK(addr.Area)
	return nil
}

func toBool(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int:
		return x != 0, true
	case int32:
		return x != 0, true
	case int64:
		return x != 0, true
	case uint16:
		return x != 0, true
	case uint32:
		return x != 0, true
	case float32:
		return x != 0, true
	case float64:
		return x != 0, true
	}
	return false, false
}

func typeMeta(t core.DataType) (int, bool, bool, error) {
	switch t {
	case core.TypeBool, core.TypeInt16, core.TypeUInt16:
		return 2, false, t == core.TypeUInt16, nil
	case core.TypeInt32, core.TypeUInt32, core.TypeFloat32:
		return 4, t == core.TypeFloat32, t == core.TypeUInt32, nil
	case core.TypeInt64, core.TypeUInt64, core.TypeFloat64:
		return 8, t == core.TypeFloat64, t == core.TypeUInt64, nil
	case core.TypeString, core.TypeBytes:
		return 2, false, false, nil
	}
	return 0, false, false, fmt.Errorf("unsupported write type: %s", t)
}

type readGroup struct {
	area  modbuslib.Area
	items []readItem
}

type readItem struct {
	tagIdx      int
	offset      uint16
	length      uint16
	startInSpan uint16
}

type readSpan struct {
	start uint16
	end   uint16
	items []readItem
}

func groupByArea(tags []core.Tag, out []core.TagValue) map[modbuslib.Area]readGroup {
	groups := map[modbuslib.Area]readGroup{}
	for i, t := range tags {
		if t.Access == core.AccessWO {
			continue
		}
		addr, err := modbuslib.ParseAddress(string(t.Address))
		if err != nil {
			out[i].Quality = core.QualityBad
			out[i].Error = err.Error()
			continue
		}
		length := registerCount(t)
		g := groups[addr.Area]
		g.area = addr.Area
		g.items = append(g.items, readItem{
			tagIdx: i,
			offset: addr.Offset,
			length: length,
		})
		groups[addr.Area] = g
	}
	return groups
}

func registerCount(t core.Tag) uint16 {
	switch t.Type {
	case core.TypeInt16, core.TypeUInt16, core.TypeBool:
		return 1
	case core.TypeInt32, core.TypeUInt32, core.TypeFloat32:
		return 2
	case core.TypeInt64, core.TypeUInt64, core.TypeFloat64:
		return 4
	case core.TypeString, core.TypeBytes:
		if lenVal, ok := asInt(t.Config["length"]); ok && lenVal > 0 {
			return uint16(lenVal)
		}
		return 1
	}
	return 1
}

// asInt 将任意基础数据类型转换为整型。
func asInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		if i, err := strconv.Atoi(x); err == nil {
			return i, true
		}
	}
	return 0, false
}

func sortByOffset(items []readItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j-1].offset > items[j].offset; j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
}

const maxSpanRegs = 120

func mergeSpans(items []readItem) []readSpan {
	var out []readSpan
	if len(items) == 0 {
		return out
	}
	cur := readSpan{start: items[0].offset, end: items[0].offset + items[0].length - 1}
	cur.items = append(cur.items, items[0])
	for i := 1; i < len(items); i++ {
		it := items[i]
		newEnd := it.offset + it.length - 1
		if it.offset <= cur.end+8 && (newEnd-cur.start+1) <= maxSpanRegs {
			if newEnd > cur.end {
				cur.end = newEnd
			}
			cur.items = append(cur.items, it)
		} else {
			finalizeSpan(&cur)
			out = append(out, cur)
			cur = readSpan{start: it.offset, end: newEnd}
			cur.items = append(cur.items, it)
		}
	}
	finalizeSpan(&cur)
	out = append(out, cur)
	return out
}

func finalizeSpan(s *readSpan) {
	for i := range s.items {
		s.items[i].startInSpan = s.items[i].offset - s.start
	}
}

func (d *driverImpl) incOK(area modbuslib.Area) {
	d.mu.Lock()
	d.stats[fmt.Sprintf("ok.%d", area)]++
	d.lastError = ""
	d.lastTime = time.Now()
	d.mu.Unlock()
}

func (d *driverImpl) incErr(err error) {
	d.mu.Lock()
	d.stats["err"]++
	d.lastError = err.Error()
	d.lastTime = time.Now()
	d.mu.Unlock()
}

func binaryBigEndianU16(b []byte) uint16 {
	if len(b) < 2 {
		return 0
	}
	return uint16(b[0])<<8 | uint16(b[1])
}

func bytesToU16(b []byte) []uint16 {
	if len(b)%2 != 0 {
		pad := make([]byte, len(b)+1)
		copy(pad, b)
		b = pad
	}
	out := make([]uint16, len(b)/2)
	for i := 0; i < len(out); i++ {
		out[i] = uint16(b[i*2])<<8 | uint16(b[i*2+1])
	}
	return out
}
