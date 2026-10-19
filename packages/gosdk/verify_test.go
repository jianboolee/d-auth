package dauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAuth 模拟一个 RS256 的 d-auth：持有私钥、serve JWKS、按需签 token。
type fakeAuth struct {
	t       *testing.T
	mu      sync.RWMutex // 保护 key/kid：httptest handler 在别的 goroutine 里读
	key     *rsa.PrivateKey
	kid     string
	fetches atomic.Int64
	srv     *httptest.Server
	// notFound 为 true 时 jwks 端点返回 404（模拟 d-auth 故障）。
	notFound atomic.Bool
}

func (f *fakeAuth) currentKey() (*rsa.PrivateKey, string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.key, f.kid
}

func (f *fakeAuth) rotate(key *rsa.PrivateKey, kid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.key, f.kid = key, kid
}

func newFakeAuth(t *testing.T) *fakeAuth {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAuth{t: t, key: key, kid: "test-kid-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		f.fetches.Add(1)
		if f.notFound.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// 动态读当前 key/kid，让 TestKeyRotation 能热换密钥。
		k, kid := f.currentKey()
		pub := &k.PublicKey
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{
			{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAuth) newToken(header map[string]any, payload map[string]any) string {
	f.t.Helper()
	key, _ := f.currentKey()
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(payload)
	h := base64.RawURLEncoding.EncodeToString(hb)
	p := base64.RawURLEncoding.EncodeToString(pb)
	if header["alg"] != "RS256" {
		// 非 RS256 的 header 只测拒签路径，签名内容随意。
		return h + "." + p + "." + base64.RawURLEncoding.EncodeToString([]byte("bogus"))
	}
	sum := sha256.Sum256([]byte(h + "." + p))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fakeAuth) validToken(issuer string) string {
	now := time.Now()
	_, kid := f.currentKey()
	return f.newToken(
		map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid},
		map[string]any{
			"iss": issuer, "sub": "u1234567890abcdefghijklmn",
			"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
			"jti": "jti-1", "roles": []string{"user"},
			"cid": "client-uuid",
		})
}

func newVerifier(f *fakeAuth, issuer string) *Verifier {
	return NewVerifier(VerifierOptions{BaseURL: f.srv.URL, Issuer: issuer})
}

func TestVerifyTokenOK(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	claims, err := v.VerifyToken(context.Background(), f.validToken("http://d-auth.test"))
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "u1234567890abcdefghijklmn" {
		t.Fatalf("sub=%q", claims.Subject)
	}
	if len(claims.Roles) != 1 || claims.Roles[0] != "user" {
		t.Fatalf("roles=%v", claims.Roles)
	}
	if claims.ClientID != "client-uuid" || claims.JTI != "jti-1" {
		t.Fatalf("claims=%+v", claims)
	}
	if claims.ExpiresAt.Before(time.Now()) {
		t.Fatal("exp should be in the future")
	}
}

func TestVerifyTokenAcceptsBearerPrefix(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	if _, err := v.VerifyToken(context.Background(), "Bearer "+f.validToken("http://d-auth.test")); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyTokenRejectsAlgDowngrade(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	_, kid := f.currentKey()
	for _, alg := range []string{"none", "HS256", "RS512"} {
		tok := f.newToken(map[string]any{"alg": alg, "kid": kid}, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
		if _, err := v.VerifyToken(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("alg=%s: want ErrInvalidToken, got %v", alg, err)
		}
	}
}

func TestVerifyTokenRejectsTamperedSignature(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	tok := f.validToken("http://d-auth.test")
	parts := strings.Split(tok, ".")
	badSig := base64.RawURLEncoding.EncodeToString(append([]byte{0}, mustB64(t, parts[2])...))
	if _, err := v.VerifyToken(context.Background(), parts[0]+"."+parts[1]+"."+badSig); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyTokenRejectsExpired(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	_, kid := f.currentKey()
	now := time.Now()
	tok := f.newToken(
		map[string]any{"alg": "RS256", "kid": kid},
		map[string]any{"iss": "http://d-auth.test", "sub": "u", "iat": now.Add(-2 * time.Hour).Unix(), "exp": now.Add(-time.Hour).Unix()})
	if _, err := v.VerifyToken(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestVerifyTokenRejectsWrongIssuer(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	if _, err := v.VerifyToken(context.Background(), f.validToken("http://evil.test")); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
	// 不配 Issuer 则不校验 iss。
	v2 := newVerifier(f, "")
	if _, err := v2.VerifyToken(context.Background(), f.validToken("http://anything.test")); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyTokenMalformed(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	for _, raw := range []string{"", "a.b", "a.b.c.d", "not a jwt", "..."} {
		if _, err := v.VerifyToken(context.Background(), raw); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("raw=%q: want ErrInvalidToken, got %v", raw, err)
		}
	}
}

func TestVerifyTokenUnknownKidAfterRefreshIsInvalid(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	// 先用有效 token 填充缓存，再签一个伪造 kid 的 RS256 token。
	// 签名用真实私钥但 kid 不在 JWKS 里：重拉后仍找不到 → ErrInvalidToken。
	if _, err := v.VerifyToken(context.Background(), f.validToken("http://d-auth.test")); err != nil {
		t.Fatal(err)
	}
	tok := f.newToken(
		map[string]any{"alg": "RS256", "kid": "forged-kid"},
		map[string]any{"iss": "http://d-auth.test", "exp": time.Now().Add(time.Hour).Unix()})
	if _, err := v.VerifyToken(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
}

func TestJWKSFetchIsThrottled(t *testing.T) {
	f := newFakeAuth(t)
	v := NewVerifier(VerifierOptions{BaseURL: f.srv.URL, Issuer: "http://d-auth.test", JWKSRefreshTTL: time.Hour})
	base := f.fetches.Load()
	// 首次有效调用拉一次。
	if _, err := v.VerifyToken(context.Background(), f.validToken("http://d-auth.test")); err != nil {
		t.Fatal(err)
	}
	if got := f.fetches.Load() - base; got != 1 {
		t.Fatalf("first fetches=%d", got)
	}
	// 之后的有效/伪造 kid 调用都不应再打网络（缓存命中 / 节流）。
	if _, err := v.VerifyToken(context.Background(), f.validToken("http://d-auth.test")); err != nil {
		t.Fatal(err)
	}
	tok := f.newToken(map[string]any{"alg": "RS256", "kid": "forged"}, map[string]any{"exp": time.Now().Add(time.Hour).Unix()})
	if _, err := v.VerifyToken(context.Background(), tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken, got %v", err)
	}
	if got := f.fetches.Load() - base; got != 1 {
		t.Fatalf("expected no extra fetches, got %d", got)
	}
}

func TestJWSKUnavailableReturnsKeySourceError(t *testing.T) {
	f := newFakeAuth(t)
	f.notFound.Store(true)
	v := newVerifier(f, "http://d-auth.test")
	if _, err := v.VerifyToken(context.Background(), f.validToken("http://d-auth.test")); !errors.Is(err, ErrKeySourceUnavailable) {
		t.Fatalf("want ErrKeySourceUnavailable, got %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	oldTok := f.validToken("http://d-auth.test")
	if _, err := v.VerifyToken(context.Background(), oldTok); err != nil {
		t.Fatal(err)
	}
	// d-auth 换密钥：旧 kid 不在缓存新列表里，触发重拉并用新公钥验签成功。
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.rotate(newKey, "test-kid-2")
	v.mu.Lock()
	v.lastFetch = time.Time{} // 绕过节流，模拟 TTL 到期
	v.mu.Unlock()
	if _, err := v.VerifyToken(context.Background(), f.validToken("http://d-auth.test")); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	// 旧密钥签的 token：kid 已从 JWKS 消失且重拉刚被节流 → ErrInvalidToken。
	if _, err := v.VerifyToken(context.Background(), oldTok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("old token after rotation: want ErrInvalidToken, got %v", err)
	}
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 公钥指纹自检：确认 x509 marshal 与 base64url n/e 一致（防手滑构造错 PublicKey）。
func TestRSAPublicKeyShape(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil || len(der) == 0 {
		t.Fatal(der)
	}
	pub := &rsa.PublicKey{N: new(big.Int).SetBytes(key.N.Bytes()), E: key.E}
	if pub.N.Cmp(key.PublicKey.N) != 0 || pub.E != key.PublicKey.E {
		t.Fatal("pubkey reconstruction mismatch")
	}
}

func TestVerifyRequestFromCookie(t *testing.T) {
	f := newFakeAuth(t)
	v := newVerifier(f, "http://d-auth.test")
	tok := f.validToken("http://d-auth.test")

	// 只有 cookie（浏览器场景）：应验签通过。
	req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req.AddCookie(&http.Cookie{Name: DefaultAccessCookieName, Value: tok})
	claims, err := v.VerifyRequest(context.Background(), req, "")
	if err != nil {
		t.Fatalf("cookie verify: %v", err)
	}
	if claims.Subject != "u1234567890abcdefghijklmn" {
		t.Fatalf("sub=%q", claims.Subject)
	}

	// Bearer 优先于 cookie。
	req2 := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	req2.AddCookie(&http.Cookie{Name: DefaultAccessCookieName, Value: "garbage"})
	if _, err := v.VerifyRequest(context.Background(), req2, ""); err != nil {
		t.Fatalf("bearer should take precedence: %v", err)
	}

	// 两者都没有：未登录，返回 ErrInvalidToken。
	req3 := httptest.NewRequest(http.MethodGet, "/whoami", nil)
	if _, err := v.VerifyRequest(context.Background(), req3, ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("want ErrInvalidToken when no credential, got %v", err)
	}
}
