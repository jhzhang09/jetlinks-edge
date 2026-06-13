// Package modbuslib 提供 Modbus 协议数据编解码工具与最小可用的 TCP 客户端。
//
// Modbus 寄存器地址约定（与 modbus-tools 一致）：
//   - 0xxxxx : 线圈（Coil），读写，FC01/05/15
//   - 1xxxxx : 离散输入（Discrete Input），只读，FC02
//   - 3xxxxx : 输入寄存器（Input Register），只读，FC04
//   - 4xxxxx : 保持寄存器（Holding Register），读写，FC03/06/16
//
// 地址转换：5 位数字 "40001" 表示 4 区第 1 个寄存器（0-based: 0）。
//
// TCP 帧格式（MBAP header + PDU）：
//
//	0   1   2   3   4   5   6   7  ...
//	[TID  ][PID ][LEN  ][UID ][FC ...]
//	- TID: 事务 ID，每次请求 +1
//	- PID: 协议 ID，Modbus 固定 0
//	- LEN: 后续字节数（UID + FC + 数据），大端
//	- UID: 从站号
//	- FC:  功能码
package modbuslib

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Area Modbus 寄存器区域。
type Area int

const (
	AreaCoil          Area = 0 // 0xxxxx
	AreaDiscreteInput Area = 1 // 1xxxxx
	AreaInputRegister Area = 3 // 3xxxxx
	AreaHolding       Area = 4 // 4xxxxx
)

// Address Modbus 地址：区号 + 偏移。
type Address struct {
	Area   Area
	Offset uint16 // 0-based
}

// ParseAddress 解析 5 位 Modbus 地址（PLC 风格）。
//
//	"00001" -> {AreaCoil, 0}
//	"10001" -> {AreaDiscreteInput, 0}
//	"30001" -> {AreaInputRegister, 0}
//	"40001" -> {AreaHolding, 0}
//	"40100" -> {AreaHolding, 99}
func ParseAddress(s string) (Address, error) {
	s = strings.TrimSpace(s)
	if len(s) != 5 {
		return Address{}, fmt.Errorf("invalid modbus address: %q", s)
	}
	switch s[0] {
	case '0':
		return parseIn(s, AreaCoil)
	case '1':
		return parseIn(s, AreaDiscreteInput)
	case '3':
		return parseIn(s, AreaInputRegister)
	case '4':
		return parseIn(s, AreaHolding)
	default:
		return Address{}, fmt.Errorf("unsupported modbus area: %q", s)
	}
}

func parseIn(s string, area Area) (Address, error) {
	if len(s) < 2 {
		return Address{}, fmt.Errorf("invalid address: %q", s)
	}
	var offset uint32
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return Address{}, fmt.Errorf("invalid digit in address: %q", s)
		}
		offset = offset*10 + uint32(c-'0')
	}
	if offset == 0 {
		return Address{}, fmt.Errorf("modbus address offset must start from 1: %q", s)
	}
	if offset > math.MaxUint16+1 {
		return Address{}, fmt.Errorf("offset overflow: %d", offset)
	}
	return Address{Area: area, Offset: uint16(offset - 1)}, nil
}

// IsRead 判断该区域是否可读（所有都支持读）。
func (a Address) IsRead() bool { return true }

// IsWrite 判断该区域是否可写。
func (a Address) IsWrite() bool {
	return a.Area == AreaCoil || a.Area == AreaHolding
}

// String 还原为 5 位字符串。
func (a Address) String() string {
	return fmt.Sprintf("%d%04d", a.Area, a.Offset+1)
}

// ============ TCP 客户端 ============

// TCPClient Modbus TCP 客户端。
// 并发安全：通过互斥锁串行化请求（Modbus 协议本身在同一条链路上也不可并行）。
type TCPClient struct {
	addr     string
	timeout  time.Duration
	idleTime time.Duration
	slaveID  byte
	mu       sync.Mutex
	conn     net.Conn
	tid      uint32
	lastUse  time.Time
}

// NewTCPClient 创建一个 Modbus TCP 客户端。
func NewTCPClient(host string, port int, slaveID byte, timeout, idleTimeout time.Duration) *TCPClient {
	return &TCPClient{
		addr:     fmt.Sprintf("%s:%d", host, port),
		timeout:  timeout,
		idleTime: idleTimeout,
		slaveID:  slaveID,
	}
}

