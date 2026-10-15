#!/usr/bin/env bash
#
# 业务机部署 / 更新 d-auth 应用容器。
#
# 用法（在业务机 /opt/d-auth 下）：
#   ./deploy-prod.sh            # 拉取镜像并启动/更新
#   ./deploy-prod.sh --check    # 只做启动前检查，不改动任何东西
#
# 前置：
#   - /opt/d-auth/.env.prod 已填好（至少 DATABASE_URL、REDIS_ADDR、DAUTH_MASTER_KEY、DAUTH_JWT_SECRET）
#   - docker login registry.cn-beijing.aliyuncs.com 已登录（账号需有 xrv 命名空间权限）
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="$DIR/docker-compose.prod.yml"
ENV_FILE="$DIR/.env.prod"
CHECK_ONLY=0
[ "${1:-}" = "--check" ] && CHECK_ONLY=1

fail() { echo "❌ $*" >&2; exit 1; }
ok()   { echo "✅ $*"; }

echo "▶ d-auth 生产部署（$DIR）"

[ -f "$COMPOSE_FILE" ] || fail "缺少 $COMPOSE_FILE"
[ -f "$ENV_FILE" ]     || fail "缺少 $ENV_FILE（见 deploy/.env.prod.example）"

# ---------- 读取 env（只取需要的键，不打印值） ----------
getenv() { sed -n "s/^$1=//p" "$ENV_FILE" | head -1; }

REGISTRY=$(getenv REGISTRY); REGISTRY=${REGISTRY:-registry.cn-beijing.aliyuncs.com/xrv/d-auth}
DOCKER_TAG=$(getenv DOCKER_TAG); DOCKER_TAG=${DOCKER_TAG:-latest}
# 端口绑所有网卡（不再绑指定内网 IP），入口机经内网 IP 访问
APP_PORT=$(getenv APP_PORT); APP_PORT=${APP_PORT:-8680}

# ---------- 启动前检查 ----------
echo "▶ 启动前检查"
for k in DATABASE_URL REDIS_ADDR DAUTH_MASTER_KEY DAUTH_JWT_SECRET; do
  v=$(getenv "$k")
  [ -n "$v" ] || fail "$ENV_FILE 中 $k 为空，请先填写"
done
ok "必填项非空"

jwt=$(getenv DAUTH_JWT_SECRET)
[ "${#jwt}" -ge 32 ] || fail "DAUTH_JWT_SECRET 只有 ${#jwt} 字符，HS256 生产要求 ≥ 32"
ok "DAUTH_JWT_SECRET 长度 ${#jwt}（≥32）"

tp=$(getenv DAUTH_TRUSTED_PROXIES)
case "$tp" in
  *172.17.255.63*) ok "可信代理含入口机 172.17.255.63" ;;
  *) echo "⚠️  DAUTH_TRUSTED_PROXIES 未包含入口机 IP，验证码 IP 限流可能误封全体：$tp" ;;
esac

command -v docker >/dev/null || fail "未安装 docker"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 不可用"
ok "docker / compose 可用"

# 端口占用（本项目已有容器时属正常更新）
if ss -ltn 2>/dev/null | grep -q ":${APP_PORT} "; then
  echo "ℹ️  端口 ${APP_PORT} 已被占用（若为上次的 dauth-app 属正常，更新会替换）"
fi

if [ "$CHECK_ONLY" = "1" ]; then
  echo "▶ --check 完成，未做任何改动"
  exit 0
fi

# ---------- 拉取并启动 ----------
echo "▶ 拉取镜像 ${REGISTRY}:${DOCKER_TAG}"
docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" pull

echo "▶ 启动 / 更新"
docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" up -d

echo "▶ 等待健康检查"
for i in $(seq 1 30); do
  st=$(docker inspect --format '{{.State.Health.Status}}' dauth-app 2>/dev/null || echo starting)
  echo "   [$i] $st"
  [ "$st" = "healthy" ] && break
  [ "$st" = "unhealthy" ] && { echo "--- 最近日志 ---"; docker logs --tail 50 dauth-app; fail "容器不健康"; }
  sleep 3
done

echo "▶ 本地探活 http://127.0.0.1:${APP_PORT}/health"
curl -fsS --max-time 5 "http://127.0.0.1:${APP_PORT}/health" && echo "" || fail "本机 /health 不可达，见 docker logs dauth-app"

echo ""
ok "部署完成：http://127.0.0.1:${APP_PORT}（内网 http://172.17.255.64:${APP_PORT}）"
echo "   入口机反代：https://auth.x-rv.com"
echo "   查看日志：docker logs -f dauth-app"