package idgen

import (
	"regexp"
	"testing"

	"github.com/google/uuid"
)

func TestNewIsV7(t *testing.T) {
	id := New()
	if id.Version() != 7 {
		t.Fatalf("got version %d want 7", id.Version())
	}
	if id == uuid.Nil {
		t.Fatal("nil uuid")
	}
}

var userIDPattern = regexp.MustCompile(`^[0-9a-z]{16}$`)

func TestNewUserIDIs16LowerAlnum(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := FormatUserID(NewUserID())
		if !userIDPattern.MatchString(got) {
			t.Fatalf("user id %q is not 16 chars of [0-9a-z]", got)
		}
	}
}

func TestNewUserIDUnique(t *testing.T) {
	seen := make(map[string]struct{}, 10000)
	for i := 0; i < 10000; i++ {
		s := FormatUserID(NewUserID())
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate user id %q at %d", s, i)
		}
		seen[s] = struct{}{}
	}
}

func TestNewUserIDTimeOrdered(t *testing.T) {
	prev := FormatUserID(NewUserID())
	for i := 0; i < 1000; i++ {
		cur := FormatUserID(NewUserID())
		// 字典序与生成顺序一致：前缀是毫秒时间戳的 base36。
		if cur < prev {
			t.Fatalf("id %q out of order after %q", cur, prev)
		}
		prev = cur
	}
}

func TestFormatParseUserIDRoundTrip(t *testing.T) {
	raw := NewUserID()
	s := FormatUserID(raw)
	back, err := ParseUserID(s)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", s, err)
	}
	if back != raw {
		t.Fatalf("round trip mismatch: %s -> %x -> %x", s, raw, back)
	}
}

// 历史 uuid v7 用户不应被误判为打包格式：FormatUserID 必须原样返回 UUID 字符串，
// ParseUserID 也要能把它解析回来。
func TestLegacyUUIDStaysUUID(t *testing.T) {
	legacy := New()
	want := legacy.String()
	if got := FormatUserID(legacy); got != want {
		t.Fatalf("legacy id formatted to %q, want %q", got, want)
	}
	back, err := ParseUserID(want)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", want, err)
	}
	if back != legacy {
		t.Fatalf("legacy round trip mismatch: %x != %x", back, legacy)
	}
}

// 区分打包串与真实 UUID 的依据是第 7 个字节：打包串映射到 0x30..0x53，
// 而任何标准 UUID（v4/v7 等）的第 8 个字节带 variant 位，恒为 0x80..0xbf。
// 这个测试守住该前提，避免以后改动字符基址时把真实 UUID 误判成打包串。
func TestStandardUUIDIsNeverPacked(t *testing.T) {
	for i := 0; i < 1000; i++ {
		v4 := uuid.New()
		if got := FormatUserID(v4); got != v4.String() {
			t.Fatalf("v4 %s was misformatted to %q", v4, got)
		}
		v7 := New()
		if got := FormatUserID(v7); got != v7.String() {
			t.Fatalf("v7 %s was misformatted to %q", v7, got)
		}
	}
}

func TestParseUserIDRejectsInvalid(t *testing.T) {
	for _, s := range []string{"", "abc", "0123456789ABCDEf", "0123456789abcde-", "0123456789abcdefg"} {
		if _, err := ParseUserID(s); err == nil {
			t.Fatalf("ParseUserID(%q) should fail", s)
		}
	}
}
