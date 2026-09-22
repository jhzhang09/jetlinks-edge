# ---- JetLinks Edge Dockerfile ----
# 多阶段构建：node 构建前端 → Go 编译 → alpine 运行时
# 用法：
#   make bundle     # 构建 frontend + build Go binary
#   docker build -t jetlinks-edge:local .
# 或直接：
#   make docker
#
# 运行：
#   docker run -d -p 7001:7001 -v $(pwd)/data:/app/data jetlinks-edge:local

# ---- 阶段 1：构建前端 ----
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- 阶段 2：构建 Go 二进制 ----
FROM golang:1.26-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/jetlinks-edge ./cmd/jetlinks-edge

# ---- 阶段 3：运行时镜像 ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
RUN addgroup -g 10001 -S jetlinks && adduser -u 10001 -S -G jetlinks -h /app jetlinks
COPY --from=go-build /out/jetlinks-edge /app/jetlinks-edge
COPY --from=web /src/web/dist /app/web/dist
COPY config.yaml /app/config.yaml
COPY plugins /app/plugins
RUN mkdir -p /app/data && chown -R jetlinks:jetlinks /app

ENV JETLINKS_EDGE_WEB_ADDR=0.0.0.0:7001 \
    JETLINKS_EDGE_WEB_STATIC_DIR=/app/web/dist \
    JETLINKS_EDGE_STORAGE_DSN=/app/data/jetlinks-edge.db

EXPOSE 7001
VOLUME ["/app/data"]
USER jetlinks:jetlinks
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -T 2 -O /dev/null http://127.0.0.1:7001/healthz || exit 1
ENTRYPOINT ["/app/jetlinks-edge", "-c", "/app/config.yaml"]
