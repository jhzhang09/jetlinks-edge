# JetLinks 集成说明

## 协议与接入模型：网关 + 子设备

JetLinks 平台内置 MQTT Broker（默认端口 11883，可改 1883）。**JetLinks Edge 采用"网关 + 子设备"模式接入**：

- **网关** = 1 个 MQTT 连接 = 1 个 NorthApp 配置实体
- **子设备** = 1 个点组 = 平台上的 1 个 deviceId
- 多个子设备共享同一条 MQTT 连接，平台从消息 topic `/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/...` 识别子设备身份
- 子设备**不需要**自己的 broker 凭据（无 SM3 密码计算），平台根据网关级 token 完成认证

这与 JetLinks 平台的 `ChildDeviceGateway` / `MqttClientDeviceGateway` 行为一致
（参考 `MqttClientDeviceGateway.java:178` 的 `helper.handleDeviceMessage(message, ...)`，
平台从消息 topic 动态发现子设备并创建 session）。

> 平台也支持"每设备 1 个 MQTT 连接"的直连模式（clientId=deviceId、SM3 密码），
> JetLinks Edge **不采用这种模式**（1000 设备要 1000 个连接，规模不可行）。
> 后续阶段可提供 `mode=direct` 选项，pkg/sm3 已预留。

## 实体关系

```
┌──────────────────┐ 1     N ┌──────────────────┐
│  NorthApp (1)    │◀────────│   Group (N)        │
│  (1 个 MQTT 连接)│ shared │  (1 台逻辑设备)    │
│                  │ connect│                    │
│  - broker        │        │  - device:         │
│  - gateway       │        │    {productId,     │
│    username      │        │     deviceId,      │
│  - gateway pwd   │        │     secureKey}     │
└──────────────────┘        └──────────────────┘
```

- **1 个 NorthApp** = 1 个 broker 连接（共享给所有引用它的 Group）
- **N 个 Group** 共享 1 个 NorthApp：每台设备一个 Group
- 设备身份（productId/deviceId）在 Group.DeviceConfig 中，**不在** NorthApp 中

## 主题格式

**已通过官方协议工程 `JetLinksMqttDeviceMessageCodec` 与 `TopicMessageCodec` 验证**：
MQTT 设备接入 Topic 与平台内部 EventBus Topic 不同，边缘网关使用官方 MQTT 子设备 Topic。

| 方向 | Topic | 载荷（JSON） |
|---|---|---|
| 边缘 → 平台 | `/{gwPid}/{gwDid}/child/{childDid}/properties/report` | `{"messageId":"...","properties":{...},"changes":{...},"timestamp":...}` |
| 平台 → 边缘 | `/{gwPid}/{gwDid}/child/{childDid}/properties/read` | `{"messageId":"...","properties":["tag1","tag2"]}` |
| 平台 → 边缘 | `/{gwPid}/{gwDid}/child/{childDid}/properties/write` | `{"messageId":"...","properties":{"tag1":123}}` |
| 平台 → 边缘 | `/{gwPid}/{gwDid}/child/{childDid}/function/invoke` | `{"messageId":"...","functionId":"reset","inputs":[]}` |
| 边缘 → 平台 | `/{gwPid}/{gwDid}/child/{childDid}/properties/read/reply` | `{"messageId":"...","properties":{...},"code":0}` |
| 边缘 → 平台 | `/{gwPid}/{gwDid}/child/{childDid}/properties/write/reply` | `{"messageId":"...","properties":{...},"code":0}` |

## 认证（按 JetLinks 平台 MQTT 接入规范）

JetLinks 平台 MQTT 认证规则（来自平台官方接入文档）：

- **`clientId`** = 平台设备实例 ID（这里 = 网关的 deviceId）
- **`username`** = `secureId + "|" + timestamp`（timestamp 为毫秒时间戳）
- **`password`** = `SM3(secureId + "|" + timestamp + "|" + secureKey)`（大写十六进制）
- **timestamp 与平台时间差 < 5 分钟**（否则认证失败）

在 JetLinks Edge 中：
- 网关（NorthApp）配置 `deviceId` / `secureId` / `secureKey` 三个字段
- 程序在每次连接时**自动按上述规则计算** username/password
- SM3 实现使用 `github.com/piligo/gmsm/sm3`（与 gmssl Python 库输出**完全一致**）
- 共享 MQTT 连接长跑时，程序每 `timestampDelta/2` 秒**主动重建连接**，让 timestamp 持续刷新

