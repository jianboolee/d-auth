package dto_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/dto"
)

// 后台详情页要展示创建/更新/登录三个时间，FromUser 必须把它们都带出来；
// 从未登录时 LastLoginAt 保持 nil（前端据此显示占位符而不是 Invalid Date）。
func TestFromUserCarriesTimestamps(t *testing.T) {
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 9, 2, 11, 30, 0, 0, time.UTC)

	out := dto.FromUser(&models.User{ID: uuid.New(), CreatedAt: created, UpdatedAt: updated},
		nil, nil, func(string) string { return "" })
	if !out.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %v, want %v", out.CreatedAt, created)
	}
	if !out.UpdatedAt.Equal(updated) {
		t.Fatalf("updated_at = %v, want %v", out.UpdatedAt, updated)
	}
	if out.LastLoginAt != nil {
		t.Fatalf("never-logged-in user should expose nil last_login_at, got %v", out.LastLoginAt)
	}

	loggedIn := updated.Add(time.Hour)
	out = dto.FromUser(&models.User{ID: uuid.New(), CreatedAt: created, UpdatedAt: updated, LastLoginAt: &loggedIn},
		nil, nil, func(string) string { return "" })
	if out.LastLoginAt == nil || !out.LastLoginAt.Equal(loggedIn) {
		t.Fatalf("last_login_at = %v, want %v", out.LastLoginAt, loggedIn)
	}
}

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
	if !out.HasPhone {
		t.Fatal("expected HasPhone true when a phone identity exists")
	}
}

