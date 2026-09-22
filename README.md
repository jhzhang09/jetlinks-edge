# JetLinks Edge

JetLinks Edge 是面向工业物联网场景的高性能、轻量级边缘采集网关，基于 Go 语言开发。

核心提供：**南向多协议设备采集**（Modbus TCP / OPC UA 及可扩展驱动）→ **统一采集与事件调度引擎** → **北向物联网平台上送**（原生支持 JetLinks 官方网关与子设备模型，以及通用 MQTT 协议）。

> **核心设计理念**：单二进制自包含（无需 Nginx / 外部数据库）、内置与进程外插件热插拔架构、物理链路与逻辑设备解耦、零外部依赖开箱即用。

---

## 界面预览 (Web Console)

边缘网关内置现代化工业控制台 Web UI，支持中英双语、☀️ 白天与 🌙 夜间高对比度主题切换（默认夜间主题），拥有基于 Canvas 2D 渲染的工控实时拓扑大屏。

### 🌌 实时工控拓扑大屏 (Topology Live Monitor)

拓扑大屏直观展示 **“南向插件 - 南向连接 - 采集组 - 北向应用 - 北向插件”** 五级全链路拓扑，区分静态插件归属与动态实时数据流，动态呈现链路连通状态与流动特效。

![实时拓扑大屏 - 夜间荧光模式](docs/assets/screenshot-topology-dark.png)

---

### 📊 核心管理界面

| ☀️ 拓扑大屏 (白天清晰模式) | 📈 仪表盘 / 运维看板 (含实时趋势图) |
| :---: | :---: |
| ![白天拓扑](docs/assets/screenshot-topology-light.png) | ![控制台总览](docs/assets/screenshot-dashboard.png) |
| **🔌 南向连接 (物理链路复用)** | **📦 采集组 (逻辑设备管理)** |
| ![通道管理](docs/assets/screenshot-connections.png) | ![采集组管理](docs/assets/screenshot-groups.png) |
| **🏷️ 点位列表与物模型配置** | **🚨 实时告警中心** |
| ![点位详情](docs/assets/screenshot-group-detail.png) | ![告警中心](docs/assets/screenshot-alarms.png) |
| **🔑 系统登录页 (默认预填凭据)** | **🧩 外部独立插件管理** |
| ![登录页](docs/assets/screenshot-login.png) | 支持查看并管理独立进程插件运行状态与 Schema |

---

## 核心功能特性

### 1. 南向驱动与协议采集
- **Modbus TCP**：
  - 支持常用功能码：FC01 (读线圈)、FC02 (读离散输入)、FC03 (读保持寄存器)、FC04 (读输入寄存器)、FC05/06 (单点写)、FC15/16 (批量写)。
  - **连续寄存器智能合并**：自动分析点位地址范围，单次报文合并读取多个点位，大幅提升采集吞吐率。
  - 支持丰富数据类型：int16/uint16/int32/uint32/int64/uint64/float32/float64/string/bytes 及寄存器 bit 位提取。
  - 完整字节序转换（AB, BA, ABCD, BADC, CDAB, DCBA）与 decimal 精度缩放系数。
- **OPC UA**：
  - 支持安全策略配置、匿名及用户密码认证。
  - 支持节点树层级可视化 Browse 浏览与批量勾选导入点位。

### 2. 北向上送与设备模型
- **JetLinks 平台官方协议深度集成**：
  - **网关 + 子设备架构**：网关本身与平台建立 1 条 MQTT 长连接，多个子设备（采集组）共享复用该连接，大幅节省云端与边缘连接资源。
  - 遵循 JetLinks 官方协议规范（`/{gwProductId}/{gwDeviceId}/child/{childDeviceId}/properties/report`）。
  - **动态安全认证**：自动按平台规则执行 MD5 签名校验，支持周期性自动刷新时间戳与长连接保活防过期。
  - 支持子设备自动注册、在线状态同步、属性上报、平台读写属性指令下发及功能调用（Function Invocation）。
- **Generic MQTT**：
  - 支持向通用 MQTT Broker（如 EMQX、Mosquitto）定时上送采集报文，支持自定义下行控制主题回写。

### 3. 双轨插件架构 (Pluggable Runtime)
- **内置原生驱动**：Go 语言原生集成的高性能 Modbus TCP、OPC UA、JetLinks MQTT、Generic MQTT。
- **进程级外部独立插件 (External Plugins)**：
  - 支持通过标准子进程 + 命名管道/标准 I/O 通信接入新协议。
  - 配置文件通过 `plugins/` 目录声明插件元信息（Manifest），独立进程运行崩溃不影响网关主服务，支持热加载与热更新。