如果**不想用 SM3 自动认证**（比如已经有网关级 token），可显式填 `username`/`password` 字段，程序会跳过 SM3 计算直接用配置值。

> **直连模式说明**：JetLinks 还支持"每设备 1 个 MQTT 连接 + SM3 密码"模式：
> 当前实现**不采用**（1000 设备要 1000 个连接），但 SecureKey 字段已预留为子设备级别。
> 网关模式更适合工业 IoT 大规模场景。

## 数据上报示例

```json
{
  "messageId": "uuid-xxx",
  "properties": {
    "temperature": 25.3,
    "pressure": 1.234,
    "running": true
  },
  "changes": {
    "temperature": 25.3
  },
  "timestamp": 1717770000000
}
```

- `properties`：本次采集的全量值
- `changes`：本次相对上次的**变化点**（用于事件触发）
- `timestamp`：采集时间戳（毫秒）

平台收到后会写入时序数据库（TimescaleDB / TDengine / IoTDB），并在事件总线上发布，
规则引擎可以订阅这些事件做告警或转发。

## 主动读

平台下发：

```json
{
  "messageId": "read-001",
  "properties": ["temperature", "pressure"]
}
```

边缘网关会从 Modbus 实时读取这些点位，并通过 `properties/read/reply` 返回：

```json
{
  "messageId": "read-001",
  "code": 0,
  "message": "ok",
  "properties": {
    "temperature": 25.3,
    "pressure": 1.234
  }
}
```

## 主动写

平台下发：

```json
{
  "messageId": "write-001",
  "properties": {
    "setpoint": 50
  }
}
```

边缘网关会通过 Modbus 把 `setpoint` 写入对应寄存器。

## 功能调用（Function Invoke）

平台下发：

```json
{
  "messageId": "func-001",
  "function": "reset",
  "inputs": {}
}
```

**当前实现限制**：函数调用没有内置映射规则。需要在后续版本加入函数路由表。
当前推荐使用 `properties/write` 实现大部分控制逻辑。

## 验证

### 用 mosquitto 验证

```bash
# 订阅边缘网关上报
mosquitto_sub -h broker_host -p 1883 \
  -u test-product -P <secureKey> \
  -t '/gw-product/gw-device-001/child/edge-plc-1/properties/report' -v

# 模拟平台下发读
mosquitto_pub -h broker_host -p 1883 \
  -u test-product -P <secureKey> \
  -t '/gw-product/gw-device-001/child/edge-plc-1/properties/read' \
  -m '{"messageId":"r1","properties":["temperature"]}'

# 观察回复
mosquitto_sub -h broker_host -p 1883 \
  -u test-product -P <secureKey> \
  -t '/gw-product/gw-device-001/child/edge-plc-1/properties/read/reply' -v
```

### 平台侧验证

登录 JetLinks 管理后台：

1. 进入 **设备实例** → 选择对应设备 → **运行状态**，应能看到最新数据。
2. 进入 **设备实例** → **物模型属性**，会显示属性值（按 properties/report 的 properties 字段）。
3. 进入 **设备日志** → **设备消息**，可看到所有上行/下行消息。
4. 在 **设备调试** 中可以手动下发读/写指令。

## 异常处理

- **平台 broker 未启动 / 网络断开**：paho mqtt 客户端**后台持续重连**（最长 30s 间隔），期间采集正常进行但上送会被忽略；broker 恢复后会遍历当前 Group 自动恢复订阅。
- **Modbus 设备断开**：点组状态变 `disconnected`，采集循环继续但所有值标记为 `bad`。
- **Modbus 异常响应**（Illegal Data Address 等）：该 Tag 单次标记为 `bad`，不影响其他 Tag。
- **NorthApp 配置错误**（broker 地址错、账号错）：API 创建时**不阻塞**（短超时 3s + 后台重试），Web 状态显示 `running=true` 但实际未连上；日志会有警告。修改 NorthApp 后会自动重启实例（所有 Group 重新订阅）。
- **Group 停用 / 删除**：从 NorthApp 注销，**最后一个引用某设备的 Group 停止时**才 Unsubscribe（refCount 跟踪），节省 broker 资源。