// Connect 主动建立连接（如已连接则复用）。
func (c *TCPClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connectLocked()
}

func (c *TCPClient) connectLocked() error {
	if c.conn != nil {
		return nil
	}
	conn, err := net.DialTimeout("tcp", c.addr, c.timeout)
	if err != nil {
		return fmt.Errorf("dial modbus: %w", err)
	}
	c.conn = conn
	c.lastUse = time.Now()
	return nil
}

// Close 关闭连接。
func (c *TCPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Connected 是否已连接。
func (c *TCPClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// checkIdle 超时则关闭连接。
func (c *TCPClient) checkIdle() {
	if c.conn == nil {
		return
	}
	if time.Since(c.lastUse) > c.idleTime {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// request 发送请求并读取响应。
func (c *TCPClient) request(pdu []byte) ([]byte, error) {
	return c.requestWithSlaveID(c.slaveID, pdu)
}

// requestWithSlaveID 发送请求并读取响应，允许动态覆盖 UnitID / slaveID。
func (c *TCPClient) requestWithSlaveID(slaveID byte, pdu []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idleTime > 0 {
		c.checkIdle()
	}
	if err := c.connectLocked(); err != nil {
		return nil, err
	}
	tid := atomic.AddUint32(&c.tid, 1)

	// MBAP header
	hdr := make([]byte, 7)
	binary.BigEndian.PutUint16(hdr[0:2], uint16(tid))
	binary.BigEndian.PutUint16(hdr[2:4], 0) // protocol id
	binary.BigEndian.PutUint16(hdr[4:6], uint16(len(pdu)+1))
	hdr[6] = slaveID
	frame := append(hdr, pdu...)

	_ = c.conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := c.conn.Write(frame); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, fmt.Errorf("write modbus: %w", err)
	}

	// 读 MBAP 头
	rhdr := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, rhdr); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, fmt.Errorf("read mbap: %w", err)
	}
	plen := binary.BigEndian.Uint16(rhdr[4:6])
	if binary.BigEndian.Uint16(rhdr[0:2]) != uint16(tid) {
		return nil, errors.New("modbus transaction id mismatch")
	}
	if binary.BigEndian.Uint16(rhdr[2:4]) != 0 {
		return nil, errors.New("invalid modbus protocol id")
	}
	if rhdr[6] != slaveID {
		return nil, errors.New("modbus unit id mismatch")
	}
	if plen < 2 || plen > 254 {
		return nil, errors.New("invalid modbus response length")
	}
	body := make([]byte, plen-1) // 已读 1 字节 (UID)
	if _, err := io.ReadFull(c.conn, body); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return nil, fmt.Errorf("read body: %w", err)
	}
	c.lastUse = time.Now()
	// body[0] = FC
	if body[0] != pdu[0] && body[0] != pdu[0]|0x80 {
		return nil, errors.New("modbus function code mismatch")
	}
	if body[0]&0x80 != 0 {
		// 异常
		ec := byte(0)
		if len(body) > 1 {
			ec = body[1]
		}
		return nil, fmt.Errorf("modbus exception: fc=%x code=%d", body[0]&^0x80, ec)
	}
	return body[1:], nil
}

// ============ FC 实现 ============

