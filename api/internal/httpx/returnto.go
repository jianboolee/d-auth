package httpx

import (
	"net"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jianboolee/d-auth/internal/response"
)

// originOf 从完整 URL 提取规范 origin（scheme://host[:port]，host 转小写、
// 去默认端口）。非 http(s)、解析失败、无 host、带 userinfo 的一律返回 ""。
// path 与 query 被忽略——return_to 允许带路径（子系统回跳常携带原始页面地址），
// 校验只看 origin。userinfo 直接拒绝：`http://evil.com@localhost:5200` 这类
// 凭据混淆 URL 即便 origin 计算正确也不该放行。
func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	// 不直接用 u.Hostname()/u.Port()：`localhost:5200.evil.com` 这种非法端口会被
	// u.Port() 静默丢弃，u.Hostname() 给出 "localhost"，绕过白名单里的
	// "http://localhost:5200"。SplitHostPort 会拒绝非法端口，宁可整体判为非法。
	host, port := u.Host, ""
	if h, p, splitErr := net.SplitHostPort(u.Host); splitErr == nil {
		host, port = h, p
	} else if strings.HasPrefix(u.Host, "[") {
		// 无端口的 IPv6 字面量（如 http://[::1]）。
		if !strings.HasSuffix(u.Host, "]") {
			return ""
		}
	} else if strings.Contains(u.Host, ":") {
		// 含冒号但 SplitHostPort 失败：端口非法（非数字/超范围），整体拒绝。
		return ""
	}
	host = strings.ToLower(host)
	if host == "" {
		return ""
	}
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	origin := u.Scheme + "://" + host
	if port != "" {
		origin += ":" + port
	}
	return origin
}

// matchReturnTarget 判断 return_to 是否命中白名单。白名单条目同样按 origin
// 解析（多写的 path 会被忽略）。localhost 与 127.0.0.1 是两个不同 origin，需
// 分别登记——本地 cookie 是 host-only 的，不跨主机名传递。
func matchReturnTarget(returnTo string, allowed []string) bool {
	origin := originOf(returnTo)
	if origin == "" {
		return false
	}
	for _, a := range allowed {
		// 非法配置条目跳过而不是放行（fail-closed）。
		if o := originOf(a); o != "" && o == origin {
			return true
		}
	}
	return false
}

// ReturnTarget GET /v1/auth/return-target —— 登录回跳的只读校验端点。
// 登录页 / 路由守卫把 URL 上的 client 与 return_to 交给它，拿到「是否允许」
// 后才跳转：白名单判断留在服务端，前端不接触名单内容。
//
// client 参数当前不参与校验（白名单是环境级、不区分 client），保留它是为白名
// 单硬化到 per-client（auth_clients.redirect_uris，见 docs/sso-return-to.md）
// 时前端无需改动。
//
// 始终返回 200：「不允许」不是错误，调用方据此退回常规登录流程（账户中心）。
func (h *Handlers) ReturnTarget(c *gin.Context) {
	returnTo := strings.TrimSpace(c.Query("return_to"))
	if returnTo == "" || !matchReturnTarget(returnTo, h.Cfg.ReturnOrigins) {
		response.OK(c, gin.H{"allowed": false})
		return
	}
	response.OK(c, gin.H{"allowed": true, "url": returnTo})
}
