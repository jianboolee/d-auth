package dto_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
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

// 新用户的 ID 对外应是 16 位串；历史 uuid 用户保持 uuid 字符串。
func TestFromUserExposesUserIDForms(t *testing.T) {
	packed := idgen.NewUserID()
	newUser := dto.FromUser(&models.User{ID: packed}, nil, nil, func(string) string { return "" })
	want := idgen.FormatUserID(packed)
	if newUser.ID != want || len(newUser.ID) != 16 {
		t.Fatalf("new user id = %q, want 16-char %q", newUser.ID, want)
	}

	legacy := idgen.New()
	oldUser := dto.FromUser(&models.User{ID: legacy}, nil, nil, func(string) string { return "" })
	if oldUser.ID != legacy.String() {
		t.Fatalf("legacy user id = %q, want uuid %q", oldUser.ID, legacy.String())
	}
}

// identity 与审计日志里的用户 ID 也要用对外的 16 位串形式。
func TestIdentityAndAuditExposeUserIDForms(t *testing.T) {
	uid := idgen.NewUserID()
	want := idgen.FormatUserID(uid)

	ident := dto.FromIdentity(&models.Identity{ID: uuid.New(), UserID: uid, Type: "phone"})
	if ident.UserID != want {
		t.Fatalf("identity user_id = %q, want %q", ident.UserID, want)
	}

	logs := dto.FromAuditLogs([]models.AuditLog{{ID: uuid.New(), ActorUserID: &uid, TargetUserID: &uid}})
	if len(logs) != 1 || logs[0].ActorUserID == nil || *logs[0].ActorUserID != want {
		t.Fatalf("audit actor = %v, want %q", logs[0].ActorUserID, want)
	}
	if logs[0].TargetUserID == nil || *logs[0].TargetUserID != want {
		t.Fatalf("audit target = %v, want %q", logs[0].TargetUserID, want)
	}
}
