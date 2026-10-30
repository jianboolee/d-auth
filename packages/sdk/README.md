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
access token 过期（默认 1h）后，SDK 会在 401 `UNAUTHORIZED` 时自动用
HttpOnly refresh cookie 续期并重试原请求；页面重新加载时 `session()` 也会静默续期。
只有 refresh 过期（默认 30d）或主动 `logout()` 后才需要重新登录。

### App / 小程序（Bearer Token）

```ts
const auth = createDAuthClient({
  baseURL: "https://auth.x-rv.com",
  mode: "token",
  clientKey: "app_ios",
});

const { access_token, refresh_token, is_new_user, user } = await auth.smsLogin(phone, code);
// 业务 API 请求头：Authorization: Bearer <access_token>
```

`is_new_user` 表示**本次请求刚刚创建了这个用户**（不是「用户很新」）。只有短信首次登录、
小程序首次登录、邮箱注册会为 `true`；密码登录与 `refresh()` 恒为 `false`。字段总是存在
（false 也会返回），不要靠「字段缺失」判断。

引导用户完善昵称/头像时只认这个标记——d-auth 建号时会写默认昵称
（微信 `"微信用户"`、短信 `maskPhoneDisplay(phone)`），昵称为空不能作为判据。

小程序：

```ts
const { is_new_user } = await auth.wechatMini("mp_xxx", wxLoginCode);
```

### 其它后端

验登录态：d-auth 默认 **RS256**，公钥在 `GET /v1/.well-known/jwks.json`，按 token header 的 `kid` 取公钥本地验签（校验 `iss` / `sub` / `roles`），无需共享密钥。浏览器场景读 `dauth_access` cookie 验签即可。Go 后端直接用 `packages/gosdk` 的 `Verifier`。

查用户：用控制台创建的 API Key（scope 含 `users:read`）。

> **Go 后端请用 [`packages/gosdk`](../gosdk/README.md)**（独立 Go module，纯标准库、零依赖），
> 而不是把本 TypeScript 包或 `api/internal/` 当库用。下面是 Node / JS 服务端用法。

```ts
import { createDAuthServiceClient } from "@donktech/d-auth-sdk";

const auth = createDAuthServiceClient({
  baseURL: "https://auth.x-rv.com", // 或 http://host:8680
  apiKey: process.env.DAUTH_API_KEY!, // sk-...
});

 await auth.internalUser(userId);
await auth.internalLookup({ phone: "+8613800000000" });
await auth.internalLookup({ email: "a@example.com" });
```

等价 HTTP：

```http
Authorization: Bearer sk-...
GET /v1/internal/users/:id
GET /v1/internal/users?phone=+86138...
```

> **服务端不要调 `me()` / `session()`**：这两个接口只认**用户登录态**，用 API Key 调用时
> `me()` 返回 401 `UNAUTHORIZED`，`session()` 返回空对象（`{}`）。服务端要「查当前用户」，
> 请自己验 JWT 拿 `sub`（22 位用户 ID），再按需用 `internalUser(sub)` 补资料。
> Go 后端完整对接见 [docs/integration-go.md](../../docs/integration-go.md)。

### 业务端读用户资料（脱敏）

展示用户信息用这组接口：手机号/邮箱已脱敏，角色折叠成 `is_admin` 布尔值。同样需要 `users:read` 密钥，**不要**从浏览器直连。

```ts
const profile = await auth.userProfile(userId);

// 批量：顺序与入参 ids 一致，查不到的 id 会被跳过
const list = await auth.users({ ids: [id1, id2, id3] });

// 精确匹配：匹配不上返回空数组，可直接用来判断「注册过没有」
const found = await auth.users({ phone: "+8613800000000" });
const byMail = await auth.users({ email: "a@example.com" });
```

等价 HTTP：

```http
GET /v1/users/:id
GET /v1/users?ids=a,b,c
GET /v1/users?phone=+86138...
GET /v1/users?email=a@example.com
```

`/v1/internal/*` 返回明文与完整 `roles`，只适合服务端内部使用；`/v1/users` 是脱敏版，可以安全地透给前端。


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
