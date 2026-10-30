#!/usr/bin/env bash
#
# 在业务机安装 / 更新 d-auth 裸机部署（服务器端执行，root）。
#
# 用法：
#   bash install.sh --check                       # 只预检，不改动
#   bash install.sh /tmp/d-auth-deploy/d-auth-bare.tar.gz
#
# 可用环境变量：
#   REMOTE_DIR=/opt/d-auth   安装目录
#   TAG=...                  版本标记（写入 /opt/d-auth/VERSION）
#   SKIP_DOCKER_STOP=1       不自动停掉旧容器（迁移到本方案时别用）
#
# 设计要点（与容器方案共存/切换的关键）：
#   1. JWT 私钥从旧容器或原目录接管。丢了这个 = 所有已签发 token 失效、
#      JWKS 公钥变化、全部下游验签失败。
#   2. 8680 是独占端口，两套方案互斥。安装前会停掉占用它的旧容器。
#   3. nginx 与 Go 是两套 systemd 服务，不再像 entrypoint.sh 那样绑在一起。
set -euo pipefail

REMOTE_DIR="${REMOTE_DIR:-/opt/d-auth}"
TAG="${TAG:-unknown}"
SKIP_DOCKER_STOP="${SKIP_DOCKER_STOP:-0}"
CHECK_ONLY=0

ARG_FILE=""
for arg in "$@"; do
  case "$arg" in
    --check) CHECK_ONLY=1 ;;
    -*) echo "未知参数：$arg" >&2; exit 1 ;;
    *)  ARG_FILE="$arg" ;;
  esac
done

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$REMOTE_DIR/.env.prod"
KEY_FILE="$REMOTE_DIR/data/jwt-private.pem"
SERVICE_SRC="$DIR/d-auth.service"
NGINX_SRC="$DIR/nginx-d-auth.conf"
NGINX_DST="/etc/nginx/conf.d/d-auth.conf"

RED=$'\033[31m'; GRN=$'\033[32m'; YEL=$'\033[33m'; RST=$'\033[0m'
if [ ! -t 1 ]; then RED=""; GRN=""; YEL=""; RST=""; fi
fail() { echo "${RED}❌ $*${RST}" >&2; exit 1; }
ok()   { echo "${GRN}✅ $*${RST}"; }
warn() { echo "${YEL}⚠️  $*${RST}"; }
step() { echo ""; echo "▶ $*"; }

[ "$(id -u)" = "0" ] || fail "请用 root 执行（需要装 nginx、写 systemd 与 /opt）"

CHECK_LABEL=""
[ "$CHECK_ONLY" = "1" ] && CHECK_LABEL=" (check)"
echo "▶ d-auth 裸机安装${CHECK_LABEL}  dir=$REMOTE_DIR tag=$TAG"

# ============================================================
# 1. 预检
# ============================================================
step "环境预检"

command -v systemctl >/dev/null || fail "没有 systemd，本脚本不适用"
ok "systemd 可用"

OS_NAME="$(. /etc/os-release 2>/dev/null && echo "${PRETTY_NAME:-unknown}" || echo unknown)"
ok "系统：$OS_NAME"

PKG=""
if command -v yum >/dev/null; then PKG=yum
elif command -v dnf >/dev/null; then PKG=dnf
elif command -v apt-get >/dev/null; then PKG=apt-get
fi
if [ -n "$PKG" ]; then ok "包管理器：$PKG"; else warn "未识别包管理器，nginx 需手工安装"; fi

if command -v nginx >/dev/null; then
  ok "nginx 已安装：$(nginx -v 2>&1)"
else
  warn "nginx 未安装，安装阶段会自动装"
fi

# ---------- .env.prod ----------
step "检查 $ENV_FILE"
[ -f "$ENV_FILE" ] || fail "缺少 $ENV_FILE（见仓库 deploy/.env.prod.example）"

getenv() { sed -n "s/^$1=//p" "$ENV_FILE" | head -1; }

