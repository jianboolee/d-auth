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

type UserInfo struct {
	OpenID  string
	UnionID string
	AppID   string
}

type Client interface {
	WebQRAuthorizeURL(state string) string
	WebCallback(ctx context.Context, code string) (UserInfo, error)
	MiniCode2Session(ctx context.Context, appID, appSecret, jsCode string) (UserInfo, error)
}

type Live struct {
	AppID       string
	AppSecret   string
	RedirectURL string
}

func (l *Live) WebQRAuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("appid", l.AppID)
	q.Set("redirect_uri", l.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "snsapi_login")
	q.Set("state", state)
	return "https://open.weixin.qq.com/connect/qrconnect?" + q.Encode() + "#wechat_redirect"
}

func (l *Live) WebCallback(ctx context.Context, code string) (UserInfo, error) {
	return exchange(ctx, l.AppID, l.AppSecret, code)
}

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

func exchange(ctx context.Context, appID, secret, code string) (UserInfo, error) {
	u := fmt.Sprintf("https://api.weixin.qq.com/sns/oauth2/access_token?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		url.QueryEscape(appID), url.QueryEscape(secret), url.QueryEscape(code))
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
	if parsed.OpenID == "" {
		return UserInfo{}, fmt.Errorf("wechat oauth: %s", string(body))
	}
	return UserInfo{OpenID: parsed.OpenID, UnionID: parsed.UnionID, AppID: appID}, nil
}

// Mock 是 dev / 测试用的假微信客户端。
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
// （`mock-union-<标识>`）、openid 按 appID 区分。这样：
//   - 同一个 `<标识>` 在不同 appID 下 → 同一个 unionid → 命中同一账号（模拟多小程序）
//   - 不同 `<标识>` → 不同 unionid → 不同账号
//
// 例：`union:alice-1` 在 appID=A 与 appID=B 都登录，两次进同一个账号。
type Mock struct {
	AppID string
}

func (m *Mock) WebQRAuthorizeURL(state string) string {
	return "/v1/auth/wechat/mock-scan?ticket=" + url.QueryEscape(state)
}

func (m *Mock) WebCallback(_ context.Context, code string) (UserInfo, error) {
	openID, unionID := mockIdentity(code)
	return UserInfo{OpenID: openID, UnionID: unionID, AppID: m.AppID}, nil
}

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
