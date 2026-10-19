package dauth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenClaims 是 d-auth access token（RS256 JWT）里子系统关心的声明。
// 字段与服务端 api/internal/infra/tokens.Claims 一一对应。
type TokenClaims struct {
	Subject   string    // sub：用户 ID（22 位小写字母数字串）
	Issuer    string    // iss
	Roles     []string  // roles：角色码列表
	ClientID  string    // cid：应用 UUID（登录时带 X-DAuth-Client 才有）
	JTI       string    // jti：token 唯一 ID
	IssuedAt  time.Time // iat
	ExpiresAt time.Time // exp
}

// VerifierOptions 是构造 Verifier 的参数。
type VerifierOptions struct {
	// BaseURL 是 d-auth origin，例如 https://auth.x-rv.com。不要带 /v1。
	BaseURL string
	// Issuer 非空时校验 token 的 iss。建议设置，值与 d-auth 的
	// DAUTH_JWT_ISSUER 一致（通常就是它的对外地址）。
	Issuer string
	// HTTPClient 可选。不传就用一个 10s 超时的默认客户端（仅用于拉 JWKS）。
	HTTPClient *http.Client
	// JWKSRefreshTTL 是公钥缓存的刷新节流：kid 未命中时距上次拉取至少
	// 间隔这么久才会再打 d-auth，防止伪造 kid 的请求把认证服务打挂。
	// 默认 5 分钟（与服务端 jwks.json 的 Cache-Control max-age 一致）。
	JWKSRefreshTTL time.Duration
}

// 判定类错误。中间件里典型用法：
//
//	claims, err := v.VerifyToken(r.Context(), raw)
//	switch {
//	case errors.Is(err, dauth.ErrInvalidToken): // 401：签名/时效/签发方不对
//	case errors.Is(err, dauth.ErrKeySourceUnavailable): // 503：暂时取不到公钥
//	}
var (
	// ErrInvalidToken 表示 token 本身不合法：格式错误、算法降级、验签失败、
	// 过期、iss 不匹配等。属于客户端错误，返回 401。
	ErrInvalidToken = errors.New("dauth: invalid token")
	// ErrKeySourceUnavailable 表示拉取 JWKS 失败且本地没有匹配的公钥。
	// 暂时性故障，建议返回 503 而不是把用户踢下线。
	ErrKeySourceUnavailable = errors.New("dauth: jwks unavailable")
)

// Verifier 用 d-auth 公开的 JWKS 验签 RS256 access token，
// 并发安全，可被多个 goroutine 共用。零值不可用，请用 NewVerifier 构造。
//
// 子系统不再需要 DAUTH_JWT_SECRET：公钥从 BaseURL + /v1/.well-known/jwks.json
// 自动拉取并按 kid 缓存；d-auth 换密钥后旧 kid 未命中会触发重拉。
type Verifier struct {
	baseURL    string
	issuer     string
	http       *http.Client
	refreshTTL time.Duration

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey // kid -> RSA 公钥
	lastFetch time.Time
}

// NewVerifier 构造验签器。BaseURL 必填。
func NewVerifier(opts VerifierOptions) *Verifier {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	ttl := opts.JWKSRefreshTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Verifier{
		baseURL:    normalizeBaseURL(opts.BaseURL),
		issuer:     strings.TrimSpace(opts.Issuer),
		http:       hc,
		refreshTTL: ttl,
		keys:       map[string]*rsa.PublicKey{},
	}
}

