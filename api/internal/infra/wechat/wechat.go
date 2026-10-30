// Package wechat 微信登录客户端。
//
// 只覆盖**小程序登录**（jscode2session）。微信扫码登录（Web OAuth）已于
// 2026-10-31 删除：前端从未接通过、生产从未配置 web 应用凭据，
// 它带一整套 Mock/Live 双实现与 ticket 状态机却一天都没对外可用过。
//
// 凭据来源（2026-10-31 统一到数据库）：每个 `auth_clients` 行自带
// `wechat_app_id` / `wechat_app_secret_enc`，由调用方传进来 ——
// 这里不再持有任何全局配置。这样同一个部署可以同时服务多个小程序，
// 每个用自己那一套凭据；配了凭据就走真实微信，没配就降级到 Mock（见 app.go）。
package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// UserInfo 微信返回的身份信息。
//
// UnionID 可能为空：微信只在该小程序绑定了开放平台账号时才返回它。
// 调用方（Store.FindWechat）必须能处理空值 —— 那是**单小程序场景的常态**。
type UserInfo struct {
	OpenID  string
	UnionID string
	// AppID 是发起本次登录的小程序 appID（不是全局配置）：
	// openid 是「用户 + AppID」的组合标识，落库时要拼进 identifier。
	AppID string
}

// Client 微信小程序登录客户端。
//
// 只有一个方法：凭据由调用方逐次传入（来自 auth_clients 表），
// 所以实现是无状态的 —— 这也是「按 client 配置」能成立的前提。
type Client interface {
	MiniCode2Session(ctx context.Context, appID, appSecret, jsCode string) (UserInfo, error)
}

// Live 真实微信（调 api.weixin.qq.com）。
type Live struct{}

func (l *Live) MiniCode2Session(ctx context.Context, appID, appSecret, jsCode string) (UserInfo, error) {
	u := fmt.Sprintf("https://api.weixin.qq.com/sns/jscode2session?appid=%s&secret=%s&js_code=%s&grant_type=authorization_code",
		url.QueryEscape(appID), url.QueryEscape(appSecret), url.QueryEscape(jsCode))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return UserInfo{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return UserInfo{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		OpenID  string `json:"openid"`
		UnionID string `json:"unionid"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return UserInfo{}, err
	}
	if parsed.ErrCode != 0 || parsed.OpenID == "" {
		return UserInfo{}, fmt.Errorf("wechat mini: %s", string(body))
	}
	return UserInfo{OpenID: parsed.OpenID, UnionID: parsed.UnionID, AppID: appID}, nil
}

// Mock 是 dev / 测试 / 未配置凭据时的假微信客户端。
//
// ## UnionID 为什么默认是空（2026-10-31 修）
//
// 原来这里恒返回 `UnionID: "mock-union"`，造成两个后果：
//
//  1. **所有 mock 登录被当成同一个人**。`FindWechat` 优先按 union_id 查，
//     于是不管 jsCode 是什么、openid 是什么，都命中第一条 union_id='mock-union'
//     的身份 —— 永远返回同一个账号。表现是：换新 code 登录也拿不到新用户，
//     解绑微信后再登录还是"老用户"。
//  2. 与真实环境不一致。**没有微信开放平台账号时，jscode2session 根本不返回
//     unionid** —— 单小程序场景本来就只有 openid。mock 造一个假的 unionid，
//     等于让开发环境跑在一条生产上不存在的路径上。
//
// 现在默认不给 unionid：查找走 `appID:openID`，与"没申请开放平台"的真实现状一致，
// 也让每个 code 是独立的一个人。
//
// ## 需要模拟「同一用户跨多端」时
//
// jsCode 用 `union:<任意标识>` 前缀，Mock 就会派生出稳定 unionid
// （`mock-union-<标识>`）。这样同一个 `<标识>` 在不同 appID 下命中同一账号
// （模拟多小程序）；不同 `<标识>` 则是不同账号。
//
// 例：`union:alice-1` 在 appID=A 与 appID=B 都登录，两次进同一个账号。
type Mock struct{}

func (m *Mock) MiniCode2Session(_ context.Context, appID, _, jsCode string) (UserInfo, error) {
	openID, unionID := mockIdentity(jsCode)
	return UserInfo{OpenID: openID, UnionID: unionID, AppID: appID}, nil
}

// mockIdentity 由 code 派生出 (openid, unionid)。
//
// 默认 unionid 为空（贴近无开放平台的真实情况）；
// `union:<标识>` 前缀才给 unionid，用于模拟多端同人。
func mockIdentity(code string) (openID, unionID string) {
	const unionPrefix = "union:"
	if rest, ok := strings.CutPrefix(code, unionPrefix); ok && rest != "" {
		return "mock-open-" + code, "mock-union-" + rest
	}
	return "mock-open-" + code, ""
}

// Resolver 按「这个 client 配了什么凭据」决定用哪种实现（2026-10-31）。
//
// 为什么需要它：凭据从环境变量搬到 auth_clients 表之后，
// "用真实微信还是 Mock"就不再是**部署级**的选择，而是**每个 client 级**的：
// 配了 appID + secret 的应用走真实微信，没配的降级到 Mock（本地开发、测试环境）。
//
// 这样同一个进程可以同时服务"已配置的小程序"和"还没申请凭据的小程序"，
// 而不用为了本地调试把整个部署切成 Mock。
type Resolver struct {
	live Client
	mock Client
}

func NewResolver() *Resolver {
	return &Resolver{live: &Live{}, mock: &Mock{}}
}

// For 返回该凭据应使用的实现。
//
// 判断条件是**两个都非空**：只有 appID 没 secret（或反之）是配置错误，
// 这种情况走 Mock 更安全 —— 真实微信那边只会返回一个看不懂的 errocode。
func (r *Resolver) For(appID, appSecret string) Client {
	if appID != "" && appSecret != "" {
		return r.live
	}
	return r.mock
}

// 编译期断言：两个实现都满足接口。
var (
	_ Client = (*Live)(nil)
	_ Client = (*Mock)(nil)
)

// NewFixedResolver 用同一个实现服务所有 client。
//
// 给测试用：测试常需要一个可控的假客户端（比 Mock 更精确地模拟特定返回），
// 而 Resolver 的字段是私有的 —— 没有这个构造函数，测试就只能复制一份 Resolver。
func NewFixedResolver(c Client) *Resolver {
	return &Resolver{live: c, mock: c}
}
