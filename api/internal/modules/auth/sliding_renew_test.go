package auth

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
// 滑动续期靠一条带条件的 UPDATE，正确性取决于 WHERE 与时间算术，所以打真库验证。
func newRenewService(t *testing.T, ttl, interval time.Duration) (*Service, *store.Store) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var mErr error
	for _, dir := range []string{"../../../migrations", "migrations", "../../migrations"} {
		if mErr = database.Migrate(db, dir); mErr == nil {
			break
		}
	}
	if mErr != nil {
		t.Fatalf("migrate: %v", mErr)
	}
	svc := &Service{
		cfg:   &config.Config{JWTRefreshTTL: ttl, SessionRenewInterval: interval},
		store: store.New(db),
	}
	return svc, store.New(db)
}

func makeSession(t *testing.T, st *store.Store, expiresAt time.Time) (string, uuid.UUID) {
	t.Helper()
	u := &models.User{Nickname: "renew-test", Status: models.StatusActive}
	if err := st.CreateUser(nil, u, models.RoleUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	s := models.Session{
		ID: idgen.New(), UserID: u.ID, RefreshTokenHash: "h",
		ExpiresAt: expiresAt, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.DB.Create(&s).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() {
		st.DB.Exec("DELETE FROM auth_sessions WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u.ID)
	})
	return u.ID, s.ID
}

func expiresAt(t *testing.T, st *store.Store, id uuid.UUID) time.Time {
	t.Helper()
	var s models.Session
	if err := st.DB.Where("id = ?", id).First(&s).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	return s.ExpiresAt
}

// 距上次延长已超过 interval 时应当续期（expires_at 前移）。
func TestSlidingRenewExtendsWhenStale(t *testing.T) {
	ttl, interval := 30*24*time.Hour, time.Hour
	svc, st := newRenewService(t, ttl, interval)
	// 会话还有 2 天到期 < (TTL - interval)，属于「该续期」的状态
	_, sid := makeSession(t, st, time.Now().Add(48*time.Hour))
	before := expiresAt(t, st, sid)

	renewed, err := svc.SlidingRenew(sid)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !renewed {
		t.Fatal("a stale session should be renewed")
	}
	after := expiresAt(t, st, sid)
	if !after.After(before) {
		t.Fatalf("expires_at should move forward: %v -> %v", before, after)
	}
	// 应被推到 now + TTL 附近（容许几秒误差）
	if d := time.Until(after); d < ttl-time.Minute {
		t.Fatalf("expires_at should be ~now+TTL, got %v from now", d)
	}
}

// 核心节流：刚延长过的会话再次请求不应再写库。
func TestSlidingRenewIsThrottled(t *testing.T) {
	ttl, interval := 30*24*time.Hour, 24*time.Hour
	svc, st := newRenewService(t, ttl, interval)
	// 刚创建：expires_at = now+TTL，落后不足 interval，因此不该续期
	_, sid := makeSession(t, st, time.Now().Add(ttl))
	before := expiresAt(t, st, sid)

	renewed, err := svc.SlidingRenew(sid)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed {
		t.Fatal("a freshly renewed session must not be renewed again (throttle)")
	}
	if after := expiresAt(t, st, sid); !after.Equal(before) {
		t.Fatalf("expires_at should be untouched, got %v -> %v", before, after)
	}
}

// 已吊销的会话不应被续期（续期不能复活一个已登出的会话）。
func TestSlidingRenewSkipsRevoked(t *testing.T) {
	svc, st := newRenewService(t, 30*24*time.Hour, time.Hour)
	_, sid := makeSession(t, st, time.Now().Add(48*time.Hour))
	now := time.Now()
	st.DB.Model(&models.Session{}).Where("id = ?", sid).
		Updates(map[string]any{"revoked_at": now, "updated_at": now})

	renewed, err := svc.SlidingRenew(sid)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed {
		t.Fatal("a revoked session must not be renewed")
	}
}

// 配置非法时关闭滑动（不做任何事），避免把每个请求都变成一次写入。
func TestSlidingRenewDisabledOnInvalidInterval(t *testing.T) {
	ttl := 30 * 24 * time.Hour
	for _, interval := range []time.Duration{0, -time.Hour, ttl, ttl + time.Hour} {
		svc, st := newRenewService(t, ttl, interval)
		_, sid := makeSession(t, st, time.Now().Add(48*time.Hour))
		before := expiresAt(t, st, sid)

		renewed, err := svc.SlidingRenew(sid)
		if err != nil {
			t.Fatalf("interval=%v: %v", interval, err)
		}
		if renewed {
			t.Fatalf("interval=%v should disable sliding", interval)
		}
		if after := expiresAt(t, st, sid); !after.Equal(before) {
			t.Fatalf("interval=%v: expires_at should be untouched", interval)
		}
	}
}

// 未知会话 id 不应报错，也不应影响任何行。
func TestSlidingRenewUnknownSession(t *testing.T) {
	svc, _ := newRenewService(t, 30*24*time.Hour, time.Hour)
	renewed, err := svc.SlidingRenew(uuid.New())
	if err != nil {
		t.Fatalf("unknown session should not error, got %v", err)
	}
	if renewed {
		t.Fatal("unknown session should not report a renewal")
	}
}

// 会话已过期时也不能被滑动复活：SessionByID 会先把它挡在门外，
// 但 SlidingRenew 自身也不应延长一个已过期的行。
func TestSlidingRenewDoesNotReviveExpired(t *testing.T) {
	svc, st := newRenewService(t, 30*24*time.Hour, time.Hour)
	_, sid := makeSession(t, st, time.Now().Add(-time.Minute))
	before := expiresAt(t, st, sid)

	renewed, err := svc.SlidingRenew(sid)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed {
		t.Fatal("an already-expired session should not be renewed")
	}
	if after := expiresAt(t, st, sid); !after.Equal(before) {
		t.Fatalf("expired session expires_at should be untouched")
	}
}
