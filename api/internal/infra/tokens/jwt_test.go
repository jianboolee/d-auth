package tokens

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/idgen"
)

func TestHS256SignParse(t *testing.T) {
	m, err := NewManager(Options{
		Issuer:    "http://d-auth.test",
		Alg:       AlgHS256,
		Secret:    "dev-jwt-hs256-change-me-please-32b!!!!",
		AccessTTL: time.Minute,
		Dev:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Alg() != AlgHS256 {
		t.Fatalf("alg=%s", m.Alg())
	}
	uid, cid := idgen.NewUserID(), uuid.Must(uuid.NewV7())
	tok, jti, err := m.Sign(uid, cid, []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	if jti == "" || tok == "" {
		t.Fatal("empty token")
	}
	claims, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != uid || claims.ClientID != cid.String() {
		t.Fatalf("claims mismatch: %+v", claims)
	}
	if len(m.JWKS()["keys"].([]map[string]any)) != 0 {
		t.Fatal("HS256 must not publish secret in JWKS")
	}
}

func TestRS256SignParseAndJWKS(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(Options{
		Issuer:    "http://d-auth.test",
		Alg:       AlgRS256,
		KeyPath:   filepath.Join(dir, "jwt.pem"),
		AccessTTL: time.Minute,
		Dev:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uid, cid := idgen.NewUserID(), uuid.Must(uuid.NewV7())
	tok, _, err := m.Sign(uid, cid, []string{"superadmin"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != uid {
		t.Fatalf("sub=%s", claims.Subject)
	}
	keys, _ := m.JWKS()["keys"].([]map[string]any)
	if len(keys) != 1 || keys[0]["alg"] != AlgRS256 {
		t.Fatalf("jwks=%v", m.JWKS())
	}
}

func TestAutoAlgUsesHS256WhenSecretSet(t *testing.T) {
	m, err := NewManager(Options{
		Issuer:    "http://d-auth.test",
		Secret:    "abcdefghijklmnopqrstuvwxyz012345",
		AccessTTL: time.Minute,
		Dev:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Alg() != AlgHS256 {
		t.Fatalf("alg=%s", m.Alg())
	}
}

// RS256 部署即使 env 残留 DAUTH_JWT_SECRET，也不得加载 HMAC、不得接受 HS256 token。
func TestRS256IgnoresResidualSecret(t *testing.T) {
	secret := "abcdefghijklmnopqrstuvwxyz012345"
	hs, err := NewManager(Options{
		Issuer:    "http://d-auth.test",
		Alg:       AlgHS256,
		Secret:    secret,
		AccessTTL: time.Minute,
		Dev:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	uid := idgen.NewUserID()
	hsTok, _, err := hs.Sign(uid, uuid.Must(uuid.NewV7()), nil)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	rs, err := NewManager(Options{
		Issuer:    "http://d-auth.test",
		Alg:       AlgRS256,
		Secret:    secret, // 残留的旧 HS256 配置，应被忽略
		KeyPath:   filepath.Join(dir, "jwt.pem"),
		AccessTTL: time.Minute,
		Dev:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.hmac) != 0 {
		t.Fatal("RS256 must not load HMAC secret")
	}
	if _, err := rs.Parse(hsTok); err == nil {
		t.Fatal("RS256 manager must reject HS256 token")
	}
}

func TestRejectUnknownAlg(t *testing.T) {
	_, err := NewManager(Options{Alg: "none", Dev: true, AccessTTL: time.Minute})
	if err == nil {
		t.Fatal("expected error")
	}
}
