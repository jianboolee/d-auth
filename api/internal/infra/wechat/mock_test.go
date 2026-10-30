package wechat

import (
	"context"
	"strings"
	"testing"
)

// Mock 的身份派生（2026-10-31 修）。
//
// 修之前 Mock 恒返回 UnionID: "mock-union"，而 FindWechat 优先按 union_id 查 ——
// 于是**所有** mock 登录都命中第一条该 union_id 的身份，全平台共用同一个账号。
// 表现是：换新 code 也拿不到新用户、解绑微信后再登录还是"老用户"，
// 整个 dev 环境的账号体系是坏的。
//
// 现在默认不给 unionid（贴近"没申请微信开放平台时微信不返回 unionid"的真实情况），
// 走 appID:openID 路径；只有显式 `union:<标识>` 前缀才派生出稳定 unionid，
// 用于模拟"同一用户跨多端"。
func TestMockMiniIdentity(t *testing.T) {
	m := &Mock{AppID: "ignored"}

	t.Run("默认不给 unionid，每个 code 是独立的人", func(t *testing.T) {
		a, err := m.MiniCode2Session(context.Background(), "app-a", "", "code-1")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := m.MiniCode2Session(context.Background(), "app-a", "", "code-2")
		if a.UnionID != "" {
			t.Fatalf("默认不该有 unionid（无开放平台时微信不返回它），实际 %q", a.UnionID)
		}
		if a.OpenID == b.OpenID {
			t.Fatal("不同 code 必须给不同 openid，否则同一个人永远命中同一个账号")
		}
	})

	t.Run("union: 前缀派生稳定 unionid，跨 appID 命中同一人", func(t *testing.T) {
		a, _ := m.MiniCode2Session(context.Background(), "app-a", "", "union:alice-1")
		b, _ := m.MiniCode2Session(context.Background(), "app-b", "", "union:alice-1")
		if a.UnionID == "" || a.UnionID != b.UnionID {
			t.Fatalf("同一 union 标识在不同 appID 下应得到相同 unionid，实际 %q / %q", a.UnionID, b.UnionID)
		}
		if !strings.HasPrefix(a.UnionID, "mock-union-") {
			t.Fatalf("unionid 应带 mock-union- 前缀，实际 %q", a.UnionID)
		}
		// openid 仍按 code 派生（真实微信里 openid 也是 per-app 不同的）
		if a.OpenID == b.OpenID {
			t.Log("注：此 mock 的 openid 不含 appID，仅用于区分 code；真实微信 openid 是 per-app 的")
		}
	})

	t.Run("不同 union 标识是不同的人", func(t *testing.T) {
		a, _ := m.MiniCode2Session(context.Background(), "app-a", "", "union:alice-1")
		b, _ := m.MiniCode2Session(context.Background(), "app-a", "", "union:bob-1")
		if a.UnionID == b.UnionID {
			t.Fatalf("不同标识应得到不同 unionid，实际都是 %q", a.UnionID)
		}
	})

	t.Run("裸 union: 前缀不产生空标识的 unionid", func(t *testing.T) {
		got, _ := m.MiniCode2Session(context.Background(), "app-a", "", "union:")
		if got.UnionID != "" {
			t.Fatalf("union: 后面为空时不该派生 unionid，实际 %q", got.UnionID)
		}
	})
}

// 扫码登录（WebCallback）同一套规则。
func TestMockWebCallbackIdentity(t *testing.T) {
	m := &Mock{AppID: "web-app"}

	plain, err := m.WebCallback(context.Background(), "scan-1")
	if err != nil {
		t.Fatal(err)
	}
	if plain.UnionID != "" {
		t.Fatalf("默认不该有 unionid，实际 %q", plain.UnionID)
	}
	if plain.AppID != "web-app" {
		t.Fatalf("appID 应原样带回，实际 %q", plain.AppID)
	}

	linked, _ := m.WebCallback(context.Background(), "union:x-1")
	if linked.UnionID != "mock-union-x-1" {
		t.Fatalf("union 前缀应派生 mock-union-x-1，实际 %q", linked.UnionID)
	}
}
