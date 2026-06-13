# JetLinks MQTT 北向传输插件说明文档

`JetLinks MQTT` 传输插件专门用于将边缘端网关采集的数据，以合规的方式对接并推送至 `JetLinks` 物联网基础平台（社区版/企业版）。该插件深入实现了 JetLinks 官方提供的**网关设备接入模型**，支持海量点位与子设备的数据共享传输。

---

## 1. 核心架构：网关 + 子设备模型

为降低设备连接消耗，本插件采用共享信道的设计：

*   **单一 MQTT 连接**：整个边缘网关在 JetLinks 平台里注册为一个**网关物理设备**，且只与平台建立一条物理 MQTT 链路。
*   **子设备数据桥接**：每一个南向采集组（如 PLC、机床）在 JetLinks 平台中对应一个**子设备**。它们不单独发起连接，而是将自己的数据打包后，交由网关物理连接进行统一上送。
*   **物理连接多路复用**：极大地降低了平台的连接并发压力，支持数以万计的子设备通过极少数的网关链路安全交互。

---

## 2. 北向传输通道配置

新建类型为 `jetlinks-mqtt` 的北向传输实例时，需要配置以下接入参数：

| 参数名称 | 英文 Key | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| **Broker 地址** | `broker` | String | 是 | JetLinks 平台 MQTT 服务的连接地址。例如 `tcp://192.168.1.200:1883` |
| **网关产品 ID** | `productId` | String | 是 | 网关产品在 JetLinks 平台注册的 ProductID |
| **网关设备 ID** | `deviceId` | String | 是 | 网关设备在 JetLinks 平台创建的 DeviceID（**作为 MQTT clientId**） |
| **Secure ID** | `secureId` | String | 是 | 网关在 JetLinks 平台的安全标识 |
| **Secure Key** | `secureKey` | String | 是 | 网关在 JetLinks 平台的安全密钥 |
| **KeepAlive** | `keepalive` | Number | 否 | 心跳周期（秒），默认 `30` |
| **时间戳容差** | `tsDelta` | Number | 否 | 时间戳有效期校验偏置（秒），默认 `300` |

---

## 3. 安全认证机制：SM3 动态哈希认证

为了防止静态密码在传输中被拦截窃取，插件严格遵循 JetLinks 平台规范，采用 **SM3 国密摘要签名算法** 进行动态哈希计算：

1.  **认证信息生成**：
    *   **`clientId`** = 网关设备的 `deviceId`
    *   **`username`** = `secureId + "|" + timestamp`（timestamp 为当前高精度毫秒时间戳）
    *   **`password`** = `SM3(secureId + "|" + timestamp + "|" + secureKey)`（大写十六进制串）
2.  **动态重连更新**：
    *   由于平台通常要求时间戳偏差必须在容差内（默认 5 分钟），本插件会在长跑时，每隔 `tsDelta / 2` 秒在后台平滑重建 MQTT 链路认证，动态刷新 timestamp 并更新哈希签名，彻底防范链路重放攻击和认证超时断线。

---

## 4. 数据传输主题 (Topic) 规划

插件按照官方网关接入协议，对所有上送消息和控制指令进行了精细编排：

### 4.1 数据上送与状态流

*   **子设备注册**：`/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/register`
*   **子设备上线**：`/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/online`
*   **子设备数据上报**：`/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/report`
    *   *Payload 格式*：`{"temp": 25.4, "status": 1}`（自动转换南向点位值为物模型属性键值对）

### 4.2 下行指令接收与响应

*   **写入属性指令**：`/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/write`
    *   *网关收到后会自动执行南向写，并向 `/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/write/reply` 返回应答*。
