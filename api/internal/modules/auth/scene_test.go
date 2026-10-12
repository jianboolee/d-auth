package auth

import "testing"

// scene 必须白名单化：它既进验证码 key 也决定短信模板，若不限制，
// 攻击者可以对同一号码不断更换 scene 拿到新配额，而未知 scene 仍会发出真实短信。
func TestSceneAllowed(t *testing.T) {
	cases := []struct {
		name     string
		allowed  map[string]struct{}
		scene    string
		fallback string
		want     string
		wantOK   bool
	}{
		{"sms empty defaults to login", smsScenes, "", "login", "login", true},
		{"sms login", smsScenes, "login", "login", "login", true},
		{"sms bind_phone", smsScenes, "bind_phone", "login", "bind_phone", true},
		{"sms delete_account", smsScenes, "delete_account", "login", "delete_account", true},
		// 关键回归：改绑用的是 bind_phone，change_phone 已无任何 Verify，必须拒绝
		{"sms change_phone rejected", smsScenes, "change_phone", "login", "", false},
		// 攻击者为绕过单号上限而自造的 scene 必须被拒
		{"sms arbitrary scene rejected", smsScenes, "x1", "login", "", false},
		{"sms register is email-only", smsScenes, "register", "login", "", false},
		{"sms reset_password not enabled yet", smsScenes, "reset_password", "login", "", false},

		{"email empty defaults to register", emailScenes, "", "register", "register", true},
		{"email register", emailScenes, "register", "register", "register", true},
		{"email reset_password", emailScenes, "reset_password", "register", "reset_password", true},
		{"email bind_email", emailScenes, "bind_email", "register", "bind_email", true},
		{"email login is sms-only", emailScenes, "login", "register", "", false},
		{"email arbitrary scene rejected", emailScenes, "whatever", "register", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := sceneAllowed(c.allowed, c.scene, c.fallback)
			if ok != c.wantOK {
				t.Fatalf("sceneAllowed(%q) ok = %v, want %v", c.scene, ok, c.wantOK)
			}
			if got != c.want {
				t.Fatalf("sceneAllowed(%q) = %q, want %q", c.scene, got, c.want)
			}
		})
	}
}

// 白名单必须与代码里实际 Verify 的 scene 一致，否则正常流程会被自己的白名单拒掉。
func TestSceneAllowlistMatchesVerifiedScenes(t *testing.T) {
	// 这些是代码里真实存在的 Verify 调用点，改动它们时这个测试会提醒同步白名单。
	for _, s := range []string{"login", "bind_phone", "delete_account"} {
		if _, ok := smsScenes[s]; !ok {
			t.Fatalf("sms scene %q is verified in code but missing from smsScenes", s)
		}
	}
	for _, s := range []string{"register", "reset_password", "bind_email"} {
		if _, ok := emailScenes[s]; !ok {
			t.Fatalf("email scene %q is verified in code but missing from emailScenes", s)
		}
	}
}
