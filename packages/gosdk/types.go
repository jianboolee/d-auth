// Package dauth 是 d-auth 账户服务的 Go 客户端，供子系统（业务后端）用一把
// API Key 调用登录、查询、改资料等能力。
//
// 为什么需要它：d-auth 的 Go 代码全部在 api/internal/ 下，外部模块无法导入，
// 而子系统的微信小程序只把自己的域名加进白名单、不想再放行 auth.x-rv.com。
// 于是由子系统后端持 API Key 代理这些调用，小程序只跟子系统通信。本包即是那层
// 代理要用的客户端：纯标准库、零第三方依赖。
//
// 典型用法（BFF 模式）：
//
//	c := dauth.New(dauth.Options{BaseURL: "https://auth.x-rv.com", APIKey: os.Getenv("DAUTH_API_KEY")})
//	tokens, err := c.SMSLogin(ctx, phone, code)      // 登录（公开接口，不需要 key）
//	me, err := c.InternalUser(ctx, tokens.User.ID)   // 查当前用户（明文，需 key）
//	nick := "新昵称"
//	_, err = c.PatchUser(ctx, tokens.User.ID, dauth.PatchUserInput{Nickname: &nick})
package dauth

import "time"

// User 是 /v1/internal 与 /v1/admin 返回的用户资料：手机号/邮箱为明文，
// Roles 是完整角色码列表。字段与 api/internal/modules/dto.User 一一对应
// （那里是 internal，无法直接复用类型，故在此复刻）。
//
// 注意 ID 是 22 位小写字母数字串，不是 uuid。业务库存外键请用 varchar(22)/text。
type User struct {
	ID          string     `json:"id"`
	Nickname    string     `json:"nickname"`
	AvatarURL   string     `json:"avatar_url"`
	Bio         string     `json:"bio"`
	Gender      int16      `json:"gender"`
	Status      string     `json:"status"`
	Roles       []string   `json:"roles"`
	HasPassword bool       `json:"has_password"`
	HasPhone    bool       `json:"has_phone"`
	Phone       string     `json:"phone,omitempty"`
	Email       string     `json:"email,omitempty"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	// UpdatedAt 是 auth_users 行最后一次被修改的时间：改昵称/简介/性别/状态、
	// 重置密码、每次登录都会推进；改绑手机号/邮箱写的是身份表，不推进此值。
	UpdatedAt time.Time `json:"updated_at"`
}

// Profile 是 /v1/users 返回的脱敏资料：手机号/邮箱已掩码，角色折叠成 IsAdmin。
// 可以安全地透给前端。
type Profile struct {
	ID        string    `json:"id"`
	Nickname  string    `json:"nickname"`
	AvatarURL string    `json:"avatar_url"`
	Bio       string    `json:"bio"`
	Gender    int16     `json:"gender"`
	Status    string    `json:"status"`
	IsAdmin   bool      `json:"is_admin"`
	HasPhone  bool      `json:"has_phone"`
	Phone     string    `json:"phone,omitempty"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// TokenResult 是登录/刷新的返回：access/refresh token 与当前用户（已脱敏）。
type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

// Identity 是一条绑定身份（手机号/邮箱/微信等）。
type Identity struct {
	ID         string         `json:"id"`
	UserID     string         `json:"user_id"`
	Type       string         `json:"type"`
	Identifier string         `json:"identifier"`
	Extra      map[string]any `json:"extra"`
	VerifiedAt *time.Time     `json:"verified_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Role 是一个角色。
type Role struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	UserCount   int64     `json:"user_count"`
	CreatedAt   time.Time `json:"created_at"`
}

// AuditLog 是一条审计记录。APIKeyID 非空表示该操作由 API Key 发起
// （此时 ActorUserID 为空——密钥调用不记具体操作人）。
type AuditLog struct {
	ID           string    `json:"id"`
	ActorUserID  *string   `json:"actor_user_id"`
	APIKeyID     *string   `json:"api_key_id"`
	Action       string    `json:"action"`
	TargetUserID *string   `json:"target_user_id"`
	IP           string    `json:"ip"`
	CreatedAt    time.Time `json:"created_at"`
}

// UserStats 是仪表盘统计。Today/Yesterday 指注册数。
type UserStats struct {
	Total     int64 `json:"total"`
	Active    int64 `json:"active"`
	Today     int64 `json:"today"`
	Yesterday int64 `json:"yesterday"`
}

// AvatarUpload 是头像直传凭证。upload_url 指向服务端中转地址（同源相对路径），
// 需带 Cookie / Bearer 与 X-DAuth-Client；服务端保存字节并更新头像。
type AvatarUpload struct {
	ObjectKey string            `json:"object_key"`
	UploadURL string            `json:"upload_url"`
	PublicURL string            `json:"public_url"`
	Headers   map[string]string `json:"headers"`
}

// Page 是分页响应里除 items 之外的公共字段。
type Page struct {
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}
