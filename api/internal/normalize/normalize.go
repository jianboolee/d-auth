package normalize

import (
	"regexp"
	"strings"

	"github.com/jianboolee/d-auth/internal/models"
)

var cnMobile = regexp.MustCompile(`^1[3-9]\d{9}$`)

func Phone(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	if strings.HasPrefix(s, "00") {
		s = "+" + s[2:]
	}
	if cnMobile.MatchString(s) {
		return "+86" + s, true
	}
	if strings.HasPrefix(s, "86") && cnMobile.MatchString(s[2:]) {
		return "+" + s, true
	}
	if strings.HasPrefix(s, "+86") && cnMobile.MatchString(s[3:]) {
		return s, true
	}
	if strings.HasPrefix(s, "+") && len(s) >= 8 && len(s) <= 16 {
		return s, true
	}
	return "", false
}

func Email(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" || !strings.Contains(s, "@") || strings.Contains(s, " ") {
		return "", false
	}
	parts := strings.Split(s, "@")
	if len(parts) != 2 || parts[0] == "" || !strings.Contains(parts[1], ".") {
		return "", false
	}
	return s, true
}

// Identifier 解析登录标识，判断它是手机号还是邮箱，并返回规范化后的值。
// 含 @ 的按邮箱处理，其余按手机号处理。返回的 typ 是 models 的身份类型常量。
func Identifier(raw string) (typ, identifier string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	if strings.Contains(s, "@") {
		if email, ok := Email(s); ok {
			return models.IdentityEmail, email, true
		}
		return "", "", false
	}
	if phone, ok := Phone(s); ok {
		return models.IdentityPhone, phone, true
	}
	return "", "", false
}

func MaskPhone(p string) string {
	if len(p) < 7 {
		return "***"
	}
	return p[:len(p)-8] + "****" + p[len(p)-4:]
}

// MaskPhoneDisplay 在脱敏的基础上去掉国际区号，用于面向用户展示（如自动生成的昵称），
// 使 +8613800138000 显示为 138****8000。
func MaskPhoneDisplay(p string) string {
	return MaskPhone(strings.TrimPrefix(p, "+86"))
}

func MaskEmail(e string) string {
	parts := strings.Split(e, "@")
	if len(parts) != 2 {
		return "***"
	}
	name := parts[0]
	if len(name) <= 2 {
		name = name[:1] + "***"
	} else {
		name = name[:1] + "***" + name[len(name)-1:]
	}
	return name + "@" + parts[1]
}
