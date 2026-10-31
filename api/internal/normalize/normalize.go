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
// 绑定手机号/短信验证码这条链路仍然走 Phone——只有后台的建号与改绑两个入口、
// 以及密码登录（见 PasswordIdentifier）放宽，虚拟号因此不能收码，也无法用它
// 注册新账号。
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
//
// 这是「需要验证码或建号」的登录路径使用的严格版本（短信登录、注册等）：
// 虚拟号在这里一律被拒，因为它收不到验证码。
// 密码登录用 PasswordIdentifier —— 密码登录既不收码也不建号，允许虚拟号。
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

// PasswordIdentifier 是密码登录专用的标识解析：在严格 Identifier 的基础上，
// 额外放行 12 开头的虚拟号。
//
// 为什么单独放行：后台给运营/测试账号建的虚拟号（120 开头）需要能通过密码登录
// 进后台，而虚拟号收不到短信，所以它只能走密码登录。放开的前提是密码登录
// 这条路既不发验证码、也不建号——校验的只是「库里已有这个标识 + 密码正确」，
// 因此放行虚拟号不会带来新的登录入口，也不会让虚拟号变成可注册的凭据。
//
// 只放行 12 开头，而不是把所有非真实号段的纯数字都放开：虚拟号的约定前缀是
// 120，缩小到 12 已经覆盖它，避免其它任意数字串也能当账号名来尝试。
// 短信登录、注册、自助改绑仍走 Identifier/Phone，严格校验保持不变。
func PasswordIdentifier(raw string) (typ, identifier string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	if typ, identifier, ok := Identifier(s); ok {
		return typ, identifier, true
	}
	// 走到这里说明严格校验没过；只再认一种形态：12 开头的纯数字虚拟号。
	// 规范化方式与 AdminPhone 一致——去空格/连字符后原样保留，不套 +86，
	// 这样能精确命中后台建号时落库的值。
	digits := strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", "")
	if strings.Contains(s, "@") {
		// 含 @ 却没过邮箱校验，不是手机号，不要再往下猜
		return "", "", false
	}
	if len(digits) >= 6 && len(digits) <= 15 && strings.HasPrefix(digits, "12") && isASCIIDigits(digits) {
		return models.IdentityPhone, digits, true
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
