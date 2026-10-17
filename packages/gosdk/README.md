# @donktech/d-auth Go SDK

子系统（业务后端，如 d-camp）用一把 API Key 调用 d-auth 的 Go 客户端：登录、查用户、
改资料、管理用户。**纯标准库、零第三方依赖**，不会把 gin/gorm 带进你的服务。

## 为什么需要它

子系统的微信小程序只能把**自己的域名**加进白名单，不想再放行 `auth.x-rv.com`。
做法是让子系统后端持 API Key 代理这些调用，小程序只跟子系统通信。本包就是那层代理
要用的客户端。

三条边界（重要）：

- **登录接口是公开的**，不需要 API Key。API Key 只用于**服务端查/改用户**。
- API Key 代表**子系统**，不代表某个用户。要「以某用户身份」做事，得先登录拿到该用户
  的 id，再用 API Key 按 id 操作（或转发用户的 access token 调 `/v1/me/*`）。
- API Key **绝不能下发到小程序**，只能存在后端。

## 接入准备（写代码前先做这几件事）

子系统需要向 d-auth 的管理员（超管）索要/配置三样东西。都在 d-auth 控制台完成：

| 事项 | 谁来配 | 在哪配 | 说明 |
| --- | --- | --- | --- |
| **API Key**（`sk-...`） | d-auth 超管 | 控制台「API Keys」页 | 给子系统**单独签发一把**，只存后端环境变量。默认 scope 是 `users:read`+`users:write`（控制台不支持按密钥收窄）。 |
| **小程序 client**（可选） | d-auth 超管 | 控制台「应用 / 小程序」页 | 仅当子系统有**自己的微信小程序**、要走 `WeChatMini` 登录时才需要。填该小程序的 `client_key`（形如 `mp_dcamp`，规则 `^[a-z][a-z0-9_-]{1,31}$`）、AppID 与 AppSecret。 |
| **可信代理**（速率限制） | d-auth 运维 | d-auth 的 `DAUTH_TRUSTED_PROXIES` | 若子系统后端代理用户的短信验证码，需把**子系统后端出口 IP** 加进去，否则所有用户的发码请求会共用一个 IP 桶（默认 100/小时）而被限流。详见「注意事项」。 |

子系统自己这边只需两个环境变量：

```bash
DAUTH_BASE_URL=https://auth.x-rv.com   # 或 http://host:8680，不要带 /v1
DAUTH_API_KEY=sk-...                   # 上面拿到的密钥
```

> d-auth 侧地址与端口见仓库根 [README.md](../../README.md)；BFF 拓扑、验签、各接口的字段级细节见
> [docs/integration-go.md](../../docs/integration-go.md)。

## 安装

```bash
go get github.com/jianboolee/d-auth/packages/gosdk@main
```

> 用 `@main` 取最新；正式发布后再改用版本号（`@v0.1.0`）。版本 tag 形如
> `packages/gosdk/v0.1.0`（Go 嵌套子模块的约定）。

本地联调（不经过远端）可在 `go.mod` 里用 `replace` 指向仓库路径：

```
require github.com/jianboolee/d-auth/packages/gosdk v0.0.0
replace github.com/jianboolee/d-auth/packages/gosdk => ../d-auth/packages/gosdk
```

> 这是 Go module 的**独立子模块**（有自己的 `go.mod`），位于 d-auth 仓库树内。
> d-auth 主 API 的代码都在 `api/internal/`，外部无法导入，所以本包自带一份与之
> 对齐的类型。

## 快速开始

```go
import (
    "context"
    "os"

    dauth "github.com/jianboolee/d-auth/packages/gosdk"
)

// 子系统后端启动时建一个，全局复用（并发安全）。
var auth = dauth.New(dauth.Options{
    BaseURL: "https://auth.x-rv.com",        // 不要带 /v1
    APIKey:  os.Getenv("DAUTH_API_KEY"),     // sk-...
})
```

### 登录（BFF 模式）

```go
ctx := context.Background()

// 1) 短信登录：拿到 d-auth 的 token 与用户（公开接口，不需要 key）
tokens, err := auth.SMSLogin(ctx, phone, code)

// 2) 用 refresh token 续期（access 默认 15 分钟）
tokens, err = auth.Refresh(ctx, tokens.RefreshToken)
defer auth.Logout(ctx, tokens.RefreshToken) // 登出：吊销会话

// 3) 退出时也可幂等登出
```

