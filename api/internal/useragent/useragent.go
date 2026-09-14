// Package useragent 把 User-Agent 归纳成便于展示的简短描述，
// 避免把整串 UA 直接暴露在界面上。
package useragent

import (
	"regexp"
	"strings"
)

// 顺序即优先级：外层应用（微信、Electron 桌面端）的 UA 里同时带着 Chrome 标识，
// 必须先匹配，否则会被误判为 Chrome。
var browsers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"微信", regexp.MustCompile(`MicroMessenger/([\d.]+)`)},
	{"ZCode", regexp.MustCompile(`ZCode/([\d.]+)`)},
	{"Electron", regexp.MustCompile(`Electron/([\d.]+)`)},
	{"Edge", regexp.MustCompile(`Edg(?:e|A|iOS)?/([\d.]+)`)},
	{"Opera", regexp.MustCompile(`OPR/([\d.]+)`)},
	{"Firefox", regexp.MustCompile(`Firefox/([\d.]+)`)},
	{"Chrome", regexp.MustCompile(`Chrome/([\d.]+)`)},
	{"Safari", regexp.MustCompile(`Version/([\d.]+) Safari/`)},
}

var systems = []struct {
	name string
	re   *regexp.Regexp
}{
	{"Windows", regexp.MustCompile(`Windows`)},
	{"iOS", regexp.MustCompile(`iPhone|iPad|iPod`)},
	{"Android", regexp.MustCompile(`Android`)},
	{"macOS", regexp.MustCompile(`Macintosh|Mac OS X`)},
	{"Linux", regexp.MustCompile(`Linux|X11`)},
}

// Describe 返回形如 "Chrome 152 · macOS" 的描述。
// 空 UA 或无法识别时返回「未知设备」。
func Describe(ua string) string {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "未知设备"
	}
	parts := make([]string, 0, 2)
	if b := matchBrowser(ua); b != "" {
		parts = append(parts, b)
	}
	if s := matchSystem(ua); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "未知设备"
	}
	return strings.Join(parts, " · ")
}

func matchBrowser(ua string) string {
	for _, b := range browsers {
		if m := b.re.FindStringSubmatch(ua); m != nil {
			return b.name + " " + majorVersion(m[1])
		}
	}
	return ""
}

func matchSystem(ua string) string {
	for _, s := range systems {
		if s.re.MatchString(ua) {
			return s.name
		}
	}
	return ""
}

// majorVersion 只保留主版本号，152.0.7680.80 -> 152。
func majorVersion(version string) string {
	if i := strings.IndexByte(version, '.'); i >= 0 {
		return version[:i]
	}
	return version
}
