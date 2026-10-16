package dauth

import (
	"context"
)

// 验证码场景常量。send 类接口的 scene 必须落在这里，否则服务端按非法场景拒绝。
const (
	// SceneLogin 短信登录验证码。
	SceneLogin = "login"
	// SceneBindPhone 绑定/更换手机号验证码。
	SceneBindPhone = "bind_phone"
	// SceneDeleteAccount 注销前验证当前手机号。
	SceneDeleteAccount = "delete_account"

	// SceneRegister 邮箱注册验证码。
	SceneRegister = "register"
	// SceneResetPassword 邮箱重置密码验证码。
	SceneResetPassword = "reset_password"
	// SceneBindEmail 绑定邮箱验证码。
	SceneBindEmail = "bind_email"
)

// SendSMS 给指定手机号发验证码。scene 用上面的常量，登录场景传 SceneLogin。
func (c *Client) SendSMS(ctx context.Context, phone, scene string) error {
	return c.do(ctx, "POST", "/v1/auth/sms/send", nil, map[string]string{
		"phone": phone, "scene": scene,
	}, nil)
}

// SMSLogin 短信验证码登录。返回 access/refresh token 与当前用户。
//
// 注意：这是公开接口，不需要 API Key；且对新手机号是「找到或创建」——未注册
// 的号码会静默建号。
func (c *Client) SMSLogin(ctx context.Context, phone, code string) (*TokenResult, error) {
	var out TokenResult
	err := c.do(ctx, "POST", "/v1/auth/sms/login", nil, map[string]string{
		"phone": phone, "code": code,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SendEmail 给指定邮箱发验证码。scene 用上面的常量，注册场景传 SceneRegister。
func (c *Client) SendEmail(ctx context.Context, email, scene string) error {
	return c.do(ctx, "POST", "/v1/auth/email/send-code", nil, map[string]string{
		"email": email, "scene": scene,
	}, nil)
}

// EmailRegister 邮箱注册。注册成功即登录，返回 token。nickname 可留空。
func (c *Client) EmailRegister(ctx context.Context, email, code, password, nickname string) (*TokenResult, error) {
	var out TokenResult
	err := c.do(ctx, "POST", "/v1/auth/email/register", nil, map[string]string{
		"email": email, "code": code, "password": password, "nickname": nickname,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// PasswordLogin 密码登录。identifier 可传手机号或邮箱。
func (c *Client) PasswordLogin(ctx context.Context, identifier, password string) (*TokenResult, error) {
	var out TokenResult
	err := c.do(ctx, "POST", "/v1/auth/password/login", nil, map[string]string{
		"identifier": identifier, "password": password,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ForgotPassword 触发邮箱重置密码流程（发验证码）。无论邮箱是否存在都返回成功，
// 避免暴露注册状态。
func (c *Client) ForgotPassword(ctx context.Context, email string) error {
	return c.do(ctx, "POST", "/v1/auth/password/forgot", nil, map[string]string{
		"email": email,
	}, nil)
}

// ResetPassword 用邮箱验证码重置密码。
func (c *Client) ResetPassword(ctx context.Context, email, code, password string) error {
	return c.do(ctx, "POST", "/v1/auth/password/reset", nil, map[string]string{
		"email": email, "code": code, "password": password,
	}, nil)
}

// WeChatMini 微信小程序登录。clientKey 必须是 d-auth 后台登记过、且配了该小程序
// AppID/AppSecret 的 client_key；code 是 wx.login 拿到的 js_code。
//
// 子系统的微信小程序走这里登录：d-auth 用后台存的小程序凭证换 openid，会话随之
// 归属到该 client_key。
func (c *Client) WeChatMini(ctx context.Context, clientKey, code string) (*TokenResult, error) {
	var out TokenResult
	err := c.do(ctx, "POST", "/v1/auth/wechat/mini", nil, map[string]string{
		"client_key": clientKey, "code": code,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Refresh 用 refresh token 换新的 access token（refresh 会轮换）。
// token 模式下 access 过期时用它续期。
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*TokenResult, error) {
	var out TokenResult
	err := c.do(ctx, "POST", "/v1/auth/refresh", nil, map[string]string{
		"refresh_token": refreshToken,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Logout 吊销一次登录会话。带 refresh token 即可定位会话（即使 access 已过期）。
// 幂等：会话不存在或已吊销也返回成功。
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	return c.do(ctx, "POST", "/v1/auth/logout", nil, map[string]string{
		"refresh_token": refreshToken,
	}, nil)
}