### 4. 现代化工业运维控制台
- **开箱即用**：前端产物内嵌于二进制，无外部 Web 静态服务器依赖；登录页默认预填凭据 `admin / admin123`，直接一键连接。
- **实时运维看板**：自研轻量趋势折线图，支持鼠标悬浮准线、多指标数值浮窗与时间轴定位。
- **双模主题与国际化**：中英文多语言切换，白天/夜间高对比度工控主题适配。
- **告警与事件中心**：链路断开、采集超时等告警实时推送，支持告警确认与历史审计。

---

## 核心概念与数据模型

JetLinks Edge 采用 **“物理链路与逻辑设备解耦”** 的核心模型：

```
[南向插件] ──> [南向连接 Connection] ──> [采集组 Group (逻辑设备)] ──> [北向应用 North App] ──> [北向插件]
                     (物理通信参数)             (轮询周期/点位Tags/设备身份)        (MQTT出口与凭据)
```

| 实体 | 对应概念 | 说明 | 关键配置项 |
|---|---|---|---|
| **南向连接** | 物理通信链路 | 负责维持到底层设备或网关的 TCP/串口 连接，可被多个采集组复用 | IP、端口、超时时间、重连间隔 |
| **采集组** | 逻辑子设备 | 对应 JetLinks 平台的 1 台子设备，统筹周期采集并绑定北向出口 | 周期（ms）、产品 ID、子设备 ID、连接绑定 |
| **点位 (Tag)** | 物模型属性 | 隶属于采集组，映射到设备的物理寄存器或地址 | 寄存器地址、数据类型、字节序、缩放倍率 |
| **北向应用** | 云端出口通道 | 对应平台的网关接入点，维护一条稳定长连接 | Broker 地址、网关产品与设备 ID、安全凭据 |

---

## 快速开始

### 方式 1：本地编译与运行（推荐）

项目 Makefile 已将前端静态资源编译嵌入集成到构建流程中，**一条命令即可完成全栈构建**：

```bash
# 1. 一键构建（自动打包前端并注入 Go 二进制）
make build

# 2. 启动网关（自动执行构建并读取默认配置启动）
make run
```

启动成功后，浏览器访问：**http://localhost:7001**，系统已默认预填凭据（账号 `admin` / 密码 `admin123`），点击**连接**即可登录。

### 方式 2：开发调试模式（前后端热更新）

```bash
# 同时启动后端服务（:7001）与前端 Vite 热重载服务（:5173，自动代理 /api）
make dev
```
开发控制台访问：**http://localhost:5173**。

### 方式 3：Docker 容器运行

```bash
# 构建本地镜像并后台启动
make docker
docker compose up -d

# 查看运行日志
docker compose logs -f
```

---

## 生产部署方案

网关编译输出为单二进制文件（内置 SQLite 存储与 Web UI），无 Nginx、Redis、外部 SQL 依赖。

### 方案 A：自包含 Bundle 一键安装（最推荐）

适合工控机、工业树莓派、Linux 网关等各类边缘硬件：

```bash
# 1. 开发机打包（输出 deployments/edge-bundle.tar.gz，约 7MB ~ 20MB）
make bundle

# 2. 将 bundle 包复制到目标工控机
scp deployments/edge-bundle.tar.gz user@edge-box:/tmp/

# 3. 登录目标机器一键安装到系统服务（Systemd 开机自启与看门狗）
ssh user@edge-box
tar -xzf /tmp/edge-bundle.tar.gz -C /opt/
cd /opt/edge-bundle
sudo ./scripts/install.sh
```

**运维管理常用命令**：
```bash
sudo systemctl status  jetlinks-edge    # 查看运行状态
sudo systemctl restart jetlinks-edge    # 重启服务
journalctl -u jetlinks-edge -f          # 实时查看日志
```

如需卸载，直接执行 `sudo /opt/edge-bundle/scripts/uninstall.sh` 即可。

### 方案 B：全平台跨架构打包与 UPX 瘦身

```bash
# 本地编译全平台（linux-amd64/arm64/armv7, darwin-amd64/arm64, windows-amd64）
make bundle-all

# 或利用 Docker 隔离环境一键生成（免本地环境依赖，自动内嵌 UPX 最高压缩）
make bundle-all-docker
```

---

## 配置说明

网关支持通过 `config.yaml` 或系统环境变量进行配置。

### 常用环境变量配置

所有配置项均支持通过 `JETLINKS_EDGE_<SECTION>_<KEY>` 格式的环境变量直接覆盖：

