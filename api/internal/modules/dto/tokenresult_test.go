package dto_test

import (
	"encoding/json"
	"testing"

	"github.com/jianboolee/d-auth/internal/modules/dto"
)

// is_new_user 是给业务方判断「要不要引导完善资料」用的，必须出现在登录响应的
// 顶层（与 access_token 平级），而不是塞进 user 对象。
func TestTokenResultExposesIsNewUserAtTopLevel(t *testing.T) {
	b, err := json.Marshal(dto.TokenResult{AccessToken: "a", IsNewUser: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := m["is_new_user"]
	if !ok {
		t.Fatalf("token result is missing top-level is_new_user: %s", b)
	}
	if got != true {
		t.Fatalf("is_new_user = %v, want true", got)
	}
	if _, nested := m["user"].(map[string]any)["is_new_user"]; nested {
		t.Fatalf("is_new_user must not be nested under user: %s", b)
	}
}

// false 也必须出现在 JSON 里（不能被 omitempty 吃掉）：调用方按固定字段
// 读取，缺字段与 false 两种写法会让前端多写一层兜底。
func TestTokenResultAlwaysEmitsIsNewUser(t *testing.T) {
	b, err := json.Marshal(dto.TokenResult{AccessToken: "a"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := m["is_new_user"]
	if !ok {
		t.Fatalf("is_new_user must always be present, got %s", b)
	}
	if got != false {
		t.Fatalf("is_new_user = %v, want false", got)
	}
}