for k in DATABASE_URL REDIS_ADDR DAUTH_MASTER_KEY; do
  [ -n "$(getenv "$k")" ] || fail "$ENV_FILE 中 $k 为空，请先填写"
done
ok "必填项非空（DATABASE_URL / REDIS_ADDR / DAUTH_MASTER_KEY）"

# 这些键由 d-auth.service 的 Environment= 提供；若 .env.prod 里也有同名键，
# systemd 会用文件里的值覆盖 unit 的，导致「改了 unit 不生效」。这里直接报错。
conflict=""
for k in APP_ENV DAUTH_HTTP_ADDR HTTP_PORT DAUTH_JWT_PRIVATE_KEY_PATH; do
  if grep -qE "^[[:space:]]*${k}=" "$ENV_FILE"; then
    conflict="$conflict $k"
  fi
done
if [ -n "$conflict" ]; then
  fail "$ENV_FILE 里出现了由 systemd unit 接管的键：$conflict
   systemd 中 EnvironmentFile 会覆盖 Environment=，留着会让 unit 里的值失效。
   请从 $ENV_FILE 删除这些行后重试。"
fi
ok ".env.prod 未与 unit 注入的键冲突"

alg=$(getenv DAUTH_JWT_ALG)
case "$alg" in
  RS256|"") ok "JWT 算法：${alg:-（空=按密钥自动判定 RS256）}" ;;
  HS256)
    jwt=$(getenv DAUTH_JWT_SECRET)
    [ "${#jwt}" -ge 32 ] || fail "HS256 需要 DAUTH_JWT_SECRET ≥ 32 字符（当前 ${#jwt}）"
    warn "HS256 模式：私钥文件不参与，但下方私钥检查仍会执行，可有可无"
    ok "HS256 密钥长度 ${#jwt}"
    ;;
  *) fail "不支持的 DAUTH_JWT_ALG=$alg" ;;
esac

tp=$(getenv DAUTH_TRUSTED_PROXIES)
case "$tp" in
  *172.17.255.63*) ok "可信代理含入口机 172.17.255.63" ;;
  *) warn "DAUTH_TRUSTED_PROXIES 不含入口机 IP，验证码 IP 限流可能误封全体：$tp" ;;
esac

case "$(getenv APP_PORT)" in
  ""|8680) ok "APP_PORT=8680（裸机 nginx 固定监听 8680，此键在裸机下不生效）" ;;
  *) warn "APP_PORT=$(getenv APP_PORT) 在裸机方案下不生效（nginx vhost 固定 8680）" ;;
esac

# ---------- 私钥来源 ----------
step "检查 JWT 私钥来源"
KEY_SRC=""
if [ -f "$KEY_FILE" ]; then
  KEY_SRC="existing"
  ok "已有 $KEY_FILE（更新场景，保留不动）"
elif command -v docker >/dev/null && docker inspect d-auth >/dev/null 2>&1; then
  KEY_SRC="container"
  ok "将从运行中的容器 d-auth 取出私钥"
elif command -v docker >/dev/null; then
  mnt=$(docker volume inspect d-auth_app_data --format '{{.Mountpoint}}' 2>/dev/null || true)
  if [ -n "$mnt" ] && [ -f "$mnt/jwt-private.pem" ]; then
    KEY_SRC="volume"
    ok "将从数据卷 d-auth_app_data 取出私钥（$mnt）"
  fi
fi
case "$KEY_SRC" in
  "") warn "找不到任何私钥来源。首次启动会生成新密钥 —— 若之前已在生产签过 token，
        所有 token 立即失效、下游验签全部失败。确认可接受才继续。" ;;
  *)  ok "私钥来源：$KEY_SRC" ;;
esac

