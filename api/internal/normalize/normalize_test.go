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

// 后台建号／改绑用的宽松校验：真实号码的结果与 Phone 完全一致，
// 另外放行 120 开头的虚拟号，且虚拟号按原值保留（不套 +86）。
func TestAdminPhone(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"cn mobile matches Phone", "13800138000", "+8613800138000", true},
		{"cn mobile with plus matches Phone", "+8613800138000", "+8613800138000", true},
		{"virtual 120 kept as-is", "12012345678", "12012345678", true},
		{"virtual with separators", "120 1234-5678", "12012345678", true},
		{"other non-real range", "10012345678", "10012345678", true},
		{"shortest accepted", "120123", "120123", true},
		{"below the digit floor", "12012", "", false},
		{"too long", "1234567890123456", "", false},
		{"letters rejected", "120abcd5678", "", false},
		{"plus too short", "+1234", "", false},
		{"empty", "  ", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := normalize.AdminPhone(c.in)
			if ok != c.wantOK {
				t.Fatalf("AdminPhone(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			}
			if got != c.want {
				t.Fatalf("AdminPhone(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// 放宽只属于后台：面向用户的 Phone 必须继续拒绝虚拟号，
// 否则虚拟号会变成能收码登录的凭据。
func TestPhoneStillRejectsVirtual(t *testing.T) {
	for _, in := range []string{"12012345678", "10012345678"} {
		if got, ok := normalize.Phone(in); ok {
			t.Errorf("Phone(%q) = %q, want rejected", in, got)
		}
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