// VerifyToken 校验一个 Bearer access token（不含 "Bearer " 前缀），
// 成功返回其声明。raw 为空、算法不是 RS256、验签失败、过期或 iss 不匹配
// 都会得到包装了 ErrInvalidToken 的错误。
func (v *Verifier) VerifyToken(ctx context.Context, raw string) (*TokenClaims, error) {
	if v.baseURL == "" {
		return nil, errors.New("dauth: BaseURL 未设置")
	}
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "Bearer "))
	headerB64, payloadB64, sigB64, ok := splitJWT(raw)
	if !ok {
		return nil, fmt.Errorf("%w: 不是三段式 JWT", ErrInvalidToken)
	}
	header, err := decodeJSONSegment[jwtHeader](headerB64)
	if err != nil {
		return nil, fmt.Errorf("%w: header: %v", ErrInvalidToken, err)
	}
	// 强制 RS256：拒绝 none / HS256 等降级攻击。
	if header.Alg != AlgRS256 {
		return nil, fmt.Errorf("%w: alg=%q，只接受 %s", ErrInvalidToken, header.Alg, AlgRS256)
	}
	if header.Kid == "" {
		return nil, fmt.Errorf("%w: 缺少 kid", ErrInvalidToken)
	}
	payload, err := decodeJSONSegment[jwtPayload](payloadB64)
	if err != nil {
		return nil, fmt.Errorf("%w: payload: %v", ErrInvalidToken, err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("%w: 签名段非法 base64url", ErrInvalidToken)
	}

	pub, err := v.publicKey(ctx, header.Kid)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(headerB64 + "." + payloadB64))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return nil, fmt.Errorf("%w: RS256 验签失败", ErrInvalidToken)
	}

	now := time.Now()
	const leeway = 30 * time.Second
	if payload.Exp == 0 {
		return nil, fmt.Errorf("%w: 缺少 exp", ErrInvalidToken)
	}
	if now.After(time.Unix(payload.Exp, 0).Add(leeway)) {
		return nil, fmt.Errorf("%w: token 已过期", ErrInvalidToken)
	}
	if payload.Iat != 0 && now.Add(leeway).Before(time.Unix(payload.Iat, 0)) {
		return nil, fmt.Errorf("%w: iat 在未来", ErrInvalidToken)
	}
	if v.issuer != "" && payload.Iss != v.issuer {
		return nil, fmt.Errorf("%w: iss=%q，期望 %q", ErrInvalidToken, payload.Iss, v.issuer)
	}
	return payload.toClaims(), nil
}

// AlgRS256 是本包唯一接受的签名算法。
const AlgRS256 = "RS256"

// DefaultAccessCookieName 是承载 access JWT 的 cookie 默认名（与服务端
// DAUTH_COOKIE_NAME 一致）。子系统做 whoami 时读它即可。
const DefaultAccessCookieName = "dauth_access"

// VerifyRequest 从请求里取出 access token 并验签：优先 Authorization: Bearer，
// 其次读 cookie（默认 dauth_access）。命中其一即返回声明。
//
// 这是子系统「判断前端是否已登录」的推荐入口——cookie 是 HttpOnly 的，
// 前端 JS 读不到，必须由后端验签后通过 whoami 之类的接口把结果透给前端。
// cookie 未携带（未登录）时返回包装了 ErrInvalidToken 的错误。
func (v *Verifier) VerifyRequest(ctx context.Context, r *http.Request, cookieName string) (*TokenClaims, error) {
	if h := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(h, "Bearer ") {
		return v.VerifyToken(ctx, h)
	}
	if cookieName == "" {
		cookieName = DefaultAccessCookieName
	}
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return v.VerifyToken(ctx, c.Value)
	}
	return nil, fmt.Errorf("%w: 请求中既无 Bearer 也无 cookie %q", ErrInvalidToken, cookieName)
}

