# @donktech/d-auth-sdk

其它业务站、App、管理后台、Node 服务调用 **d-auth** 的官方客户端。不要复制登录接口，统一走这套 SDK。

仓库：`git@github.com:jianboolee/d-auth.git`

包内已带 `dist/`，`file:` / Git 子目录安装后即可 `import`，无需再构建。

## 安装

同组织其它项目（推荐，源码旁路）：

```json
{
  "dependencies": {
    "@donktech/d-auth-sdk": "file:../d-auth/packages/sdk"
  }
}
```

pnpm 从 Git 装子目录：

```bash
pnpm add github:jianboolee/d-auth#path:packages/sdk
```

发布 npm 之后：

```bash
npm i @donktech/d-auth-sdk
```

## 对外地址

生产环境 **只有一个入口端口 8680**（或域名 `https://auth.x-rv.com`），控制台和 API 同域：

| 用途 | 地址 |
| --- | --- |
| 控制台 | `https://auth.x-rv.com/` 或 `http://host:8680/` |
| HTTP API | `https://auth.x-rv.com/v1/...` |
| JWKS | `https://auth.x-rv.com/v1/.well-known/jwks.json` |

`baseURL` 填 **源站 origin**，不要带 `/v1`。SDK 会自己拼 `/v1/...`。本站控制台可用空字符串表示同源。

本地开发 Vite 会把 `/v1` 代理到 8681，业务站联调请指向 `http://localhost:8682` 或已起的 `http://localhost:8680`。

## 用法

### Web（跨子域 Cookie）

业务站需在 d-auth 后台「应用 / 小程序」里登记 `client_key`。Cookie 域为 `.x-rv.com` 时，`a.x-rv.com` 与 `auth.x-rv.com` 可共用登录态。

跨域放行由服务端环境变量 `DAUTH_ALLOWED_ORIGINS` 控制，不在后台按应用配置。

```ts
import { createDAuthClient, isApiError } from "@donktech/d-auth-sdk";

export const auth = createDAuthClient({
  baseURL: "https://auth.x-rv.com",
  mode: "cookie",
  clientKey: "web_ticket", // 与后台 clients.client_key 一致
});

try {
  await auth.smsLogin("13800000000", "123456");
  const me = await auth.me();
} catch (e) {
  if (isApiError(e)) console.error(e.code, e.message);
}
```

请求会自动带 `credentials: include` 和 `X-DAuth-Client`。

### App / 小程序（Bearer Token）

```ts
const auth = createDAuthClient({
  baseURL: "https://auth.x-rv.com",
  mode: "token",
  clientKey: "app_ios",
});

const { access_token, refresh_token, user } = await auth.smsLogin(phone, code);
// 业务 API 请求头：Authorization: Bearer <access_token>
```

小程序：

```ts
await auth.wechatMini("mp_xxx", wxLoginCode);
```

### 其它后端（不要把本仓库当 Go 库 import）

验登录态：配了 `JWT_SECRET` / `DAUTH_JWT_SECRET` 即为 **HS256**，其它服务共用这把密钥校验（`iss` / `sub` / `roles` / `sid`）。仅 RS256 时才拉 JWKS。

查用户：用控制台创建的 API Key（scope 含 `users:read`）。

```ts
import { createDAuthServiceClient } from "@donktech/d-auth-sdk";

const auth = createDAuthServiceClient({
  baseURL: "https://auth.x-rv.com", // 或 http://host:8680
  apiKey: process.env.DAUTH_API_KEY!, // dak_live_...
});

await auth.internalUser(userId);
await auth.internalLookup({ phone: "+8613800000000" });
await auth.internalLookup({ email: "a@example.com" });
```

等价 HTTP：

```http
Authorization: Bearer dak_live_...
GET /v1/internal/users/:id
GET /v1/internal/users?phone=+86138...
```

## 导出一览

| 导出 | 用途 |
| --- | --- |
| `createDAuthClient` | 浏览器 / App 登录态 |
| `createDAuthServiceClient` | 服务端 API Key |
| `DAuthClient` | 类本身，便于类型标注 |
| `isApiError` | 判断 `{ code, message, status }` |
| `DAUTH_API_PREFIX` / `DAUTH_JWKS_PATH` | 路径常量 |

## 客户端约定

- `clientKey`：每个网站 / App / 小程序一条，对应表 `clients`
- 写操作在 Cookie 模式下必须有 `X-DAuth-Client`（SDK 已加）
- 错误体 `{ code, message }`，例如 `PHONE_TAKEN`、`INVALID_CODE`
