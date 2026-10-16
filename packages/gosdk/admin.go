package dauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// ListUsers 分页列出用户（/v1/admin/users）。需要 users:read 密钥。
// q 走模糊搜索，status/role 过滤，sort 支持 created_at/last_login_at/nickname，
// order 为 asc|desc；created_from/created_to 是 RFC3339 半开区间。
func (c *Client) ListUsers(ctx context.Context, q UserListQuery) (*UserPage, error) {
	vals := url.Values{}
	putIf(vals, "q", q.Q)
	putIf(vals, "status", q.Status)
	putIf(vals, "role", q.Role)
	putIf(vals, "created_from", q.CreatedFrom)
	putIf(vals, "created_to", q.CreatedTo)
	putIf(vals, "sort", q.Sort)
	putIf(vals, "order", q.Order)
	if q.Page > 0 {
		vals.Set("page", strconv.Itoa(q.Page))
	}
	if q.Size > 0 {
		vals.Set("size", strconv.Itoa(q.Size))
	}
	var out struct {
		Items []User `json:"items"`
		Total int64  `json:"total"`
		Page  int    `json:"page"`
		Size  int    `json:"size"`
	}
	if err := c.do(ctx, "GET", "/v1/admin/users", vals, nil, &out); err != nil {
		return nil, err
	}
	return &UserPage{Items: out.Items, Total: out.Total, Page: out.Page, Size: out.Size}, nil
}

// UserListQuery 是 ListUsers 的过滤/分页参数，零值即不传。
type UserListQuery struct {
	Q           string
	Status      string
	Role        string
	CreatedFrom string
	CreatedTo   string
	Sort        string
	Order       string
	Page        int
	Size        int
}

// UserPage 是分页用户列表。
type UserPage struct {
	Items []User
	Total int64
	Page  int
	Size  int
}

// AdminUser 取用户详情与身份列表（/v1/admin/users/:id）：
// User 为明文，Identities 是绑定身份。需要 users:read 密钥。
func (c *Client) AdminUser(ctx context.Context, id string) (*User, []Identity, error) {
	var out struct {
		User       User       `json:"user"`
		Identities []Identity `json:"identities"`
	}
	if err := c.do(ctx, "GET", "/v1/admin/users/"+url.PathEscape(id), nil, nil, &out); err != nil {
		return nil, nil, err
	}
	return &out.User, out.Identities, nil
}

// UserSessions 分页取某用户的登录会话（/v1/admin/users/:id/sessions）。
// 后台是审计视角：已失效/已下线的会话同样返回，用 RevokedAt 区分。需要 users:read。
func (c *Client) UserSessions(ctx context.Context, id string, page, size int) ([]Session, int64, error) {
	vals := url.Values{}
	if page > 0 {
		vals.Set("page", strconv.Itoa(page))
	}
	if size > 0 {
		vals.Set("size", strconv.Itoa(size))
	}
	var out struct {
		Items []Session `json:"items"`
		Total int64     `json:"total"`
	}
	if err := c.do(ctx, "GET", "/v1/admin/users/"+url.PathEscape(id)+"/sessions", vals, nil, &out); err != nil {
		return nil, 0, err
	}
	return out.Items, out.Total, nil
}

