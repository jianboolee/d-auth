# d-auth 裸机部署

把本地构建的 Go 二进制 + 前端静态文件直接传到业务机，用 **systemd + 系统 nginx** 运行。
Docker 方案完整保留，两者随时可切换。

> 本文件是使用手册（怎么用、目录结构、命令）。
> 遇到问题、想知道哪些已验证哪些没有、或要改这套部署，请看
> [PITFALLS.md](PITFALLS.md) —— 那里有实施期踩过的坑和当前完成度分界。

## 为什么两套方案能共存

| 关注点 | Docker 方案 | 裸机方案 | 是否共用 |
| --- | --- | --- | --- |
| 对外端口 | 宿主 8680 → 容器 80 | 系统 nginx 监听 8680 | **相同**，入口机配置不用改 |
| API 端口 | 容器内 127.0.0.1:8681 | 本机 127.0.0.1:8681 | **相同** |
| 前端托管 | 容器内 nginx | 系统 nginx（`/opt/d-auth/web`） | 产物同源 |
| 环境变量 | compose + `.env.prod` | systemd unit + `.env.prod` | **同一份 `.env.prod`** |
| 迁移 | `entrypoint.sh` 跑 `migrate up`，`app.New` 再跑一次 | `app.New` 启动时自动跑 | 逻辑相同 |
| 进程守护 | Docker `restart: unless-stopped` | systemd `Restart=always` | 能力对等 |
| JWT 私钥 | `app_data` 卷 `/app/data/jwt-private.pem` | `/opt/d-auth/data/jwt-private.pem` | **切换时必须搬运** |
| 日志 | `docker logs -f d-auth` | `journalctl -u d-auth -f` | — |

因为对外端口一致，入口机 `39.107.77.69` 的 `proxy_pass http://172.17.255.64:8680` 一个字都不用改，
切换对上游完全透明。`DAUTH_TRUSTED_PROXIES` 也沿用原值：裸机少了 docker NAT 这一跳，但
Gin 从 `X-Forwarded-For` 右往左回溯，最右仍是入口机 IP `172.17.255.63`，行为一致。

## 目录结构（业务机）

```
/opt/d-auth/
├── bin/server          Go API（静态二进制）
├── bin/migrate         独立迁移器（可选）
├── web/                Vite 产物
├── migrations/*.sql
├── data/               ← 唯一的持久化状态
│   ├── jwt-private.pem RS256 私钥
│   └── uploads/        mock OSS 上传文件（仅未配 OSS 时使用）
├── .env.prod           与 docker 方案共用
├── docker-compose.prod.yml   保留，供回滚
└── VERSION             当前部署的 tag
```

`data/` 是整套方案里**唯一**需要备份的目录。删掉 `jwt-private.pem` = 所有已签发 token 失效。

## 用法

```bash
# 全流程：构建 → 上传 → 远端安装
make bare-deploy

# 只构建产物包（dist/d-auth-bare-<tag>.tar.gz）
make bare-build

# 只预检，不改动服务器
deploy/baremetal/deploy.sh --check

# 回滚到 Docker
make bare-rollback
```

覆盖默认目标机：`make bare-deploy PROD_SSH=root@1.2.3.4`。
覆盖架构：`make bare-build BARE_GOARCH=arm64`（默认 `linux/amd64`，与镜像一致）。

## 首次切换会发生什么

`install.sh` 按顺序做这些事，其中第 4 步是最关键的一步：

1. 预检 `.env.prod`、私钥来源、端口占用、包管理器
2. 解包到临时目录并校验 `bin/server` / `web/index.html` / `migrations`
3. 建 `dauth` 系统用户与 `/opt/d-auth/{bin,web,migrations,data}`
4. **从运行中的容器（或 `d-auth_app_data` 卷）取出 `jwt-private.pem`** → 权限 `600 dauth:dauth`
5. 停掉旧容器（`compose down`，或退回 `docker stop d-auth`）
6. 铺文件、处理 SELinux
7. 装 nginx（缺则 `yum install -y nginx`）、下发 vhost、`nginx -t` 失败自动回滚
8. 装 `d-auth.service`、`enable` + `restart`
9. 轮询 `127.0.0.1:8681/health`，再探 `127.0.0.1:8680/health`

