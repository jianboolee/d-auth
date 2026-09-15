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

// AdminPhone 是后台建号／改绑手机号使用的宽松校验，与面向用户的 Phone 刻意分开：
// 后台允许给运营、测试账号分配一个不属于任何真实号段的占位号码（如 120 开头的
// 虚拟号），这类号码通过 Phone 会被拒，但后台需要能存下来。
//
// 登录、注册、自助改绑仍然走 Phone——只有后台的建号与改绑两个入口放宽，
// 虚拟号因此不会变成立即可以收码登录的凭据。
//
// 规范化规则：
//   - 先按 Phone 处理，真实号码的写法（本地 11 位 / 带 86 / + 国际号）保持原来结果；
//   - 其余纯数字 6-15 位原样保留，不猜国码——120 开头的虚拟号原样存，避免
//     给它硬套一个 +86 后与用户可见值不一致。
func AdminPhone(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	if p, ok := Phone(s); ok {
		return p, true
	}
	if len(s) >= 6 && len(s) <= 15 && isASCIIDigits(s) {
		return s, true
	}
	return "", false
}

func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
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

// MaskTarget 按标识形态自动选择脱敏方式：含 @ 的当邮箱，其余当手机号。
// 用于日志（手机号/邮箱都不应明文落到日志里）。
func MaskTarget(s string) string {
	if s == "" {
		return ""
	}
	if strings.Contains(s, "@") {
		return MaskEmail(s)
	}
	return MaskPhoneDisplay(s)
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
