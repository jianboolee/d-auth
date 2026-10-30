#!/usr/bin/env bash
#
# 上传并安装 d-auth 裸机包到业务机（本地执行）。
#
# 用法：
#   ./deploy.sh                 # 构建 -> 上传 -> 远端 install.sh
#   ./deploy.sh --no-build      # 用已有 tar 包，只上传安装
#   ./deploy.sh --check         # 只让远端做预检，不改动任何东西
#   PROD_SSH=root@1.2.3.4 ./deploy.sh
#
# 需要免密 SSH 到业务机。默认目标与 d-camp / d-specs 一致：root@47.95.176.70。
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
cd "$ROOT"

PROD_SSH="${PROD_SSH:-root@47.95.176.70}"
PROD_PORT="${PROD_PORT:-22}"
REMOTE_DIR="${REMOTE_DIR:-/opt/d-auth}"
INCOMING="/tmp/d-auth-deploy"
TAG="${TAG:-$(git describe --tags --always --dirty 2>/dev/null || echo latest)}"

NO_BUILD=0
CHECK_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --no-build) NO_BUILD=1 ;;
    --check)    CHECK_ONLY=1 ;;
    *) echo "未知参数：$arg" >&2; exit 1 ;;
  esac
done

ARCHIVE="$ROOT/dist/d-auth-bare-${TAG}.tar.gz"

SSH_OPTS=(-p "$PROD_PORT" -o BatchMode=yes -o ConnectTimeout=10)
SCP_OPTS=(-P "$PROD_PORT" -o BatchMode=yes -o ConnectTimeout=10)

if [ "$NO_BUILD" = "0" ] && [ "$CHECK_ONLY" = "0" ]; then
  "$DIR/build.sh"
fi

if [ "$CHECK_ONLY" = "0" ]; then
  [ -f "$ARCHIVE" ] || { echo "❌ 缺少 $ARCHIVE，先执行 ./build.sh" >&2; exit 1; }
fi

echo "🔌 检查 SSH 连通性 $PROD_SSH:$PROD_PORT"
ssh "${SSH_OPTS[@]}" "$PROD_SSH" 'true' || {
  echo "❌ 无法连接（检查免密登录与端口）" >&2
  exit 1
}

echo "📤 上传安装脚本"
ssh "${SSH_OPTS[@]}" "$PROD_SSH" "mkdir -p '$INCOMING'"
scp "${SCP_OPTS[@]}" "$DIR/install.sh" "$PROD_SSH:$INCOMING/install.sh" >/dev/null
scp "${SCP_OPTS[@]}" "$DIR/d-auth.service" "$PROD_SSH:$INCOMING/d-auth.service" >/dev/null
scp "${SCP_OPTS[@]}" "$DIR/nginx-d-auth.conf" "$PROD_SSH:$INCOMING/nginx-d-auth.conf" >/dev/null

if [ "$CHECK_ONLY" = "1" ]; then
  echo "▶ 远端预检（不改动任何东西）"
  ssh "${SSH_OPTS[@]}" "$PROD_SSH" "bash '$INCOMING/install.sh' --check"
  exit 0
fi

echo "📤 上传部署包 $TAG"
scp "${SCP_OPTS[@]}" "$ARCHIVE" "$PROD_SSH:$INCOMING/d-auth-bare.tar.gz" >/dev/null

echo "🚀 远端安装"
ssh "${SSH_OPTS[@]}" "$PROD_SSH" \
  "REMOTE_DIR='$REMOTE_DIR' TAG='$TAG' bash '$INCOMING/install.sh' '$INCOMING/d-auth-bare.tar.gz'"