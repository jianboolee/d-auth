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

// 用户 ID 现在与其它实体一致：标准 uuid v7。
func TestNewUserIDIsUUIDv7(t *testing.T) {
	for i := 0; i < 100; i++ {
		raw := NewUserID()
		if raw.Version() != 7 {
			t.Fatalf("got version %d want 7", raw.Version())
		}
		got := FormatUserID(raw)
		if _, err := uuid.Parse(got); err != nil {
			t.Fatalf("FormatUserID returned %q, not a parseable uuid: %v", got, err)
		}
		if got != raw.String() {
			t.Fatalf("FormatUserID should equal the uuid rendering: %q != %q", got, raw.String())
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

// uuid v7 的字符串序与生成顺序一致（前 48 位是毫秒时间戳）。
func TestNewUserIDTimeOrdered(t *testing.T) {
	prev := FormatUserID(NewUserID())
	for i := 0; i < 1000; i++ {
		cur := FormatUserID(NewUserID())
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

// 历史的 16 位打包串必须仍然能解析出来，否则老链接与老 JWT 的 sub 会失效。
// 库里存量行就存着那种字节，这条守住「只读兼容」。
func TestParseUserIDAcceptsLegacyPackedString(t *testing.T) {
	// "00tld2hok0w6f2f5" 是 dev 库里一条真实存量用户的对外旧形式，
	// 其字节渲染为 30304d45-3d32-4148-4430-50363f323f35。
	const legacyDisplay = "00tld2hok0w6f2f5"
	const storedBytes = "30304d45-3d32-4148-4430-50363f323f35"

	u, err := ParseUserID(legacyDisplay)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", legacyDisplay, err)
	}
	if got := u.String(); got != storedBytes {
		t.Fatalf("legacy display decoded to %s, want %s", got, storedBytes)
	}

	// 存量值现在按 uuid 渲染对外输出——即库里看到的那串，两者从此一致。
	if got := FormatUserID(u); got != storedBytes {
		t.Fatalf("FormatUserID(legacy) = %s, want the stored uuid rendering %s", got, storedBytes)
	}

	// 同一个 ID 的两种写法应解析到同一个 uuid。
	fromUUID, err := ParseUserID(storedBytes)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", storedBytes, err)
	}
	if fromUUID != u {
		t.Fatalf("two spellings of one id parsed differently: %s != %s", fromUUID, u)
	}
}

// 16 位串仍然会被当作打包形式解析（这是兼容路径的代价，不是缺陷），
// 所以任意 16 位小写字母数字串都会得到一个 uuid 而不是报错。
func TestParseUserIDTreats16CharsAsPacked(t *testing.T) {
	u, err := ParseUserID(strings.Repeat("0", 16))
	if err != nil {
		t.Fatalf("16-char input should parse as a legacy packed id: %v", err)
	}
	if u.String() != "30303030-3030-3030-3030-303030303030" {
		t.Fatalf("unexpected decode: %s", u)
	}
}

func TestLegacyUUIDRoundTrip(t *testing.T) {
	legacy := New()
	want := legacy.String()
	if got := FormatUserID(legacy); got != want {
		t.Fatalf("FormatUserID = %q, want %q", got, want)
	}
	back, err := ParseUserID(want)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", want, err)
	}
	if back != legacy {
		t.Fatalf("round trip mismatch: %x != %x", back, legacy)
	}
}

func TestParseUserIDRejectsInvalid(t *testing.T) {
	for _, s := range []string{"", "abc", "0123456789ABCDEf", "0123456789abcde-", "0123456789abcdefg"} {
		if _, err := ParseUserID(s); err == nil {
			t.Fatalf("ParseUserID(%q) should fail", s)
		}
	}
}
