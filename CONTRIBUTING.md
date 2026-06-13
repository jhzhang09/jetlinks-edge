# 贡献指南

感谢你对 JetLinks Edge 的关注！以下是参与贡献的指引，请在提交 PR 前阅读。

## 开发环境准备

| 工具 | 最低版本 | 说明 |
|------|---------|------|
| Go | 1.23+ | 后端编译 |
| Node.js | 20+ | 前端构建 |
| npm | 随 Node 附带 | 前端依赖管理 |
| Make | — | 可选，用于快捷构建命令 |
| golangci-lint | latest | Go 代码静态检查 |

### 克隆与初始化

```bash
git clone https://github.com/jhzhang09/jetlinks-edge.git
cd jetlinks-edge

# 后端依赖
go mod download

# 前端依赖
cd web && npm ci && cd ..
```

## 构建与测试

### 后端

```bash
# 编译
make build

# 运行全部测试
go test ./...

# 代码检查
golangci-lint run ./...
```

### 前端

```bash
cd web

# 开发模式（热更新）
npm run dev

# 生产构建
npm run build
```

### 全平台打包

```bash
bash scripts/build-bundle.sh --all
# 产物输出到 deployments/
```

## 分支策略

- **`main`**：稳定主干分支，始终保持可构建状态。
- **`feature/xxx`**：功能开发分支，从 `main` 创建，完成后合并回 `main`。
- **`fix/xxx`**：缺陷修复分支，同上。
- **`release/vX.Y`**：发布准备分支（如需要）。

```
feature/xxx ──→ main
fix/xxx     ──→ main
```

> 请勿直接向 `main` 推送提交，一律通过 Pull Request 合并。

## 提交规范

本项目遵循 [Conventional Commits](https://www.conventionalcommits.org/zh-hans/) 规范。

### 格式

```
<类型>(<作用域>): <简要描述>

[可选正文]

[可选脚注]
```

### 类型

| 类型 | 说明 |
|------|------|
| `feat` | 新功能 |
| `fix` | 缺陷修复 |
| `perf` | 性能优化 |
| `refactor` | 重构（不改变外部行为） |
| `docs` | 文档变更 |
| `test` | 测试补充或修改 |
| `chore` | 构建、CI、依赖等辅助变更 |
| `style` | 代码格式（不影响逻辑） |

### 作用域示例

`modbus`、`mqtt`、`web`、`store`、`api`、`build`、`ci`、`i18n`

### 示例

```
feat(modbus): 支持 Modbus RTU 串口驱动

实现 RS485/RS232 串口通道采集，复用现有点位模型和字节序配置。

Closes #42
```

## Pull Request 要求

1. **一个 PR 只做一件事**：避免在同一个 PR 中混合功能开发和无关重构。
2. **通过 CI 检查**：确保 lint、测试和前端构建全部通过。
3. **描述清晰**：说明改了什么、为什么改、如何验证。
4. **更新文档**：如果改动影响用户可见行为，请同步更新 `README.md` 或 `docs/`。
5. **更新变更日志**：在 `CHANGELOG.md` 的 `[Unreleased]` 下添加条目。

### PR 描述模板

```markdown
## 变更说明

简要说明本 PR 的目的和改动内容。

## 验证方式

- [ ] `go test ./...` 通过
- [ ] `golangci-lint run ./...` 无新增问题
- [ ] 前端 `npm run build` 构建成功
- [ ] 手动验证：<描述验证步骤>

## 关联 Issue

Closes #xxx
```

## 代码规范

- Go 代码遵循 [Effective Go](https://go.dev/doc/effective_go) 和 `golangci-lint` 默认规则。
- 前端代码遵循项目 ESLint 配置。
- 注释解释"为什么"，而不只是重复"做了什么"。
- 公共 API 和导出函数必须有 GoDoc 注释。

## 许可证

提交贡献即表示你同意将代码以 [Apache 2.0](./LICENSE) 许可证发布。
