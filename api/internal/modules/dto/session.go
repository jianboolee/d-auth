package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/useragent"
)

// Session 是对外暴露的登录会话。相比 models.Session 只去掉内部字段，
// 并额外给出已归纳的 device，避免前端展示整串 User-Agent。
type Session struct {
	ID        uuid.UUID  `json:"id"`
	IP        string     `json:"ip"`
	Device    string     `json:"device"`
	UserAgent string     `json:"user_agent"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func FromSession(s *models.Session) Session {
	return Session{
		ID:        s.ID,
		IP:        s.IP,
		Device:    useragent.Describe(s.UserAgent),
		UserAgent: s.UserAgent,
		ExpiresAt: s.ExpiresAt,
		RevokedAt: s.RevokedAt,
		CreatedAt: s.CreatedAt,
	}
}

func FromSessions(list []models.Session) []Session {
	out := make([]Session, 0, len(list))
	for i := range list {
		out = append(out, FromSession(&list[i]))
	}
	return out
}