# ---------- 端口 ----------
step "检查 8680 / 8681 占用"
# port_owner 有三种结果，必须区分，不能把「查不了」混同于「没占用」：
#   0 = 有进程在监听该端口（输出最多 3 行）
#   1 = 确认没有进程监听
#   2 = 没有可用的检测工具（ss / lsof 都不存在），结论未知
#
# 三种状态是必要的：Ubuntu minimal 默认既无 ss 也无 lsof，早期版本在这里
# 静默返回「空闲」，于是端口真被占用时也放行，随后 systemd 启动失败，报的是
# 一个与端口无关的难懂错误。宁可显式报「不知道」也不要假阴性地放行。
#
# 匹配必须精确：不能拿工具的整体输出当结论。实测在 Rosetta 模拟层下 lsof
# 会返回大量与本端口无关的进程，把「空闲」误报成「被占用」。ss 走 -ltnH
# （-H 去表头），第 4 列是本地地址，形如 0.0.0.0:8680 / [::]:8680；
# lsof 走 -iTCP:PORT -sTCP:LISTEN 后再用 grep -F 兜一层。
# 两者都按「地址列以 :PORT 结尾」匹配，避免 680 命中 8680。
port_owner() {
  local port="$1" out=""
  if command -v ss >/dev/null 2>&1; then
    out=$(ss -ltnH 2>/dev/null | awk -v p=":${port}" '$4 ~ (p "$")')
  elif command -v lsof >/dev/null 2>&1; then
    out=$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | tail -n +2 | grep -F ":${port}")
  else
    return 2
  fi
  [ -n "$out" ] || return 1
  echo "$out" | head -3
  return 0
}

# 调用方统一按哨兵值分支，避免 `if owners=$(...)` 把 1 和 2 混在一起。
PORT_UNKNOWN=""
check_port() {
  local port="$1" label="$2" out rc
  out=$(port_owner "$port"); rc=$?
  case "$rc" in
    0)
      echo "   ${port} 被占用："
      echo "$out" | sed 's/^/     /'
      return 0
      ;;
    1)
      ok "${port} 空闲"
      return 1
      ;;
    *)
      PORT_UNKNOWN="${PORT_UNKNOWN} ${port}"
      warn "无法检测 ${port} 占用（缺 ss / lsof），这个端口的状态未知"
      return 2
      ;;
  esac
}

# 调用方统一按返回码分支：0=占用，1=空闲，2=无法检测。
# 注意 check_port 返回非 0 属正常控制流，用 `|| rc=$?` 捕获以免 set -e 中断。
rc=0
check_port 8680 "8680" || rc=$?
[ "$rc" = "0" ] && warn "若为上次的 d-auth 容器属正常，安装阶段会替换"

rc=0
check_port 8681 "8681" || rc=$?
if [ "$rc" = "0" ]; then
  if systemctl is-active --quiet d-auth 2>/dev/null; then
    ok "为 d-auth.service 自身（更新场景属正常）"
  else
    fail "8681 被别的进程占用，裸机方案需要它（API 端口）"
  fi
fi

if [ -n "$PORT_UNKNOWN" ]; then
  warn "以下端口无法确认是否被占用：${PORT_UNKNOWN}"
  warn "安装阶段会直接启动服务；若端口冲突，systemd 会报 bind 失败（见 journalctl）"
fi

if [ "$CHECK_ONLY" = "1" ]; then
  step "预检完成，未做任何改动"
  exit 0
fi

# ============================================================
# 2. 解包
# ============================================================
step "解包产物"
[ -n "$ARG_FILE" ] || fail "缺少部署包路径（--check 之外必须提供 tar.gz）"
[ -f "$ARG_FILE" ] || fail "部署包不存在：$ARG_FILE"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
tar -xzf "$ARG_FILE" -C "$STAGE"
for f in bin/server web/index.html migrations; do
  [ -e "$STAGE/$f" ] || fail "部署包内容不完整：缺 $f"
done
ok "server=$(du -h "$STAGE/bin/server" | cut -f1)"

# ============================================================
# 3. 用户与目录
# ============================================================
step "准备用户与目录"
if id dauth >/dev/null 2>&1; then
  ok "用户 dauth 已存在"