// Stats 取仪表盘统计（/v1/admin/stats）。需要 users:read 密钥。
func (c *Client) Stats(ctx context.Context) (*UserStats, error) {
	var out UserStats
	if err := c.do(ctx, "GET", "/v1/admin/stats", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Roles 列出所有角色（/v1/admin/roles）。需要 users:read 密钥。
func (c *Client) Roles(ctx context.Context) ([]Role, error) {
	var out []Role
	if err := c.do(ctx, "GET", "/v1/admin/roles", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AuditLogs 分页读审计日志（/v1/admin/audit-logs）。需要 users:read 密钥。
// 注意：由 API Key 发起的操作只记 api_key_id、不记具体操作人。
func (c *Client) AuditLogs(ctx context.Context, q string, page, size int) ([]AuditLog, int64, error) {
	vals := url.Values{}
	putIf(vals, "q", q)
	if page > 0 {
		vals.Set("page", strconv.Itoa(page))
	}
	if size > 0 {
		vals.Set("size", strconv.Itoa(size))
	}
	var out struct {
		Items []AuditLog `json:"items"`
		Total int64      `json:"total"`
	}
	if err := c.do(ctx, "GET", "/v1/admin/audit-logs", vals, nil, &out); err != nil {
		return nil, 0, err
	}
	return out.Items, out.Total, nil
}

// CreateUser 后台建号（/v1/admin/users）。手机号走后台宽松校验（允许 120 开头的
// 虚拟号）。至少提供手机号或邮箱之一。需要 users:write 密钥。
func (c *Client) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	var out User
	if err := c.do(ctx, "POST", "/v1/admin/users", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateUserInput 是建号参数。
type CreateUserInput struct {
	Nickname string   `json:"nickname"`
	Phone    string   `json:"phone"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Roles    []string `json:"roles"`
}

// PatchUser 改用户的资料/状态/角色（/v1/admin/users/:id）。需要 users:write 密钥。
//
// 指针为 nil 的字段不改（零值也不会覆盖）。可以改 Nickname / Bio / Gender /
// Status / Roles —— 与自助端 PATCH /v1/me 同样的字段语义，便于业务端代客改资料。
// Status 置为 disabled 会撤销该用户全部会话。
func (c *Client) PatchUser(ctx context.Context, id string, in PatchUserInput) (*User, error) {
	var out User
	if err := c.do(ctx, "PATCH", "/v1/admin/users/"+url.PathEscape(id), nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PatchUserInput 是改用户的可选字段集。指针 nil = 不改；Roles nil = 不动角色，
// 传空切片则清空全部角色（服务端能区分 null 与 []）。
type PatchUserInput struct {
	Nickname *string  `json:"nickname,omitempty"`
	Bio      *string  `json:"bio,omitempty"`
	Gender   *int16   `json:"gender,omitempty"`
	Status   *string  `json:"status,omitempty"`
	Roles    []string `json:"roles"`
}

// SetPhone 直接改绑手机号（管理员操作，不校验验证码）。手机号唯一，
// 冲突返回 PHONE_TAKEN。需要 users:write 密钥。
func (c *Client) SetPhone(ctx context.Context, id, phone string) (*User, error) {
	var out User
	if err := c.do(ctx, "PATCH", "/v1/admin/users/"+url.PathEscape(id)+"/phone", nil,
		map[string]string{"phone": phone}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetEmail 直接改绑邮箱（管理员操作，不校验验证码）。邮箱唯一，
// 冲突返回 EMAIL_TAKEN。需要 users:write 密钥。
func (c *Client) SetEmail(ctx context.Context, id, email string) (*User, error) {
	var out User
	if err := c.do(ctx, "PATCH", "/v1/admin/users/"+url.PathEscape(id)+"/email", nil,
		map[string]string{"email": email}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AdminResetPassword 重置用户密码（管理员操作）。成功后撤销该用户全部会话。
// 需要 users:write 密钥。
func (c *Client) AdminResetPassword(ctx context.Context, id, password string) error {
	return c.do(ctx, "POST", "/v1/admin/users/"+url.PathEscape(id)+"/password", nil,
		map[string]string{"password": password}, nil)
}

// AdminSignAvatar 为指定用户签发头像直传凭证（/v1/admin/users/:id/avatar/sign）。
// 需要 users:write 密钥。
func (c *Client) AdminSignAvatar(ctx context.Context, id, contentType string) (*AvatarUpload, error) {
	var out AvatarUpload
	if err := c.do(ctx, "POST", "/v1/admin/users/"+url.PathEscape(id)+"/avatar/sign", nil,
		map[string]string{"content_type": contentType}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AdminUploadAvatar 一步换头像：签发凭证 → 把字节 PUT 到服务端中转地址 → 返回新头像地址。
// 与自助端不同，这里没有 confirm 步骤。需要 users:write 密钥。
//
// 走的是服务端中转（非 OSS 直传），所以 PUT 也要带同一把 API Key。
func (c *Client) AdminUploadAvatar(ctx context.Context, id string, fileName string, data []byte) (string, error) {
	contentType := detectImageContentType(fileName, data)
	signed, err := c.AdminSignAvatar(ctx, id, contentType)
	if err != nil {
		return "", err
	}
	if err := c.putAvatar(ctx, signed.UploadURL, contentType, data); err != nil {
		return "", err
	}
	return signed.PublicURL, nil
}

// putAvatar 把头像字节 PUT 到中转地址。upload_url 可能是同源相对路径，
// 也可能是绝对 URL；带 API Key 以便同源路径通过鉴权。
func (c *Client) putAvatar(ctx context.Context, uploadURL, contentType string, data []byte) error {
	target := uploadURL
	if !isAbsoluteURL(uploadURL) {
		target = c.baseURL + uploadURL
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", target, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dauth: 构造头像上传请求: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dauth: 上传头像: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		var env envelope
		_ = json.Unmarshal(raw, &env)
		code := env.Code
		if code == "" {
			code = "HTTP_ERROR"
		}
		return &APIError{Code: code, Message: env.Message, Status: res.StatusCode}
	}
	return nil
}

// DeleteUser 注销用户（墓碑式删除，不可逆）。confirm 必须是「确认注销」。
// 限超级管理员，API Key 调用会被拒绝（403）。仅当调用方持有超管会话时可用。
func (c *Client) DeleteUser(ctx context.Context, id, confirm string) error {
	return c.do(ctx, "DELETE", "/v1/admin/users/"+url.PathEscape(id), nil,
		map[string]string{"confirm": confirm}, nil)
}

func putIf(v url.Values, key, val string) {
	if val != "" {
		v.Set(key, val)
	}
}
