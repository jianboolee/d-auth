package useragent

import "testing"

func TestDescribe(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want string
	}{
		{
			name: "chrome on macOS",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36",
			want: "Chrome 152 · macOS",
		},
		{
			// 外层是 ZCode（Electron），UA 里也含 Chrome，应识别为 ZCode
			name: "zcode desktop",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) ZCode/3.9.1 Chrome/146.0.7680.80 Electron/41.0.3 Safari/537.36",
			want: "ZCode 3 · macOS",
		},
		{
			name: "safari on macOS",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
			want: "Safari 17 · macOS",
		},
		{
			name: "edge on windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.2210.91",
			want: "Edge 120 · Windows",
		},
		{
			name: "wechat on iOS",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.42",
			want: "微信 8 · iOS",
		},
		{
			name: "firefox on linux",
			ua:   "Mozilla/5.0 (X11; Linux x86_64; rv:109.0) Gecko/20100101 Firefox/115.0",
			want: "Firefox 115 · Linux",
		},
		{
			name: "android chrome",
			ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			want: "Chrome 120 · Android",
		},
		{name: "empty", ua: "", want: "未知设备"},
		{name: "whitespace", ua: "   ", want: "未知设备"},
		{name: "unknown", ua: "curl/8.4.0", want: "未知设备"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Describe(c.ua); got != c.want {
				t.Fatalf("Describe(%q)\n got %q\nwant %q", c.ua, got, c.want)
			}
		})
	}
}

func TestMajorVersion(t *testing.T) {
	if got := majorVersion("152.0.7680.80"); got != "152" {
		t.Fatalf("got %q want 152", got)
	}
	if got := majorVersion("17"); got != "17" {
		t.Fatalf("got %q want 17", got)
	}
}
