package normalize

import (
	"regexp"
	"strings"
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

func MaskPhone(p string) string {
	if len(p) < 7 {
		return "***"
	}
	return p[:len(p)-8] + "****" + p[len(p)-4:]
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
