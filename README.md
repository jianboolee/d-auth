# d-auth

独立账户服务。仓库：`git@github.com:jianboolee/d-auth.git`

控制台、HTTP API、JWKS **共用一个对外入口**。其它项目不要直连容器内的 8681。

## 端口（独占 8680–8682）

| 端口 | 用途 |
| --- | --- |
| **8680** | 生产对外：nginx 托管前端，`/v1` `/health` `/static` 反代 API |
| 8681 | API。本地 `make api`；生产只在容器回环 |
| 8682 | 本地 Vite |

Postgres / Redis 不再单独占端口，用默认的 5432 / 6379。

刻意避开 5173、8080、以及 d-specs 的 8400 段。冲突时用 `make api API_PORT=8691` 这类变量覆盖。

## 本地开发

```bash
cp .env.example .env
make up          # Postgres :5432 + Redis :6379（默认端口）
make api         # http://localhost:8681（air 热重载，启动时自动 migrate）
make web         # http://localhost:8682 （/v1 代理到 8681）
```

`make api` 用 air 热重载，改 `.go` 会自动重建重启（首次用需 `go install github.com/air-verse/air@v1.66.0`）。
不想用热重载时直接跑：`cd api && HTTP_PORT=8681 go run ./cmd/server`。

端口起不来先看占用。本项目独占 8680-8682，占用者多半是上次没退干净的 `make api` / `make web`：

```bash
make stop        # 停掉 8680-8682 上的残留进程与 dev 容器栈，不动 Postgres / Redis
```

`make api` / `make web` 启动前也会预检端口，被占用时直接打印占用进程的 PID 与命令行。
要连 Postgres / Redis 一起停，用 `make down`。

或热重载一体（容器内）：`make docker-dev`，浏览器打开 http://localhost:8682。

小程序 web-view / iframe 使用无侧栏地址：`/embed/login`、`/embed/account/profile`。

开发环境短信/邮件不真发，验证码会打印到 API 服务终端（不会出现在 HTTP 响应里）。用 `13800000000` 首次登录会成为 `superadmin`。

## 部署（与 d-specs 相同形态）

全栈单镜像：nginx:80 + Go API:8681。宿主机映射 **8680:80**。

```bash
# 构建（默认 linux/amd64，镜像 jianboo/d-auth）
make docker-build
make docker-release          # 推送
make docker-run              # 本机试跑，访问 http://localhost:8680

# 服务器 / compose
cp deploy/.env.example deploy/.env   # 填 POSTGRES_PASSWORD、DAUTH_MASTER_KEY
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d
```

Portainer：粘贴 [deploy/portainer-stack.yml](deploy/portainer-stack.yml)，环境变量至少填 `POSTGRES_PASSWORD`、`DAUTH_MASTER_KEY`。已有数据库用 [deploy/portainer-stack-external-db.yml](deploy/portainer-stack-external-db.yml)。

外层 nginx 模板见 [deploy/nginx/auth.x-rv.com.conf](deploy/nginx/auth.x-rv.com.conf)，推送：`make nginx-push NGINX_SERVER=root@host`。

生产建议：

- `DAUTH_PUBLIC_URL=https://auth.x-rv.com`
- `DAUTH_COOKIE_DOMAIN=.x-rv.com`
- `DAUTH_COOKIE_SECURE=true`

## 给其它项目用的 SDK

业务前端请用 [`@donktech/d-auth-sdk`](packages/sdk/README.md)，不要复制登录接口。

```ts
import { createDAuthClient } from "@donktech/d-auth-sdk";

const auth = createDAuthClient({
  baseURL: "https://auth.x-rv.com", // 或 http://host:8680；不要带 /v1
  mode: "cookie",                   // App 用 "token"
  clientKey: "web_ticket",
});
```

同仓库旁路安装：

```json
{
  "dependencies": {
    "@donktech/d-auth-sdk": "file:../d-auth/packages/sdk"
  }
}
```

其它 **Go 服务不要 import 本模块**。推荐 **HS256**：设置 `DAUTH_JWT_SECRET`（或 `JWT_SECRET`）后按该算法签发，其它服务用同一把密钥验 `iss` / `sub` / `roles` / `sid`。未配密钥时仍为 RS256，公钥在 `GET /v1/.well-known/jwks.json`。查改用户用 `Authorization: Bearer dak_live_...`，或：

```ts
import { createDAuthServiceClient } from "@donktech/d-auth-sdk";

const auth = createDAuthServiceClient({
  baseURL: "https://auth.x-rv.com",
  apiKey: process.env.DAUTH_API_KEY!,
});
const user = await auth.internalLookup({ phone: "13800000000" });
```

## 业务端读取用户资料

业务系统（前端或后端）显示用户信息时，直接调 d-auth 查，不必在自己的库里存一份用户表：

| 接口 | 用途 | 返回 |
| --- | --- | --- |
| `GET /v1/users/:id` | 用户详情 | 单个资料对象 |
| `GET /v1/users?ids=a,b,c` | 批量取资料（最多 100 个，查不到的静默跳过） | `{ items, total }` |
| `GET /v1/users?phone=…` 或 `?email=…` | 按手机号/邮箱精确匹配 | `{ items, total }`（0 或 1 条） |

`ids` / `phone` / `email` 只能给其中一个，都不给或给多个返回 400 `INVALID_BODY`。

**鉴权**：需要 scope 含 `users:read` 的 API Key（控制台「接口密钥」创建，`dak_live_...`）。这个接口**不接受**浏览器里的用户登录态 —— 业务前端不要直连，请由自己的后端持密钥转发，否则密钥会泄露到浏览器。

**这两个接口的区别**：

- `/v1/users`：手机号/邮箱**脱敏**（`138****8000`），角色折叠成布尔值 `is_admin`，适合前端展示。
- `/v1/internal/users`：返回**明文**手机号/邮箱与完整 `roles`，适合服务端做业务判断。别把它的结果原样吐给前端。

资料里的 `phone` / `email` 未绑定时不返回；`has_phone` 为 `false`。

```ts
const auth = createDAuthServiceClient({ baseURL: "https://auth.x-rv.com", apiKey: process.env.DAUTH_API_KEY! });

const me = await auth.userProfile(userId);          // 单个
const list = await auth.users({ ids: [id1, id2] }); // 批量，顺序与入参一致
const hit = await auth.users({ phone: "13800000000" }); // 匹配不上返回 []
```

### 缓存

用户资料在 Redis 中缓存，读取接口命中缓存即可省掉数据库往返。改动资料、角色、身份、密码、头像、注销等写路径都会显式让缓存失效，`DAUTH_USER_CACHE_TTL`（默认 `60s`）只是兜底的最大陈旧窗口。配 `0` 即关闭缓存（每次回源数据库，功能不受影响）。Redis 不可用时自动降级为直查数据库，不会让接口报错。

嵌入用户管理页（可选）：[`@donktech/d-auth-admin`](packages/admin/README.md) 的 `createAuthAccountRoutes` / `createAuthAdminRoutes`。
