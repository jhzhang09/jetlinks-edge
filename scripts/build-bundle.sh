#!/bin/bash
# 在 macOS/Linux 主机上打包自包含的发布包。
# **每次都会重新编译 Go 二进制和前端**，确保产物与源码一致。
#
# 用法（单个）：
#   bash build-bundle.sh                          # 打包当前平台
#   GOOS=linux GOARCH=arm64 bash build-bundle.sh    # 打包指定平台
#
# 用法（全部平台）：
#   bash build-bundle.sh --all                      # 打包所有常见平台
#
# 产物：
#   单个：deployments/edge-bundle.tar.gz  (或 edge-bundle-{os}-{arch}.tar.gz)
#   全部：deployments/edge-bundle-{os}-{arch}.tar.gz + sha256sums.txt
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# 脚本位于 jetlinks-edge/scripts/，所以 ROOT 是它的父目录。
ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
cd "$ROOT"

# 版本号：优先使用 VERSION 环境变量，其次 git describe，最后 fallback dev
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')}"
echo ">>> version: $VERSION"

ALL_PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
)

MODE="single"
OUTPUT_DIR="deployments"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --all)
      MODE="all"
      shift
      ;;
    --local)
      MODE="single"
      shift
      ;;
    --output)
      [[ -n "${2:-}" ]] || { echo "ERROR: --output requires a directory" >&2; exit 1; }
      OUTPUT_DIR="$2"
      shift 2
      ;;
    *)
      echo "ERROR: unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# Go 默认按 CPU 数并发编译 package，低资源机器或进程数紧张时容易触发
# fork/exec resource temporarily unavailable。全平台发布打包优先稳定，必要时可通过
# GO_BUILD_PARALLELISM=4 bash scripts/build-bundle.sh --all 覆盖来换取速度。
GO_BUILD_PARALLELISM="${GO_BUILD_PARALLELISM:-1}"
[[ "$GO_BUILD_PARALLELISM" =~ ^[1-9][0-9]*$ ]] || {
  echo "ERROR: GO_BUILD_PARALLELISM must be a positive integer" >&2
  exit 1
}

COMMAND_RETRY_ATTEMPTS="${COMMAND_RETRY_ATTEMPTS:-3}"
COMMAND_RETRY_DELAY_SECONDS="${COMMAND_RETRY_DELAY_SECONDS:-2}"
PLATFORM_SETTLE_SECONDS="${PLATFORM_SETTLE_SECONDS:-3}"
[[ "$COMMAND_RETRY_ATTEMPTS" =~ ^[1-9][0-9]*$ ]] || {
  echo "ERROR: COMMAND_RETRY_ATTEMPTS must be a positive integer" >&2
  exit 1
}
[[ "$COMMAND_RETRY_DELAY_SECONDS" =~ ^[0-9]+$ ]] || {
  echo "ERROR: COMMAND_RETRY_DELAY_SECONDS must be a non-negative integer" >&2
  exit 1
}
[[ "$PLATFORM_SETTLE_SECONDS" =~ ^[0-9]+$ ]] || {
  echo "ERROR: PLATFORM_SETTLE_SECONDS must be a non-negative integer" >&2
  exit 1
}

# 资源耗尽时连 sleep 都可能无法 fork，因此这里用 bash 内建变量做短暂停顿。
wait_without_fork() {
  local seconds=${1:-$COMMAND_RETRY_DELAY_SECONDS}
  local end=$((SECONDS + seconds))
  while (( SECONDS < end )); do
    :
  done
}

run_with_retries() {
  local attempt=1
  local status=0

  while true; do
    local restore_errexit=0
    case "$-" in
      *e*)
        restore_errexit=1
        set +e
        ;;
    esac
    "$@"
    status=$?
    if [[ "$restore_errexit" -eq 1 ]]; then
      set -e
    fi
    if [[ "$status" -eq 0 ]]; then
      return 0
    fi
    if [[ "$attempt" -ge "$COMMAND_RETRY_ATTEMPTS" ]]; then
      return "$status"
    fi

    echo "    WARN: command failed with exit ${status}, retrying (${attempt}/${COMMAND_RETRY_ATTEMPTS}): $*" >&2
    wait_without_fork "$COMMAND_RETRY_DELAY_SECONDS"
    attempt=$((attempt + 1))
  done
}