// ReadCoils FC01。
func (c *TCPClient) ReadCoils(addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 2000 {
		return nil, errors.New("quantity out of range (1-2000)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x01
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.request(pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, (int(quantity)+7)/8)
}

// ReadDiscreteInputs FC02。
func (c *TCPClient) ReadDiscreteInputs(addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 2000 {
		return nil, errors.New("quantity out of range (1-2000)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x02
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.request(pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, (int(quantity)+7)/8)
}

// ReadHoldingRegisters FC03。
func (c *TCPClient) ReadHoldingRegisters(addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 125 {
		return nil, errors.New("quantity out of range (1-125)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x03
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.request(pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, int(quantity)*2)
}

// ReadInputRegisters FC04。
func (c *TCPClient) ReadInputRegisters(addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 125 {
		return nil, errors.New("quantity out of range (1-125)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x04
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.request(pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, int(quantity)*2)
}

func responseData(resp []byte, expected int) ([]byte, error) {
	if len(resp) < 1 {
		return nil, errors.New("empty response")
	}
	byteCount := int(resp[0])
	if byteCount != expected {
		return nil, fmt.Errorf("byte count mismatch: got %d want %d", byteCount, expected)
	}
	if len(resp) != byteCount+1 {
		return nil, fmt.Errorf("response data length mismatch: got %d want %d", len(resp)-1, byteCount)
	}
	return resp[1:], nil
}

// WriteSingleCoil FC05。
func (c *TCPClient) WriteSingleCoil(addr uint16, value uint16) error {
	pdu := make([]byte, 5)
	pdu[0] = 0x05
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], value)
	_, err := c.request(pdu)
	return err
}

// WriteSingleRegister FC06。
func (c *TCPClient) WriteSingleRegister(addr uint16, value uint16) error {
	pdu := make([]byte, 5)
	pdu[0] = 0x06
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], value)
	_, err := c.request(pdu)
	return err
}

// WriteMultipleCoils FC15。
func (c *TCPClient) WriteMultipleCoils(addr uint16, values []bool) error {
	n := uint16(len(values))
	if n < 1 || n > 1968 {
		return errors.New("quantity out of range (1-1968)")
	}
	byteCount := (int(n) + 7) / 8
	pdu := make([]byte, 6+byteCount)
	pdu[0] = 0x0F
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], n)
	pdu[5] = byte(byteCount)
	for i, v := range values {
		if v {
			pdu[6+i/8] |= 1 << (uint(i) % 8)
		}
	}
	_, err := c.request(pdu)
	return err
}

// WriteMultipleRegisters FC16。
func (c *TCPClient) WriteMultipleRegisters(addr uint16, values []uint16) error {
	n := uint16(len(values))
	if n < 1 || n > 123 {
		return errors.New("quantity out of range (1-123)")
	}
	pdu := make([]byte, 6+n*2)
	pdu[0] = 0x10
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], n)
	pdu[5] = byte(n * 2)
	for i, v := range values {
		binary.BigEndian.PutUint16(pdu[6+i*2:], v)
	}
	_, err := c.request(pdu)
	return err
}

// ============ 数据编解码 ============

// DecodeBytes 把 N 个寄存器（每个 2 字节）按字节序解为基本类型。
// byteOrder: AB BA ABCD BADC CDAB DCBA
//   - 1 字节类型忽略 byteOrder
//   - 2 字节类型使用前两个字符
//   - 4 字节类型使用全部 4 字符
func DecodeBytes(data []byte, byteOrder string, isUnsigned bool, isFloat bool) (interface{}, error) {
	n := len(data)
	switch n {
	case 1:
		return data[0] != 0, nil
	case 2:
		bo, err := normalizeOrder(byteOrder, "AB")
		if err != nil {
			return nil, err
		}
		bs := reorder(data, bo)
		if isFloat {
			return nil, errors.New("float type requires 4 bytes")
		}
		if isUnsigned {
			return binary.BigEndian.Uint16(bs), nil
		}
		return int16(binary.BigEndian.Uint16(bs)), nil
	case 4:
		bo, err := normalizeOrder(byteOrder, "ABCD")
		if err != nil {
			return nil, err
		}
		bs := reorder(data, bo)
		if isFloat {
			return math.Float32frombits(binary.BigEndian.Uint32(bs)), nil
		}
		if isUnsigned {
			return binary.BigEndian.Uint32(bs), nil
		}
		return int32(binary.BigEndian.Uint32(bs)), nil
	case 8:
		bo, err := normalizeOrder(byteOrder, "ABCDEFGH")
		if err != nil {
			return nil, err
		}
		bs := reorder(data, bo)
		if isFloat {
			return math.Float64frombits(binary.BigEndian.Uint64(bs)), nil
		}
		if isUnsigned {
			return binary.BigEndian.Uint64(bs), nil
		}
		return int64(binary.BigEndian.Uint64(bs)), nil
	default:
		return nil, fmt.Errorf("unsupported decode length: %d bytes", n)
	}
}

