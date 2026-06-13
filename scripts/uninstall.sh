#!/bin/bash
# 卸载 JetLinks Edge：停服务、删文件、删用户、删数据。
# 数据目录默认会被删除（除非指定 --keep-data）。
set -euo pipefail

PREFIX=/opt/jetlinks-edge
USER=jetlinks
SERVICE_NAME=jetlinks-edge
KEEP_DATA=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix)   PREFIX="$2"; shift 2 ;;
    --user)     USER="$2"; shift 2 ;;
    --keep-data) KEEP_DATA=1; shift ;;
    *) echo "unknown: $1" >&2; exit 1 ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "ERROR: please run as root" >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  echo "[1/3] stopping service ..."
  systemctl stop ${SERVICE_NAME} 2>/dev/null || true
  systemctl disable ${SERVICE_NAME} 2>/dev/null || true
  rm -f /etc/systemd/system/${SERVICE_NAME}.service
  systemctl daemon-reload
fi

if [[ $KEEP_DATA -eq 0 ]]; then
  echo "[2/3] removing $PREFIX (with data) ..."
  rm -rf "$PREFIX"
else
  echo "[2/3] removing $PREFIX/jetlinks-edge + $PREFIX/web (keeping data) ..."
  rm -f "$PREFIX/jetlinks-edge"
  rm -rf "$PREFIX/web"
fi

if id "$USER" >/dev/null 2>&1; then
  echo "[3/3] removing user $USER ..."
  userdel -r "$USER" 2>/dev/null || true
fi

echo "=== uninstall complete ==="