else
  useradd --system --home-dir "$REMOTE_DIR" --shell /sbin/nologin dauth
  ok "已创建系统用户 dauth"
fi

mkdir -p "$REMOTE_DIR"/{bin,web,migrations,data}
# /opt/d-auth 需要给 nginx 用户 o+x 才能进到 web/ 读文件
chmod 755 "$REMOTE_DIR"
ok "目录就绪：$REMOTE_DIR/{bin,web,migrations,data}"

# ============================================================
# 4. 接管私钥（必须在停容器之前）
# ============================================================
step "接管 JWT 私钥"
case "$KEY_SRC" in
  existing)
    ok "沿用 $KEY_FILE"
    ;;
  container)
    docker cp d-auth:/app/data/jwt-private.pem "$KEY_FILE" >/dev/null
    ok "已从容器取出私钥"
    ;;
  volume)
    cp "$mnt/jwt-private.pem" "$KEY_FILE"
    ok "已从数据卷取出私钥"
    ;;
  *)
    warn "无可用私钥，首次启动将自动生成（旧 token 会全部失效）"
    ;;
esac
if [ -f "$KEY_FILE" ]; then
  chown dauth:dauth "$KEY_FILE"
  chmod 600 "$KEY_FILE"
  ok "私钥权限 600 dauth:dauth"
fi

# ============================================================
# 5. 停掉占用端口的旧容器
# ============================================================
step "停止旧容器（两套方案互斥，8680 只能有一个占用者）"
if [ "$SKIP_DOCKER_STOP" = "1" ]; then
  warn "SKIP_DOCKER_STOP=1，跳过"
elif ! command -v docker >/dev/null; then
  ok "未安装 docker，无需处理"
elif docker inspect d-auth >/dev/null 2>&1; then
  if [ -f "$REMOTE_DIR/docker-compose.prod.yml" ] && docker compose version >/dev/null 2>&1; then
    ( cd "$REMOTE_DIR" && docker compose -f docker-compose.prod.yml --env-file .env.prod down ) >/dev/null 2>&1 \
      && ok "已通过 compose 停止容器" \
      || { docker stop d-auth >/dev/null 2>&1 && ok "compose 停止失败，已直接 docker stop"; }
  else
    docker stop d-auth >/dev/null 2>&1 && ok "已 docker stop d-auth"
  fi
else
  ok "没有名为 d-auth 的容器"
fi

# ============================================================
# 6. 铺产物
# ============================================================
step "安装文件"
install -m 0755 "$STAGE/bin/server"  "$REMOTE_DIR/bin/server"
if [ -f "$STAGE/bin/migrate" ]; then
  install -m 0755 "$STAGE/bin/migrate" "$REMOTE_DIR/bin/migrate"
