package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/idgen"
)

const (
	AlgHS256 = "HS256"
	AlgRS256 = "RS256"
)

type Claims struct {
	Roles []string `json:"roles"`
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
	ClientID  string `json:"cid,omitempty"`
}

type Options struct {
	Issuer    string
	Alg       string
	Secret    string
	KeyPath   string
	AccessTTL time.Duration
	Dev       bool
}

type Manager struct {
	issuer    string
	accessTTL time.Duration
	alg       string
	hmac      []byte
	priv      *rsa.PrivateKey
	pub       *rsa.PublicKey
	kid       string
}

func NewManager(opts Options) (*Manager, error) {
	alg, err := resolveAlg(opts.Alg, opts.Secret)
	if err != nil {
		return nil, err
	}
	m := &Manager{issuer: opts.Issuer, accessTTL: opts.AccessTTL, alg: alg}

	if alg == AlgHS256 || strings.TrimSpace(opts.Secret) != "" {
		secret, err := loadOrCreateHMAC(opts)
		if err != nil {
			return nil, err
		}
		m.hmac = []byte(secret)
	}
	if alg == AlgRS256 {
		priv, err := loadOrCreateKey(opts.KeyPath)
		if err != nil {
			return nil, err
		}
		m.priv = priv
		m.pub = &priv.PublicKey
	}

	switch alg {
	case AlgHS256:
		if len(m.hmac) == 0 {
			return nil, fmt.Errorf("HS256 requires DAUTH_JWT_SECRET / JWT_SECRET")
		}
		m.kid = hmacKeyID(m.hmac)
	case AlgRS256:
		if m.priv == nil {
			return nil, fmt.Errorf("RS256 private key missing")
		}
		m.kid = rsaKeyID(m.pub)
	}
	return m, nil
}

func resolveAlg(raw, secret string) (string, error) {
	alg := strings.ToUpper(strings.TrimSpace(raw))
	switch alg {
	case "", "AUTO":
		if strings.TrimSpace(secret) != "" {
			return AlgHS256, nil
		}
		return AlgRS256, nil
	case AlgHS256, AlgRS256:
		return alg, nil
	default:
		return "", fmt.Errorf("unsupported JWT alg %q (use HS256 or RS256)", raw)
	}
}

func (m *Manager) Alg() string { return m.alg }

func (m *Manager) Sign(userID string, sessionID, clientID uuid.UUID, roles []string) (string, string, error) {
	jti := uuid.NewString()
	now := time.Now()
	// sub 是用户 ID（22 位小写字母数字串）。
	claims := Claims{
		Roles: roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   idgen.FormatUserID(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			ID:        jti,
		},
		SessionID: sessionID.String(),
		ClientID:  clientID.String(),
	}

	var tok *jwt.Token
	var key any
	switch m.alg {
	case AlgHS256:
		tok = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		key = m.hmac
	default:
		tok = jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		key = m.priv
	}
	tok.Header["kid"] = m.kid
	signed, err := tok.SignedString(key)
	if err != nil {
		return "", "", err
	}
	return signed, jti, nil
}

func (m *Manager) Parse(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		switch t.Method.Alg() {
		case AlgHS256:
			if len(m.hmac) == 0 {
				return nil, fmt.Errorf("HS256 not configured")
			}
			return m.hmac, nil
		case AlgRS256:
			if m.pub == nil {
				return nil, fmt.Errorf("RS256 not configured")
			}
			return m.pub, nil
		default:
			return nil, fmt.Errorf("unexpected alg %s", t.Method.Alg())
		}
	}, jwt.WithLeeway(30*time.Second), jwt.WithValidMethods([]string{AlgHS256, AlgRS256}))
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func (m *Manager) JWKS() map[string]any {
	keys := make([]map[string]any, 0, 1)
	if m.pub != nil {
		n := base64.RawURLEncoding.EncodeToString(m.pub.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(bigEndianInt(m.pub.E))
		keys = append(keys, map[string]any{
			"kty": "RSA",
			"use": "sig",
			"alg": AlgRS256,
			"kid": rsaKeyID(m.pub),
			"n":   n,
			"e":   e,
		})
	}
	// HS256 共享密钥不能放进公开 JWKS。其它服务用 JWT_SECRET 验签。
	return map[string]any{"keys": keys}
}

func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

func hmacKeyID(secret []byte) string {
	sum := sha256.Sum256(secret)
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

func rsaKeyID(pub *rsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

func bigEndianInt(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte(v)}, b...)
		v >>= 8
	}
	return b
}

func loadOrCreateHMAC(opts Options) (string, error) {
	if s := strings.TrimSpace(opts.Secret); s != "" {
		if err := checkHMACSecret(s, opts.Dev); err != nil {
			return "", err
		}
		return s, nil
	}
	if !opts.Dev {
		return "", fmt.Errorf("HS256 requires DAUTH_JWT_SECRET / JWT_SECRET")
	}
	target := filepath.Join("data", "jwt-hs256.secret")
	if raw, err := os.ReadFile(target); err == nil {
		s := strings.TrimSpace(string(raw))
		if s != "" {
			return s, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
		_ = os.WriteFile(target, []byte(s+"\n"), 0o600)
	}
	return s, nil
}

func checkHMACSecret(s string, dev bool) error {
	if len(s) >= 32 {
		return nil
	}
	if dev {
		return nil
	}
	return fmt.Errorf("JWT HMAC secret must be at least 32 characters")
}

func loadOrCreateKey(path string) (*rsa.PrivateKey, error) {
	target := path
	if target == "" {
		target = filepath.Join("data", "jwt-private.pem")
	}
	if _, err := os.Stat(target); err == nil {
		return parsePEMFile(target)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
		_ = writePEM(target, key)
	}
	return key, nil
}

func parsePEMFile(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("no pem in %s", path)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not rsa private key")
	}
	return key, nil
}

func writePEM(path string, key *rsa.PrivateKey) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func PrettyJWKS(m *Manager) ([]byte, error) {
	return json.Marshal(m.JWKS())
}
