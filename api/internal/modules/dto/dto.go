package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/normalize"
)

type User struct {
	ID          uuid.UUID  `json:"id"`
	Nickname    string     `json:"nickname"`
	AvatarURL   string     `json:"avatar_url"`
	Bio         string     `json:"bio"`
	Gender      int16      `json:"gender"`
	Status      string     `json:"status"`
	Roles       []string   `json:"roles"`
	HasPassword bool       `json:"has_password"`
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
		ID:          u.ID,
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
		case models.IdentityEmail:
			out.Email = id.Identifier
		}
	}
	return out
}

func Masked(u User) User {
	if u.Phone != "" {
		u.Phone = normalize.MaskPhone(u.Phone)
	}
	if u.Email != "" {
		u.Email = normalize.MaskEmail(u.Email)
	}
	return u
}
