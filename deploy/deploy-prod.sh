#!/usr/bin/env bash
#
# 业务机部署 / 更新 d-auth 应用容器。
#
# 用法（在业务机 /opt/d-auth 下）：
#   ./deploy-prod.sh            # 拉取镜像并启动/更新
#   ./deploy-prod.sh --check    # 只做启动前检查，不改动任何东西
#
# 前置：
#   - /opt/d-auth/.env.prod 已填好（至少 DATABASE_URL、REDIS_ADDR、DAUTH_MASTER_KEY；
#     RS256 下无需 DAUTH_JWT_SECRET，私钥首启自动生成到 app_data 卷）
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
for k in DATABASE_URL REDIS_ADDR DAUTH_MASTER_KEY; do
  v=$(getenv "$k")
  [ -n "$v" ] || fail "$ENV_FILE 中 $k 为空，请先填写"
done
ok "必填项非空"

alg=$(getenv DAUTH_JWT_ALG)
case "$alg" in
  RS256)
    ok "DAUTH_JWT_ALG=RS256（非对称，公钥走 /v1/.well-known/jwks.json；私钥在 app_data 卷的 jwt-private.pem，勿删卷）"
    if [ -n "$(getenv DAUTH_JWT_SECRET)" ]; then
      echo "⚠️  DAUTH_JWT_ALG=RS256 时 DAUTH_JWT_SECRET 不生效，建议从 $ENV_FILE 删除"
    fi
    ;;
  HS256|"")
    jwt=$(getenv DAUTH_JWT_SECRET)
    if [ -z "$alg" ] && [ -z "$jwt" ]; then
      fail "$ENV_FILE 未设置 DAUTH_JWT_ALG 且无 DAUTH_JWT_SECRET：请设 DAUTH_JWT_ALG=RS256（推荐）"
    fi
    [ "${#jwt}" -ge 32 ] || fail "DAUTH_JWT_SECRET 只有 ${#jwt} 字符，HS256 生产要求 ≥ 32"
    ok "DAUTH_JWT_ALG=HS256，密钥长度 ${#jwt}（≥32）"
    ;;
  *)
    fail "不支持的 DAUTH_JWT_ALG=$alg（可选 RS256 / HS256）"
    ;;
esac

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
  echo "ℹ️  端口 ${APP_PORT} 已被占用（若为上次的 d-auth 属正常，更新会替换）"
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
  st=$(docker inspect --format '{{.State.Health.Status}}' d-auth 2>/dev/null || echo starting)
  echo "   [$i] $st"
  [ "$st" = "healthy" ] && break
  [ "$st" = "unhealthy" ] && { echo "--- 最近日志 ---"; docker logs --tail 50 d-auth; fail "容器不健康"; }
  sleep 3
done

echo "▶ 本地探活 http://127.0.0.1:${APP_PORT}/health"
curl -fsS --max-time 5 "http://127.0.0.1:${APP_PORT}/health" && echo "" || fail "本机 /health 不可达，见 docker logs d-auth"

echo ""
ok "部署完成：http://127.0.0.1:${APP_PORT}（内网 http://172.17.255.64:${APP_PORT}）"
echo "   入口机反代：https://auth.x-rv.com"
echo "   查看日志：docker logs -f d-auth"