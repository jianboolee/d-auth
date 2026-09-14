package tokens

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
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
	uid, sid, cid := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	tok, jti, err := m.Sign(uid, sid, cid, []string{"user"})
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
	if claims.Subject != uid.String() || claims.SessionID != sid.String() {
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
	uid, sid, cid := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	tok, _, err := m.Sign(uid, sid, cid, []string{"superadmin"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != uid.String() {
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

func TestRejectUnknownAlg(t *testing.T) {
	_, err := NewManager(Options{Alg: "none", Dev: true, AccessTTL: time.Minute})
	if err == nil {
		t.Fatal("expected error")
	}
}