run_with_retries mkdir -p "$OUTPUT_DIR"

build_frontend() {
  if ! command -v npm >/dev/null 2>&1; then
    if [[ -d "web/dist" ]]; then
      echo ">>> npm not found, but web/dist already exists. Using existing frontend build."
      return 0
    else
      echo "ERROR: npm not found and web/dist does not exist. Cannot build frontend." >&2
      return 1
    fi
  fi
  echo ">>> building web frontend ..."
  cd web
  npm install --no-audit --no-fund
  npm run build
  cd "$ROOT"
}

build_one() {
  local os=$1 arch=$2
  local pure_arch=${arch}
  local exe_name="jetlinks-edge"
  if [[ "$os" == "windows" ]]; then
    exe_name="jetlinks-edge.exe"
  fi

  local bin_dir="bin/${os}_${pure_arch}"
  local bundle="$OUTPUT_DIR/edge-bundle-${os}-${pure_arch}"

  echo ">>> building for ${os}/${arch} ..."
  run_with_retries rm -rf "$bundle" || return 1
  run_with_retries mkdir -p "$bin_dir" "$bundle/bin" || return 1

  run_with_retries env GOMAXPROCS="$GO_BUILD_PARALLELISM" CGO_ENABLED=0 GOOS="$os" GOARCH="${arch}" go build -buildvcs=false -p "$GO_BUILD_PARALLELISM" -ldflags="-s -w -X main.version=${VERSION}" -o "${bin_dir}/${exe_name}" ./cmd/jetlinks-edge || {
    echo "    WARN: build failed for ${os}/${arch}, skipping"
    run_with_retries rm -rf "$bundle" || true
    return 1
  }

  if command -v upx >/dev/null 2>&1; then
    echo "    UPX compressing ${bin_dir}/${exe_name} ..."
    upx -9 "${bin_dir}/${exe_name}" || echo "    WARN: UPX compression failed for ${bin_dir}/${exe_name}, skipping"
  else
    echo "    WARN: upx not found, skipping compression"
  fi

  run_with_retries cp "${bin_dir}/${exe_name}" "$bundle/bin/${exe_name}" || return 1

  # （前端构建产物已在编译时通过 go:embed 机制直接内嵌进二进制中，无需额外拷贝）

  # 复制配置文件（示例）
  run_with_retries cp config.yaml "$bundle/config.yaml" || return 1

  # README
  local readme
  read -r -d '' readme <<EOF || true
JetLinks Edge ${VERSION} — ${os}/${pure_arch}
========================================

JetLinks 平台的 Go 语言边缘网关。Modbus TCP 采集 → JetLinks MQTT 上送。

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  快速开始
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  1. 解压
     tar xzf edge-bundle-${os}-${pure_arch}.tar.gz
     cd edge-bundle-${os}-${pure_arch}

  2. 直接运行（测试）
     chmod +x bin/${exe_name}
     ./bin/${exe_name} -c config.yaml
     → 访问 http://localhost:7001  · 登录 admin / admin123

  3. 安装到系统（生产）
     sudo /opt/edge-bundle/scripts/install.sh
     # 或自定义安装路径：
     sudo /opt/edge-bundle/scripts/install.sh --prefix /srv/jetlinks-edge --port 8080

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  包内文件
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  bin/${exe_name}           Go 编译后的二进制（已内置静态网页服务，自包含运行）
  config.yaml               配置文件模板（默认开启内嵌前端模式）
  scripts/install.sh        一键安装脚本（需 root）
  scripts/uninstall.sh      一键卸载脚本
  README.txt                本文件

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  安装后（install.sh 做了什么）
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  - 创建 jetlinks 系统用户
  - 复制二进制到 /opt/jetlinks-edge/
  - 生成 /opt/jetlinks-edge/config.yaml（全部绝对路径，不受 cwd 影响）
  - 写入 systemd 单元 → 开机自启 + 崩溃自动拉起（Restart=on-failure）
  - 启用 systemd 安全加固

  日常操作：
    journalctl -u jetlinks-edge -f       # 查看实时日志
    sudo systemctl restart jetlinks-edge # 重启
    sudo systemctl status  jetlinks-edge # 查看状态

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  配置（config.yaml 关键项）
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  web:
    addr: "0.0.0.0:7001"          # 监听地址和端口
    jwt_secret: "...change-me..."  # 生产请务必修改
    static_dir: ""                # 留空（默认开启二进制内嵌前端）

  storage:
    driver: sqlite                 # sqlite 或 postgres
    dsn: "/opt/jetlinks-edge/data/jetlinks-edge.db"

  log:
    level: info                    # debug / info / warn / error

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  使用流程
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  1. 登录 Web 管理界面（http://<ip>:7001）
  2. 创建网关（"北向网关" → "新建网关"）
     填写 JetLinks 平台的 Broker 地址 + gateway 凭据
  3. 创建子设备（"南向子设备" → "新建点组"）
     选择驱动 modbus-tcp，配置 Modbus 地址和点位
  4. 在 JetLinks 平台查看设备数据

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  技术支持
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  项目地址: https://github.com/jetlinks/jetlinks-community
  文档目录: docs/
EOF
  printf '%s\n' "$readme" > "$bundle/README.txt"

  run_with_retries mkdir -p "$bundle/scripts" || return 1
  run_with_retries cp "$SCRIPT_DIR/install.sh" "$bundle/scripts/" || return 1
  run_with_retries cp "$SCRIPT_DIR/uninstall.sh" "$bundle/scripts/" || return 1

  run_with_retries tar czf "$OUTPUT_DIR/edge-bundle-${os}-${pure_arch}.tar.gz" -C "$OUTPUT_DIR" "edge-bundle-${os}-${pure_arch}" || return 1
  run_with_retries rm -rf "$bundle" || return 1
}