fi
rm -rf "$REMOTE_DIR/web"/* "$REMOTE_DIR/migrations"/*
cp -R "$STAGE/web/." "$REMOTE_DIR/web/"
cp "$STAGE/migrations/"*.sql "$REMOTE_DIR/migrations/"
echo "$TAG" > "$REMOTE_DIR/VERSION"

# web/ 由 nginx（可能以 nginx 用户身份）读取；bin/ 由 dauth 执行。
chown -R root:root "$REMOTE_DIR/bin" "$REMOTE_DIR/migrations"
chown -R dauth:dauth "$REMOTE_DIR/data"
chmod -R a+rX "$REMOTE_DIR/web" "$REMOTE_DIR/bin" "$REMOTE_DIR/migrations"
ok "文件已安装（$(ls "$REMOTE_DIR/web" | wc -l | tr -d ' ') 个前端条目，$(ls "$REMOTE_DIR/migrations" | wc -l | tr -d ' ') 个迁移）"

# SELinux
if command -v getenforce >/dev/null && [ "$(getenforce 2>/dev/null)" = "Enforcing" ]; then
  if command -v chcon >/dev/null; then
    chcon -R -t httpd_sys_content_t "$REMOTE_DIR/web" 2>/dev/null \
      && ok "已为 web/ 设置 httpd_sys_content_t" \
      || warn "chcon 失败；若 nginx 返回 403，手动执行：chcon -R -t httpd_sys_content_t $REMOTE_DIR/web"
  else
    warn "SELinux Enforcing 且无 chcon，nginx 可能 403"
  fi
fi

# ============================================================
# 7. nginx
# ============================================================
step "配置 nginx"
if ! command -v nginx >/dev/null; then
  [ -n "$PKG" ] || fail "nginx 未安装且无法自动安装，请手工装后重跑"
  echo "   安装 nginx（$PKG）"
  case "$PKG" in
    yum|dnf) "$PKG" install -y nginx >/dev/null ;;
    apt-get) apt-get update -qq && apt-get install -y nginx >/dev/null ;;
  esac
  command -v nginx >/dev/null || fail "nginx 安装失败"
  ok "nginx 已安装"
fi

[ -f "$NGINX_SRC" ] || fail "缺少 $NGINX_SRC（应与 install.sh 同目录上传）"

# 与其他 vhost 抢 8680 的冲突检测
if [ -f "$NGINX_DST" ]; then
  cp "$NGINX_DST" "$NGINX_DST.bak.$(date +%Y%m%d_%H%M%S)"
  ok "已备份旧的 $NGINX_DST"
fi
install -m 0644 "$NGINX_SRC" "$NGINX_DST"

if nginx -t 2>/dev/null; then
  ok "nginx 配置校验通过"
else
  echo "--- nginx -t 输出 ---"
  nginx -t || true
  # 回滚到最近的备份，避免留下坏配置
  latest=$(ls -1t "$NGINX_DST".bak.* 2>/dev/null | head -1 || true)
  if [ -n "$latest" ]; then
    cp "$latest" "$NGINX_DST"
    warn "已回滚到 $latest"
  else
    rm -f "$NGINX_DST"
    warn "已移除新配置"
  fi
  fail "nginx 配置校验失败，已回滚"
fi

systemctl enable nginx >/dev/null 2>&1 || true
systemctl reload nginx 2>/dev/null || systemctl restart nginx
ok "nginx 已 reload"

# ============================================================
# 8. systemd 服务
# ============================================================
step "安装 systemd 服务"
[ -f "$SERVICE_SRC" ] || fail "缺少 $SERVICE_SRC"
install -m 0644 "$SERVICE_SRC" /etc/systemd/system/d-auth.service
systemctl daemon-reload
systemctl enable d-auth >/dev/null 2>&1
systemctl restart d-auth
ok "d-auth.service 已启动"

# ============================================================
# 9. 探活
# ============================================================
step "等待 API 就绪（启动时会自动跑迁移）"
ready=0
for i in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:8681/health >/dev/null 2>&1; then
    ok "API 就绪（${i}s）"
    ready=1
    break
  fi
  if ! systemctl is-active --quiet d-auth; then
    echo "--- journalctl -u d-auth -n 40 ---"
    journalctl -u d-auth -n 40 --no-pager || true
    fail "d-auth.service 已退出"
  fi
  sleep 1
done
[ "$ready" = "1" ] || {
  echo "--- journalctl -u d-auth -n 40 ---"
  journalctl -u d-auth -n 40 --no-pager || true
  fail "30s 内 /health 未就绪"
}

step "探活 8680（nginx 单入口）"
if curl -fsS --max-time 5 http://127.0.0.1:8680/health; then
  echo ""
else
  echo "--- journalctl -u nginx -n 20 ---"
  journalctl -u nginx -n 20 --no-pager 2>/dev/null || true
  fail "8680 /health 不可达"
fi

echo ""
ok "部署完成（$TAG）"
echo "   本机：http://127.0.0.1:8680    内网：http://172.17.255.64:8680"
echo "   入口机反代：https://auth.x-rv.com"
echo "   日志：journalctl -u d-auth -f"
echo "   回滚到 docker：bash rollback-to-docker.sh"