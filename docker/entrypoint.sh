#!/bin/sh
# 单容器同时跑 nginx 与 Go API。任一进程退出则容器退出。
set -eu

MIGRATE_BIN=/app/migrate
SERVER_BIN=/app/server
stop_requested=0

case "${1:-serve}" in
  migrate)
    shift
    exec "$MIGRATE_BIN" "$@"
    ;;
  server)
    shift
    exec "$SERVER_BIN" "$@"
    ;;
  serve)
    ;;
  *)
    exec "$@"
    ;;
esac

mkdir -p /app/data

if [ "${RUN_MIGRATIONS:-true}" = "true" ]; then
  echo "[entrypoint] applying database migrations..."
  "$MIGRATE_BIN" up
else
  echo "[entrypoint] skipping migrations (RUN_MIGRATIONS=${RUN_MIGRATIONS})"
fi

echo "[entrypoint] starting api server on :${HTTP_PORT:-8681}"
"$SERVER_BIN" &
server_pid=$!

echo "[entrypoint] starting nginx on :80"
nginx -g 'daemon off;' &
nginx_pid=$!

shutdown() {
  stop_requested=1
  kill -TERM "$server_pid" "$nginx_pid" 2>/dev/null || true
}

trap shutdown TERM INT

while :; do
  if [ "$stop_requested" = "1" ]; then
    wait "$server_pid" 2>/dev/null || true
    wait "$nginx_pid" 2>/dev/null || true
    echo "[entrypoint] stopped"
    exit 0
  fi
  if ! kill -0 "$server_pid" 2>/dev/null; then
    echo "[entrypoint] api server exited unexpectedly, stopping container"
    shutdown
    wait "$server_pid" 2>/dev/null || true
    wait "$nginx_pid" 2>/dev/null || true
    exit 1
  fi
  if ! kill -0 "$nginx_pid" 2>/dev/null; then
    echo "[entrypoint] nginx exited unexpectedly, stopping container"
    shutdown
    wait "$server_pid" 2>/dev/null || true
    wait "$nginx_pid" 2>/dev/null || true
    exit 1
  fi
  sleep 1
done
