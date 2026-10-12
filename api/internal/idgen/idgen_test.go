package idgen

import (
	"strings"
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

// 用户 ID 是 22 位小写字母数字串，全部字符都在字母表内。
func TestNewUserIDShape(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := NewUserID()
		if len(id) != UserIDLen {
			t.Fatalf("got length %d want %d: %q", len(id), UserIDLen, id)
		}
		for j := 0; j < len(id); j++ {
			if !strings.ContainsRune(userIDAlphabet, rune(id[j])) {
				t.Fatalf("id %q has illegal char %q", id, id[j])
			}
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

// 字符分布大致均匀：拒绝采样若不生效，前几个字符会明显偏多。
// 只做粗略上界校验（1e4 个样本、36 个字符，期望每个约 278 次），
// 用来抓住「取模偏置」这类系统性错误，不做严格统计检验。
func TestNewUserIDDistribution(t *testing.T) {
	const n = 10000
	counts := make(map[byte]int, len(userIDAlphabet))
	for i := 0; i < n; i++ {
		id := NewUserID()
		for j := 0; j < len(id); j++ {
			counts[id[j]]++
		}
	}
	expected := float64(n*UserIDLen) / float64(len(userIDAlphabet))
	for i := 0; i < len(userIDAlphabet); i++ {
		c := counts[userIDAlphabet[i]]
		if float64(c) < expected*0.9 || float64(c) > expected*1.1 {
			t.Fatalf("char %q appears %d times, expected ~%.0f (bias?)", userIDAlphabet[i], c, expected)
		}
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
		t.Fatalf("round trip mismatch: %s -> %s", s, back)
	}
}

// FormatUserID 做归一化：去掉两侧空白，输出裸串。
func TestFormatUserIDTrims(t *testing.T) {
	id := NewUserID()
	if got := FormatUserID("  " + id + "\n"); got != id {
		t.Fatalf("FormatUserID(%q) = %q, want %q", "  "+id+"\n", got, id)
	}
}

// uuid 字符串不再被接受：硬切到 22 位格式后，旧 uuid 一律解析失败。
// 这条把「不再兼容 uuid」钉住，避免以后有人以为还兼容。
func TestParseUserIDRejectsUUID(t *testing.T) {
	for _, s := range []string{
		uuid.NewString(),
		"00000000-0000-7000-8000-000000000000",
		"018f3c1a-7b2d-7c4e-8f01-1234567890ab",
	} {
		if _, err := ParseUserID(s); err == nil {
			t.Fatalf("ParseUserID(%q) should fail: uuid form is no longer accepted", s)
		}
	}
}

// 16 位那个更早的 xid 实验形态同样不再被接受。
func TestParseUserIDRejects16CharForm(t *testing.T) {
	for _, s := range []string{"00tld2hok0w6f2f5", strings.Repeat("0", 16), "abcdefghijklmnop"} {
		if _, err := ParseUserID(s); err == nil {
			t.Fatalf("ParseUserID(%q) should fail: the packed form is no longer accepted", s)
		}
	}
}

func TestParseUserIDRejectsInvalid(t *testing.T) {
	for _, s := range []string{
		"",
		"abc",
		strings.Repeat("a", UserIDLen-1),
		strings.Repeat("a", UserIDLen+1),
		"0123456789abcdefghijkA", // uppercase, exactly UserIDLen
		"0123456789abcdefghijk-", // symbol, exactly UserIDLen
		"0123456789abcdefghijk_", // underscore, exactly UserIDLen
	} {
		if _, err := ParseUserID(s); err == nil {
			t.Fatalf("ParseUserID(%q) should fail", s)
		}
	}
}

func TestParseUserIDAcceptsValid(t *testing.T) {
	for _, s := range []string{"abcdefghijklmnopqrstuv", "0123456789012345678901"} {
		if _, err := ParseUserID(s); err != nil {
			t.Fatalf("ParseUserID(%q) should succeed: %v", s, err)
		}
	}
}
