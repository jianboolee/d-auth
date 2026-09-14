#!/usr/bin/env bash
#
# 推送本目录下的 nginx 站点配置到服务器 /etc/nginx/conf.d/
#
# 用法：
#   ./upload.sh                        # 推送本目录全部 *.conf
#   ./upload.sh specs.21rv.com.conf    # 只推送指定文件
#
# 可用环境变量覆盖默认值：
#   SERVER=root@1.2.3.4 PORT=2222 ./upload.sh
#
# 流程：备份远端同名文件 -> 上传 -> nginx -t -> reload
#       nginx -t 失败时用备份自动回滚，并以非 0 退出。
set -euo pipefail

SERVER="${SERVER:-root@39.107.77.69}"
PORT="${PORT:-22}"
REMOTE_DIR="${REMOTE_DIR:-/etc/nginx/conf.d}"
BACKUP_DIR="$REMOTE_DIR/backups"
RELOAD_CMD="${RELOAD_CMD:-systemctl reload nginx}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SSH_OPTS=(-p "$PORT" -o BatchMode=yes -o ConnectTimeout=10)
SCP_OPTS=(-P "$PORT" -o BatchMode=yes -o ConnectTimeout=10)

# ---------- 收集待推送文件 ----------
if [ "$#" -gt 0 ]; then
    FILES=("$@")
else
    FILES=()
    for f in "$SCRIPT_DIR"/*.conf; do
        [ -f "$f" ] && FILES+=("$(basename "$f")")
    done
fi

if [ "${#FILES[@]}" -eq 0 ]; then
    echo "❌ 没有找到要推送的 .conf 文件" >&2
    exit 1
fi

for f in "${FILES[@]}"; do
    if [ ! -f "$SCRIPT_DIR/$f" ]; then
        echo "❌ 本地文件不存在: $SCRIPT_DIR/$f" >&2
        exit 1
    fi
done

echo "🎯 目标: $SERVER:$REMOTE_DIR (port $PORT)"
echo "📄 文件: ${FILES[*]}"

# ---------- 连通性检查 ----------
echo "🔌 检查 SSH 连通性..."
if ! ssh "${SSH_OPTS[@]}" "$SERVER" 'true'; then
    echo "❌ 无法连接 $SERVER:$PORT（请检查免密登录与端口）" >&2
    exit 1
fi

TS="$(date +%Y%m%d_%H%M%S)"
ROLLBACK=""   # 每行 "<文件名>|<备份路径或空>"

# ---------- 备份并上传 ----------
for f in "${FILES[@]}"; do
    echo ""
    echo "📦 备份 $f ..."
    backup_path="$(ssh "${SSH_OPTS[@]}" "$SERVER" "
        set -e
        mkdir -p '$BACKUP_DIR'
        if [ -f '$REMOTE_DIR/$f' ]; then
            bp='$BACKUP_DIR/${f%.conf}_backup_$TS.conf'
            cp '$REMOTE_DIR/$f' \"\$bp\"
            echo \"\$bp\"
        fi
    ")"
    if [ -n "$backup_path" ]; then
        echo "   ✅ 已备份 -> $backup_path"
    else
        echo "   ⚠️  远端无同名文件，跳过备份（回滚时会删除）"
    fi
    ROLLBACK+="$f|$backup_path"$'\n'

    echo "🚀 上传 $f ..."
    scp "${SCP_OPTS[@]}" "$SCRIPT_DIR/$f" "$SERVER:$REMOTE_DIR/$f.tmp.$TS" >/dev/null
    ssh "${SSH_OPTS[@]}" "$SERVER" \
        "mv '$REMOTE_DIR/$f.tmp.$TS' '$REMOTE_DIR/$f' && chown root:root '$REMOTE_DIR/$f' && chmod 644 '$REMOTE_DIR/$f'"
    echo "   ✅ 已更新"
done

# ---------- 语法校验，失败则回滚 ----------
echo ""
echo "🔧 测试 nginx 配置..."
if ssh "${SSH_OPTS[@]}" "$SERVER" 'nginx -t'; then
    echo "✅ 配置测试通过"
else
    echo "❌ 配置测试失败，开始回滚..." >&2
    while IFS='|' read -r f bp; do
        [ -z "$f" ] && continue
        if [ -n "$bp" ]; then
            ssh "${SSH_OPTS[@]}" "$SERVER" "cp '$bp' '$REMOTE_DIR/$f'" && echo "   ↩️  已恢复 $f"
        else
            ssh "${SSH_OPTS[@]}" "$SERVER" "rm -f '$REMOTE_DIR/$f'" && echo "   ↩️  已删除新上传的 $f"
        fi
    done <<< "$ROLLBACK"
    echo "❌ 已回滚，服务器配置未被改动" >&2
    exit 1
fi

# ---------- 重载 ----------
echo "🔄 重载 nginx..."
ssh "${SSH_OPTS[@]}" "$SERVER" "$RELOAD_CMD"

echo ""
echo "🎉 完成：${FILES[*]} 已上线"
