package normalize_test

import (
	"testing"

	"github.com/jianboolee/d-auth/internal/normalize"
)

func TestMaskPhoneDisplay(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"cn mobile with country code", "+8613800138000", "138****8000"},
		{"cn mobile without country code", "13800138000", "138****8000"},
		{"international keeps prefix", "+14155552671", "+141****2671"},
		{"too short", "+86", "***"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalize.MaskPhoneDisplay(c.in); got != c.want {
				t.Fatalf("MaskPhoneDisplay(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// 密码登录的 identifier 由后端判断类型：含 @ 视为邮箱，否则视为手机号。
func TestIdentifier(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantType string
		wantID   string
		wantOK   bool
	}{
		{"cn mobile", "13800138000", "phone", "+8613800138000", true},
		{"cn mobile with 86", "8613800138000", "phone", "+8613800138000", true},
		{"cn mobile with plus", "+8613800138000", "phone", "+8613800138000", true},
		{"mobile with spaces", " 138 0013 8000 ", "phone", "+8613800138000", true},
		{"email lowercased", " Foo@Example.COM ", "email", "foo@example.com", true},
		{"empty", "   ", "", "", false},
		{"not a phone or email", "abcdef", "", "", false},
		{"broken email", "foo@", "", "", false},
		{"phone too short", "138001", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			typ, id, ok := normalize.Identifier(c.in)
			if ok != c.wantOK {
				t.Fatalf("Identifier(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			}
			if typ != c.wantType || id != c.wantID {
				t.Fatalf("Identifier(%q) = (%q, %q), want (%q, %q)", c.in, typ, id, c.wantType, c.wantID)
			}
		})
	}
}
