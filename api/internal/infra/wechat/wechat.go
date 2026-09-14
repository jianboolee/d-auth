package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type Mock struct {
	AppID string
}

func (m *Mock) WebQRAuthorizeURL(state string) string {
	return "/v1/auth/wechat/mock-scan?ticket=" + url.QueryEscape(state)
}

func (m *Mock) WebCallback(_ context.Context, code string) (UserInfo, error) {
	return UserInfo{OpenID: "mock-open-" + code, UnionID: "mock-union", AppID: m.AppID}, nil
}

func (m *Mock) MiniCode2Session(_ context.Context, appID, _, jsCode string) (UserInfo, error) {
	return UserInfo{OpenID: "mini-open-" + jsCode, UnionID: "mock-union", AppID: appID}, nil
}
