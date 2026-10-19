# Cookie 跨子域

生产对外入口是 **8680**（或 `https://auth.x-rv.com`），不要让业务站直连 8681。

- `DAUTH_COOKIE_DOMAIN=.x-rv.com`
- `DAUTH_COOKIE_SECURE=true`
- `DAUTH_COOKIE_NAME=__Secure-dauth_access`（不要用 `__Host-`，无法设 Domain）
- `DAUTH_COOKIE_REFRESH_NAME=__Secure-dauth_refresh`
- `DAUTH_ALLOWED_ORIGINS=https://a.x-rv.com,https://b.x-rv.com,https://auth.x-rv.com`

## 两枚 cookie

会话状态存在 Redis（refresh token 白名单），浏览器侧由两枚 HttpOnly cookie 承载：

| cookie | 内容 | Path | 谁能读到 |
| --- | --- | --- | --- |
| `dauth_access` | access JWT（RS256，默认 1h） | `/` | 业务后端 + d-auth（验签即认人） |
| `dauth_refresh` | refresh token（不透明串，30d） | `/v1/auth` | **只有 d-auth**（业务任何路径都收不到） |

- **读路径纯验签**：业务后端读 `dauth_access`，用 JWKS 验签（Go 用 gosdk 的
  `Verifier.VerifyRequest`），不查库、不查 Redis、不调 d-auth。
- **refresh cookie 用 Path 隔离**：`Path=/v1/auth` 保证它只发给 d-auth 的续期接口，
  业务系统不会拿到 refresh token。续期是一次同站 XHR（`POST /v1/auth/refresh`，
  `credentials: "include"`），拿到新 `dauth_access` 后重试原请求，无需整页跳转。
- **吊销窗口**：读路径不查存储，因此停用/改密/登出后旧 access token 最多再活一个
  TTL（默认 1h）。真正的吊销点是 refresh：白名单里删掉后无法再续期。TTL 因此是
  唯一的吊销窗口，按需调小。

业务站用 `@donktech/d-auth-sdk`，`mode: "cookie"`，`baseURL` 填账户中心 origin。请求自动 `credentials: include` 且带 `X-DAuth-Client`。

App / 小程序用 `mode: "token"`，不要依赖 Cookie。

其它后端：默认验 RS256（公钥在 `GET /v1/.well-known/jwks.json`，Go 用 gosdk 的 `Verifier`）；旧 HS256 部署共享 `DAUTH_JWT_SECRET` 的场景已废弃。用户管理用 `Authorization: Bearer sk-...`。

## 安全边界

`dauth_access` 是跨 `.x-rv.com` 共享的、可直接重放的 Bearer 凭据——任一子域的服务端都能
从请求头读到它。这是「cookie 载 JWT + 跨子域共享」的固有代价，也是 TTL 保持克制（默认 1h）的原因。

## CSRF

access token 进 cookie 后会随请求自动携带，业务后端的状态变更接口需自行做 CSRF 防护
（SameSite=Lax 只挡跨站 POST）。d-auth 自身的 cookie 写操作仍要求 `X-DAuth-Client` 头。
