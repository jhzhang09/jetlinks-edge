# 统一术语

项目代码、API、页面和文档统一使用以下术语：

| 中文术语 | 代码模型 | 含义 | 不再使用的同义词 |
|---|---|---|---|
| 南向插件 | `DriverLifecycle` / `ExtensionDescriptor` | 实现现场协议能力的插件 | 南向驱动、采集插件 |
| 南向连接 | `Connection` | 一个真实物理链路和插件实例 | 南向采集、物理通道、通道 |
| 采集组 | `Group` | 共享南向连接的逻辑设备和调度单元 | 点组、南向设备、采集任务 |
| 点位 | `Tag` | 可读写的数据点 | 标签、属性点 |
| 北向应用 | `NorthApp` | 一个面向平台或数据目的地的运行实例 | 北向传输、网关、上送通道 |
| 北向插件 | `NorthMessageHandler` / `ExtensionDescriptor` | 实现北向协议的插件 | 传输插件 |
| 边缘节点 | JetLinks Edge 进程 | 当前运行的边缘网关实例 | 当前网关、本地网关 |

`driver`、`connection`、`group`、`tag` 和 `northApp` 作为既有 API/数据库标识继续保留；术语统一不改变现有数据契约。

