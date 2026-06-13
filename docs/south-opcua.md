# OPC UA 南向采集插件说明文档

`OPC UA`（OPC Unified Architecture）是面向工业 4.0 的跨平台、开放、安全可靠的统一架构通信协议。本边缘网关集成的 OPC UA 南向采集插件，支持对大型工业服务器（如 OPC UA Server、SCADA、Kepware、工业机床内置 OPC 服务）中的数据节点进行高可靠性的点位读取与交互。

---

## 1. 南向采集通道配置

新建 OPC UA 通道时，配置页面包括网络及安全相关设置：

| 参数名称 | 英文 Key | 类型 | 必填 | 说明 |
| :--- | :--- | :--- | :--- | :--- |
| **Endpoint URL** | `endpoint` | String | 是 | OPC UA 服务器连接终端地址。例如：`opc.tcp://192.168.1.50:4840` |
| **安全策略** | `securityPolicy` | String | 否 | 支持 `None`、`Basic128Rsa15`、`Basic256`、`Basic256Sha256`（默认 `None`） |
| **安全模式** | `securityMode` | String | 否 | 支持 `None`、`Sign`、`SignAndEncrypt`（默认 `None`） |
| **认证方式** | `authType` | String | 否 | 支持 `Anonymous`（匿名，默认）或 `Username`（用户名密码） |
| **用户名** | `username` | String | 否 | 选择 `Username` 认证时填写 |
| **密码** | `password` | String | 否 | 选择 `Username` 认证时填写 |

---

## 2. 点位（Tag）映射配置

OPC UA 的核心概念是节点（Nodes）。在配置点位时，地址的指向为 OPC UA 专有的 NodeID。

### 2.1 节点地址格式 (`address`)

`address` 必须符合标准的 NodeID 表达格式（区分命名空间索引和标识符类型）：
*   **格式**：`ns={namespaceIndex};{identifierType}={identifier}`
*   **标识符类型说明**：
    *   `s`：字符串标识符（String）。例如 `ns=2;s=Device1.Temperature`
    *   `i`：数值标识符（Numeric）。例如 `ns=1;i=1002`
    *   `g`：GUID 标识符（UUID）。例如 `ns=2;g=09087a11-....`
    *   `b`：字节串/不透明标识符（Opaque）。

> [!TIP]
> 如果直接人工录入 NodeID 存在困难或容易出错，推荐使用下面介绍的**“批量节点拉取”**功能。

---

## 3. 最佳实践：基于 Node Browse 的树状批量添加点位

为避免人工查找和打字输入庞杂 NodeID 的繁琐过程，本系统提供了直观的**可视化 OPC UA 节点浏览器**：

1.  **物理通道建立**：首先确保已配置并启用了 OPC UA 通道，且显示为在线连接状态。
2.  **拉取节点树**：进入对应的采集组详情页，点击右上角的 **“拉取节点”** 按钮。
3.  **树状目录浏览**：系统会实时建立与远程服务器的连接，并在弹窗中以可视化树状目录的形式，按层级结构展示出服务器的所有可用 Node（Object、Variables）。
4.  **按需勾选**：用户可以展开目录，直接鼠标勾选需要采集的各种变量节点（Variables）。
5.  **批量入库**：勾选完成后，点击“批量添加 (X)”，系统会自动把这些变量的 NodeID、名称、类型提取并转化为本地的采集组点位（Tag），一键完成大规模点位导入。