// publicKey 按 kid 取公钥：缓存未命中则（节流地）重拉 JWKS 再查一次。
// 语义约定：
//   - 节流窗口内未命中 → ErrInvalidToken（列表刚拉过就是最新的，没有这个 kid
//     多半是伪造 token；密钥轮换后子系统最多滞后一个 TTL 窗口才会认识新 kid，
//     窗口内新签 token 会被拒一次，用户重新登录即可）。
//   - 需要重拉但拉取失败 → ErrKeySourceUnavailable（暂时性故障，返回 503）。
//   - 重拉成功但仍未命中 → ErrInvalidToken。
func (v *Verifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k := v.keys[kid]; k != nil {
		return k, nil
	}
	// 节流：距上次拉取不足下限就不再打 d-auth，防止伪造 kid 的请求把认证服务打挂。
	// 缓存全空时允许更快重试（下限 5s），否则启动期一次网络抖动会让验签器长时间不可用。
	minGap := v.refreshTTL
	if len(v.keys) == 0 && minGap > 5*time.Second {
		minGap = 5 * time.Second
	}
	if !v.lastFetch.IsZero() && time.Since(v.lastFetch) < minGap {
		return nil, fmt.Errorf("%w: JWKS（刚刷新过）中没有 kid %q", ErrInvalidToken, kid)
	}
	v.lastFetch = time.Now()
	if err := v.fetchJWKSLocked(ctx); err != nil {
		return nil, err
	}
	if k := v.keys[kid]; k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("%w: JWKS 中没有 kid %q", ErrInvalidToken, kid)
}

// fetchJWKSLocked 拉取并替换公钥缓存。调用方必须持有 v.mu。
// JWKS 端点返回裸 {"keys": [...]}，没有统一 envelope，所以不走 Client.do。
func (v *Verifier) fetchJWKSLocked(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+"/v1/.well-known/jwks.json", nil)
	if err != nil {
		return fmt.Errorf("%w: 构造请求: %v", ErrKeySourceUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := v.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: 请求 jwks: %v", ErrKeySourceUnavailable, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: 读取 jwks: %v", ErrKeySourceUnavailable, err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: jwks HTTP %d", ErrKeySourceUnavailable, res.StatusCode)
	}
	var doc struct {
		Keys []jwkEntry `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%w: 解析 jwks: %v", ErrKeySourceUnavailable, err)
	}
	next := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, j := range doc.Keys {
		if j.Kty != "RSA" || j.Kid == "" {
			continue
		}
		// 只接受用于 RS256 验签的 key；use/alg 缺省按约定放行。
		if j.Use != "" && j.Use != "sig" {
			continue
		}
		if j.Alg != "" && j.Alg != AlgRS256 {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(j.N)
		if err != nil || len(n) == 0 {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(j.E)
		if err != nil || len(e) == 0 {
			continue
		}
		next[j.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if len(next) == 0 {
		return fmt.Errorf("%w: jwks 里没有可用的 RSA key（d-auth 是否配了 DAUTH_JWT_ALG=RS256？）", ErrKeySourceUnavailable)
	}
	v.keys = next
	return nil
}

type jwkEntry struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// jwtPayload 对应服务端 tokens.Claims 的 JSON 形状。
type jwtPayload struct {
	Iss   string   `json:"iss"`
	Sub   string   `json:"sub"`
	Aud   any      `json:"aud,omitempty"`
	Exp   int64    `json:"exp"`
	Iat   int64    `json:"iat"`
	JTI   string   `json:"jti"`
	Roles []string `json:"roles"`
	CID   string   `json:"cid"`
}

func (p *jwtPayload) toClaims() *TokenClaims {
	c := &TokenClaims{
		Subject:  p.Sub,
		Issuer:   p.Iss,
		Roles:    p.Roles,
		ClientID: p.CID,
		JTI:      p.JTI,
	}
	if p.Iat != 0 {
		c.IssuedAt = time.Unix(p.Iat, 0)
	}
	if p.Exp != 0 {
		c.ExpiresAt = time.Unix(p.Exp, 0)
	}
	return c
}

func splitJWT(raw string) (header, payload, sig string, ok bool) {
	if raw == "" || strings.ContainsAny(raw, " \t\n") {
		return "", "", "", false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func decodeJSONSegment[T any](seg string) (*T, error) {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return nil, fmt.Errorf("非法 base64url: %w", err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("JSON 解析: %w", err)
	}
	return &v, nil
}
