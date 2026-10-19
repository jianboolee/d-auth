# Go 业务后端对接 d-auth

业务系统（Go 后端 + 前端）如何调用 d-auth 的登录、查询、改资料能力。适用于同组织的其它 Go 服务。

> 前端对接看 [`@donktech/d-auth-sdk`](../packages/sdk/README.md)；**子系统后端用它自带的 Go SDK**
> [`packages/gosdk`](../packages/gosdk/README.md)（一把 API Key 调登录/查/改，零依赖）；Cookie
> 跨子域看 [cookie.md](cookie.md)。
>
> 历史提示：仓库早年写「Go 服务不要 import 本模块」——那指的是不要把 `api/internal/` 当库。
> `packages/gosdk` 是**独立子模块**（自带 `go.mod`、纯标准库），可以作为依赖引入，两者不冲突。

## 拓扑：两套凭证，三个接口面

```
业务前端 ──登录/刷新──► d-auth（签发 access JWT + refresh token；access 存 dauth_access cookie）
   │                      ▲
   │ 业务请求(带 JWT 或 cookie) │ 服务端查询/代客操作(带 sk- API Key)
   ▼                      │
业务 Go 后端 ──────────────┘
  · 本地验签 JWT 拿到 sub / roles
  · 需要资料时调 /v1/internal/users/:id
```

| 身份 | 凭证 | 可调接口 | 持有方 |
| --- | --- | --- | --- |
| 用户会话 | access JWT（`dauth_access` cookie 或 `Authorization: Bearer`） | `/v1/me/*`、`/v1/auth/session` | 浏览器 / App |
| 会话续期 | refresh token（`dauth_refresh` cookie，Path 限死 `/v1/auth`） | `/v1/auth/refresh`、`/v1/auth/logout` | 浏览器 / App |
| 服务密钥 | `sk-...`（scope `users:read` / `users:write`） | `/v1/users/*`、`/v1/internal/*`、`/v1/admin/*` | 业务 Go 后端 |
| 管理员会话 | 用户登录态 + `admin` / `superadmin` 角色 | `/v1/admin/*` | 控制台 |

会话状态（refresh 白名单）存在 d-auth 的 Redis 里，**业务侧看不到、也不需要它**。

两条硬边界：

- **`/v1/me` 只认用户态**（`RequireUser`）。API Key 调用一律 401 `UNAUTHORIZED`。
- **`/v1/users`、`/v1/internal`、`/v1/admin` 只认 API Key 或管理员会话**（`RequireScope`）。普通用户 token 会被 403。

因此「查当前登录用户」不能靠 API Key 反查，业务后端应**自己验 JWT 拿 `sub`**，再按需补资料。

## 一、配置准备

d-auth 侧（`.env`）：

```bash
DAUTH_JWT_ALG=RS256                       # 非对称签名，公钥走 /v1/.well-known/jwks.json
DAUTH_PUBLIC_URL=https://auth.x-rv.com   # 默认 issuer 取它
# Cookie 模式（业务前端跨子域）另需：
DAUTH_COOKIE_DOMAIN=.x-rv.com
DAUTH_COOKIE_SECURE=true
DAUTH_ALLOWED_ORIGINS=https://a.x-rv.com,https://b.x-rv.com
```

> RS256 私钥在 d-auth 首次启动时自动生成（`data/jwt-private.pem`，容器部署落在持久卷里）。
> **不需要、也不要把任何 JWT 密钥给业务后端**——这是与旧 HS256 方案最大的区别。

业务 Go 后端侧：

```bash
DAUTH_BASE_URL=https://auth.x-rv.com    # 或 http://host:8680，不要带 /v1
DAUTH_JWT_ISSUER=https://auth.x-rv.com  # 验签时校验 iss
DAUTH_API_KEY=sk-...                    # 控制台「API Keys」创建；只有查/改用户需要
```

控制台为业务前端登记一条 `clients.client_key`（Cookie 写操作需要 `X-DAuth-Client`）。

**用哪把 API Key**：`users:read` 够查资料；`users:write` 能改用户（含手机号/密码/角色），权限面接近管理员。
注意新建 API Key **默认同时带 `users:read` 和 `users:write`**，只做查询的话应单独收窄。

## 二、登录：前端直连，后端不参与

登录接口都在 d-auth，业务 Go 后端不要代理，避免把密码/验证码经手一遍。可选：

