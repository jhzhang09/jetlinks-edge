# 变更日志

本文件基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/) 规范。

## [Unreleased]

## [0.4.0] - 2026-06-14 · Industrial Terminal Edition

### 新增
- ☀️ 白天 / 🌙 夜间高对比度主题切换（默认夜间主题）
- 侧边栏一键折叠与展开收拢，响应式视口自适应
- Makefile 集成 `bundle-docker`，支持在隔离容器内自动下载 Linux 绿色版 UPX 完成瘦身打包

### 改进
- 重构 Canvas 连线在白天背景下的去雾清晰实线绘制逻辑，夜间主题保留发光荧光特效
- 放大并加粗侧边栏主菜单字体，提升导向可读性
- 优化新增/编辑表单输入框的垂直间距与紧凑排版
- 引入本地构建的 `upx` 检测与自动安全降级机制

### 安全
- 升级 `eclipse/paho.mqtt.golang` 1.4.0 → 1.5.1，修复 **CVE-2025-10543**（topic 字段编码错误导致数据泄露）
- 升级 `jackc/pgx/v5` 5.6.0 → 5.9.2，修复 **GHSA-j88v-2ch3-qfwx**（simple protocol + dollar-quoted 场景下的 SQL 注入）
- 升级 `golang-jwt/jwt/v5` 5.2.0 → 5.2.2，修复 **GHSA-mh63-6h87-95cp** 安全公告
- 升级 `golang.org/x/crypto` 0.31.0 → 0.45.0、升级 `golang.org/x/net` 0.21.0 → 0.47.0，覆盖周期内多个高危 CVE

### 依赖与工具链
- 升级 `golang.org/x/sync` 0.10.0 → 0.18.0、`golang.org/x/sys` 0.28.0 → 0.38.0、`golang.org/x/text` 0.21.0 → 0.31.0
- 升级 `google.golang.org/protobuf` 1.31.0 → 1.33.0
- 升级 `gin-contrib/cors` 1.5.0 → 1.6.0（修复 wildcard domain 解析 bug）
- 升级前端 `vite` 5.4.21 → 8.0.16、`@vitejs/plugin-vue` 5.2.4 → 6.0.7，移除不再使用的 `esbuild`
- `go.mod` 升级 `go` 指令从 1.23.0 → 1.25.0（pgx 5.9 要求）

### 修复
- 修复侧边栏最底部展开/收起折叠按钮失效的 Bug
- 修复白天主题下普通事件日志及总览表格文字偏白导致无法辨识的问题
- 修复 `internal/core` 测试中的 data race（mock North 处理器加锁）
- 修复 `web/vite.config.ts` 在 vite 8 + rolldown 下的 `manualChunks` 类型不兼容（改为 ManualChunksFunction）

### 工程化
- 升级 `golangci-lint` v1.64.8 → v2.12.2 + `golangci-lint-action` v6 → v9；清理 v2 默认规则暴露的 22 个历史 lint 问题（errcheck 9、staticcheck 2、unused 11）
- GitHub Actions 全面改用 `go-version-file: go.mod` 与 `node-version-file: web/package.json`，版本号单一来源 = 配置文件本身
- release workflow 增加 `verify` 依赖（lint + race test + frontend build），发版前自动门禁

## [0.3.0]

### 新增
- 实现 JetLinks 官方网关与子设备映射模型，多个子设备（点组）共享同一条物理 MQTT 网关长连接上报
- 引入基于 SM3 算法的动态计算鉴权与连接机制
- 新增 timestamp 周期自动刷新与长连接重连防超时过期
- 独立的北向应用管理及南向子设备点组的注册、上线与数据上送控制

## [0.2.0]

### 新增
- 完整的 Vue 多语言国际化（i18n）架构，支持中英双语一键切换

### 改进
- 重构 Modbus 驱动调度器，实现连续寄存器地址智能自动合并及单次报文多点位批量高效读取，大幅提高采集吞吐率

## [0.1.0]

### 新增
- 单二进制边缘网关基础框架搭建
- 集成 R2DBC/GORM SQLite & PostgreSQL 存储双驱动
- JWT 安全登录认证
- Modbus TCP 南向采集驱动
- 北向 MQTT 客户端上送
- Vue 3 + Vite + Naive UI Web 管理控制台

[Unreleased]: https://github.com/jhzhang09/jetlinks-edge/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/jhzhang09/jetlinks-edge/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/jhzhang09/jetlinks-edge/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/jhzhang09/jetlinks-edge/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/jhzhang09/jetlinks-edge/releases/tag/v0.1.0
