#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_ROOT="$REPO_ROOT/shiftory-server"
BIN_DIR="${SHIFTORY_BIN_DIR:-$REPO_ROOT/bin}"
if [[ "$BIN_DIR" != /* ]]; then
  BIN_DIR="$REPO_ROOT/$BIN_DIR"
fi

usage() {
  cat <<'EOF'
Usage: scripts/start-linux.sh <build|migrate|run>

Environment:
  SHIFTORY_ENV_FILE       Optional explicit env file path.
  SHIFTORY_BIN_DIR        Output directory for compiled binaries (default: ./bin).
EOF
}

build() {
  mkdir -p "$BIN_DIR"
  (cd "$SERVER_ROOT" && go build -o "$BIN_DIR/shiftory-api" ./cmd/api)
  (cd "$SERVER_ROOT" && go build -o "$BIN_DIR/shiftory-migrate" ./cmd/migrate)
}

migrate() {
  "$BIN_DIR/shiftory-migrate"
}

run() {
  exec "$BIN_DIR/shiftory-api"
}

case "${1:-}" in
  build) build ;;
  migrate) migrate ;;
  run) run ;;
  *) usage >&2; exit 2 ;;
esac
