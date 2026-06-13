# ---- JetLinks Edge Makefile ----
# 目标：
#   make build       构建后端二进制（版本号自动从 git tag 注入）
#   make run         本地直接运行（需先 build）
#   make dev         开发模式：同时启动后端 + 前端 vite dev server
#   make web         构建前端
#   make test        运行 Go 单元测试
#   make lint        运行 golangci-lint 静态检查
#   make bundle      打自包含 Linux 发布包（无 nginx 依赖，适合小机器）
#   make docker      打包 Docker 镜像
#   make tidy        整理 go.mod

GO ?= go
APP := jetlinks-edge
DIST := web/dist
BIN := bin/$(APP)

# 版本号：优先使用 VERSION 环境变量，其次 git describe，最后 fallback dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run dev web web-dev test lint vet bundle bundle-all docker tidy clean \
	bundle-docker bundle-all-docker

all: web build

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/jetlinks-edge
	@if command -v upx >/dev/null 2>&1; then \
		echo "Compressing $(BIN) with UPX..."; \
		upx -9 $(BIN); \
	else \
		echo "Warning: upx command not found, skipping compression."; \
	fi

run: build
	./$(BIN) -c config.yaml

web:
	cd web && npm install --no-audit --no-fund && npm run build

# 开发模式：后端（端口 7001）+ 前端 vite（端口 5173 代理 /api 到 7001）
# Ctrl-C 同时终止两个进程。
dev: build
	@trap 'kill 0' INT TERM; \
	./$(BIN) -c config.yaml & \
	cd web && npm install --no-audit --no-fund && npm run dev & \
	wait

web-dev:
	cd web && npm install --no-audit --no-fund && npm run dev

# ---- 测试与静态检查 ----

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "Warning: golangci-lint not found, skipping. Install: https://golangci-lint.run/usage/install/"; \
	fi

tidy:
	$(GO) mod tidy

docker:
	docker build -t jetlinks-edge:local -f Dockerfile .

# 打包自包含发布包（默认当前平台）
#   make bundle         # → deployments/edge-bundle.tar.gz（当前平台）
#   make bundle-all     # → 全部平台 + sha256sums.txt
bundle:
	VERSION=$(VERSION) bash scripts/build-bundle.sh

bundle-all:
	VERSION=$(VERSION) bash scripts/build-bundle.sh --all

# 使用 Docker 进行容器化打包（无需本地安装 upx 和 node 环境）
# 能够完美实现各平台二进制的编译及自动 upx 瘦身压缩
bundle-docker: web
	docker run --rm \
		-v "$$(pwd)":/app \
		-w /app \
		golang:1.23-bookworm \
		bash -c "apt-get update && apt-get install -y xz-utils && curl -L -o /tmp/upx.tar.xz https://github.com/upx/upx/releases/download/v4.2.4/upx-4.2.4-amd64_linux.tar.xz && tar -xf /tmp/upx.tar.xz -C /tmp && mv /tmp/upx-4.2.4-amd64_linux/upx /usr/local/bin/ && VERSION=$(VERSION) bash scripts/build-bundle.sh"

bundle-all-docker: web
	docker run --rm \
		-v "$$(pwd)":/app \
		-w /app \
		golang:1.23-bookworm \
		bash -c "apt-get update && apt-get install -y xz-utils && curl -L -o /tmp/upx.tar.xz https://github.com/upx/upx/releases/download/v4.2.4/upx-4.2.4-amd64_linux.tar.xz && tar -xf /tmp/upx.tar.xz -C /tmp && mv /tmp/upx-4.2.4-amd64_linux/upx /usr/local/bin/ && VERSION=$(VERSION) bash scripts/build-bundle.sh --all"

clean:
	rm -rf bin web/dist deployments/*.tar.gz deployments/sha256sums.txt