小程序登录走 `WeChatMini`（需要 d-auth 后台已登记该小程序的 client_key 并配好
AppID/AppSecret）：

```go
tokens, err := auth.WeChatMini(ctx, "mp_dcamp", jsCode)
```

BFF 建议：d-auth 的 token **只存在你的后端**，用你自己签发的会话 id 索引；小程序只持
你的会话凭证。这样 d-auth token 不落到客户端。

### 查当前登录用户 / 用户资料

```go
// 明文资料（含手机号/邮箱明文与完整 roles），服务端业务判断用
me, err := auth.InternalUser(ctx, tokens.User.ID)

// 按手机号/邮箱精确匹配
u, err := auth.InternalLookup(ctx, phone, "")

// 脱敏资料（可安全透给前端）：手机号掩码、角色折叠成 is_admin
p, err := auth.UserProfile(ctx, userID)
list, err := auth.Users(ctx, []string{id1, id2}, "", "") // 批量，最多 100
```

### 改资料 / 管理用户

用户改自己（转发用户 access token 到 `/v1/me/*`）不在本包范围——那需要用户态凭据。
**代客改**用 API Key：

```go
nick := "新昵称"
bio := "签名"
gender := int16(2)
u, err := auth.PatchUser(ctx, userID, dauth.PatchUserInput{
    Nickname: &nick, Bio: &bio, Gender: &gender,
})

auth.SetPhone(ctx, userID, "13800000000")   // 直接改绑（不校验验证码）
auth.SetEmail(ctx, userID, "a@example.com")
auth.AdminResetPassword(ctx, userID, "newpass123") // 会撤销该用户全部会话
avatar, err := auth.AdminUploadAvatar(ctx, userID, "a.png", pngBytes)
```

指针字段为 `nil` 表示不改（不会被零值覆盖）。`Status` 置 `disabled` 会撤销全部会话。

### 其他管理能力

```go
page, err := auth.ListUsers(ctx, dauth.UserListQuery{Q: "张", Status: "active", Page: 1, Size: 20})
stats, err := auth.Stats(ctx)
roles, err := auth.Roles(ctx)
logs, total, err := auth.AuditLogs(ctx, "", 1, 20)
sessions, total, err := auth.UserSessions(ctx, userID, 1, 10)
```

## 能力矩阵

| 能力 | 方法 | 需要的凭证 |
| --- | --- | --- |
| 短信/密码/邮箱/微信登录、刷新、登出 | `SMSLogin` `PasswordLogin` `EmailRegister` `WeChatMini` `Refresh` `Logout` | **无**（公开接口） |
| 查明文资料（按 id / 手机 / 邮箱） | `InternalUser` `InternalLookup` | `users:read` |
| 查脱敏资料（单个 / 批量 / 匹配） | `UserProfile` `Users` | `users:read` |
| 用户列表 / 详情 / 会话 / 统计 / 角色 / 审计 | `ListUsers` `AdminUser` `UserSessions` `Stats` `Roles` `AuditLogs` | `users:read` |
| 建号 / 改资料 / 改绑 / 重置密码 / 头像 | `CreateUser` `PatchUser` `SetPhone` `SetEmail` `AdminResetPassword` `AdminSignAvatar` `AdminUploadAvatar` | `users:write` |
| 注销用户（不可逆） | `DeleteUser` | **超管会话**（API Key 会被拒） |

可改字段：`PatchUser` 支持 `nickname` / `bio` / `gender` / `status` / `roles`；改绑手机/邮箱用
`SetPhone` / `SetEmail`。

## 错误处理

服务端错误会还原成 `*dauth.APIError`：

```go
import "errors"

if _, err := auth.SetPhone(ctx, id, phone); err != nil {
    var apiErr *dauth.APIError
    if errors.As(err, &apiErr) && apiErr.Code == "PHONE_TAKEN" {
        // 手机号已被占用
    }
}
```

常见 `code`：`INVALID_BODY` `INVALID_ID` `INVALID_PHONE` `INVALID_EMAIL`（400）、
`UNAUTHORIZED`（401）、`FORBIDDEN`/`CSRF`（403）、`DISABLED`（403）、`NOT_FOUND`（404）、
`PHONE_TAKEN`/`EMAIL_TAKEN`（409）、`SEND_TOO_FAST`/`SEND_LIMIT`（429）。

## 注意事项

