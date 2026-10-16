package dauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options 是构造 Client 的参数。
type Options struct {
	// BaseURL 是账户中心 origin，例如 https://auth.x-rv.com 或 http://host:8680。
	// 不要带 /v1，本包会自己拼。
	BaseURL string
	// APIKey 是控制台创建的 sk-... 密钥（scope 含 users:read / users:write）。
	// 只有「服务端查/改用户」需要它；登录等公开接口不需要。
	APIKey string
	// HTTPClient 可选。不传就用一个 10s 超时的默认客户端。
	HTTPClient *http.Client
}

// Client 是 d-auth 客户端。零值不可用，请用 New 构造。
// 并发安全：可被多个 goroutine 共用。
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New 构造客户端。BaseURL 必填；APIKey 可在只调公开接口（登录）时留空。
func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL: normalizeBaseURL(opts.BaseURL),
		apiKey:  strings.TrimSpace(opts.APIKey),
		http:    hc,
	}
}

// normalizeBaseURL 去掉尾部斜杠与可能被误带的 /v1 后缀，与 TS SDK 行为一致。
func normalizeBaseURL(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	s = strings.TrimSuffix(s, "/v1")
	return s
}

// APIError 是服务端返回的业务错误。用 errors.As 取值：
//
//	var apiErr *dauth.APIError
//	if errors.As(err, &apiErr) && apiErr.Code == "PHONE_TAKEN" { ... }
type APIError struct {
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("dauth: HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

// envelope 是 d-auth 的统一响应体。
type envelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// do 发一次请求并把 data 解码进 out。out 可为 nil（只关心是否成功）。
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	if c.baseURL == "" {
		return errors.New("dauth: BaseURL 未设置")
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("dauth: 序列化请求体: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("dauth: 构造请求: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("dauth: 请求 %s %s: %w", method, path, err)
	}
	defer res.Body.Close()

	// 限制读取上限，避免异常响应把内存打爆。
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("dauth: 读取响应: %w", err)
	}

	var env envelope
	if len(raw) > 0 {
		// 解析失败不致命：下面按状态码决定如何报错。
		_ = json.Unmarshal(raw, &env)
	}

	if res.StatusCode >= 400 {
		code := env.Code
		if code == "" {
			code = "HTTP_ERROR"
		}
		msg := env.Message
		if msg == "" {
			msg = res.Status
		}
		return &APIError{Code: code, Message: msg, Status: res.StatusCode}
	}

	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("dauth: 解析响应 data: %w", err)
		}
	}
	return nil
}
