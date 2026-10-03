#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "[失败] 应用服务安装未完成，请检查上方错误" >&2' ERR

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "[检查] 安装权限、部署文件与目录"
if [[ "$EUID" != 0 ]]; then
  echo "请使用 sudo bash $0" >&2
  exit 1
fi
if [[ ! "$APP_DIR" =~ ^/[a-zA-Z0-9_./-]+$ || "$APP_DIR" == /home/* || "$APP_DIR" == /root/* ]]; then
  echo "[失败] 请将部署包放到 /opt/shiftory 等系统目录，路径不能包含空格或特殊字符" >&2
  exit 1
fi
test -x "$APP_DIR/bin/shiftory-api"
test -f "$APP_DIR/web/index.html"
test -f "$APP_DIR/config/production.yaml"
test -f "$APP_DIR/deploy/shiftory.service"

echo "[配置] 准备应用运行环境"
id shiftory >/dev/null 2>&1 || useradd --system --user-group --home-dir "$APP_DIR" --shell /usr/sbin/nologin shiftory
mkdir -p "$APP_DIR/uploads" "$APP_DIR/var"
chown -R shiftory:shiftory "$APP_DIR/uploads" "$APP_DIR/var"
chown root:shiftory "$APP_DIR/config/production.yaml"
chmod 640 "$APP_DIR/config/production.yaml"

echo "[创建] /etc/systemd/system/shiftory.service，自动填写部署路径"
sed "s|/YOUR/DEPLOY/DIRECTORY|$APP_DIR|g" "$APP_DIR/deploy/shiftory.service" > /etc/systemd/system/shiftory.service
chmod 644 /etc/systemd/system/shiftory.service
echo "[检查] systemd 服务配置"
systemd-analyze verify /etc/systemd/system/shiftory.service
echo "[启动] 应用服务与开机自启"
systemctl daemon-reload
systemctl enable --now shiftory
echo "[检查] 应用运行状态"
systemctl is-active shiftory
echo "[完成] 应用服务已安装，日志查看命令: sudo journalctl -u shiftory -n 50 --no-pager"
