package password_test

import (
	"testing"

	"github.com/jianboolee/d-auth/internal/infra/password"
	"github.com/jianboolee/d-auth/internal/normalize"
)

func TestArgon2id(t *testing.T) {
	h, err := password.Hash("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if !password.Verify("correct-horse", h) {
		t.Fatal("verify failed")
	}
	if password.Verify("wrong", h) {
		t.Fatal("wrong password accepted")
	}
}

func TestPhone(t *testing.T) {
	p, ok := normalize.Phone("13800138000")
	if !ok || p != "+8613800138000" {
		t.Fatalf("got %s %v", p, ok)
	}
}