build_all() {
  echo "=== Building bundles for all platforms ===" >&2
  run_with_retries rm -f "$OUTPUT_DIR"/edge-bundle-*.tar.gz "$OUTPUT_DIR"/sha256sums.txt || return 1

  local built=0
  local failed=0
  local failed_platforms=()
  for plat in "${ALL_PLATFORMS[@]}"; do
    local os="${plat%/*}"
    local arch="${plat##*/}"
    if build_one "$os" "$arch"; then
      built=$((built + 1))
    else
      failed=$((failed + 1))
      failed_platforms+=("$plat")
    fi
    wait_without_fork "$PLATFORM_SETTLE_SECONDS"
  done

  if [[ "$built" -eq 0 ]]; then
    echo "ERROR: no bundle was built successfully" >&2
    return 1
  fi

  if [[ "$failed" -gt 0 ]]; then
    echo "ERROR: failed platform(s): ${failed_platforms[*]}" >&2
    return 1
  fi

  # sha256
  cd "$OUTPUT_DIR"
  if command -v shasum >/dev/null 2>&1; then
    run_with_retries shasum -a 256 edge-bundle-*.tar.gz > sha256sums.txt || { cd "$ROOT"; return 1; }
  elif command -v sha256sum >/dev/null 2>&1; then
    run_with_retries sha256sum edge-bundle-*.tar.gz > sha256sums.txt || { cd "$ROOT"; return 1; }
  else
    echo "WARN: shasum/sha256sum not found, sha256sums.txt was not generated" >&2
  fi
  cd "$ROOT"

  echo ""
  echo "=== All bundles ready ===" >&2
  ls -lh "$OUTPUT_DIR"/edge-bundle-*.tar.gz "$OUTPUT_DIR"/sha256sums.txt 2>/dev/null >&2 || true
}

# 每次都重新构建前端，确保产物和源码一致
build_frontend

if [[ "$MODE" == "all" ]]; then
  build_all
else
  OS="${GOOS:-$(go env GOOS)}"
  ARCH="${GOARCH:-$(go env GOARCH)}"
  build_one "$OS" "$ARCH"
  mv "$OUTPUT_DIR/edge-bundle-${OS}-${ARCH}.tar.gz" "$OUTPUT_DIR/edge-bundle.tar.gz" 2>/dev/null || true
fi
