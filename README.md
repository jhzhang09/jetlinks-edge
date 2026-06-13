# JetLinks Edge

JetLinks 平台的 Go 语言边缘网关。**当前已实现**：Modbus TCP 采集 → JetLinks MQTT 上送。
**预留扩展**：南向驱动（OPC-UA、Siemens S7、Modbus RTU、BACnet、MQTT-Client…）和北向传输（HTTP webhook、Sparkplug B、InfluxDB…）均可通过接口扩展。

> **v0.4 · Industrial Terminal Edition**：中英双语界面 · JetLinks 网关+子设备模型按官方协议 V1.3.1 · SM3 认证 · 全平台二进制打包 (linux/mac/win) · 单项自包含部署

> 架构参考 [EMQX Neuron](https://github.com/emqx/neuron)：南向驱动 + 北向传输 + 点组/点位模型 + Web 管理。

## 功能特性

- **南向驱动**：Modbus TCP（FC01/02/03/04/05/06/15/16）
  - 智能合并连续寄存器，**单次请求读多个点位**（不是按点位逐个轮询）
  - 支持线圈位提取（bit 位）
  - 支持 int16/uint16/int32/uint32/int64/uint64/float32/float64/string/bytes
  - 支持 AB/BA/ABCD/BADC/CDAB/DCBA 等字节序
  - 支持 decimal 缩放系数
- **北向上送**：JetLinks MQTT 网关客户端
  - **JetLinks 网关 + 子设备 模型**：整个边缘网关作为 1 个网关设备连接平台，多个子设备（点组）共享同一条 MQTT 连接
  - 主题严格遵循 JetLinks 官方协议 V1.3.1：`/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/report`
  - 支持属性上报、读属性、写属性、功能调用、指令回复、子设备注册
  - 断线自动重连
  - **多设备共享一条连接**：一个网关 = 一个 MQTT 连接，N 个子设备按需订阅/退订
  - **对应 JetLinks 平台 `ChildDeviceGateway` / `MqttClientDeviceGateway` 模型**
- **Web 管理与可视化**：
  - **现代控制台 UI**：支持 ☀️ 白天模式 / 🌙 夜间模式 精细化高对比度主题切换（默认夜间主题）。
  - **响应式侧边栏**：支持菜单栏一键折叠与展开收拢，优化紧凑化视口适配。
  - **精细化表单排版**：优化了新增/编辑配置框的行间距和上下紧凑度，最大化利用显示空间。
  - **中英文多语言一键切换**。
  - **实时拓扑关系图**：基于 Canvas 交互渲染，动态展示北向网关、本地网关与南向设备点组的连接关系与状态（白天主题去雾清晰显示，夜间主题展现酷炫荧光特效）。
  - **实时告警中心**：设备故障与网络异常告警的实时动态推送、查询与归档。
  - **操作日志与事件流**：控制台级日志动态呈现，实时观测数据采集及上报状态。
  - **数据基础管理**：JWT 登录认证、点组/点位 CRUD 操作、北向传输管理、采集状态总览等。
- **部署**：自包含发布包（macOS / Linux / Windows），可选一键安装 + systemd
- **预留扩展**：通过 `SouthDriver` / `NorthHandler` 接口接入新协议，不影响核心调度



## 插件生态与使用文档

本边缘网关采用模块化插件机制，当前已内置支持以下南向采集与北向传输插件，请参阅详细说明文档：

### 南向采集插件
*   [Modbus TCP 采集插件](docs/south-modbus.md) - 支持寄存器区域合并、批量读取优化、字节序转换及 decimal 缩放。
*   [OPC UA 采集插件](docs/south-opcua.md) - 支持安全策略、用户名/密码认证以及**基于 Browse 的节点树可视化批量添加点位**的最佳实践。

### 北向传输插件
*   [JetLinks MQTT 传输插件](docs/north-jetlinks.md) - 实现网关+子设备接入模型、SM3 国密算法动态哈希签名认证及数据按官方协议格式上送。
*   [Generic MQTT 传输插件](docs/north-generic.md) - 支持向通用 MQTT Broker（如 EMQX、Mosquitto）推送采集值，并支持基于自定义 Topic 的下行指令回写控制。

## 目录结构

```
jetlinks-edge/
├── cmd/jetlinks-edge/        # 入口
├── internal/
│   ├── config/               # 配置加载（支持环境变量覆盖）
│   ├── core/                 # 核心：Driver / NorthApp 接口 + Runner 调度器 + refCount 订阅
│   ├── driver/modbus/        # Modbus TCP 南向驱动（区域合并、批量读取）
│   ├── northbound/jetlinksmqtt/ # JetLinks MQTT 北向传输（网关+子设备模型、SM3 认证、refCount 按需订阅）
│   ├── store/                # 持久化（SQLite / PostgreSQL）
│   ├── web/                  # HTTP API（Gin + JWT）
│   └── logger/               # zap 结构化日志
├── pkg/
│   └── modbuslib/            # Modbus 编解码与 TCP 客户端（自研，零外部依赖）
├── web/                      # Vue 3 + Vite + Naive UI 前端（中英双语）
├── scripts/                  # 测试脚本（mock modbus server、e2e 测试）
├── deployments/              # 自包含发布包（一键安装到 Linux 小机器）
├── Dockerfile                # Docker 多阶段构建（可选）
├── docker-compose.yml        # Docker Compose 一键启动（可选）
├── docs/                     # 详细文档：架构、Modbus 配置、JetLinks 集成
├── Makefile                  # 构建、运行、打包、测试
├── config.yaml             # 默认配置（开箱即用）
├── go.mod / go.sum
└── README.md
```

## 快速开始

### 方式 1：本地直接运行

```bash
# 1. 准备
cd jetlinks-edge

# 2. 编译后端
make build

# 3. 构建前端（生产部署需要；dev 模式可跳过）
make web

# 4. 启动
./bin/jetlinks-edge -c config.yaml
```

启动后访问 http://localhost:7001，使用 `admin / admin123` 登录。

### 方式 2：Docker 部署

```bash
make docker
docker-compose up -d
```

### 方式 3：开发模式（前后端分离、热更新）

```bash
# 终端 A：后端
make run

# 终端 B：前端（自动代理 /api 到 7001）
make web-dev
```

前端 dev server: http://localhost:5173

## 使用流程

> **核心架构**：JetLinks Edge 采用 **“南向采集 - 采集组 - 点位”** 三层物理与逻辑拓扑解耦架构：
> 1. **南向采集 (Connection)**：代表南向物理网络通道（如 Modbus TCP 链路），定义协议驱动与连通参数。
> 2. **采集组 (Group)**：代表平台逻辑子设备，定义周期采集间隔，并绑定北向上送网关。多个逻辑采集组可共享/复用同一个南向采集。
> 3. **点位 (Tag)**：隶属于采集组，定义具体寄存器地址及值转换规则。

| 实体 | 含义 | 包含的字段 |
|---|---|---|
| **网关**（北向传输）| 1 个 MQTT 连接，1 个 JetLinks 网关设备 | broker、gateway username、gateway password、clientId |
| **南向采集** | 1 条物理网络通道，定义物理连接与驱动 | name、driver (如 modbus-tcp)、config (IP, 端口等) |
| **采集组**（逻辑设备）| 1 台子设备，周期采集并绑定北向上送 | name、connectionId、intervalMs、**device: {productId, deviceId}**、northAppId |
| **点位**（Tag） | 1 个数据点，隶属于采集组 | name、address、type、byteOrder、access、scale |

### 1. 在 JetLinks 平台准备工作

1. 创建**网关产品**（或复用现有产品）
2. 在该产品下创建**多台子设备**（不需要单独的 secureKey / SM3 密码）
3. 确认 JetLinks 内置 MQTT broker 可用（默认端口 11883 或 1883）
4. 给网关产品配置好接入账号（**网关级** token，不是设备级）

### 2. 在边缘网关注册

#### 步骤 A：创建网关（"网关管理" → "新建网关"）

| 字段 | 示例值 | 说明 |
|---|---|---|
| 名称 | 产线网关-001 | 仅作显示 |
| 类型 | `jetlinks-mqtt` | |
| Broker | `tcp://你的 JetLinks broker:1883` | |
| **ProductID** | `gw-product` | **网关在 JetLinks 平台所属的产品 ID**（不是子设备的）|
| **DeviceID** | `gw-device-001` | **作为 MQTT clientId**（JetLinks 平台网关的 deviceId）|
| **SecureID** | `sec-abc` | 平台网关的 secureId |
| **SecureKey** | `<网关 secureKey>` | 平台网关的 secureKey |
| KeepAlive | 30 | 秒 |
| timestamp 容差 | 300 | 秒（默认 5 分钟） |

**重要**：网关在 JetLinks 平台里**也是一台设备**——有自己的 ProductID/DeviceID/SecureID/SecureKey。子设备的 productId/deviceId 在"南向采集组"页面配置。

**认证**（程序自动按 JetLinks 规范计算）：
- `clientId` = `deviceId`
- `username` = `secureId + "|" + timestamp`（timestamp = 当前毫秒时间戳）
- `password` = `SM3(secureId + "|" + timestamp + "|" + secureKey)`（大写十六进制）

> 共享的 MQTT 连接长跑时，程序会每 `timestampDelta/2` 秒主动重建连接，刷新 timestamp，避免平台规则"差 < 5 分钟"过期。
> broker 不可达时**也能创建成功**，实例会在后台持续重连。
> **多个子设备共享这个网关 → 多个子设备共享这条 MQTT 连接**。

**register/online 消息**：网关连上 broker 后会为每个已注册的子设备 publish：
- `/{gwPid}/{gwDid}/child/{childDid}/register`（子设备注册）
- `/{gwPid}/{gwDid}/child/{childDid}/online`（子设备在线）
网关**本身**不需要 register——它已是平台上一台真实设备，由人工在 JetLinks 平台创建。

#### 步骤 B：创建南向采集（"通道管理" → "新建南向采集"）

| 字段 | 示例值 | 说明 |
|---|---|---|
| 名称 | Modbus设备-1 | 物理网络通道展示名称 |
| 驱动 | `modbus-tcp` | 南向采集协议驱动 |
| 主机 / 端口 | `127.0.0.1` / `502` | 硬件连通参数 |

#### 步骤 C：创建逻辑采集组（"采集组管理" → "新建采集组"）

| 字段 | 示例值 | 说明 |
|---|---|---|
| 名称 | plc-1 | 采集组名称 |
| 南向采集 | 选择 "Modbus设备-1" | 绑定的通道连接（可实现物理链路多组复用） |
| 采集周期 | `1000` ms | 本组点位的采集轮询间隔 |
| **网关** | 选择 "产线网关-001" | 数据北向上送绑定通道 |
| **子设备身份** | productId=`test-product`，deviceId=`edge-plc-1` | **在 JetLinks 平台上对应的子设备身份** |

> - **必须填子设备身份**（绑定了北向网关时）
> - **物理链路多组复用**：多个采集组可以复用同一个南向采集，避免建立过多 TCP 连接造成系统瘫痪。
> - **不选网关** = 只采集不上送（本地纯监控场景）

#### 步骤 D：添加点位（Tag）

进入采集组详情 → "添加点位"：name、address、type、byteOrder、access、scale。

#### 步骤 E：验证与拓扑监视

- 拓扑页面：观察 **“南向插件 - 南向采集 - 采集组 - 北向传输 - 北向传输插件”** 五列等高拓扑连线大屏及实时在线连通状态。
- 数据页面：Web 页面观察实时值。
- 在 JetLinks 平台：查看网关与子设备的运行状态、属性数据、消息日志。

### 3. 修改和切换

| 想做什么 | 怎么做 |
|---|---|
| 修改 broker / 网关账号 | "网关管理" → 编辑 → 保存。所有使用它的子设备共享新连接 |
| 添加新子设备 | "南向点组" → 新建 → 选已有网关 + 填新 deviceId |
| 切换子设备的上送通道 | "南向点组" → 编辑 → 选另一个网关。Modbus 连接**不中断** |
| 删除网关 | 自动解除所有子设备的绑定 + 销毁共享连接 |
| 临时停用上送 | 编辑网关把"启用"关掉，或编辑点组清空"网关" |

### 4. 关于"每设备独立连接"模式（v0.4 预留）

JetLinks 也支持"每台设备作为独立 MQTT 客户端"模式：clientId=deviceId、username=secureId+'|'+timestamp、password=SM3(...)。这种模式**更接近设备原始规范**，但 1000 设备要 1000 个 MQTT 连接，平台可能不支持这种规模。

**JetLinks Edge 默认采用"网关+子设备"模式**（即本文档描述）。如确需"每设备独立连接 + SM3 认证"模式，请通过 issue 提出（会在 v0.4+ 实现 mode=direct 选项）。

### 5. 平台下发指令



使用 `mosquitto_pub` 模拟平台下发：

```bash
# 读属性
mosquitto_pub -h broker_host -p 1883 \
  -u test-product -P <secureKey> \
  -t /<网关产品ID>/<网关设备ID>/child/<子设备ID>/properties/read \
  -m '{"messageId":"req-1","properties":["temperature"]}'

# 写属性
mosquitto_pub -h broker_host -p 1883 \
  -u test-product -P <secureKey> \
  -t /<网关产品ID>/<网关设备ID>/child/<子设备ID>/properties/write \
  -m '{"messageId":"req-2","properties":{"temperature": 250}}'
```

边缘网关会通过 Modbus 协议向设备执行读/写，并通过 `/{pid}/{did}/properties/{read|write}/reply` 主题回复。

## 配置

详见 [docs/configuration.md](docs/configuration.md)。

主要环境变量：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `JETLINKS_EDGE_WEB_ADDR` | `0.0.0.0:7001` | Web 监听地址 |
| `JETLINKS_EDGE_WEB_JWT_SECRET` | （需修改）| JWT 签名密钥 |
| `JETLINKS_EDGE_WEB_DEFAULT_USER` | `admin` | 首次启动默认账号 |
| `JETLINKS_EDGE_WEB_DEFAULT_PASSWORD` | `admin123` | 首次启动默认密码 |
| `JETLINKS_EDGE_LOG_LEVEL` | `info` | 日志级别 |
| `JETLINKS_EDGE_STORAGE_DSN` | `data/jetlinks-edge.db` | SQLite 文件路径或 Postgres DSN |

## 架构与扩展

详见 [docs/architecture.md](docs/architecture.md)。

### 新增南向驱动

```go
// internal/driver/opcua/opcua.go
package opcua

func NewDriver(ctx context.Context, name string, cfg core.DriverConfig) (core.SouthDriver, error) {
    // ...
}

func Register(r *core.DriverRegistry) {
    r.Register("opc-ua", NewDriver)
}
```

在 `cmd/jetlinks-edge/main.go` 中：
```go
opcua.Register(driverRegistry)
```

### 新增北向传输

```go
// internal/northbound/kafka/app.go
package kafka

func NewApp(ctx context.Context, appID string, cfg core.NorthAppConfig) (core.NorthHandler, error) {
    // ...
}

func Register(r *core.NorthRegistry) {
    r.Register("kafka", NewApp)
}
```

## 部署

边缘网关部署目标是**单机、单二进制、零额外依赖**（不需要 nginx、不需要数据库服务器）。
下面是按规模从轻到重的 4 种方式。**小机器生产推荐方式 1。**

### 方式 0：单二进制（最快，5 分钟）

适合：本地测试、单台小机器、内网工控机。

```bash
# 1. 在本地构建（macOS/Linux 均可）
go build -ldflags="-s -w" -o bin/jetlinks-edge ./cmd/jetlinks-edge
cd web && npm install --no-audit --no-fund && npm run build && cd ..

# 2. 准备发布目录
mkdir -p deploy
cp bin/jetlinks-edge deploy/
cp -r web/dist deploy/
cp config.yaml deploy/config.yaml
sed -i 's|static_dir:.*|static_dir: "web/dist"|' deploy/config.yaml

# 3. 打包并传
tar czf jetlinks-edge.tar.gz -C deploy .
scp jetlinks-edge.tar.gz user@server:/tmp/

# 4. 服务器上启动
ssh user@server
tar xzf /tmp/jetlinks-edge.tar.gz -C /opt/
cd /opt
./jetlinks-edge -c config.yaml
# 访问 http://<server-ip>:7001
```

**注意**：`config.yaml` 里 `storage.dsn` 用相对路径时，db 会写到 **可执行文件** 所在目录的 `data/` 下，不受 cwd 影响。

### 方式 1：自包含 bundle（推荐，10 分钟）

适合：工控机、ARM 小盒子、网关设备——任何能跑 Linux 的小机器。

`make bundle` 一行打包，**单 tar 包、无 nginx 依赖、约 7MB**：

```bash
make bundle       # → deployments/edge-bundle.tar.gz（默认 linux/amd64）
make bundle-all   # → 全部平台打包（见下方全平台发布）
```

包内：

```
edge-bundle/
├── bin/jetlinks-edge    # Go 二进制
├── web/dist/            # 前端产物
├── scripts/
│   ├── install.sh       # 一键安装
│   ├── uninstall.sh     # 一键卸载
│   └── build-bundle.sh  # 重新打 bundle
└── README.txt
```

**部署到目标机器**：

```bash
scp deployments/edge-bundle.tar.gz user@server:~
ssh user@server
tar xzf edge-bundle.tar.gz -C /opt/
cd /opt/edge-bundle
sudo ./scripts/install.sh    # 默认装到 /opt/jetlinks-edge
```

`install.sh` 自动完成：
- 创建 `jetlinks` 系统用户
- 安装到 `/opt/jetlinks-edge`，**全部绝对路径**
- 生成 config.yaml（db 路径、web 路径均为绝对路径）
- 写入 systemd unit，开机自启 + 崩溃自愈
- systemd 安全加固（ProtectSystem=strict、ReadWritePaths 等）

**自定义**：

```bash
sudo ./scripts/install.sh --prefix /srv/jetlinks-edge   # 改路径
sudo ./scripts/install.sh --port 8080                  # 改端口
sudo ./scripts/install.sh --no-systemd                 # 不要 systemd
```

**运维**：

```bash
journalctl -u jetlinks-edge -f        # 实时日志
sudo systemctl status jetlinks-edge   # 运行状态
sudo systemctl restart jetlinks-edge  # 重启
```

**升级**（数据不受影响）：

```bash
# 本地重新 make bundle，上传
sudo /opt/edge-bundle/scripts/install.sh
```

**卸载**：

```bash
sudo /opt/edge-bundle/scripts/uninstall.sh            # 删全部
sudo /opt/edge-bundle/scripts/uninstall.sh --keep-data  # 保留数据
```

### 方式 2：Docker

适合：CI/CD、Kubernetes、统一镜像管理。

```bash
make docker                     # 构建镜像 jetlinks-edge:local
docker compose up -d            # 启动
docker compose logs -f jetlinks-edge
```

镜像特性：
- 前端嵌入 `/app/web/dist`，**无 nginx sidecar**
- SQLite 用 volume 持久化
- 镜像约 30-50MB（alpine + 二进制）

### 方式 3：手动 systemd

适合：物理机 / VM、不装 Docker、但要进程托管。

```bash
sudo mkdir -p /opt/jetlinks-edge/{data,web}
sudo cp jetlinks-edge /opt/jetlinks-edge/
sudo cp -r web/dist /opt/jetlinks-edge/web/
sudo useradd -r -s /bin/false jetlinks
sudo chown -R jetlinks:jetlinks /opt/jetlinks-edge

# 写入 /etc/systemd/system/jetlinks-edge.service（见 install.sh 中的模板）
sudo systemctl daemon-reload
sudo systemctl enable --now jetlinks-edge
journalctl -u jetlinks-edge -f
```

### 服务器要求

| 项 | 要求 |
|---|---|
| OS | Linux x86_64 / arm64，glibc ≥ 2.31 |
| 端口 | 7001 |
| 资源 | 256MB RAM / 50MB 磁盘 |
| 网络 | 能连 JetLinks MQTT broker |

### 选哪个

| 场景 | 推荐 |
|---|---|
| 一台工控机，想跑起来看看 | 方式 0 |
| **多台小机器生产部署** | **方式 1（bundle）** |
| K8s / CI | 方式 2（Docker） |
| 不想装 Docker 也不想用脚本 | 方式 3 |
| 需要全平台发布包（linux/mac/win） | `make bundle-all` |

### 全平台二进制打包与 UPX 瘦身

为适应边缘网关在低性能、小存储设备上的运行要求，打包脚本已集成了 **UPX 二进制压缩与瘦身机制**。

#### 1. 打包方式选择
根据编译机器（开发机）的本地环境，支持以下两种打包方式：

##### 方式 A：Docker 容器化打包（推荐，免安装环境）
**适合**：本地未安装 `upx` 工具，或本地编译环境不全的用户。
只需在本地启动 Docker 守护进程，然后运行：
```bash
make bundle-docker      # 仅打包当前平台（自动在 Golang 容器中配置 upx 进行压缩）
make bundle-all-docker  # 一次性生成所有平台的瘦身发布包
```
* **特点**：前端会在您本地编译，而 Go 后端编译和 `upx` 压缩会在 Linux 容器中执行。容器内部自动拉取官方 Linux 版本的 UPX 对二进制进行体积剥离，无需在您的宿主机上安装任何额外依赖，最终产出包直接写回到本地宿主机的 `deployments/` 目录中。

##### 方式 B：本地直接打包（自动降级兼容）
直接在本地宿主机上编译打包：
```bash
make bundle             # 打包当前平台
make bundle-all         # 一次性打包所有常见平台
# 或指定输出目录：
bash scripts/build-bundle.sh --all --output ./releases
```
* **自动降级机制**：打包脚本在编译完成后会自动检测系统中的 `upx` 命令行工具：
  * 如果检测到 `upx` 存在，会自动使用最高压缩率（`upx -9`）处理二进制，使单个发布包体积从约 **20MB** 降至 **6MB ~ 8MB** 左右。
  * 如果未检测到 `upx`，脚本仅会打出警告提示：`Warning: upx command not found, skipping compression.`，然后**自动安全降级，以原样常规体积产出发布包**，整个打包和编译流程绝对不会因工具缺失而报错中断。

可直接将这些生成的 tar.gz 和 sha256sums.txt 上传到 GitHub Releases、本地 FTP 或 OSS 进行边缘部署。

---

## 测试

项目自带两套本地测试脚本，**只依赖 Python 3 标准库**，无第三方依赖。

### 1. 健康检查

```bash
curl http://localhost:7001/healthz
# {"status":"ok"}
```

### 2. 模拟 Modbus TCP 设备

`scripts/mock_modbus_server.py` 是一个纯 Python 实现的 Modbus TCP 响应器，
实现 FC03/FC04/FC06，监听 5020 端口，启动时预置寄存器 0/1/2 = 100/200/300。

```bash
# 默认端口 5020
python3 scripts/mock_modbus_server.py

# 或自定义端口
python3 scripts/mock_modbus_server.py 5020
```

终端会打印所有 Modbus 请求，方便调试：

```
[mock-modbus] listening on 127.0.0.1:5020 (HOLDING[0..2] = 100, 200, 300)
[mock-modbus] connected: ('127.0.0.1', 63011)
  <- read holding start=0 qty=3 -> [100, 200, 300]
  <- write single addr=0 val=999
```

然后在 Web 管理界面 / API 中添加一个 host=127.0.0.1 port=5020 unitId=1 的点组即可。

### 3. 端到端测试

`scripts/e2e_test.py` 是一站式端到端测试脚本，验证：登录 → 创建点组 → 添加点位
→ 等待采集 → 验证实时值 → 主动写寄存器 → 主动读寄存器。

```bash
# 终端 A：启动 mock modbus
python3 scripts/mock_modbus_server.py

# 终端 B：启动 jetlinks-edge
./bin/jetlinks-edge -c config.yaml

# 终端 C：跑测试
python3 scripts/e2e_test.py
```

预期输出：

```
== 1. 登录 ==
   token = eyJhbGciOiJIUzI1NiIsInR5cCI6Ik...
== 2. 创建点组 ==
   gid = ...
== 3. 添加 3 个点位 ==
   tag reg1 = ...
   tag reg2 = ...
   tag reg3 = ...
== 4. 等待采集 3s ==
== 5. 读实时值（应 100/200/300） ==
   reg3 = 300 (good)
   reg2 = 200 (good)
   reg1 = 100 (good)
== 6. 主动写 reg1 = 999 ==
   {'id': '...', 'value': 999, 'written': True}
== 7. 主动读 reg1 ==
   {'tagId': '...', 'name': 'reg1', 'value': 999, 'quality': 'good', ...}
== 验证通过 ==
```

### 4. 模拟 JetLinks MQTT Broker

如果需要验证北向上送逻辑，用 mosquitto 模拟 JetLinks 平台侧 broker：

```bash
docker run -d -p 1883:1883 eclipse-mosquitto
# 或 brew install mosquitto && mosquitto
```

订阅上报主题验证数据：

```bash
mosquitto_sub -h 127.0.0.1 -p 1883 \
  -u test-product -P test-secret \
  -t '/+/+/child/+/properties/report' -v
```

### 5. 单元测试

`pkg/modbuslib` 是与外部依赖解耦的纯库，**建议**为它编写单元测试：

```go
// pkg/modbuslib/codec_test.go 示例
package modbuslib

import "testing"

func TestParseAddress(t *testing.T) {
    cases := []struct {
        in   string
        area Area
        off  uint16
    }{
        {"00001", AreaCoil, 0},
        {"10001", AreaDiscreteInput, 0},
        {"30001", AreaInputRegister, 0},
        {"40001", AreaHolding, 0},
        {"40100", AreaHolding, 99},
    }
    for _, c := range cases {
        a, err := ParseAddress(c.in)
        if err != nil {
            t.Errorf("ParseAddress(%q) err: %v", c.in, err)
            continue
        }
        if a.Area != c.area || a.Offset != c.off {
            t.Errorf("ParseAddress(%q) = %v, want {%d %d}", c.in, a, c.area, c.off)
        }
    }
}
```

跑测试：

```bash
go test ./pkg/modbuslib/...
```

（`internal/*` 包的单元测试需要抽象 Store / Driver 接口的 fake 实现，可以参考
`core.Store` 写一个内存版 fake。）

## 已知限制（v0.4）

- **南向南向采集限制**：目前只支持 Modbus TCP 采集，RTU 串口版本未实现（已预留 `SouthDriver` 接口以供后续开发）。
- **用户权限控制**：目前统一使用默认的 `admin` 管理员账号，暂不支持多用户及细粒度的角色权限划分。
- **系统 OTA 升级**：暂不支持固件或软件版本的在线 OTA 升级。

## 版本历史 (Changelog)

### v0.4 (Current)
* **三层拓扑重构**：重塑了整体工业拓扑物理链路，拆分并解耦为 **“南向采集 - 采集组 - 点位”** 三层拓扑架构，实现了多路逻辑采集组对单一南向采集的链路连接复用（物理链路多组复用）。
* **批量查询性能优化**：对后端数据层加载（GORM / GORM Session）进行了深入的重构与性能剖析，引入 Batch 批量通道映射，彻底清除了点组/点位列表加载时的多次循环 N+1 SQL 瓶颈。
* **等高与精致化拓扑图大屏**：重构了 Web 实时拓扑页面（`TopologyView.vue`），统一卡片为 **86px 固定等高** 排布以实现像素级对齐线，并将状态 Badge（已连接/离线/停止）尺寸精致缩小（`font-size: 8.5px`，`padding: 1.5px 5px`），呼吸圆点降至 `5px`，美观紧致。
* **南向采集运维指标屏**：在南向采集页面（`ConnectionsView.vue`）上方，引入了与总体仪表盘设计语言一致的 Health Panel，可一目了然查看网关南向采集总数、启用率、在线率及支持的南向驱动协议数。
* **主题与体验**：新增 ☀️ 白天 / 🌙 夜间高对比度主题切换（默认夜间主题），重构 Canvas 连线在白天背景下的去雾清晰实线绘制逻辑，夜间主题保留发光荧光特效。
* **侧边栏折叠**：修复了侧边栏最底部展开/收起折叠按钮失效的 Bug，实现了平滑视口自适应收拢。
* **视觉优化**：放大并加粗了侧边栏主菜单字体，极大提升导向可读性；精细优化了新增/编辑表单输入框的垂直间距与紧凑排版，节约屏幕操作空间。
* **Bug 修复**：解决了白天主题下普通事件日志及总览表格文字偏白导致无法辨识的 bug。
* **编译与打包**：引入了本地构建的 `upx` 检测与自动安全降级机制；在 Makefile 中集成了 `bundle-docker`，支持在隔离容器内自动下载配置 Linux 绿色版 upx 完成瘦身打包（免除开发机安装环境污染）。
* **流水线配置**：新增 Apache 2.0 英文许可证（LICENSE），并补充了 CONTRIBUTING.md 开发贡献规范和基于 GitHub Actions 的 CI (`ci.yml`) / CD (`release.yml`) 自动发布脚本。

### v0.3
* **北向上送**：实现了 **JetLinks 官方网关与子设备映射模型**。支持多个子设备逻辑实体（点组）共享同一条物理 MQTT 网关长连接上报。
* **国密安全**：引入基于 SM3 算法的动态计算鉴权与连接机制，新增 timestamp 周期自动刷新与长连接重连防超时过期。
* **业务管理**：提供了独立的北向传输管理及南向子设备点组的注册、上线与数据上送控制。

### v0.2
* **多语言支持**：引入了完整的 Vue 多语言国际化（i18n）架构，支持中英双语一键切换。
* **采集引擎优化**：重构了 Modbus 驱动调度器，实现连续寄存器地址智能自动合并及单次报文多点位批量高效读取，大幅提高采集吞吐率。

### v0.1
* **核心基础脚手架**：搭建了单二进制边缘网关基础框架，集成 R2DBC/GORM SQLite & Postgres 存储双驱动、JWT 安全登录、Modbus 采集、北向 MQTT 客户端及 Vue 3 Web 控制台。

---

## 后续路线图 (Roadmap)

### 近期计划 (v0.5)
- **南向协议扩展**：开发 Modbus RTU（串口 RS485/RS232）南向通道驱动。
- **高可用优化**：引入多实例部署下的缓存同步机制，设计基于 Redis 的分布式锁与状态发布订阅。
- **数据本地暂存**：支持断线重连期间的本地时序数据暂存（数据缓冲），在 MQTT 连接恢复后自动补发。

### 中期计划 (v0.6)
- **主流工控协议集成**：引入 OPC-UA 驱动和西门子 S7 协议驱动。
- **北向传输规范**：支持 Sparkplug B 工业物联网规范协议的北向上送。
- **OTA 与热配置**：支持配置模板的一键热导入与导出，设计轻量级系统固件与网关在线 OTA 升级。

### 远期规划 (v1.0)
- **边缘计算逻辑**：引入边缘规则引擎（支持本地简单的 IF-THEN 触发与告警映射）。
- **脚本清洗沙箱**：支持在边缘端载入轻量级 JS 或 Python 脚本对采集的原始报文和点位值进行过滤、清洗与特征值预计算。

---

## License

Apache 2.0
