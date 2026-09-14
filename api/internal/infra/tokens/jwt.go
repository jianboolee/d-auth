package tokens

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	Roles []string `json:"roles"`
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
	ClientID  string `json:"cid,omitempty"`
}

type Manager struct {
	issuer    string
	accessTTL time.Duration
	priv      *rsa.PrivateKey
	pub       *rsa.PublicKey
	kid       string
}

func NewManager(issuer, keyPath string, accessTTL time.Duration) (*Manager, error) {
	priv, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &Manager{
		issuer:    issuer,
		accessTTL: accessTTL,
		priv:      priv,
		pub:       &priv.PublicKey,
		kid:       keyID(&priv.PublicKey),
	}, nil
}

func (m *Manager) Sign(userID, sessionID, clientID uuid.UUID, roles []string) (string, string, error) {
	jti := uuid.NewString()
	now := time.Now()
	claims := Claims{
		Roles: roles,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			ID:        jti,
		},
		SessionID: sessionID.String(),
		ClientID:  clientID.String(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = m.kid
	signed, err := tok.SignedString(m.priv)
	if err != nil {
		return "", "", err
	}
	return signed, jti, nil
}

func (m *Manager) Parse(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unexpected alg")
		}
		return m.pub, nil
	}, jwt.WithLeeway(30*time.Second))
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
	n := base64.RawURLEncoding.EncodeToString(m.pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(bigEndianInt(m.pub.E))
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": m.kid,
			"n":   n,
			"e":   e,
		}},
	}
}

func (m *Manager) AccessTTL() time.Duration { return m.accessTTL }

func keyID(pub *rsa.PublicKey) string {
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