第 4 步在停容器**之前**执行，是因为容器还活着时 `docker cp` 最可靠。

## 三个不能踩的坑

**1. `HTTP_PORT` 与 `DAUTH_HTTP_ADDR` 不能同时用。**
`config.go` 里 `DAUTH_HTTP_ADDR` 优先。只设 `HTTP_PORT` 会绑 `0.0.0.0:8681`，
API 直接暴露在公网（安全组若放行就是可访问的）。unit 里显式设
`DAUTH_HTTP_ADDR=127.0.0.1:8681`，并且 `install.sh` 会拒绝 `.env.prod` 里出现
`HTTP_PORT` / `APP_ENV` / `DAUTH_HTTP_ADDR` / `DAUTH_JWT_PRIVATE_KEY_PATH` ——
systemd 中 `EnvironmentFile=` 会覆盖 `Environment=`，留着会让 unit 里的值失效。

**2. 私钥必须搬运，不能让它重新生成。**
找不到 `jwt-private.pem` 时 `tokens/jwt.go` 会静默生成新密钥：老 token 全部失效、
`/v1/.well-known/jwks.json` 换公钥、所有下游 Go 服务验签失败。回滚脚本同样会
把私钥写回数据卷，保证裸机期间签发的 token 在回滚后依然有效。

**3. `data/` 目录权限。**
`jwt-private.pem` 必须 `600 dauth:dauth`，服务以非 root 的 `dauth` 用户运行。
`/opt/d-auth` 本身需要 `755`（不是 `700`），否则 nginx 用户进不去 `web/`。

## nginx 与 Go 为什么拆成两个服务

容器里 `entrypoint.sh` 把 nginx 和 Go 塞进同一个 PID 命名空间，任一挂掉整体退出 ——
那是容器只能有一个入口的妥协。裸机上照搬会得到同样的耦合，正确的拆法是：

- `d-auth.service` 只跑 Go，`User=dauth`，带 systemd 沙箱（`ProtectSystem=full`、
  `ReadWritePaths=/opt/d-auth/data`、`NoNewPrivileges` 等）
- `nginx.service` 跑静态与反代

两者独立重启、日志分开。顺带的好处是 nginx 变成全机共用服务，以后加站点不用动它。

## 健康检查

容器方案里 `HEALTHCHECK` 只影响 `docker ps` 显示和 `deploy-prod.sh` 的等待，
**不会**因 unhealthy 自动重启容器（`restart: unless-stopped` 只处理进程退出）。
所以裸机方案不做定时健康检查，可靠性与原方案持平。

想要更强（比原方案更好）可以加 timer：

```ini
# /etc/systemd/system/d-auth-healthcheck.service
[Unit]
Description=d-auth health probe
[Service]
Type=oneshot
ExecStart=/bin/sh -c 'curl -fsS --max-time 3 http://127.0.0.1:8681/health >/dev/null || systemctl restart d-auth'
```

```ini
# /etc/systemd/system/d-auth-healthcheck.timer
[Unit]
Description=Probe d-auth every minute
[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
[Install]
WantedBy=timers.target
```

启用：`systemctl enable --now d-auth-healthcheck.timer`。这是**可选增强**，未包含在
`install.sh` 中。

## 已知限制

- **8680 独占**：两套方案互斥，不能同时跑。`install.sh` 与 `rollback-to-docker.sh`
  都会主动停掉对面那一套。
- **无零停机滚动**：`systemctl restart` 会中断几百毫秒。本项目是单一实例 + 外部
  DB/Redis，与原 docker 方案相同，无回滚窗口。
- **SELinux**：`install.sh` 会自动 `chcon -R -t httpd_sys_content_t /opt/d-auth/web`，
  但若策略被重置（如 `restorecon`），nginx 会 403，重跑一次即可。
- **不打包私钥**：`build.sh` 刻意不把 `data/` 打进 tar 包，私钥只由 `install.sh`
  从现场接管。