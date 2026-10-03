#!/usr/bin/env bash
set -Eeuo pipefail
trap 'echo "[失败] 部署包未完成，请检查上方错误" >&2' ERR

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT="${1:-$REPO_ROOT/release/shiftory-$(date +%Y%m%d-%H%M%S)}"
[[ "$OUTPUT" == /* ]] || OUTPUT="$PWD/$OUTPUT"
export GOTOOLCHAIN=go1.26.8

echo "[检查] 构建环境与输出目录"
command -v go >/dev/null
command -v npm >/dev/null
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); if (!(major === 22 && minor >= 18 || major === 24 && minor >= 12 || major > 24)) throw Error("需要 Node.js 22.18+ 或 24.12+")'
if [[ -e "$OUTPUT" ]]; then
  echo "[失败] 输出目录已存在，请指定新目录: $OUTPUT" >&2
  exit 1
fi
echo "[安装] 前端依赖，使用 pnpm@11.19.0 和锁文件"
cd "$REPO_ROOT/shiftory-web"
npm exec --yes --package=pnpm@11.19.0 -- pnpm install --frozen-lockfile
echo "[检查] 前端类型"
npm exec --yes --package=pnpm@11.19.0 -- pnpm type-check
echo "[构建] 前端静态页面"
npm exec --yes --package=pnpm@11.19.0 -- pnpm build-only

echo "[构建] Linux 后端，架构 ${GOARCH:-amd64}"
mkdir -p "$OUTPUT/bin" "$OUTPUT/web" "$OUTPUT/config" "$OUTPUT/scripts" "$OUTPUT/deploy"
cd "$REPO_ROOT/shiftory-server"
CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" go build -trimpath -ldflags='-s -w' -o "$OUTPUT/bin/shiftory-api" ./cmd/api
CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" go build -trimpath -ldflags='-s -w' -o "$OUTPUT/bin/shiftory-migrate" ./cmd/migrate

echo "[打包] 前端、公开配置样例、基础服务脚本与 systemd 模板"
cp -R "$REPO_ROOT/shiftory-web/dist/." "$OUTPUT/web/"
sed -e 's|web_dir: ../shiftory-web/dist|web_dir: ./web|' \
  -e 's|public_origin: http://localhost:18763|public_origin: http://YOUR_SERVER_IP:18763|' \
  -e 's|password: "123456"|password: "YOUR_SHIFTORY_DATABASE_PASSWORD"|' \
  "$REPO_ROOT/config/production.example.yaml" > "$OUTPUT/config/production.yaml"
cp "$REPO_ROOT/scripts/start-services.sh" "$REPO_ROOT/scripts/install-service.sh" "$OUTPUT/scripts/"
cp "$REPO_ROOT/deploy/shiftory.service" "$OUTPUT/deploy/"
cp "$REPO_ROOT/docs/deployment.md" "$OUTPUT/DEPLOYMENT.md"
chmod 600 "$OUTPUT/config/production.yaml"
echo "[完成] 部署包: $OUTPUT"
echo "[下一步] 手动复制目录到服务器，按 DEPLOYMENT.md 填写配置并执行安装脚本"
