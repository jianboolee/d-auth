package dauth

import (
	"context"
	"net/url"
	"strings"
)

// UserProfile 取单个用户的脱敏资料（/v1/users/:id）。需要 users:read 密钥。
// 手机号/邮箱已掩码，角色折叠成 is_admin，可安全透给前端。
func (c *Client) UserProfile(ctx context.Context, id string) (*Profile, error) {
	var out Profile
	if err := c.do(ctx, "GET", "/v1/users/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Users 批量取脱敏资料（按 ids）或精确匹配（按 phone / email）。三选一。
//
// 批量时查不到的 id 会被跳过；匹配不上返回空切片而不是错误，便于「这个手机号
// 注册过没有」这类判断。ids 最多 100 个。
func (c *Client) Users(ctx context.Context, ids []string, phone, email string) ([]Profile, error) {
	q := url.Values{}
	if len(ids) > 0 {
		q.Set("ids", strings.Join(ids, ","))
	}
	if phone != "" {
		q.Set("phone", phone)
	}
	if email != "" {
		q.Set("email", email)
	}
	var out struct {
		Items []Profile `json:"items"`
	}
	if err := c.do(ctx, "GET", "/v1/users", q, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// InternalUser 取单个用户的明文资料（/v1/internal/users/:id）：手机号/邮箱为
// 明文，Roles 是完整角色列表。需要 users:read 密钥。
//
// 这是「查当前登录用户」在服务端侧的标准取法：登录拿到 user.ID 后按 id 查。
// 明文资料不要原样吐给前端。
func (c *Client) InternalUser(ctx context.Context, id string) (*User, error) {
	var out User
	if err := c.do(ctx, "GET", "/v1/internal/users/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// InternalLookup 按手机号或邮箱精确匹配明文资料（/v1/internal/users?phone=|email=）。
// 与 InternalUser 一样需要 users:read 密钥。两者只能给其一。
func (c *Client) InternalLookup(ctx context.Context, phone, email string) (*User, error) {
	q := url.Values{}
	if phone != "" {
		q.Set("phone", phone)
	}
	if email != "" {
		q.Set("email", email)
	}
	var out User
	if err := c.do(ctx, "GET", "/v1/internal/users", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
