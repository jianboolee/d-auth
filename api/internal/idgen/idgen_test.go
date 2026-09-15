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

// 16 位小写字母数字串不再被接受。它不是合法的 uuid，早期那版把 base36 串打包
// 进 uuid 字节的格式已经废弃——这条把新行为钉住，避免以后有人以为还兼容。
func TestParseUserIDRejects16CharForm(t *testing.T) {
	for _, s := range []string{"00tld2hok0w6f2f5", strings.Repeat("0", 16), "abcdefghijklmnop"} {
		if _, err := ParseUserID(s); err == nil {
			t.Fatalf("ParseUserID(%q) should fail: the packed form is no longer accepted", s)
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
