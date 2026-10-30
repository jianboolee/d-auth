#!/usr/bin/env bash
#
# 从裸机部署回滚到 Docker 部署（业务机执行，root）。
#
# 用法：
#   bash rollback-to-docker.sh            # 停裸机服务，恢复容器
#   bash rollback-to-docker.sh --check    # 只预检
#
# 前置：
#   $REMOTE_DIR/docker-compose.prod.yml 与 .env.prod 存在（原 docker 方案的文件）
#   registry.cn-beijing.aliyuncs.com 已登录
#
# 关键点：把裸机期间签发的 JWT 私钥写回 d-auth_app_data 卷，否则回滚后
# 旧 token 全部失效、JWKS 公钥变化、下游验签失败。
set -euo pipefail

REMOTE_DIR="${REMOTE_DIR:-/opt/d-auth}"
CHECK_ONLY=0
[ "${1:-}" = "--check" ] && CHECK_ONLY=1

ENV_FILE="$REMOTE_DIR/.env.prod"
KEY_FILE="$REMOTE_DIR/data/jwt-private.pem"
COMPOSE_FILE="$REMOTE_DIR/docker-compose.prod.yml"
NGINX_DST="/etc/nginx/conf.d/d-auth.conf"
VOLUME="d-auth_app_data"

RED=$'\033[31m'; GRN=$'\033[32m'; YEL=$'\033[33m'; RST=$'\033[0m'
if [ ! -t 1 ]; then RED=""; GRN=""; YEL=""; RST=""; fi
fail() { echo "${RED}❌ $*${RST}" >&2; exit 1; }
ok()   { echo "${GRN}✅ $*${RST}"; }
warn() { echo "${YEL}⚠️  $*${RST}"; }
step() { echo ""; echo "▶ $*"; }

[ "$(id -u)" = "0" ] || fail "请用 root 执行"

echo "▶ 回滚 d-auth 到 Docker 部署（$REMOTE_DIR）"

# ============================================================
# 预检
# ============================================================
step "预检"
command -v docker >/dev/null || fail "未安装 docker"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 不可用"
ok "docker / compose 可用"

[ -f "$COMPOSE_FILE" ] || fail "缺少 $COMPOSE_FILE（原 docker 方案的 compose 文件）"
[ -f "$ENV_FILE" ]     || fail "缺少 $ENV_FILE"
ok "compose 与 env 文件就位"

# compose 文件里的容器名，用于后续探活
[ -f "$KEY_FILE" ] && ok "发现裸机私钥 $KEY_FILE（将写回数据卷）" \
                    || warn "无 $KEY_FILE；容器将沿用卷里原有的私钥"

if [ "$CHECK_ONLY" = "1" ]; then
  step "--check 完成，未做任何改动"
  exit 0
fi

# ============================================================
# 1. 停裸机服务、撤 nginx vhost
# ============================================================
step "停止裸机部署"
if systemctl is-enabled --quiet d-auth 2>/dev/null || systemctl is-active --quiet d-auth 2>/dev/null; then
  systemctl disable --now d-auth >/dev/null 2>&1 || true
  ok "d-auth.service 已停止并取消开机自启"
else
  ok "d-auth.service 未启用"
fi

if [ -f "$NGINX_DST" ]; then
  cp "$NGINX_DST" "$NGINX_DST.bak.$(date +%Y%m%d_%H%M%S)"
  rm -f "$NGINX_DST"
  if nginx -t >/dev/null 2>&1; then
    systemctl reload nginx 2>/dev/null || systemctl restart nginx
    ok "已移除 $NGINX_DST 并 reload nginx（释放 8680）"
  else
    fail "移除 vhost 后 nginx -t 失败，请人工检查"
  fi
else
  ok "没有 $NGINX_DST，跳过"
fi

# ============================================================
# 2. 私钥写回数据卷
# ============================================================
step "同步 JWT 私钥到数据卷 $VOLUME"
if [ -f "$KEY_FILE" ]; then
  mnt=$(docker volume inspect "$VOLUME" --format '{{.Mountpoint}}' 2>/dev/null || true)
  if [ -z "$mnt" ]; then
    # 卷不存在时先创建，再取其挂载点
    docker volume create "$VOLUME" >/dev/null
    mnt=$(docker volume inspect "$VOLUME" --format '{{.Mountpoint}}' 2>/dev/null || true)
  fi
  [ -n "$mnt" ] || fail "无法获取 $VOLUME 挂载点"
  install -D -m 0600 "$KEY_FILE" "$mnt/jwt-private.pem"
  ok "私钥已写入 $mnt/jwt-private.pem"
else
  warn "跳过（容器会用卷里已有的密钥）"
fi

# ============================================================
# 3. 起容器
# ============================================================
step "拉取并启动容器"
( cd "$REMOTE_DIR" && docker compose -f docker-compose.prod.yml --env-file .env.prod pull )
( cd "$REMOTE_DIR" && docker compose -f docker-compose.prod.yml --env-file .env.prod up -d )
ok "compose up -d 完成"

# ============================================================
# 4. 探活
# ============================================================
step "等待容器健康"
APP_PORT=$(sed -n 's/^APP_PORT=//p' "$ENV_FILE" | head -1)
APP_PORT="${APP_PORT:-8680}"

health=""
for i in $(seq 1 30); do
  health=$(docker inspect --format '{{.State.Health.Status}}' d-auth 2>/dev/null || echo starting)
  echo "   [$i] $health"
  [ "$health" = "healthy" ] && break
  [ "$health" = "unhealthy" ] && { echo "--- docker logs d-auth ---"; docker logs --tail 50 d-auth; fail "容器不健康"; }
  sleep 3
done
[ "$health" = "healthy" ] || warn "30 次轮询后仍为 $health，继续探测端口"

step "探活 http://127.0.0.1:${APP_PORT}/health"
curl -fsS --max-time 5 "http://127.0.0.1:${APP_PORT}/health" && echo "" \
  || fail "端口不可达，见 docker logs d-auth"

echo ""
ok "已回滚到 Docker 部署"
echo "   本机：http://127.0.0.1:${APP_PORT}"
echo "   日志：docker logs -f d-auth"