| 场景 | 接口 | SDK |
| --- | --- | --- |
| 短信验证码登录 | `POST /v1/auth/sms/login` | `smsLogin` |
| 密码登录（手机号或邮箱） | `POST /v1/auth/password/login` | `passwordLogin` |
| 邮箱注册（含密码） | `POST /v1/auth/email/register` | `emailRegister` |
| 微信扫码 / 小程序 | `POST /v1/auth/wechat/qr/start`、`/v1/auth/wechat/mini` | `wechatQrStart`、`wechatMini` |

登录成功返回：

```json
{
  "code": "OK", "message": "ok",
  "data": {
    "access_token": "eyJ...", "refresh_token": "...",
    "token_type": "Bearer", "expires_in": 900,
    "user": { "id": "…22位…", "nickname": "…", "roles": ["user"], "has_phone": true, "phone": "138****8000" }
  }
}
```

默认 access 1 小时、refresh 30 天。`data.user` 是**本人视角**（手机号/邮箱已脱敏）。

**两种前端模式**：

- **Web（cookie）**：跨子域共享登录态，JS 拿不到 token。access token 在 `dauth_access` cookie 里
  （`Path=/`，业务后端可见），refresh token 在 `dauth_refresh` cookie 里（`Path=/v1/auth`，只有
  d-auth 能收到）。业务后端读 `dauth_access` 验签即可，不必取 `sid`。
- **App / 第三方（token）**：前端持有 `access_token` + `refresh_token`，业务请求头带 `Authorization: Bearer <access_token>`。

access token 过期（默认 1h）后，前端向 d-auth 发一次 `POST /v1/auth/refresh`
（cookie 模式 `credentials: "include"`；token 模式带 `refresh_token`）拿到新 token 再重试。

## 三、Go 后端验签中间件（RS256 + JWKS）

用 `packages/gosdk` 自带的 `Verifier`：公钥从 `GET /v1/.well-known/jwks.json` 自动拉取并按 `kid`
缓存（默认 5 分钟节流），纯标准库实现，**不需要任何 JWT 密钥、不引入第三方依赖**。

```go
// auth.go
package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	dauth "github.com/jianboolee/d-auth/packages/gosdk"
)

type ctxKey struct{}

var verifier = dauth.NewVerifier(dauth.VerifierOptions{
	BaseURL: os.Getenv("DAUTH_BASE_URL"),
	Issuer:  os.Getenv("DAUTH_JWT_ISSUER"),
})

// Middleware 校验 access token：优先 Authorization: Bearer，其次读 dauth_access
// cookie（浏览器场景）。把 claims 注入 context。
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 第二个参数传 "" 即用默认 cookie 名 dauth_access。
		claims, err := verifier.VerifyRequest(r.Context(), r, "")
		if err != nil {
			// ErrKeySourceUnavailable = 暂时取不到公钥（d-auth 故障），
			// 返回 503 而不是把用户踢下线；其余（未登录/过期/伪造/降级）返回 401。
			if errors.Is(err, dauth.ErrKeySourceUnavailable) {
				http.Error(w, "auth unavailable", http.StatusServiceUnavailable)
			} else {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			}
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, claims)))
	})
}

func FromContext(ctx context.Context) (*dauth.TokenClaims, bool) {
	c, ok := ctx.Value(ctxKey{}).(*dauth.TokenClaims)
	return c, ok
}

// IsAdmin 与 d-auth 的判定一致：admin 或 superadmin。
// （TokenClaims 是 gosdk 的类型，本地判定就写成函数。）
func IsAdmin(c *dauth.TokenClaims) bool {
	for _, r := range c.Roles {
		if r == "admin" || r == "superadmin" {
			return true
		}
	}
	return false
}
```

`VerifyToken` 内部强制 `alg=RS256`（拒绝 none/HS256 降级伪造）、按 `kid` 匹配公钥、校验
`exp`/`iat`（30 秒容差）与 `iss`。返回的 `dauth.TokenClaims` 含 `Subject`（22 位用户 ID，
不是 uuid）、`Roles`、`ClientID`、`JTI`、`IssuedAt`、`ExpiresAt`。

> 旧 HS256 方案的共享密钥中间件已废弃：d-auth 不再签发 HS256 token，JWKS 里也不会有对称密钥。

### 验签的信任边界

本地验签只证明 token 未被篡改，**不代表账号当前仍可用**：

- d-auth 的读路径现在也是纯验签（不查库），因此**它和业务侧一样**看不到「账号被停用」
  「用户已登出」「改密码」的即时效果。