// EncodeBytes 把数值编码为 N 字节的 Modbus 寄存器值（每个寄存器 2 字节）。
func EncodeBytes(value interface{}, byteOrder string, isUnsigned bool, isFloat bool, size int) ([]byte, error) {
	if size == 1 {
		b, ok := toByte(value)
		if !ok {
			return nil, fmt.Errorf("cannot convert %T to byte", value)
		}
		return []byte{b}, nil
	}
	if size == 2 {
		bo, err := normalizeOrder(byteOrder, "AB")
		if err != nil {
			return nil, err
		}
		var bs [2]byte
		if isFloat {
			return nil, errors.New("float type requires 4 bytes")
		}
		if isUnsigned {
			v, ok := toUint16(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to uint16", value)
			}
			binary.BigEndian.PutUint16(bs[:], v)
		} else {
			v, ok := toInt16(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to int16", value)
			}
			binary.BigEndian.PutUint16(bs[:], uint16(v))
		}
		return reorder(bs[:], reverseOrder(bo)), nil
	}
	if size == 4 {
		bo, err := normalizeOrder(byteOrder, "ABCD")
		if err != nil {
			return nil, err
		}
		var bs [4]byte
		if isFloat {
			f, ok := toFloat32(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to float32", value)
			}
			binary.BigEndian.PutUint32(bs[:], math.Float32bits(f))
		} else if isUnsigned {
			v, ok := toUint32(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to uint32", value)
			}
			binary.BigEndian.PutUint32(bs[:], v)
		} else {
			v, ok := toInt32(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to int32", value)
			}
			binary.BigEndian.PutUint32(bs[:], uint32(v))
		}
		return reorder(bs[:], reverseOrder(bo)), nil
	}
	if size == 8 {
		bo, err := normalizeOrder(byteOrder, "ABCDEFGH")
		if err != nil {
			return nil, err
		}
		var bs [8]byte
		if isFloat {
			f, ok := toFloat64(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to float64", value)
			}
			binary.BigEndian.PutUint64(bs[:], math.Float64bits(f))
		} else if isUnsigned {
			v, ok := toUint64(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to uint64", value)
			}
			binary.BigEndian.PutUint64(bs[:], v)
		} else {
			v, ok := toInt64(value)
			if !ok {
				return nil, fmt.Errorf("cannot convert %T to int64", value)
			}
			binary.BigEndian.PutUint64(bs[:], uint64(v))
		}
		return reorder(bs[:], reverseOrder(bo)), nil
	}
	return nil, fmt.Errorf("unsupported encode size: %d", size)
}

func toByte(v interface{}) (byte, bool) {
	switch x := v.(type) {
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case byte:
		return x, true
	case int:
		return byte(x), true
	case int16:
		return byte(x), true
	case int32:
		return byte(x), true
	case int64:
		return byte(x), true
	case uint16:
		return byte(x), true
	case uint32:
		return byte(x), true
	case uint64:
		return byte(x), true
	case float32:
		return byte(x), true
	case float64:
		return byte(x), true
	}
	return 0, false
}

func toInt16(v interface{}) (int16, bool) {
	switch x := v.(type) {
	case int:
		return int16(x), true
	case int16:
		return x, true
	case int32:
		return int16(x), true
	case int64:
		return int16(x), true
	case uint16:
		return int16(x), true
	case uint32:
		return int16(x), true
	case uint64:
		return int16(x), true
	case float32:
		return int16(x), true
	case float64:
		return int16(x), true
	}
	return 0, false
}

func toUint16(v interface{}) (uint16, bool) {
	switch x := v.(type) {
	case int:
		return uint16(x), true
	case int16:
		return uint16(x), true
	case int32:
		return uint16(x), true
	case int64:
		return uint16(x), true
	case uint16:
		return x, true
	case uint32:
		return uint16(x), true
	case uint64:
		return uint16(x), true
	case float32:
		return uint16(x), true
	case float64:
		return uint16(x), true
	}
	return 0, false
}

func toInt32(v interface{}) (int32, bool) {
	switch x := v.(type) {
	case int:
		return int32(x), true
	case int32:
		return x, true
	case int64:
		return int32(x), true
	case uint32:
		return int32(x), true
	case uint64:
		return int32(x), true
	case float32:
		return int32(x), true
	case float64:
		return int32(x), true
	}
	return 0, false
}

func toUint32(v interface{}) (uint32, bool) {
	switch x := v.(type) {
	case int:
		return uint32(x), true
	case int32:
		return uint32(x), true
	case int64:
		return uint32(x), true
	case uint32:
		return x, true
	case uint64:
		return uint32(x), true
	case float32:
		return uint32(x), true
	case float64:
		return uint32(x), true
	}
	return 0, false
}

func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint64:
		return int64(x), true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

