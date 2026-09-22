#!/bin/sh
set -eu
# 迁移成功后才启动 API，两者接收同一组模式与配置参数
/app/shiftory-migrate "$@"
exec /app/shiftory-api "$@"