1. **速率限制会塌缩**：短信验证码限流**同时**按目标手机号和来源 IP 计数，默认**每 IP
   10/分钟、100/小时**。子系统后端代理**全部用户**的短信时，全体共用一个 IP 桶，必然触发。
   需把子系统后端加入 d-auth 的 `DAUTH_TRUSTED_PROXIES` 并**转发真实设备 IP**
   （`X-Forwarded-For`）。每用户限流（10/小时、60s 冷静期）不受影响。
2. **API Key ≈ 管理员凭证**：默认 scope 即含 `users:write`，能改任意用户的手机号/邮箱/
   密码/角色。为子系统**单独签发一把 key**，只存后端。
3. **Admin 改绑手机/邮箱不校验验证码**（以密钥为凭据）。若需要「用户本人改绑」的验证语义，
   应改用转发用户 access token 调 `/v1/me/*`。
4. **审计只记 `api_key_id`、不记操作人**：子系统的所有写在 d-auth 审计里无法区分到具体人，
   需要按人追溯得在子系统侧自己记日志。
5. **token 吊销不即时生效（token 模式）**：d-auth 的 Bearer 校验只查账号 `status`，
   登出/改密码后已签发的 access token 在过期前仍可用。敏感操作请缩短 `DAUTH_JWT_ACCESS_TTL`
   或实时查一次资料确认 `status`。
6. **用户 ID 是 22 位字母数字串，不是 uuid**，业务库存 `varchar(22)`。

## 与 TypeScript SDK 的关系

前端（含小程序 web-view）用 [`@donktech/d-auth-sdk`](../sdk/README.md)；Go 后端用本包。
两者的接口语义一致。更多集成细节见 [docs/integration-go.md](../../docs/integration-go.md)。

## 附：接入提示词（可直接复制发给子系统）

启动一个新子系统接入时，把下面整段复制给子系统（或它的编程助手）即可。这是一份自包含的
交接说明，指向本仓库的公开文档。

```text
对接 d-auth 账户服务（仅供你自己的 Go 后端 + 小程序，不要让小程序直连 d-auth）。

背景：d-auth 是独立的账户中心，负责登录 / 用户资料。你的小程序只能把「自己的域名」加进
微信白名单，所以由你的 Go 后端持一枚 API Key 代理 d-auth 调用，小程序只跟你的后端通信，
小程序侧不需要、也不应该出现 auth.x-rv.com。

请先读这两份文档：
- 接入主文档（先看这个）：https://github.com/jianboolee/d-auth/blob/main/packages/gosdk/README.md
- 深入参考（拓扑 / BFF / 踩坑）：https://github.com/jianboolee/d-auth/blob/main/docs/integration-go.md

安装客户端：
  go get github.com/jianboolee/d-auth/packages/gosdk@main

你要向 d-auth 管理员索要 / 确认三样东西：
1. 一枚 API Key（sk-...）—— 只存后端环境变量，绝不下发到小程序；
2. 若你的小程序要走微信登录：让 d-auth 侧登记你的小程序 client（client_key + AppID + AppSecret）；
3. 若你代理用户的短信验证码：让 d-auth 把「你后端的出口 IP」加入 DAUTH_TRUSTED_PROXIES
   （否则所有用户共用一个 IP 桶，默认 100/小时会被限流）。

你只需要两个环境变量：
  DAUTH_BASE_URL=https://auth.x-rv.com
  DAUTH_API_KEY=sk-...

核心用法（Go）：
  auth := dauth.New(dauth.Options{BaseURL: os.Getenv("DAUTH_BASE_URL"), APIKey: os.Getenv("DAUTH_API_KEY")})
  tokens, err := auth.SMSLogin(ctx, phone, code)          // 登录（公开接口，不需要 key）
  me, err := auth.InternalUser(ctx, tokens.User.ID)       // 查当前登录用户（明文）
  nick := "新昵称"
  _, err = auth.PatchUser(ctx, tokens.User.ID, dauth.PatchUserInput{Nickname: &nick}) // 改资料

关键约束：
- 登录接口是公开的，API Key 只用于「服务端查 / 改用户」；
- API Key 代表子系统、不代表某个用户，要按用户操作需先登录拿到其 id；
- d-auth 的 token 建议只存你的后端（BFF），小程序用你自己签发的会话凭证；
- 报错统一是 *dauth.APIError，用 errors.As 取 Code 分支处理。

请据此产出接入方案并实现，遇到不确定的接口 / 字段再回读上面两份文档。
```