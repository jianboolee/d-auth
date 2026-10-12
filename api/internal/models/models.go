package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusDeleted  = "deleted"

	IdentityPhone      = "phone"
	IdentityEmail      = "email"
	IdentityWechatOpen = "wechat_open"
	IdentityWechatMini = "wechat_mini"

	RoleSuperAdmin = "superadmin"
	RoleAdmin      = "admin"
	RoleUser       = "user"

	// ClientKeyConsole 是控制台自身的应用标识：web/src/api/client.ts 硬编码了这个
	// 值，登录流程在缺少 X-DAuth-Client 时也回退到它。改名或删除会让控制台发出的
	// 请求对不上应用、会话不再归属任何应用，因此后台禁止对它做这两件事。
	//
	// 若将来要把它改成生产用的名字，必须同步修改前端硬编码的值并重新部署。
	ClientKeyConsole = "web_console"
)

type TextArray []string

func (a TextArray) Value() (driver.Value, error) {
	if a == nil {
		return "{}", nil
	}
	parts := make([]string, len(a))
	for i, s := range a {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		parts[i] = `"` + s + `"`
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a *TextArray) Scan(value any) error {
	if value == nil {
		*a = TextArray{}
		return nil
	}
	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("unsupported array type %T", value)
	}
	s = strings.Trim(s, "{}")
	if s == "" {
		*a = TextArray{}
		return nil
	}
	*a = splitPGArray(s)
	return nil
}

func splitPGArray(s string) TextArray {
	var out TextArray
	var cur strings.Builder
	inQuote := false
	escape := false
	for _, r := range s {
		if escape {
			cur.WriteRune(r)
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == ',' && !inQuote {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	out = append(out, cur.String())
	return out
}

type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("unsupported json type %T", value)
	}
	return json.Unmarshal(b, m)
}

type User struct {
	// ID 是 22 位小写字母数字串（见 internal/idgen.NewUserID）。库里的 text 列
	// 存的就是这个裸串，接口返回的也是它——「库里看到的值与接口返回的值」永远一致。
	// 它随机不可排序，任何按时间的排序都走 created_at。
	ID           string     `gorm:"type:text;primaryKey" json:"id"`
	Nickname     string     `json:"nickname"`
	AvatarKey    string     `json:"avatar_key"`
	Bio          string     `json:"bio"`
	Gender       int16      `json:"gender"`
	PasswordHash *string    `json:"-"`
	Status       string     `json:"status"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	LastLoginIP  string     `json:"last_login_ip"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

func (User) TableName() string { return "auth_users" }

type Identity struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	UserID     string     `gorm:"type:text" json:"user_id"`
	Type       string     `json:"type"`
	Identifier string     `json:"identifier"`
	Extra      JSONMap    `gorm:"type:jsonb" json:"extra"`
	VerifiedAt *time.Time `json:"verified_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (Identity) TableName() string { return "auth_identities" }

type Role struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Role) TableName() string { return "auth_roles" }

type UserRole struct {
	UserID    string    `gorm:"type:text;primaryKey" json:"user_id"`
	RoleID    uuid.UUID `gorm:"type:uuid;primaryKey" json:"role_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserRole) TableName() string { return "auth_user_roles" }

type Client struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	ClientKey          string    `json:"client_key"`
	Name               string    `json:"name"`
	Type               string    `json:"type"`
	WechatAppID        string    `json:"wechat_app_id"`
	WechatAppSecretEnc string    `json:"-"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (Client) TableName() string { return "auth_clients" }

type APIKey struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	KeyHash    string     `json:"-"`
	Scopes     TextArray  `gorm:"type:text[]" json:"scopes"`
	CreatedBy  *string    `gorm:"type:text" json:"created_by"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (APIKey) TableName() string { return "auth_api_keys" }

type Session struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	UserID           string     `gorm:"type:text" json:"user_id"`
	ClientID         *uuid.UUID `json:"client_id"`
	RefreshTokenHash string     `json:"-"`
	AccessJTI        string     `json:"-"`
	IP               string     `json:"ip"`
	UserAgent        string     `json:"user_agent"`
	ExpiresAt        time.Time  `json:"expires_at"`
	RevokedAt        *time.Time `json:"revoked_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (Session) TableName() string { return "auth_sessions" }

type AuditLog struct {
	ID           uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	ActorUserID  *string    `gorm:"type:text" json:"actor_user_id"`
	APIKeyID     *uuid.UUID `json:"api_key_id"`
	Action       string     `json:"action"`
	TargetUserID *string    `gorm:"type:text" json:"target_user_id"`
	IP           string     `json:"ip"`
	UserAgent    string     `json:"user_agent"`
	Metadata     JSONMap    `gorm:"type:jsonb" json:"metadata"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (AuditLog) TableName() string { return "auth_audit_logs" }

// LegalDoc 是站点级法务文档（用户协议 / 隐私政策），全局一份，由超管在后台维护。
// URL 与 Content 二选一：URL 非空时前端跳转到该地址，否则渲染 Content（markdown）。
type LegalDoc struct {
	Kind      string    `gorm:"primaryKey" json:"kind"`
	URL       string    `json:"url"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LegalDoc) TableName() string { return "auth_legal_docs" }

const (
	// 目前只有这两种，见 migrations/0002_legal_docs.sql 的预置行。
	LegalKindTerms   = "terms"
	LegalKindPrivacy = "privacy"
)

// LegalKinds 是允许的 kind 全集，用于校验入参。
var LegalKinds = []string{LegalKindTerms, LegalKindPrivacy}

// IsLegalKind 判断 kind 是否为受支持的文档类型。
func IsLegalKind(kind string) bool {
	for _, k := range LegalKinds {
		if k == kind {
			return true
		}
	}
	return false
}
