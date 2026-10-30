package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/normalize"
)

// User 是对外暴露的用户信息。
// HasPhone 表示是否已绑定手机号：手机号是主要登录能力，前端据此决定是否
// 把用户引导到绑定手机号页。
type User struct {
	// 22 位小写字母数字串，与数据库 text 列存的完全一致。
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
	// UpdatedAt 是 auth_users 这一行最后一次被修改的时间：改昵称、改状态、
	// 重置密码、以及每次登录（会一并更新 last_login_at）都会推进它。
	//
	// 注意它不覆盖 auth_identities 的变更——改绑手机号/邮箱写的是身份表，
	// 不会推进这里的 updated_at（自助改绑与后台改绑都一样）。
	UpdatedAt time.Time `json:"updated_at"`
}

type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	// IsNewUser 表示「本次请求刚刚创建了这个用户」，而不是「这个用户很新」。
	// 业务方（如 d-camp）据此决定是否引导完善昵称/头像。
	//
	// 只有真正建号的登录路径才会是 true：短信登录首次建号、小程序登录首次建号、
	// 邮箱注册。密码登录与续期永远不会建号，恒为 false——字段仍然返回，
	// 免得调用方还要处理「字段缺失」与「false」两种写法。
	IsNewUser bool `json:"is_new_user"`
	User      User `json:"user"`
}

// Profile 是给业务系统看的用户资料。
//
// 刻意不含 User 的全部字段：roles 明细折叠成 IsAdmin 布尔值（业务端要知道
// 「是不是管理员」几乎都是为了展示标识，不需要知道对方还有哪些自定义角色），
// 并且不含 has_password / last_login_at —— 前者是账号安全信息，后者是隐私，
// 带上它们还会让公开资料随「登录」「改密码」失效，白丢缓存命中。
type Profile struct {
	ID        string `json:"id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
	Gender    int16  `json:"gender"`
	Status    string `json:"status"`
	// IsAdmin 表示是否为管理角色（admin 或 superadmin）。
	IsAdmin  bool `json:"is_admin"`
	HasPhone bool `json:"has_phone"`
	// Phone / Email 已脱敏，如 138****8000、z***n@example.com。
	Phone     string    `json:"phone,omitempty"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// IsAdminRoles 判断角色列表中是否含管理角色。
func IsAdminRoles(roles []string) bool {
	for _, r := range roles {
		if r == models.RoleAdmin || r == models.RoleSuperAdmin {
			return true
		}
	}
	return false
}

// FromUser 组装对外用户信息。avatar 由调用方注入，用于把 avatar_key 转成公开地址
// （含图片处理后缀），避免各模块重复实现 URL 拼接。
func FromUser(u *models.User, roles []string, idents []models.Identity, avatar func(string) string) User {
	out := User{
		ID:          idgen.FormatUserID(u.ID),
		Nickname:    u.Nickname,
		AvatarURL:   avatar(u.AvatarKey),
		Bio:         u.Bio,
		Gender:      u.Gender,
		Status:      u.Status,
		Roles:       roles,
		HasPassword: u.PasswordHash != nil && *u.PasswordHash != "",
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
	for _, id := range idents {
		switch id.Type {
		case models.IdentityPhone:
			out.Phone = id.Identifier
			out.HasPhone = true
		case models.IdentityEmail:
			out.Email = id.Identifier
		}
	}
	return out
}

// Masked 脱敏手机号与邮箱。手机号用 MaskPhoneDisplay 去掉国际区号，
// 让国内号码显示成 138****8000 而不是 +86138****8000。
func Masked(u User) User {
	if u.Phone != "" {
		u.Phone = normalize.MaskPhoneDisplay(u.Phone)
	}
	if u.Email != "" {
		u.Email = normalize.MaskEmail(u.Email)
	}
	return u
}

// FromUserProfile 由已组装好的 User 生成公开资料：脱敏手机号/邮箱，
// 并把角色明细折叠成 IsAdmin。
//
// 入参是 User 而不是各个原始模型，是为了让缓存命中的条目能直接复用——
// 缓存的正是 User（既服务 internal 的明文返回，也服务这里的脱敏输出）。
func FromUserProfile(u User) Profile {
	m := Masked(u)
	return Profile{
		ID:        m.ID,
		Nickname:  m.Nickname,
		AvatarURL: m.AvatarURL,
		Bio:       m.Bio,
		Gender:    m.Gender,
		Status:    m.Status,
		IsAdmin:   IsAdminRoles(u.Roles),
		HasPhone:  m.HasPhone,
		Phone:     m.Phone,
		Email:     m.Email,
		CreatedAt: m.CreatedAt,
	}
}

// Identity 对外暴露的绑定身份。user_id 是用户的 22 位字母数字 ID。
type Identity struct {
	ID         uuid.UUID      `json:"id"`
	UserID     string         `json:"user_id"`
	Type       string         `json:"type"`
	Identifier string         `json:"identifier"`
	Extra      models.JSONMap `json:"extra"`
	VerifiedAt *time.Time     `json:"verified_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

func FromIdentity(id *models.Identity) Identity {
	return Identity{
		ID:         id.ID,
		UserID:     idgen.FormatUserID(id.UserID),
		Type:       id.Type,
		Identifier: id.Identifier,
		Extra:      id.Extra,
		VerifiedAt: id.VerifiedAt,
		CreatedAt:  id.CreatedAt,
	}
}

func FromIdentities(list []models.Identity) []Identity {
	out := make([]Identity, 0, len(list))
	for i := range list {
		out = append(out, FromIdentity(&list[i]))
	}
	return out
}

// Mask 脱敏手机号与邮箱标识。第三方标识（微信 openid 等）不是用户隐私，
// 且界面靠它区分账号，保持原样。
func (i Identity) Mask() Identity {
	switch i.Type {
	case models.IdentityPhone:
		i.Identifier = normalize.MaskPhoneDisplay(i.Identifier)
	case models.IdentityEmail:
		i.Identifier = normalize.MaskEmail(i.Identifier)
	}
	return i
}

// FromIdentitiesMasked 供用户自助接口使用。自助接口若只脱敏用户信息、
// 却在这里原样返回 identifier，绑定列表就成了绕过脱敏的入口。
func FromIdentitiesMasked(list []models.Identity) []Identity {
	out := FromIdentities(list)
	for i := range out {
		out[i] = out[i].Mask()
	}
	return out
}

// AuditLog 对外暴露审计记录。actor/target 是用户的 22 位字母数字 ID。
type AuditLog struct {
	ID           uuid.UUID  `json:"id"`
	ActorUserID  *string    `json:"actor_user_id"`
	APIKeyID     *uuid.UUID `json:"api_key_id"`
	Action       string     `json:"action"`
	TargetUserID *string    `json:"target_user_id"`
	IP           string     `json:"ip"`
	CreatedAt    time.Time  `json:"created_at"`
}

func FromAuditLogs(list []models.AuditLog) []AuditLog {
	out := make([]AuditLog, 0, len(list))
	for i := range list {
		item := AuditLog{
			ID:        list[i].ID,
			APIKeyID:  list[i].APIKeyID,
			Action:    list[i].Action,
			IP:        list[i].IP,
			CreatedAt: list[i].CreatedAt,
		}
		if list[i].ActorUserID != nil {
			s := idgen.FormatUserID(*list[i].ActorUserID)
			item.ActorUserID = &s
		}
		if list[i].TargetUserID != nil {
			s := idgen.FormatUserID(*list[i].TargetUserID)
			item.TargetUserID = &s
		}
		out = append(out, item)
	}
	return out
}