- 唯一的即时吊销点是 refresh：登出/改密/停用会删掉 Redis 里的 refresh 白名单项，之后无法续期。
- 已签发的 access token 在 TTL（默认 1h）内仍然有效。

需要「停用/登出即刻生效」的场景（支付、敏感操作），要么把 access TTL 调短，要么为此类操作实时查一次
`GET /v1/internal/users/:id` 确认 `status == "active"`。详见文末[注意事项](#九注意事项)。

## 四、查当前登录用户 / 用户资料

### 4.1 只要身份：读 JWT，不请求 d-auth

`sub`（用户 ID）、`roles`（是否管理员）都在 token 里。业务库外键存 `sub` 即可，**不要另存用户表**。

```go
claims, _ := auth.FromContext(r.Context())
userID := claims.Subject        // 22 位用户 ID
if auth.IsAdmin(claims) { /* … */ }
```

### 4.2 要展示字段：用 API Key 查 d-auth

业务后端持 `sk-` 密钥调查询接口。两种数据可见度：

| 接口 | 返回 | 用途 |
| --- | --- | --- |
| `GET /v1/users/:id` | **脱敏**（`138****8000`、`z***n@x.com`），roles 折叠成 `is_admin` | 可直接透给前端 |
| `GET /v1/users?ids=a,b,c` | 同上，最多 100 个，查不到的静默跳过 | 列表批量 |
| `GET /v1/users?phone=…` / `?email=…` | 同上，0 或 1 条 | 判断「注册过没有」 |
| `GET /v1/internal/users/:id` | **明文**手机/邮箱 + 完整 `roles` | 服务端业务判断，别透给前端 |
| `GET /v1/internal/users?phone=…` / `?email=…` | 同上 | 同上 |

`ids` / `phone` / `email` 三选一，都不给或给多个返回 400 `INVALID_BODY`。

Go 客户端示例：

```go
// dauth.go
package dauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New() *Client {
	return &Client{
		BaseURL: os.Getenv("DAUTH_BASE_URL"), // 不带 /v1
		APIKey:  os.Getenv("DAUTH_API_KEY"),
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
}

// envelope 是 d-auth 的统一响应体：{ code, message, data }。
type envelope[T any] struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// apiError 让调用方能按 code 分支（如 NOT_FOUND）。
type apiError struct {
	Code    string
	Message string
	Status  int
}

func (e *apiError) Error() string { return fmt.Sprintf("dauth %d %s: %s", e.Status, e.Code, e.Message) }

// InternalUser 取明文资料（服务端用）。id 是 22 位用户 ID。
func (c *Client) InternalUser(id string) (*User, error) {
	var out envelope[User]
	if err := c.get("/v1/internal/users/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

// InternalLookup 按手机号或邮箱精确匹配明文资料。
func (c *Client) InternalLookup(phone, email string) (*User, error) {
	q := url.Values{}
	if phone != "" {
		q.Set("phone", phone)
	}
	if email != "" {
		q.Set("email", email)
	}
	var out envelope[User]
	if err := c.get("/v1/internal/users", q, &out); err != nil {
		return nil, err
	}
	return &out.Data, nil
}

func (c *Client) get(path string, q url.Values, dest any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		var e struct{ Code, Message string }
		_ = json.NewDecoder(res.Body).Decode(&e)
		return &apiError{Code: e.Code, Message: e.Message, Status: res.StatusCode}
	}
	return json.NewDecoder(res.Body).Decode(dest)
}

// User 对应 dto.User（/v1/internal）。roles 完整；phone/email 明文。
type User struct {
	ID          string   `json:"id"`
	Nickname    string   `json:"nickname"`
	AvatarURL   string   `json:"avatar_url"`
	Bio         string   `json:"bio"`
	Gender      int16    `json:"gender"`
	Status      string   `json:"status"`
	Roles       []string `json:"roles"`
	HasPassword bool     `json:"has_password"`
	HasPhone    bool     `json:"has_phone"`
	Phone       string   `json:"phone,omitempty"`
	Email       string   `json:"email,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}
```

> 写业务外键时注意：`id` 是 22 位小写字母数字串，列用 `varchar(22)` / `text`，**不要当 uuid 存**。

### 4.3 「我是谁」的标准做法

业务前端把 access token 带给业务后端（token 模式）→ 业务后端本地验签拿 `sub` → 需要昵称头像时调
`InternalUser(sub)`。全程不需要 d-auth 参与「认人」，只把 d-auth 当资料源。

## 五、修改资料

分两条线，按「改谁」选。

### 5.1 用户改自己（业务后端转发用户态请求）

业务后端把用户的 `Authorization` 原样转发给 d-auth 的 `/v1/me/*`：

| 操作 | 接口 | 说明 |
| --- | --- | --- |
| 改昵称/简介/性别 | `PATCH /v1/me` | body 只认 `nickname` / `bio` / `gender` |
| 改密码 | `POST /v1/me/password` | 成功后**踢掉其它设备的续期能力**（保留当前设备） |
| 换绑手机号 | `POST /v1/me/phone/change` | 需新手机号验证码（`bind_phone`） |
| 绑定邮箱 | `POST /v1/me/email/bind` | 需邮箱验证码 |
| 换头像 | `POST /v1/me/avatar/sign` → `PUT` → `confirm` | 三步，见 SDK |

Cookie 模式下这些是写操作，**必须带 `X-DAuth-Client`**（即登记的 `client_key`），否则 403 `CSRF`。

### 5.2 业务后端代客操作（API Key）

用 `users:write` 密钥调 admin 面。这些接口**不校验验证码**，以密钥为凭据：

| 操作 | 接口 |
| --- | --- |
| 改昵称/简介/性别/状态/角色 | `PATCH /v1/admin/users/:id` |
| 直接改绑手机号 | `PATCH /v1/admin/users/:id/phone` |
| 直接改绑邮箱 | `PATCH /v1/admin/users/:id/email` |
| 重置密码 | `POST /v1/admin/users/:id/password` |
| 建号 | `POST /v1/admin/users` |
| 注销（墓碑式，不可逆） | `DELETE /v1/admin/users/:id`（**限 superadmin**） |

`PATCH /v1/admin/users/:id` 现已支持 `nickname` / `bio` / `gender` / `status` / `roles`（与自助端
`PATCH /v1/me` 同字段语义），指针为 `nil` 的字段不改。Go SDK 对应 `dauth.PatchUserInput`。

**慎用**：这是后台管理语义。业务后端如果只为「代客改个昵称」就把整条 admin 面接进来，等于让业务服务拥有一把
能改任意用户手机号/密码/角色、乃至注销账号的密钥，且审计只记 `api_key_id`、不记具体操作人。
只有在确实需要代客管理时使用，并单独签发一把收窄 scope 的密钥。

## 六、子系统 / 小程序 BFF 模式（推荐给 d-camp 这类子系统）

小程序只能把**自己的域名**加进白名单，不想放行 `auth.x-rv.com`。做法：子系统 Go 后端持一把
API Key，代理全部 d-auth 调用；小程序只跟子系统后端通信。用 [`packages/gosdk`](../packages/gosdk/README.md)。

```
小程序 ──► d-camp 后端 ──┬─► d-auth /v1/auth/*            （登录，公开，无需 key）
  （只认 d-camp 域名）    │   拿回 access/refresh token，只存后端
                        ├─► d-auth /v1/internal/users/:id  （查用户，Bearer sk-...）
                        └─► d-auth PATCH /v1/admin/users/:id（改资料，Bearer sk-...）
```

三条关键点：

- **登录不需要 API Key**：`/v1/auth/*` 是公开接口（小程序登录例外，见下）。
- **API Key 代表子系统，不代表某个用户**：要「以某用户身份」做事，先登录拿到该用户的 `id`，
  再用 API Key 按 id 查/改。若需要「用户本人改密码/改绑」那种**要验证码**的语义，则转发该用户的
  access token 调 `/v1/me/*`（那部分不属于 API Key 能力）。
- **token 只存后端**：BFF 用自己签发的会话 id 索引 d-auth 的 token，小程序只持有 d-camp 的会话凭证。

```go
// d-camp 后端
var auth = dauth.New(dauth.Options{BaseURL: os.Getenv("DAUTH_BASE_URL"), APIKey: os.Getenv("DAUTH_API_KEY")})

// 登录（或登录后按 id 补资料 / 代客改资料）
tokens, _ := auth.SMSLogin(ctx, phone, code)
me, _ := auth.InternalUser(ctx, tokens.User.ID)
nick := "新昵称"
auth.PatchUser(ctx, tokens.User.ID, dauth.PatchUserInput{Nickname: &nick})
```

### 小程序登录

子系统的小程序走 `WeChatMini`，前提是 **d-auth 后台为该子系统登记了一条 client**：`client_key` +
该小程序的 `wechat_app_id` / `wechat_app_secret`（后台「应用 / 小程序」页，或 `POST /v1/admin/clients`，
限超管）。之后：

```go
tokens, err := auth.WeChatMini(ctx, "mp_dcamp", jsCode) // jsCode 来自 wx.login
```

d-auth 用这条 client 的小程序凭证换 openid；会话归属到该 client_key。

## 七、刷新与登出

- **刷新**：`POST /v1/auth/refresh`。token 模式带 `refresh_token`；cookie 模式由浏览器自动带上
  `dauth_refresh` cookie（Path=`/v1/auth`，业务系统收不到）。refresh 每次都会**轮换**，
  新旧 token 单次有效。
- **前端续期姿势**：业务请求 401 时，向 d-auth 发一次 refresh（cookie 模式 `credentials: "include"`），
  拿到新 `dauth_access` cookie 后重试原请求即可，无需整页跳转。
- **登出**：`POST /v1/auth/logout` 删掉 refresh 白名单项并清 cookie，幂等。已签发的 access token
  在 TTL 内仍可用（见注意事项）。

## 八、错误处理

统一响应体：

```json
{ "code": "OK", "message": "ok", "data": { } }
```

失败时 HTTP 状态码与 `code`：

| 状态 | code | 含义 |
| --- | --- | --- |
| 400 | `INVALID_BODY` / `INVALID_ID` / `INVALID_PHONE` / `INVALID_EMAIL` | 参数错 |
| 400 | `INVALID_CODE` / `CODE_LOCKED` | 验证码错 / 锁定 |
| 401 | `UNAUTHORIZED` | 登录失效 / API Key 无效或过期 |
| 401 | `INVALID_CREDENTIALS` | 密码错误 |
| 403 | `FORBIDDEN` / `CSRF` | 权限不足 / 缺 `X-DAuth-Client` |
| 403 | `DISABLED` | 账号已停用 |
| 404 | `NOT_FOUND` | 用户不存在 |
| 409 | `PHONE_TAKEN` / `EMAIL_TAKEN` | 手机号 / 邮箱已被占用 |
| 429 | `SEND_TOO_FAST` / `SEND_LIMIT` | 验证码发送限流 |

`/v1/users` 按手机号/邮箱匹配不上时返回 `200` + `{items: [], total: 0}`，**不是 404** —— 用它判断「注册过没有」时读数组长度。

## 九、注意事项

1. **吊销不即时生效**：读路径（业务本地验签、以及 d-auth 自己）都是「验签即信」，**不查任何存储**。
   登出、改密码、停用后，已签发的 access token 在一个 TTL（默认 1h）内仍可用。唯一的即时动作是
   「停止续期」——refresh 白名单项被删后无法再换新 access。要缩窗口就把 `DAUTH_JWT_ACCESS_TTL` 调小，
   或对敏感操作实时查 `/v1/internal/users/:id` 确认 `status`。
2. **别用 API Key 调 `/v1/me` 或 `/v1/auth/session`**：前者 401，后者对 API Key 返回空对象。`createDAuthServiceClient` 生成的 client 仍暴露这两个方法，容易误用。
3. **默认 API Key 权限过大**：新建即含 `users:write`。只查询的业务方要把 scope 收窄到 `users:read`。
4. **脱敏边界**：`/v1/users` 可安全透给前端，`/v1/internal` 是明文，别原样返回前端。
5. **资料缓存**：用户资料在 Redis 缓存，`DAUTH_USER_CACHE_TTL` 默认 60s。写路径会显式失效，所以查询最多滞后一个 TTL。
6. **ID 类型**：用户 ID 是 22 位字母数字串，不是 uuid，业务库存 `varchar(22)`。
7. **BFF 代理会塌缩验证码限流**：短信/邮件验证码限流**同时**按目标与来源 IP 计数，默认**每 IP
   10/分钟、100/小时**（`DAUTH_CODE_PER_IP_MINUTE` / `DAUTH_CODE_PER_IP_HOUR`）。子系统后端代理
   **全部用户**的验证码发送时，全体共用一个 IP 桶，必然触发。需把子系统后端加入 d-auth 的
   `DAUTH_TRUSTED_PROXIES` 并**转发真实设备 IP**（`X-Forwarded-For`），否则要么误伤所有人、要么防线失效。
   每用户限流（10/小时、60s 冷静期）不受影响。