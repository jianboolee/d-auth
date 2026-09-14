package dto_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/dto"
)

func TestFromUserUsesInjectedAvatarBuilder(t *testing.T) {
	hash := "hashed"
	user := &models.User{
		ID:           uuid.New(),
		Nickname:     "小野驴",
		AvatarKey:    "uploads/u/a.png",
		Status:       "active",
		PasswordHash: &hash,
	}
	idents := []models.Identity{
		{Type: models.IdentityPhone, Identifier: "+8613800138000"},
		{Type: models.IdentityEmail, Identifier: "a@example.com"},
	}

	var gotKey string
	avatar := func(key string) string {
		gotKey = key
		if key == "" {
			return ""
		}
		return "https://cdn.example.com/" + key + "?x-oss-process=image/resize,m_fill,w_360,h_360"
	}

	out := dto.FromUser(user, []string{"admin"}, idents, avatar)

	if gotKey != "uploads/u/a.png" {
		t.Fatalf("avatar builder got key %q", gotKey)
	}
	want := "https://cdn.example.com/uploads/u/a.png?x-oss-process=image/resize,m_fill,w_360,h_360"
	if out.AvatarURL != want {
		t.Fatalf("got %q want %q", out.AvatarURL, want)
	}
	if out.Phone != "+8613800138000" || out.Email != "a@example.com" {
		t.Fatalf("identity mapping failed: %q %q", out.Phone, out.Email)
	}
	if !out.HasPassword {
		t.Fatal("expected HasPassword true")
	}
}

func TestFromUserEmptyAvatarKeyWithRealConfig(t *testing.T) {
	cfg := &config.Config{
		OSSCDNBase:      "https://cdn.example.com",
		OSSAvatarSuffix: "?x-oss-process=image/resize,m_fill,w_360,h_360",
	}
	user := &models.User{ID: uuid.New(), Nickname: "x", AvatarKey: ""}

	out := dto.FromUser(user, nil, nil, cfg.AvatarURL)
	if out.AvatarURL != "" {
		t.Fatalf("empty avatar key should yield empty url, got %q", out.AvatarURL)
	}
}