// 邮箱注册/微信登录创建的账号没有手机号，HasPhone 必须是 false，
// 前端据此引导用户去绑定手机号。
func TestFromUserHasPhoneFalseWithoutPhoneIdentity(t *testing.T) {
	user := &models.User{ID: uuid.New(), Nickname: "邮箱用户"}
	idents := []models.Identity{
		{Type: models.IdentityEmail, Identifier: "a@example.com"},
		{Type: models.IdentityWechatOpen, Identifier: "openid-1"},
	}
	out := dto.FromUser(user, nil, idents, func(string) string { return "" })
	if out.HasPhone {
		t.Fatal("expected HasPhone false without a phone identity")
	}
	if out.Phone != "" {
		t.Fatalf("expected empty phone, got %q", out.Phone)
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

// 对外的用户 ID 一律是 uuid 字符串，与数据库 uuid 列的渲染一致。
func TestFromUserExposesUserIDForms(t *testing.T) {
	fresh := idgen.NewUserID()
	newUser := dto.FromUser(&models.User{ID: fresh}, nil, nil, func(string) string { return "" })
	want := idgen.FormatUserID(fresh)
	if newUser.ID != want || newUser.ID != fresh.String() {
		t.Fatalf("new user id = %q, want uuid %q", newUser.ID, fresh.String())
	}

	legacy := idgen.New()
	oldUser := dto.FromUser(&models.User{ID: legacy}, nil, nil, func(string) string { return "" })
	if oldUser.ID != legacy.String() {
		t.Fatalf("legacy user id = %q, want uuid %q", oldUser.ID, legacy.String())
	}
}

// 存量那条早期实验写入的行（16 个字节全在 0x30..0x53）对外也要按 uuid 渲染，
// 也就是数据库里看到的那串——这正是本次改动要消除的「库里与界面不一致」。
// 这里直接构造字节，不再经由已废弃的 16 位解析路径。
func TestFromUserRendersPackedRowAsUUID(t *testing.T) {
	packed, err := uuid.Parse("30304d45-3d32-4148-4430-50363f323f35")
	if err != nil {
		t.Fatalf("parse stored id: %v", err)
	}
	out := dto.FromUser(&models.User{ID: packed}, nil, nil, func(string) string { return "" })
	if out.ID != "30304d45-3d32-4148-4430-50363f323f35" {
		t.Fatalf("packed row exposed as %q, want the uuid column rendering", out.ID)
	}
}

// identity 与审计日志里的用户 ID 同样用 uuid 字符串形式。
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

// 自助接口用 dto.Masked 脱敏；手机号要去掉 +86 区号再脱敏。
func TestMaskedHidesPhoneAndEmail(t *testing.T) {
	user := dto.User{
		ID:    "abc",
		Phone: "+8613800138000",
		Email: "zhangsan@example.com",
	}
	got := dto.Masked(user)
	if got.Phone != "138****8000" {
		t.Fatalf("phone = %q, want 138****8000", got.Phone)
	}
	if got.Email == user.Email || got.Email != "z***n@example.com" {
		t.Fatalf("email = %q, want masked z***n@example.com", got.Email)
	}
	// 未绑定/空值不应变成 "***"
	empty := dto.Masked(dto.User{ID: "abc"})
	if empty.Phone != "" || empty.Email != "" {
		t.Fatalf("empty values should stay empty, got %q %q", empty.Phone, empty.Email)
	}
}

// 国际号码保留国家码，只脱敏中间部分。
func TestMaskedKeepsInternationalPrefix(t *testing.T) {
	got := dto.Masked(dto.User{Phone: "+14155552671"})
	if got.Phone != "+141****2671" {
		t.Fatalf("phone = %q, want +141****2671", got.Phone)
	}
}

// identities 列表也要脱敏，否则它就成了绕过 /me 脱敏的入口；
// 第三方标识（微信 openid）不属于隐私，保持原样。
func TestFromIdentitiesMasked(t *testing.T) {
	list := []models.Identity{
		{Type: models.IdentityPhone, Identifier: "+8613800138000"},
		{Type: models.IdentityEmail, Identifier: "zhangsan@example.com"},
		{Type: models.IdentityWechatOpen, Identifier: "openid-abc"},
	}
	out := dto.FromIdentitiesMasked(list)
	if out[0].Identifier != "138****8000" {
		t.Fatalf("phone identifier = %q", out[0].Identifier)
	}
	if out[1].Identifier != "z***n@example.com" {
		t.Fatalf("email identifier = %q", out[1].Identifier)
	}
	if out[2].Identifier != "openid-abc" {
		t.Fatalf("third-party identifier should be untouched, got %q", out[2].Identifier)
	}
	// FromIdentities（后台用）不应脱敏
	plain := dto.FromIdentities(list)
	if plain[0].Identifier != "+8613800138000" {
		t.Fatalf("FromIdentities must keep plaintext for admin, got %q", plain[0].Identifier)
	}
}

// 公开资料必须脱敏手机号/邮箱：这是 /v1/users 与 internal 接口的分界线。
func TestFromUserProfileMasksIdentifiers(t *testing.T) {
	u := dto.User{
		ID:       "u1",
		Nickname: "小野驴",
		Phone:    "+8613800138000",
		Email:    "zhangsan@example.com",
		HasPhone: true,
		Roles:    []string{"user"},
	}
	got := dto.FromUserProfile(u)
	if got.Phone != "138****8000" {
		t.Fatalf("phone = %q, want masked 138****8000", got.Phone)
	}
	if got.Email != "z***n@example.com" {
		t.Fatalf("email = %q, want masked z***n@example.com", got.Email)
	}
	if !got.HasPhone || got.ID != "u1" || got.Nickname != "小野驴" {
		t.Fatalf("profile lost basic fields: %+v", got)
	}
}

// 管理角色决定 is_admin，但角色明细不该出现在公开资料里。
func TestFromUserProfileIsAdmin(t *testing.T) {
	cases := []struct {
		roles []string
		want  bool
	}{
		{[]string{"user"}, false},
		{nil, false},
		{[]string{"user", "vip"}, false},
		{[]string{"admin"}, true},
		{[]string{"user", "admin"}, true},
		{[]string{"superadmin"}, true},
	}
	for _, tc := range cases {
		got := dto.FromUserProfile(dto.User{ID: "u", Roles: tc.roles})
		if got.IsAdmin != tc.want {
			t.Fatalf("roles %v -> is_admin %v, want %v", tc.roles, got.IsAdmin, tc.want)
		}
	}
}

// 未绑定的空手机/邮箱不能变成 "***"（Masked 已保证，这里守住 Profile 这条出口）。
func TestFromUserProfileKeepsEmptyIdentifiersEmpty(t *testing.T) {
	got := dto.FromUserProfile(dto.User{ID: "u"})
	if got.Phone != "" || got.Email != "" {
		t.Fatalf("empty identifiers should stay empty, got %q %q", got.Phone, got.Email)
	}
	if got.HasPhone {
		t.Fatal("HasPhone should be false when no phone identity exists")
	}
}
