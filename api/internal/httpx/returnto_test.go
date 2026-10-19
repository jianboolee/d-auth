package httpx

import "testing"

func TestOriginOf(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain host", "http://localhost:5200", "http://localhost:5200"},
		{"trailing slash", "http://localhost:5200/", "http://localhost:5200"},
		{"path and query ignored", "https://note.x-rv.com/a/b?x=1#frag", "https://note.x-rv.com"},
		{"uppercase host normalized", "HTTP://LocalHost:5200/x", "http://localhost:5200"},
		{"default http port dropped", "http://localhost:80/x", "http://localhost"},
		{"default https port dropped", "https://auth.x-rv.com:443", "https://auth.x-rv.com"},
		{"non-default port kept", "https://auth.x-rv.com:8443", "https://auth.x-rv.com:8443"},
		{"surrounding spaces trimmed", "  http://localhost:5200  ", "http://localhost:5200"},
		{"https scheme distinct", "https://localhost:5200", "https://localhost:5200"},
		{"no scheme", "localhost:5200", ""},
		{"javascript scheme", "javascript:alert(1)", ""},
		{"data scheme", "data:text/html,<h1>x</h1>", ""},
		{"userinfo rejected", "http://evil.com@localhost:5200", ""},
		{"userinfo only", "http://user:pass@localhost:5200", ""},
		{"empty", "", ""},
		{"protocol relative", "//evil.com/x", ""},
		{"host missing", "http:///path", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := originOf(tc.raw); got != tc.want {
				t.Fatalf("originOf(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestMatchReturnTarget(t *testing.T) {
	allowed := []string{"http://localhost:5200", "https://note.x-rv.com", "http://127.0.0.1:5200"}

	cases := []struct {
		name     string
		returnTo string
		want     bool
	}{
		{"exact origin", "http://localhost:5200", true},
		{"with path", "http://localhost:5200/some/page", true},
		{"with query and hash", "http://localhost:5200/edit?id=1#x", true},
		{"trailing slash", "http://localhost:5200/", true},
		{"other allowlisted origin", "https://note.x-rv.com/", true},
		{"localhost vs 127.0.0.1 both registered", "http://127.0.0.1:5200", true},
		{"evil origin", "https://evil.example.com", false},
		{"port mismatch", "http://localhost:5201", false},
		{"scheme mismatch", "https://localhost:5200", false},
		{"prefix attack (same host, evil port in host part)", "http://localhost:5200.evil.com", false},
		{"userinfo attack", "http://evil.com@localhost:5200", false},
		{"protocol relative", "//localhost:5200", false},
		{"javascript", "javascript:alert(1)", false},
		{"empty", "", false},
		{"subdomain attack", "http://localhost.evil.com:5200", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchReturnTarget(tc.returnTo, allowed); got != tc.want {
				t.Fatalf("matchReturnTarget(%q) = %v, want %v", tc.returnTo, got, tc.want)
			}
		})
	}
}

func TestMatchReturnTargetFailClosed(t *testing.T) {
	// 空配置 / 全非法配置：一切拒绝。
	if matchReturnTarget("http://localhost:5200", nil) {
		t.Fatal("nil allowlist must reject everything")
	}
	if matchReturnTarget("http://localhost:5200", []string{"not a url", "//evil.com"}) {
		t.Fatal("invalid allowlist entries must not match anything")
	}
}
