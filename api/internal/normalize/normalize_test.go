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