func toUint64(v interface{}) (uint64, bool) {
	switch x := v.(type) {
	case int:
		return uint64(x), true
	case int32:
		return uint64(x), true
	case int64:
		return uint64(x), true
	case uint64:
		return x, true
	case float32:
		return uint64(x), true
	case float64:
		return uint64(x), true
	}
	return 0, false
}

func toFloat32(v interface{}) (float32, bool) {
	switch x := v.(type) {
	case int:
		return float32(x), true
	case int32:
		return float32(x), true
	case int64:
		return float32(x), true
	case float32:
		return x, true
	case float64:
		return float32(x), true
	}
	return 0, false
}

func toFloat64(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func normalizeOrder(order, expected string) (string, error) {
	order = strings.ToUpper(strings.TrimSpace(order))
	if order == "" {
		order = expected
	}
	if len(order) != len(expected) {
		return "", fmt.Errorf("invalid byte order %q for %d bytes", order, len(expected))
	}
	seen := make(map[byte]struct{}, len(order))
	for i := 0; i < len(order); i++ {
		ch := order[i]
		if ch < 'A' || int(ch-'A') >= len(order) {
			return "", fmt.Errorf("invalid byte order %q", order)
		}
		if _, exists := seen[ch]; exists {
			return "", fmt.Errorf("invalid byte order %q", order)
		}
		seen[ch] = struct{}{}
	}
	return order, nil
}

func reorder(data []byte, order string) []byte {
	if len(order) == len(data) {
		out := make([]byte, len(data))
		for i := 0; i < len(order); i++ {
			out[i] = data[order[i]-'A']
		}
		return out
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out
}

func reverseOrder(order string) string {
	rs := []rune(order)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

// ============ WithSlaveID 实现 ============

func (c *TCPClient) ReadCoilsWithSlaveID(slaveID byte, addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 2000 {
		return nil, errors.New("quantity out of range (1-2000)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x01
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.requestWithSlaveID(slaveID, pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, (int(quantity)+7)/8)
}

func (c *TCPClient) ReadDiscreteInputsWithSlaveID(slaveID byte, addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 2000 {
		return nil, errors.New("quantity out of range (1-2000)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x02
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.requestWithSlaveID(slaveID, pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, (int(quantity)+7)/8)
}

func (c *TCPClient) ReadHoldingRegistersWithSlaveID(slaveID byte, addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 125 {
		return nil, errors.New("quantity out of range (1-125)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x03
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.requestWithSlaveID(slaveID, pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, int(quantity)*2)
}

func (c *TCPClient) ReadInputRegistersWithSlaveID(slaveID byte, addr, quantity uint16) ([]byte, error) {
	if quantity < 1 || quantity > 125 {
		return nil, errors.New("quantity out of range (1-125)")
	}
	pdu := make([]byte, 5)
	pdu[0] = 0x04
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], quantity)
	resp, err := c.requestWithSlaveID(slaveID, pdu)
	if err != nil {
		return nil, err
	}
	return responseData(resp, int(quantity)*2)
}

func (c *TCPClient) WriteSingleCoilWithSlaveID(slaveID byte, addr uint16, value uint16) error {
	pdu := make([]byte, 5)
	pdu[0] = 0x05
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], value)
	_, err := c.requestWithSlaveID(slaveID, pdu)
	return err
}

func (c *TCPClient) WriteSingleRegisterWithSlaveID(slaveID byte, addr uint16, value uint16) error {
	pdu := make([]byte, 5)
	pdu[0] = 0x06
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], value)
	_, err := c.requestWithSlaveID(slaveID, pdu)
	return err
}

func (c *TCPClient) WriteMultipleRegistersWithSlaveID(slaveID byte, addr uint16, values []uint16) error {
	n := uint16(len(values))
	if n < 1 || n > 123 {
		return errors.New("quantity out of range (1-123)")
	}
	pdu := make([]byte, 6+n*2)
	pdu[0] = 0x10
	binary.BigEndian.PutUint16(pdu[1:3], addr)
	binary.BigEndian.PutUint16(pdu[3:5], n)
	pdu[5] = byte(n * 2)
	for i, v := range values {
		binary.BigEndian.PutUint16(pdu[6+i*2:8+i*2], v)
	}
	_, err := c.requestWithSlaveID(slaveID, pdu)
	return err
}
