#!/usr/bin/env bash
#
# 本地构建 d-auth 裸机部署包（不产出镜像）。
#
# 产物：dist/d-auth-bare-<TAG>.tar.gz，解包后结构为
#   bin/server        Go API（linux/amd64 静态二进制）
#   bin/migrate       独立迁移器（可选，server 启动时也会自动迁移）
#   web/              Vite 构建的前端静态文件
#   migrations/       迁移 SQL
#
# 用法：
#   ./build.sh                 # 用 git describe 作为 TAG
#   TAG=v1.2.3 ./build.sh
#   BARE_GOARCH=arm64 ./build.sh
#
# 与 Dockerfile 的对应关系：
#   web 阶段 -> npm ci + build:sdk + build:web（顺序必须一致，web 依赖 sdk）
#   api 阶段 -> go build -trimpath -ldflags="-s -w"（CGO_ENABLED=0，纯静态）
# 因此本脚本产出的内容与镜像内 /app 和 /usr/share/nginx/html 完全同源。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TAG="${TAG:-$(git describe --tags --always --dirty 2>/dev/null || echo latest)}"
BARE_GOOS="${BARE_GOOS:-linux}"
BARE_GOARCH="${BARE_GOARCH:-amd64}"

STAGE="$ROOT/dist/bare"
ARCHIVE="$ROOT/dist/d-auth-bare-${TAG}.tar.gz"

echo "▶ 构建 d-auth 裸机包 ${TAG} (${BARE_GOOS}/${BARE_GOARCH})"

# ---------- 前端 ----------
if [ ! -d node_modules ]; then
  echo "▶ 安装前端依赖（首次）"
  npm ci --no-audit --no-fund
fi

echo "▶ 构建 SDK"
npm run build:sdk

echo "▶ 构建前端"
npm run build:web

if [ ! -f web/dist/index.html ]; then
  echo "❌ web/dist/index.html 不存在，前端构建失败" >&2
  exit 1
fi

# ---------- 后端 ----------
echo "▶ 交叉编译 Go 二进制"
mkdir -p "$STAGE/bin" "$STAGE/web" "$STAGE/migrations"
(
  cd api
  # CGO_ENABLED=0：依赖链里没有 cgo（pgx 纯 Go），编出的是可直接跑的静态二进制。
  CGO_ENABLED=0 GOOS="$BARE_GOOS" GOARCH="$BARE_GOARCH" \
    go build -trimpath -ldflags="-s -w" -o "$STAGE/bin/server" ./cmd/server
  CGO_ENABLED=0 GOOS="$BARE_GOOS" GOARCH="$BARE_GOARCH" \
    go build -trimpath -ldflags="-s -w" -o "$STAGE/bin/migrate" ./cmd/migrate
)

# ---------- 组装 ----------
echo "▶ 组装产物"
rm -rf "$STAGE/web"/* "$STAGE/migrations"/*
cp -R web/dist/. "$STAGE/web/"
cp api/migrations/*.sql "$STAGE/migrations/"
# 私钥不入包：它是运行时状态，由 install.sh 从现有容器/目录接管。
rm -f "$STAGE/data/jwt-private.pem" 2>/dev/null || true

echo "▶ 打包"
rm -f "$ARCHIVE"
tar -czf "$ARCHIVE" -C "$STAGE" .

echo ""
echo "✅ $ARCHIVE"
echo "   $(du -h "$ARCHIVE" | cut -f1)   server=$(du -h "$STAGE/bin/server" | cut -f1)"
echo ""
echo "下一步：make bare-deploy   （或 ./deploy.sh，会先构建再上传）"