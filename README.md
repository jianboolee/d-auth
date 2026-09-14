# d-auth

独立账户服务。仓库：`git@github.com:jianboolee/d-auth.git`

控制台、HTTP API、JWKS **共用一个对外入口**。其它项目不要直连容器内的 8681。

## 端口（独占 8680–8684）

| 端口 | 用途 |
| --- | --- |
| **8680** | 生产对外：nginx 托管前端，`/v1` `/health` `/static` 反代 API |
| 8681 | API。本地 `make api`；生产只在容器回环 |
| 8682 | 本地 Vite |
| 8683 | 本地 Postgres（`make up`） |
| 8684 | 本地 Redis（`make up`） |

刻意避开 5173、8080、以及 d-specs 的 8400 段。冲突时用 `make api API_PORT=8691` 这类变量覆盖。

## 本地开发

```bash
cp .env.example .env
make up          # Postgres :8683 + Redis :8684
make api         # http://localhost:8681（启动时自动 migrate）
make web         # http://localhost:8682 （/v1 代理到 8681）
```

或热重载一体：`make docker-dev`，浏览器打开 http://localhost:8682。

开发环境短信/邮件不真发，发码接口返回 `debug_code`。用 `13800000000` 首次登录会成为 `superadmin`。

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

嵌入用户管理页（可选）：[`@donktech/d-auth-admin`](packages/admin/README.md) 的 `createAuthAdminRoutes`。
