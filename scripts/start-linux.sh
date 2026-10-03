#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_ROOT="$REPO_ROOT/shiftory-server"
BIN_DIR="${SHIFTORY_BIN_DIR:-$REPO_ROOT/bin}"
if [[ "$BIN_DIR" != /* ]]; then
  BIN_DIR="$REPO_ROOT/$BIN_DIR"
fi

# usage 输出 Linux 构建、迁移与启动参数的用法
usage() {
  cat <<'EOF'
Usage: scripts/start-linux.sh <build|migrate|run> [--env development|production] [--config path.yaml]

Environment:
  SHIFTORY_BIN_DIR        Output directory for compiled binaries (default: ./bin).
EOF
}

# build 在后端目录构建 API 与一次性迁移程序到指定输出目录
build() {
  export GOTOOLCHAIN=go1.26.8
  mkdir -p "$BIN_DIR"
  (cd "$SERVER_ROOT" && go build -o "$BIN_DIR/shiftory-api" ./cmd/api)
  (cd "$SERVER_ROOT" && go build -o "$BIN_DIR/shiftory-migrate" ./cmd/migrate)
}

# migrate 将启动参数原样转交迁移程序并等待退出
migrate() {
  "$BIN_DIR/shiftory-migrate" "$@"
}

# run 以 API 替换当前 Shell 进程，使服务直接接收停机信号
run() {
  exec "$BIN_DIR/shiftory-api" "$@"
}

ACTION="${1:-}"
if [[ $# -gt 0 ]]; then shift; fi
case "$ACTION" in
  build) build ;;
  migrate) migrate "$@" ;;
  run) run "$@" ;;
  *) usage >&2; exit 2 ;;
esac
