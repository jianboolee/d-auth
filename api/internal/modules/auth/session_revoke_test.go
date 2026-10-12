package auth

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
// RevokeSession 只说一句 SQL，但它的正确性完全取决于 WHERE 条件，所以直接打真库。
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
	// 测试的工作目录是本包目录，迁移在 api/migrations；
	// 兼容从 api 或仓库根跑的情况，逐个候选尝试。
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

func createTestUser(t *testing.T, st *store.Store) string {
	t.Helper()
	u := &models.User{Nickname: "revoke-test", Status: models.StatusActive}
	if err := st.CreateUser(nil, u, models.RoleUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		st.DB.Exec("DELETE FROM auth_sessions WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u.ID)
	})
	return u.ID
}

func createSession(t *testing.T, st *store.Store, userID string, refreshToken string) uuid.UUID {
	t.Helper()
	s := models.Session{
		ID:               idgen.New(),
		UserID:           userID,
		RefreshTokenHash: store.HashToken(refreshToken),
		ExpiresAt:        time.Now().Add(24 * time.Hour),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := st.DB.Create(&s).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s.ID
}

func revoked(t *testing.T, st *store.Store, id uuid.UUID) bool {
	t.Helper()
	var s models.Session
	if err := st.DB.Where("id = ?", id).First(&s).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	return s.RevokedAt != nil
}

// token 模式的关键路径：access token 已过期时只剩 refresh token 能定位会话。
func TestRevokeSessionByRefreshToken(t *testing.T) {
	svc, st := newTestService(t)
	userID := createTestUser(t, st)
	raw := "refresh-token-abc"
	sid := createSession(t, st, userID, raw)

	if err := svc.RevokeSession(uuid.Nil, raw); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !revoked(t, st, sid) {
		t.Fatal("session should be revoked when located by refresh token")
	}
}

func TestRevokeSessionBySessionID(t *testing.T) {
	svc, st := newTestService(t)
	userID := createTestUser(t, st)
	sid := createSession(t, st, userID, "refresh-token-def")

	if err := svc.RevokeSession(sid, ""); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !revoked(t, st, sid) {
		t.Fatal("session should be revoked when located by session id")
	}
}

// refresh token 优先于 session ID：即使传了不匹配的 sid，也应按 token 吊销。
func TestRevokeSessionPrefersRefreshToken(t *testing.T) {
	svc, st := newTestService(t)
	userID := createTestUser(t, st)
	raw := "refresh-token-ghi"
	sid := createSession(t, st, userID, raw)
	other := createSession(t, st, userID, "another-token")

	if err := svc.RevokeSession(other, raw); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !revoked(t, st, sid) {
		t.Fatal("session matching the refresh token should be revoked")
	}
	if revoked(t, st, other) {
		t.Fatal("session id passed alongside a matching token should be left alone")
	}
}

// 完全没有凭据（未登录调用退出）应返回 nil，不报错。
func TestRevokeSessionWithoutCredentialsIsNoop(t *testing.T) {
	svc, st := newTestService(t)
	_ = createTestUser(t, st)

	if err := svc.RevokeSession(uuid.Nil, ""); err != nil {
		t.Fatalf("expected nil error for an anonymous logout, got %v", err)
	}
}

// 未知 token / 未知 session ID 不应影响任何行，且不报错（幂等）。
func TestRevokeSessionUnknownCredentials(t *testing.T) {
	svc, st := newTestService(t)
	userID := createTestUser(t, st)
	sid := createSession(t, st, userID, "known-token")

	if err := svc.RevokeSession(uuid.Nil, "no-such-token"); err != nil {
		t.Fatalf("revoke with unknown token: %v", err)
	}
	if err := svc.RevokeSession(uuid.New(), ""); err != nil {
		t.Fatalf("revoke with unknown session id: %v", err)
	}
	if revoked(t, st, sid) {
		t.Fatal("an unrelated session must not be touched")
	}
}

// 重复退出幂等：第二次调用不应报错，且已吊销状态保持。
func TestRevokeSessionIdempotent(t *testing.T) {
	svc, st := newTestService(t)
	userID := createTestUser(t, st)
	raw := "refresh-token-jkl"
	sid := createSession(t, st, userID, raw)

	if err := svc.RevokeSession(uuid.Nil, raw); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := svc.RevokeSession(uuid.Nil, raw); err != nil {
		t.Fatalf("second revoke should be a no-op, got %v", err)
	}
	if !revoked(t, st, sid) {
		t.Fatal("session should stay revoked")
	}
}
