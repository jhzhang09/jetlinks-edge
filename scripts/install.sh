#!/bin/bash
# 在 Linux 服务器上首次安装 JetLinks Edge。
# 需 root 权限（创建用户、目录、systemd 单元）。
#
# 用法：
#   sudo ./install.sh              # 默认安装到 /opt/jetlinks-edge
#   sudo ./install.sh --prefix /srv/jetlinks-edge   # 自定义路径
#   sudo ./install.sh --no-systemd                  # 只装文件，不注册服务
set -euo pipefail

PREFIX=/opt/jetlinks-edge
USER=jetlinks
SERVICE_NAME=jetlinks-edge
ENABLE_SYSTEMD=1
WEB_PORT=7001

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix)    PREFIX="$2"; shift 2 ;;
    --user)      USER="$2"; shift 2 ;;
    --port)      WEB_PORT="$2"; shift 2 ;;
    --no-systemd) ENABLE_SYSTEMD=0; shift ;;
    -h|--help)
      sed -n '2,15p' "$0"; exit 0 ;;
    *) echo "unknown: $1" >&2; exit 1 ;;
  esac
done

# 0. 检查 root
if [[ $EUID -ne 0 ]]; then
  echo "ERROR: please run as root (sudo $0)" >&2
  exit 1
fi

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# install.sh 位于 edge-bundle-xxx/scripts/，BUNDLE_ROOT 是 bundle 包根目录
BUNDLE_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
echo "=== JetLinks Edge Installer ==="
echo "  prefix: $PREFIX"
echo "  user:   $USER"
echo "  port:   $WEB_PORT"

# 1. 创建用户（不存在时）
if ! id "$USER" >/dev/null 2>&1; then
  echo "[1/5] creating system user $USER ..."
  useradd -r -s /bin/false -d "$PREFIX" "$USER"
else
  echo "[1/5] user $USER already exists"
fi

# 2. 建目录并复制文件
echo "[2/5] installing files to $PREFIX ..."
mkdir -p "$PREFIX" "$PREFIX/data" "$PREFIX/web"
cp -f "$BUNDLE_ROOT/bin/jetlinks-edge" "$PREFIX/jetlinks-edge"
chmod +x "$PREFIX/jetlinks-edge"
cp -r "$BUNDLE_ROOT/web/." "$PREFIX/web/"

# 3. 生成 config.yaml（绝对路径，避免 cwd 问题）
cat > "$PREFIX/config.yaml" <<EOF
# 由 install.sh 自动生成。生产建议改 jwt_secret。
web:
  addr: "0.0.0.0:${WEB_PORT}"
  jwt_secret: "change-me-$(date +%s)"
  token_ttl: 24h
  default_user: admin
  default_password: admin123
  static_dir: "${PREFIX}/web/dist"

log:
  level: info
  output: stdout

storage:
  driver: sqlite
  dsn: "${PREFIX}/data/jetlinks-edge.db"

collector:
  max_concurrency: 100
  read_timeout: 3s
  write_timeout: 3s
  reconnect_delay: 5s
EOF

# 4. 数据目录权限
chown -R "$USER":"$USER" "$PREFIX"
chmod 700 "$PREFIX/data"

# 5. systemd 单元
if [[ $ENABLE_SYSTEMD -eq 1 ]] && command -v systemctl >/dev/null 2>&1; then
  echo "[3/5] installing systemd unit ..."
  cat > /etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=JetLinks Edge Industrial Gateway
After=network.target

[Service]
Type=simple
User=${USER}
Group=${USER}
WorkingDirectory=${PREFIX}
ExecStart=${PREFIX}/jetlinks-edge -c ${PREFIX}/config.yaml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

StandardOutput=journal
StandardError=journal

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${PREFIX}/data ${PREFIX}/web
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable ${SERVICE_NAME}
  echo "[4/5] starting service ..."
  systemctl restart ${SERVICE_NAME}
  sleep 2
  systemctl --no-pager --full status ${SERVICE_NAME} | head -10 || true
  echo
  echo "[5/5] DONE. View logs: journalctl -u ${SERVICE_NAME} -f"
else
  echo "[3/5] systemd unavailable or skipped. Start manually: ${PREFIX}/jetlinks-edge -c ${PREFIX}/config.yaml"
fi

echo
echo "=== install complete ==="
echo "  binary:   ${PREFIX}/jetlinks-edge"
echo "  config:   ${PREFIX}/config.yaml"
echo "  data:     ${PREFIX}/data/"
echo "  web/dist: ${PREFIX}/web/dist/"
echo "  web URL:  http://<server-ip>:${WEB_PORT}"
echo "  login:    admin / admin123"
