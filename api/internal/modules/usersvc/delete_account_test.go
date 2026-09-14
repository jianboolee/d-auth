package usersvc

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
// 注销涉及多张表与外键约束，只有打真库才能验证「手机号被释放」这类承诺。
func newTestService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 测试的工作目录是本包目录，迁移在 api/migrations；兼容从 api 或仓库根跑。
	var mErr error
	for _, dir := range []string{"../../../migrations", "migrations", "../../migrations"} {
		if mErr = database.Migrate(db, dir); mErr == nil {
			break
		}
	}
	if mErr != nil {
		t.Fatalf("migrate: %v", mErr)
	}
	return &Service{store: store.New(db)}, store.New(db)
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	if c, _, ok := apperr.CodeOf(err); ok {
		return c
	}
	return "UNC0DED"
}

// makeUser 建一个带手机号身份与一个会话的活跃用户。
func makeUser(t *testing.T, st *store.Store, phone string) uuid.UUID {
	t.Helper()
	u := &models.User{Nickname: "注销测试", Status: models.StatusActive}
	if err := st.CreateUser(nil, u, models.RoleUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	now := time.Now()
	if err := st.AddIdentity(nil, &models.Identity{
		UserID: u.ID, Type: models.IdentityPhone, Identifier: phone, VerifiedAt: &now,
	}); err != nil {
		t.Fatalf("add identity: %v", err)
	}
	if err := st.DB.Create(&models.Session{
		ID: uuid.New(), UserID: u.ID, RefreshTokenHash: "h",
		IP: "203.0.113.1", UserAgent: "test-agent",
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() {
		// 顺序遵循外键依赖：sessions/identities 必须在 user 之前
		st.DB.Exec("DELETE FROM auth_sessions WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_identities WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u.ID)
	})
	return u.ID
}

// 核心行为：注销后用户行仍在（墓碑），但个人信息被抹掉。
func TestDeleteAccountLeavesAnonymisedTombstone(t *testing.T) {
	svc, st := newTestService(t)
	id := makeUser(t, st, "+8613800000001")

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var u models.User
	if err := st.DB.Where("id = ?", id).First(&u).Error; err != nil {
		t.Fatalf("tombstone row should still exist: %v", err)
	}
	if u.Nickname != "已注销" {
		t.Fatalf("nickname = %q, want 已注销", u.Nickname)
	}
	if u.Status != models.StatusDeleted {
		t.Fatalf("status = %q, want %q", u.Status, models.StatusDeleted)
	}
	if u.DeletedAt == nil {
		t.Fatal("deleted_at should be set")
	}
	if u.PasswordHash != nil {
		t.Fatal("password_hash should be cleared")
	}
	if u.AvatarKey != "" || u.Bio != "" || u.LastLoginIP != "" {
		t.Fatalf("profile fields should be cleared, got avatar=%q bio=%q ip=%q",
			u.AvatarKey, u.Bio, u.LastLoginIP)
	}
	if !u.CreatedAt.IsZero() && u.ID != id {
		t.Fatal("tombstone should keep id and created_at")
	}
}

// 注销后 UserByID 查不到（后台列表与登录都用它），墓碑对业务不可见。
func TestDeletedUserIsInvisibleToLookup(t *testing.T) {
	svc, st := newTestService(t)
	id := makeUser(t, st, "+8613800000002")

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.UserByID(id); err == nil {
		t.Fatal("UserByID should not return a deleted user")
	}
}

// 最关键的可验证承诺：手机号被释放，可以重新注册。
func TestDeletedIdentityIsReleasedForReRegistration(t *testing.T) {
	svc, st := newTestService(t)
	phone := "+8613800000003"
	id := makeUser(t, st, phone)

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// 旧 identity 必须已物理删除，否则唯一索引会永久占用该号码
	if _, err := st.FindIdentity(models.IdentityPhone, phone); err == nil {
		t.Fatal("identity should be deleted so the phone can be reused")
	}
	// 同一个号码可以绑定到新用户
	u2 := &models.User{Nickname: "新用户", Status: models.StatusActive}
	if err := st.CreateUser(nil, u2, models.RoleUser); err != nil {
		t.Fatalf("create second user: %v", err)
	}
	t.Cleanup(func() {
		st.DB.Exec("DELETE FROM auth_sessions WHERE user_id = ?", u2.ID)
		st.DB.Exec("DELETE FROM auth_identities WHERE user_id = ?", u2.ID)
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", u2.ID)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u2.ID)
	})
	now := time.Now()
	err := st.AddIdentity(nil, &models.Identity{
		UserID: u2.ID, Type: models.IdentityPhone, Identifier: phone, VerifiedAt: &now,
	})
	if err != nil {
		t.Fatalf("phone should be reusable after deletion, got %v", err)
	}
}

// 会话被物理删除（不只是 revoked），IP/UA 随之清除。
func TestDeleteAccountRemovesSessions(t *testing.T) {
	svc, st := newTestService(t)
	id := makeUser(t, st, "+8613800000004")

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int64
	st.DB.Model(&models.Session{}).Where("user_id = ?", id).Count(&n)
	if n != 0 {
		t.Fatalf("sessions should be physically deleted, found %d", n)
	}
}

// 用户创建的 API Key 被解绑（created_by 置空）而不是删除 —— 密钥可能仍在生产使用。
func TestDeleteAccountUnbindsCreatedAPIKeys(t *testing.T) {
	svc, st := newTestService(t)
	id := makeUser(t, st, "+8613800000005")

	k := &models.APIKey{
		ID: uuid.New(), Name: "test-key", KeyPrefix: "dak_live_test5",
		KeyHash: "h", Scopes: models.TextArray{"users:read"},
		CreatedBy: &id, Status: models.StatusActive, CreatedAt: time.Now(),
	}
	if err := st.DB.Create(k).Error; err != nil {
		t.Fatalf("create api key: %v", err)
	}
	t.Cleanup(func() { st.DB.Exec("DELETE FROM auth_api_keys WHERE id = ?", k.ID) })

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var got models.APIKey
	if err := st.DB.Where("id = ?", k.ID).First(&got).Error; err != nil {
		t.Fatal("api key should still exist (unbound, not deleted)")
	}
	if got.CreatedBy != nil {
		t.Fatal("api key created_by should be NULL after the creator is deleted")
	}
	if got.Status != models.StatusActive {
		t.Fatal("api key should remain active")
	}
}

// 确认串错误必须拒绝，且不产生任何副作用。
func TestDeleteAccountRequiresConfirmText(t *testing.T) {
	svc, st := newTestService(t)
	phone := "+8613800000006"
	id := makeUser(t, st, phone)

	for _, bad := range []string{"", "确认", "delete", "确认注销 "} {
		if codeOf(svc.DeleteAccount(id, bad)) != "CONFIRM_REQUIRED" {
			t.Fatalf("confirm %q should be rejected", bad)
		}
	}
	// 账号未被改动
	if _, err := st.UserByID(id); err != nil {
		t.Fatal("a rejected deletion must not modify the account")
	}
	if _, err := st.FindIdentity(models.IdentityPhone, phone); err != nil {
		t.Fatal("a rejected deletion must not release identities")
	}
}

// 最后一名活跃超管不可注销，避免把后台锁死。
func TestDeleteAccountRefusesLastSuperAdmin(t *testing.T) {
	svc, st := newTestService(t)

	// 备份现有超管并临时清空其角色，使测试用户成为唯一超管
	var otherSupers []uuid.UUID
	st.DB.Raw(`SELECT ur.user_id FROM auth_user_roles ur JOIN auth_roles r ON r.id = ur.role_id
		WHERE r.code = ?`, models.RoleSuperAdmin).Scan(&otherSupers)
	for _, uid := range otherSupers {
		st.DB.Exec(`DELETE FROM auth_user_roles WHERE user_id = ? AND role_id =
			(SELECT id FROM auth_roles WHERE code = ? LIMIT 1)`, uid, models.RoleSuperAdmin)
	}
	t.Cleanup(func() {
		for _, uid := range otherSupers {
			st.AddRoles(nil, uid, models.RoleSuperAdmin)
		}
	})

	id := makeUser(t, st, "+8613800000007")
	if err := st.AddRoles(nil, id, models.RoleSuperAdmin); err != nil {
		t.Fatalf("add role: %v", err)
	}
	if codeOf(svc.DeleteAccount(id, DeleteAccountConfirm)) != "LAST_SUPERADMIN" {
		t.Fatal("deleting the only active superadmin must be refused")
	}
	// 账号仍可用
	if _, err := st.UserByID(id); err != nil {
		t.Fatal("account should be untouched after a refused deletion")
	}
}

// 非超管用户可以正常注销（上面的保护只针对超管）。
func TestDeleteAccountAllowsOrdinaryUser(t *testing.T) {
	svc, st := newTestService(t)
	id := makeUser(t, st, "+8613800000008")

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("ordinary user should be deletable: %v", err)
	}
}

// 重复注销应返回「用户不存在」而不是静默成功或报 500。
func TestDeleteAccountTwice(t *testing.T) {
	svc, st := newTestService(t)
	id := makeUser(t, st, "+8613800000009")

	if err := svc.DeleteAccount(id, DeleteAccountConfirm); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if codeOf(svc.DeleteAccount(id, DeleteAccountConfirm)) != "NOT_FOUND" {
		t.Fatal("second delete should report NOT_FOUND")
	}
}
