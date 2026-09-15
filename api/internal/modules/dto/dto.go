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
	// uuid 字符串，与数据库 uuid 列的渲染一致。
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
}

type TokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
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

// Identity 对外暴露的绑定身份。user_id 转成对外的用户 ID 形式（uuid 字符串）。
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

// AuditLog 对外暴露审计记录。actor/target 是用户 ID，同样用 uuid 字符串形式。
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
