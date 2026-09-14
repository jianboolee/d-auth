# Cookie 跨子域

生产对外入口是 **8680**（或 `https://auth.x-rv.com`），不要让业务站直连 8681。

- `DAUTH_COOKIE_DOMAIN=.x-rv.com`
- `DAUTH_COOKIE_SECURE=true`
- `DAUTH_COOKIE_NAME=__Secure-dauth_session`（不要用 `__Host-`，无法设 Domain）
- `DAUTH_ALLOWED_ORIGINS=https://a.x-rv.com,https://b.x-rv.com,https://auth.x-rv.com`

业务站用 `@donktech/d-auth-sdk`，`mode: "cookie"`，`baseURL` 填账户中心 origin。请求自动 `credentials: include` 且带 `X-DAuth-Client`。

App / 小程序用 `mode: "token"`，不要依赖 Cookie。

其它后端：`GET /v1/.well-known/jwks.json` 验 RS256 JWT；用户管理用 `Authorization: Bearer dak_live_...`。
