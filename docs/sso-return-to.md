# 子系统 SSO 登录回跳（return_to）

子系统（d-note 等）未登录时把浏览器整页跳到 d-auth 登录页，登录成功后自动跳回
子系统。本文档记录这条链路的约定、校验语义，以及白名单选型的决策与硬化路径。

## 跳转约定

子系统前端在用户未登录时：

```
{页面 origin}/login?client={应用标识}&return_to={encodeURIComponent(当前页完整 URL)}
```

- `return_to`：登录成功后要回跳的目标。只按 **origin** 校验，URL 的路径/查询
  不校验、原样回跳（子系统希望回到用户原本在看的页面）。
- `client`：应用标识（如 `web_dnote`）。**当前不参与校验**，保留它是为白名单
  硬化到 per-client 后前端无需改动（见下方 TODO）。

登录页在两种时机使用 `return_to`：

1. **未登录**：登录成功（短信/密码/注册）后询问服务端该地址是否允许，允许则
   `window.location.replace` 整页跳回，否则进账户中心（原行为）。
2. **已登录**（d-auth 中心会话仍有效，这是免登回跳的核心场景）：路由守卫直接
   询问服务端，允许则立刻跳回子系统，不再停留在账户中心。

## 校验：`GET /v1/auth/return-target`

```
GET /v1/auth/return-target?client=…&return_to=…
→ 200 {"code":"OK","data":{"allowed":true,"url":"<原样 return_to>"}}   命中白名单
→ 200 {"code":"OK","data":{"allowed":false}}                          未命中/不合法
```

- 公开只读接口，**永远 200**：「不允许」不是错误，调用方据此退回常规流程。
- 校验在服务端完成，前端不接触白名单内容，只消费判定结果。防开放重定向的
  边界集中在这一处（`api/internal/httpx/returnto.go`）。
- 非法输入一律拒绝（fail-closed）：非 http(s) 协议、带 userinfo
  （`http://evil.com@localhost:5200` 这类凭据混淆）、非法端口、空值。
  `localhost` 与 `127.0.0.1` 是两个不同 origin，需分别登记（本地会话 cookie
  是 host-only 的，跨主机名不会携带，混用只会白跳一次）。

## 配置：`DAUTH_ALLOWED_RETURN_ORIGINS`

逗号分隔的 origin 列表（如 `https://note.x-rv.com,http://localhost:5200`）。
缺省含 `http://localhost:5200,http://127.0.0.1:5200`（本地 d-note）。条目即使
多写了 path 也按 origin 归一后匹配。

## 页面 origin ≠ API origin（本次 404 的教训）

子系统的 SSO 跳转地址**必须指向页面端口，不是 API 端口**：

| 环境 | API（`DAUTH_PUBLIC_URL` / issuer） | 页面（跳转应指向这里） |
|---|---|---|
| 本地 dev | 8681（gin，只有 `/v1`、`/health`、`/static`，**没有 SPA fallback**） | **8682**（Vite） |
| docker 单入口 | 8680（nginx 反代 `/v1`→8681） | **8680**（nginx `try_files` 回退 index.html） |
| 生产 | auth.x-rv.com（nginx 一体化） | auth.x-rv.com |

gin 没有、也刻意不加 SPA fallback：本地 dev 的前端产物由 Vite 即时提供，8681
serve 一份 `web/dist` 快照只会造成「页面永远落后于源码」的假象。代价是指向
API 端口的页面 URL 会 404——曾踩坑：d-note 的 `DAUTH_SSO_BASE_URL` 缺省回退到
`DAUTH_BASE_URL`（API origin 8681），跳过去就是 404。本地开发须显式配置指向
页面端口：

```
# d-note backend/.env
DAUTH_SSO_BASE_URL=http://localhost:8682
```

生产两者同域名（nginx 一体化），无此问题。另外 `DAUTH_PUBLIC_URL` 兼作 JWT
`iss`（子系统按它验签），**不要**为了页面跳转去改它。

## 决策记录：环境级白名单，而非 per-client

d-note 侧的对接 spec（`d-note/docs/dauth-sso-required-changes.md`）原设计给
`auth_clients` 表加 `redirect_uris` 字段、按 client 隔离回跳白名单。d-auth 侧
当前采用**环境级白名单**（`DAUTH_ALLOWED_RETURN_ORIGINS`），不区分 client。

理由与边界：

- 生产上 d-auth 会话 cookie 本就是 `Domain=.x-rv.com`、对所有子系统可见，
  per-client 隔离挡不住「已登录态跨子域自动生效」这一层；两种设计当前的实质
  差距只在纵深防御——自家某个子域被攻破后，环境级名单允许把用户连带会话
  跳去任何已登记子域，per-client 名单可以把爆炸半径限制在该 client 内。
- 环境级名单实现小、无迁移、无后台 UI，符合当前子系统数量少（1 个）的现状。

**TODO（硬化路径，子系统增多或接入不可控子域时执行）**：白名单迁移到
`auth_clients.redirect_uris`（照搬 spec 的改造 1/2/3/5）。`return-target` 端点
已接收 `client` 参数、`returnto.ts` helper 已透传，届时只需把
`matchReturnTarget` 的名单来源从 `cfg.ReturnOrigins` 换成按 `client` 查库，
前端与子系统零改动。未登记 `client` 时建议回退到环境级名单或直接拒绝，
迁移时定夺。

## 验收

- 未登录访问 `http://localhost:8682/login?client=web_dnote&return_to=http%3A%2F%2Flocalhost%3A5200%2F`
  → 登录成功后整页跳回 d-note，且 d-note 已登录。
- `return_to=https://evil.example.com` → 不跳转，停在账户中心。
- 已登录直接访问带合法 `return_to` 的 `/login` → 立即跳回（免登场景）。
- 重置密码流程：`/forgot` 回 `/login` 时透传 query，登录成功仍能回跳。