| 环境变量 | 默认值 | 作用说明 |
|---|---|---|
| `JETLINKS_EDGE_WEB_ADDR` | `0.0.0.0:7001` | HTTP Web 管理控制台与 REST API 监听地址 |
| `JETLINKS_EDGE_WEB_PRODUCTION` | `false` | 生产模式标识；为 true 时强制要求非默认密码与安全密钥 |
| `JETLINKS_EDGE_WEB_JWT_SECRET` | 默认开发密钥 | JWT Token 签名秘钥（生产环境建议自定义） |
| `JETLINKS_EDGE_WEB_DEFAULT_USER` | `admin` | 首次初始化默认管理员账号 |
| `JETLINKS_EDGE_WEB_DEFAULT_PASSWORD`| `admin123` | 首次初始化默认密码 |
| `JETLINKS_EDGE_LOG_LEVEL` | `info` | 日志输出级别（`debug` / `info` / `warn` / `error`） |
| `JETLINKS_EDGE_STORAGE_DRIVER` | `sqlite` | 存储引擎（支持 `sqlite`、`postgres`） |
| `JETLINKS_EDGE_STORAGE_DSN` | `data/jetlinks-edge.db` | 数据库路径或连接串（相对路径自动按可执行文件所在目录解析） |
| `JETLINKS_EDGE_PLUGINS_ENABLED` | `true` | 是否启用进程级外部扩展插件 |
| `JETLINKS_EDGE_PLUGINS_DIRECTORY` | `plugins` | 外部插件配置文件与可执行程序所在目录 |

---

## 本地仿真与测试

项目 `scripts/` 目录内置测试工具，无需依赖外部硬件即可闭环验证：

### 1. 模拟 Modbus TCP 设备
内置纯 Python 实现的 Modbus TCP 仿真服务器（支持保持寄存器 FC03/FC04/FC06）：
```bash
python3 scripts/mock_modbus_server.py 5020
```
在网关控制台创建南向连接 `127.0.0.1:5020` 即可采集测试数据。

### 2. 一键自动化端到端测试 (E2E)
```bash
# 启动 mock 服务和网关后执行 E2E 全链路检测
python3 scripts/e2e_test.py
```
自动验证：登录认证 → 创建连接 → 采集组建立 → 点位批量采集与校验 → 寄存器主动读写。

### 3. 运行代码检查与单元测试
```bash
make test    # 运行全项目单元测试（带 -race 竞争检测）
make vet     # Go 代码静态语法规约检查
```

---

## 详细文档指南

| 文档 | 内容概述 |
|---|---|
| [系统总体架构设计](docs/architecture.md) | 核心接口设计、Runner 调度引擎、引用计数多路复用模型 |
| [概念与术语规范](docs/terminology.md) | 连接、采集组、点位、北向应用、插件生命周期核心定义 |
| [外部进程插件开发规范](docs/external-plugins.md) | 独立进程插件通信协议、JSON Manifest 定义与接入指引 |
| [Modbus TCP 插件与点位配置](docs/south-modbus.md) | 寄存器地址映射规则、批量读取算法、字节序与缩放转换 |
| [OPC UA 插件使用指引](docs/south-opcua.md) | 安全策略配置、节点树可视化 Browse 与批量导入技巧 |
| [JetLinks 平台接入集成指南](docs/north-jetlinks.md) | JetLinks 网关与子设备物模型上送、下行指令回复实操 |
| [Generic MQTT 插件使用说明](docs/north-generic.md) | 通用 MQTT 数据上报与下行控制指令映射 |
| [运维中心与拓扑看板设计](docs/operations-center-design.md) | 拓扑图、实时趋势图与告警中心交互架构设计 |

---

## 路线图 (Roadmap)

- [x] **v0.5.0**：
  - 进程级外部独立插件机制与管理页面
  - 拓扑图“插件规范与物理/逻辑拓扑”视觉分层与动态数据流渲染
  - 实时健康趋势图带悬浮准线、多维度值提示与时间轴定位
  - 登录页开箱即用体验（自动预填凭据）与 `0.0.0.0` 外部访问支持
- [ ] **v0.6.0**：
  - Modbus RTU（串口 RS485 / RS232）南向通道原生支持
  - 本地离线数据缓存（网络中断数据暂存，恢复后有序补发）
- [ ] **v0.7.0**：
  - Siemens S7 工业协议原生支持
  - Sparkplug B 工业物联网标准协议北向上送
  - 配置模板热导入/导出与配置版本快照

---

## License

[Apache 2.0](LICENSE)
