# ---- JetLinks Edge Makefile ----
# 目标：
#   make build       同时构建前端与后端二进制（版本号自动从 git tag 注入，嵌入最新前端静态资源）
#   make run         本地运行边缘服务（自动完成最新编译后启动）
#   make dev         开发模式：启动后端（端口 7001）+ 前端 Vite 热重载服务（端口 5173）
#   make test        运行 Go 单元测试
#   make lint        运行静态代码检查
#   make bundle      打包发布包（自包含当前平台）
#   make bundle-all  打包全平台发布包
#   make clean       清理编译产物

GO ?= go
APP := jetlinks-edge
BIN := bin/$(APP)

# 版本号：优先使用 VERSION 环境变量，其次 git describe，最后 fallback dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run dev web test lint vet bundle bundle-all docker tidy clean \
	bundle-docker bundle-all-docker

all: build

# 核心构建：先自动编译前端，再编译嵌入最新静态资源的 Go 后端
build: web
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/jetlinks-edge
	@if command -v upx >/dev/null 2>&1; then \
		echo "Compressing $(BIN) with UPX..."; \
		upx -9 $(BIN) >/dev/null 2>&1 || true; \
	fi

run: build
	./$(BIN) -c config.yaml

# 构建前端：若 node_modules 缺失则自动安装，随后进行前端生产打包
web:
	@if [ ! -d "web/node_modules" ]; then \
		echo "Installing frontend dependencies..."; \
		cd web && npm install --no-audit --no-fund; \
	fi
	cd web && npm run build

# 开发模式：后端（端口 7001）+ 前端 vite（端口 5173 代理 /api 到 7001）
# Ctrl-C 同时终止两个进程。
dev: build
	@trap 'kill 0' INT TERM; \
	./$(BIN) -c config.yaml & \
	cd web && npm run dev & \
	wait

# ---- 测试与静态检查 ----

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
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
		golang:1.26-bookworm \
		bash -c "apt-get update && apt-get install -y xz-utils && curl -L -o /tmp/upx.tar.xz https://github.com/upx/upx/releases/download/v4.2.4/upx-4.2.4-amd64_linux.tar.xz && tar -xf /tmp/upx.tar.xz -C /tmp && mv /tmp/upx-4.2.4-amd64_linux/upx /usr/local/bin/ && VERSION=$(VERSION) bash scripts/build-bundle.sh"

bundle-all-docker: web
	docker run --rm \
		-v "$$(pwd)":/app \
		-w /app \
		golang:1.26-bookworm \
		bash -c "apt-get update && apt-get install -y xz-utils && curl -L -o /tmp/upx.tar.xz https://github.com/upx/upx/releases/download/v4.2.4/upx-4.2.4-amd64_linux.tar.xz && tar -xf /tmp/upx.tar.xz -C /tmp && mv /tmp/upx-4.2.4-amd64_linux/upx /usr/local/bin/ && VERSION=$(VERSION) bash scripts/build-bundle.sh --all"

clean:
	rm -rf bin web/dist deployments/*.tar.gz deployments/sha256sums.txt
