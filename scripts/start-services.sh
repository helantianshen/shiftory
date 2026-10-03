#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "[失败] 基础服务配置未完成，请检查上方错误" >&2' ERR

ACTION="${1:-all}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
REDIS_PORT="${REDIS_PORT:-6379}"
if [[ "$ACTION" != all && "$ACTION" != postgres && "$ACTION" != redis ]]; then
  echo "用法: $0 [all|postgres|redis]" >&2
  exit 2
fi
echo "[检查] Docker 服务与权限"
docker info >/dev/null

if [[ "$ACTION" == all || "$ACTION" == postgres ]]; then
  if docker container inspect postgres >/dev/null 2>&1; then
    echo "[跳过] postgres 已存在，不修改容器、账号或数据"
    docker ps -a --filter name='^/postgres$' --format '{{.Names}} {{.Status}} {{.Ports}}'
  else
    # 已有数据卷必须由管理员确认恢复方式，不能按空库初始化
    if docker volume inspect postgres-data >/dev/null 2>&1; then
      echo "[失败] postgres-data 已存在，请先确认数据并手动恢复 postgres 容器" >&2
      exit 1
    fi
    echo "[配置] 输入 PostgreSQL 管理员和 Shiftory 应用密码"
    read -r -s -p 'PostgreSQL 管理员密码: ' POSTGRES_PASSWORD
    echo
    read -r -s -p 'Shiftory 数据库密码: ' SHIFTORY_POSTGRES_PASSWORD
    echo
    : "${POSTGRES_PASSWORD:?管理员密码不能为空}"
    : "${SHIFTORY_POSTGRES_PASSWORD:?应用密码不能为空}"
    export POSTGRES_PASSWORD SHIFTORY_POSTGRES_PASSWORD
    echo "[拉取] postgres:18"
    docker pull postgres:18
    echo "[启动] postgres，127.0.0.1:$POSTGRES_PORT，持久卷 postgres-data"
    docker run -d --name postgres --restart unless-stopped \
      -p "127.0.0.1:$POSTGRES_PORT:5432" -e POSTGRES_PASSWORD \
      -e 'POSTGRES_INITDB_ARGS=--encoding=UTF8 --locale=C' \
      -v postgres-data:/var/lib/postgresql \
      --health-cmd='pg_isready -U postgres -d postgres -h 127.0.0.1' \
      --health-interval=2s --health-timeout=3s --health-retries=30 postgres:18
    echo "[等待] PostgreSQL 初始化，最多等待 60 秒"
    for ((attempt=0; attempt<30; attempt++)); do
      if [[ "$(docker inspect --format '{{.State.Health.Status}}' postgres)" == healthy ]]; then break; fi
      sleep 2
    done
    [[ "$(docker inspect --format '{{.State.Health.Status}}' postgres)" == healthy ]]
    echo "[配置] 创建独立的 shiftory 账号与数据库"
    docker exec -i -e SHIFTORY_POSTGRES_PASSWORD postgres psql -U postgres -d postgres -v ON_ERROR_STOP=1 <<'SQL'
\getenv app_password SHIFTORY_POSTGRES_PASSWORD
CREATE ROLE shiftory LOGIN PASSWORD :'app_password';
CREATE DATABASE shiftory OWNER shiftory TEMPLATE template0 ENCODING 'UTF8';
SQL
    unset POSTGRES_PASSWORD SHIFTORY_POSTGRES_PASSWORD
    echo "[完成] PostgreSQL 已就绪，请将应用密码填写到 config/production.yaml"
  fi
fi

if [[ "$ACTION" == all || "$ACTION" == redis ]]; then
  if docker container inspect redis >/dev/null 2>&1; then
    echo "[跳过] redis 已存在，不修改容器或数据"
    docker ps -a --filter name='^/redis$' --format '{{.Names}} {{.Status}} {{.Ports}}'
  else
    echo "[拉取] redis:7-alpine"
    docker pull redis:7-alpine
    echo "[启动] redis，127.0.0.1:$REDIS_PORT，持久卷 redis-data"
    docker run -d --name redis --restart unless-stopped \
      -p "127.0.0.1:$REDIS_PORT:6379" -v redis-data:/data \
      --health-cmd='redis-cli ping' --health-interval=5s --health-timeout=3s --health-retries=10 \
      redis:7-alpine redis-server --appendonly yes --appendfsync everysec --maxmemory-policy noeviction
    echo "[检查] Redis 响应"
    for ((attempt=0; attempt<10; attempt++)); do
      if docker exec redis redis-cli ping; then break; fi
      sleep 1
    done
    docker exec redis redis-cli ping
    echo "[完成] Redis 已就绪，应用配置使用本机端口 $REDIS_PORT，默认无密码"
  fi
fi
echo "[完成] 基础服务脚本执行完毕"
