# 外部插件协议与热插拔

JetLinks Edge 支持两类插件：随主程序编译的内置插件，以及通过独立进程加载的外部插件。外部插件不依赖 Go 的 `plugin.so`，因此 Linux、macOS、Windows 均使用同一套生命周期模型，也不要求插件与主程序使用相同 Go 编译器版本。

## 目录与清单

默认扫描 `config.yaml` 同目录下的 `plugins/`。每个插件由一个 JSON 清单和一个可执行文件组成：

```json
{
  "apiVersion": "jetlinks-edge-plugin/v1",
  "kind": "driver",
  "command": "./example-driver",
  "args": [],
  "descriptor": {
    "type": "example-driver",
    "name": "Example Driver",
    "version": "1.0.0",
    "capabilities": ["polling", "read", "write"],
    "connectionSchema": [],
    "configSchema": [],
    "tagSchema": []
  }
}
```

- `kind=driver`：南向插件。
- `kind=north`：北向插件。
- `command` 必须位于插件目录内且具有可执行权限，不能通过相对路径逃逸插件目录。
- 外部插件不能覆盖内置插件或其他外部插件的 `descriptor.type`。
- 主程序会校验清单和可执行文件的 SHA-256；替换任意一项后再次扫描会被识别为更新。
- 插件进程只继承 `PATH`、语言、时区和 Windows 系统目录等基础环境，不继承数据库、JWT 或其他主进程环境变量。

## 进程协议

主程序通过标准输入逐行发送 JSON 请求，插件通过标准输出逐行返回 JSON 响应。标准错误作为插件日志接入主程序日志。

请求：

```json
{"id":1,"method":"connect","payload":{"instanceId":"conn-1","config":{}}}
```

响应：

```json
{"id":1,"result":{"connected":true,"stats":{}}}
```

失败响应：

```json
{"id":1,"error":"connection refused"}
```

南向插件方法：

| 方法 | 请求 | 响应 |
|---|---|---|
| `connect` | `instanceId`, `config` | `DriverStatus` |
| `disconnect` | 空 | 空 |
| `readTags` | `tags` | `TagValue[]` |
| `writeTag` | `tag`, `value` | 空 |
| `invokeFunction` | `functionId`, `inputs` | 任意 JSON 值 |
| `browse` | `nodeId` | `NodeItem[]` |

北向插件方法：

| 方法 | 请求 | 响应 |
|---|---|---|
| `start` | `instanceId`, `config` | `NorthState` |
| `onMessage` | `NorthMessage` | 空 |
| `stop` | 空 | 空 |

每个 Connection 或 NorthApp 拥有独立插件进程。请求可以并发发出，响应必须带回原 `id`，顺序不作要求。

北向插件需要执行平台下行命令或读取采集组状态时，可以向标准输出写入受控 Host Call：

```json
{"id":9001,"hostCall":true,"method":"executeCommand","payload":{"id":"m1","groupId":"group-1","type":"read-property","payload":{"properties":["temperature"]}}}
```

主程序通过标准输入返回：

```json
{"id":9001,"hostCall":true,"result":{"id":"m1","code":0,"message":"success","payload":{"temperature":23.5}}}
```

允许的 Host Call 只有：

| 方法 | 作用 |
|---|---|
| `executeCommand` | 经 `NorthAppConfig.CommandExecutor` 回到 Runner，执行有归属校验的南向读写或功能调用 |
| `groupStatus` | 经 `GroupStatusProvider` 读取指定采集组状态 |

外部插件不能通过 Host Call 访问数据库、其他插件进程或任意主程序方法。

## 热插拔流程

1. 将清单和可执行文件原子写入插件目录。
2. 目录监听器会在文件稳定后自动扫描；也可在“插件中心”点击“扫描并应用”，或调用 `POST /api/v1/plugins/reload` 主动触发。
3. 新增插件立即进入注册表；更新插件会平滑重建引用它的运行时实例；移除插件会停止对应实例但保留数据库配置。
4. 重新放回兼容插件并扫描后，原配置会重新加载。

热重载接口仅允许 `admin` 角色调用。
