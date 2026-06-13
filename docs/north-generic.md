# 通用 MQTT Broker 北向传输插件说明文档

`Generic MQTT` 传输插件允许边缘网关以标准 MQTT 客户端身份，将轮询采集到的数据上送到任意兼容 MQTT 3.1.1/5.0 协议标准的第三方 MQTT 代理服务器（如 EMQX、Mosquitto、ActiveMQ、HiveMQ、AWS IoT Core 等），满足灵活的多中心上送及异构系统对接需求。

---

## 1. 北向传输通道配置

新建通用 MQTT 传输通道时，需要提供如下连接配置项：

| 参数名称 | 英文 Key | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| **Broker 地址** | `broker` | String | 是 | MQTT 代理服务连接地址。例如 `tcp://broker.hivemq.com:1883` |
| **Client ID** | `clientId` | String | 否 | 客户端唯一标识。留空则系统随机生成 |
| **用户名** | `username` | String | 否 | 连接认证用户名 |
| **密码** | `password` | String | 否 | 连接认证密码 |
| **KeepAlive** | `keepalive` | Number | 否 | 心跳检测间隔（秒），默认 `60` |

---

## 2. 采集组定制化上送参数

当具体的采集组（Group）绑定通用 MQTT 传输实例时，可以通过配置实现自定义的数据流主题规划：

### 2.1 发布主题配置 (`publishTopic`)

*   **说明**：采集组定时轮询出新数据后，推送到该主题中。
*   **默认主题**：如果未配置，系统将默认推送到 `/edge/groups/{groupId}/report`。
*   **数据报文格式 (Payload)**：
    ```json
    {
      "time": "2026-06-13T20:15:37+08:00",
      "groupId": "2ac00896-7931-41ae-a2e1-092122068553",
      "tags": {
        "temperature": 24.5,
        "humidity": 65.2
      }
    }
    ```

### 2.2 控制写入与回复主题 (`writeTopic`)

*   **说明**：用于接收下行写点位指令的订阅主题。网关在启动后会自动订阅该主题。
*   **默认控制主题**：未配置时默认为 `/edge/groups/{groupId}/write`。
*   **下行指令格式**：第三方系统需向该主题发送指定 JSON：
    ```json
    {
      "tag": "temperature",
      "value": 26.0
    }
    ```
*   **命令回复**：写入完成后，网关会自动将回写结果推送到 `{writeTopic}/reply`（默认 `/edge/groups/{groupId}/write/reply`）主题中，报文内容如下：
    ```json
    {
      "tag": "temperature",
      "success": true,
      "message": "write successfully"
    }
    ```
