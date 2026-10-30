# d-auth 项目约定

## 部署：默认用裸机方案

**提到「部署」「上线」「发版」时，默认指裸机方案**，不要主动改成 Docker。

```bash
make bare-check      # 预检服务器
make bare-deploy     # 构建 + 上传 + 安装（systemd + 系统 nginx）
make bare-rollback   # 需要时切回 docker
```

方案说明见 [deploy/baremetal/README.md](deploy/baremetal/README.md)。
**踩过的坑、当前完成度（哪些验证过、哪些没验证）、与 Docker 的差异清单，
以及上线前 checklist，都在 [deploy/baremetal/PITFALLS.md](deploy/baremetal/PITFALLS.md)。**
改部署相关的东西之前先读它，尤其是「必须守住的约束」一节。

### 当前状态（2026-10-31）

构建链路与 nginx 配置已在本地真实环境（Postgres + Redis + 二进制 + nginx 容器）
端到端验证通过；**systemd unit 加载、`install.sh` 完整执行、`rollback-to-docker.sh`
均未在真实服务器验证**，尚未上线过。首次部署请按 PITFALLS.md 的 checklist 逐项确认。

### 为什么是它

产物是「Go 静态二进制 + Vite 静态文件」，服务器上不需要镜像仓库、不需要拉镜像、
也不需要 Docker。systemd 承担进程守护，系统 nginx 承担静态托管与 `/v1` 反代。
构建和排查都在本地/服务器直接可见，比 `docker buildx push → compose pull` 少两跳。

### 与 Docker 方案的关系

**Docker 方案完整保留且不要删**（`Dockerfile`、`docker/`、`deploy/*.yml`、
`deploy/deploy-prod.sh`），随时可切回去：

- 两套方案对外端口相同（8680），入口机 `deploy/nginx/auth.x-rv.com.conf` 无需改动
- 共用同一份 `deploy/.env.prod`
- **互斥**：8680 只能有一个占用者，切换脚本会自动停掉对面那一套

### 改部署时必须守住的约束

1. **JWT 私钥不能丢。** 私钥在 `/opt/d-auth/data/jwt-private.pem`（Docker 下是
   `d-auth_app_data` 卷）。切换方案时必须搬运，否则所有已签发 token 失效、
   JWKS 公钥变化、全部下游服务验签失败。两个切换脚本已内置搬运逻辑。
2. **API 必须绑回环。** unit 里设 `DAUTH_HTTP_ADDR=127.0.0.1:8681`，不要改成
   `HTTP_PORT` —— 后者会绑 `0.0.0.0` 把 API 暴露到公网。
3. **unit 接管的键不要重复写进 `.env.prod`。** systemd 中 `EnvironmentFile=`
   会覆盖 `Environment=`，重复会让 unit 里的值失效。涉及 `APP_ENV`、
   `DAUTH_HTTP_ADDR`、`HTTP_PORT`、`DAUTH_JWT_PRIVATE_KEY_PATH`。
   `install.sh` 会检查并拒绝。
4. **`DAUTH_TRUSTED_PROXIES` 必须含入口机 IP。** 否则所有请求被算作同一 IP，
   验证码 IP 限流会误封全体用户。
5. **`/opt/d-auth` 权限是 755。** 不能是 700，否则 nginx 用户进不去 `web/`。

## 其它

- 不要在新代码里引入 cgo：后端现有依赖链（`jackc/pgx/v5`）是纯 Go，`CGO_ENABLED=0`
  交叉编译是裸机方案能成立的前提。
- 前端不要在构建期注入环境变量：`web/src/api/client.ts` 用同源相对路径
  （`baseURL: ""`），这样本地 build 的产物与服务器上跑的一致。
- UI 组件库：项目已基于 naive-ui 实现，暂不迁移；新项目不要选它（原因见用户级 AGENTS.md）。