package sessionstore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// 需要真实 redis：通过 REDIS_TEST_ADDR 提供地址，否则跳过。
// 白名单的轮换/吊销正确性取决于 Redis 里的原子脚本，用真 redis 验证才有意义。
func newTestStore(t *testing.T, ttl time.Duration) (*Store, *redis.Client) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if ttl <= 0 {
		ttl = time.Hour
	}
	return New(rdb, ttl), rdb
}

// 每个用例用独立 user id，避免相互干扰。
func uniqueUser(t *testing.T) string { return "u_" + t.Name() }

func TestIssueThenRotate(t *testing.T) {
	s, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	user := uniqueUser(t)
	t.Cleanup(func() { _ = s.RevokeUserAll(ctx, user) })

	oldRaw := "refresh-old-1"
	entry := Entry{UserID: user, ClientID: "cid-1"}
	if err := s.Issue(ctx, oldRaw, entry); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if n, _ := s.Count(ctx, user); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}

	// 轮换：旧键消失、新键可取。
	newRaw := "refresh-new-1"
	got, err := s.Rotate(ctx, oldRaw, newRaw)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if got.UserID != user || got.ClientID != "cid-1" {
		t.Fatalf("rotated entry mismatch: %+v", got)
	}
	if _, err := s.Lookup(ctx, oldRaw); err == nil {
		t.Fatal("old refresh should be gone after rotate")
	}
	if _, err := s.Lookup(ctx, newRaw); err != nil {
		t.Fatalf("new refresh should be present: %v", err)
	}
	// 用户索引应只剩新键：count 仍为 1。
	if n, _ := s.Count(ctx, user); n != 1 {
		t.Fatalf("count after rotate = %d, want 1", n)
	}
}

// 并发用同一枚旧 refresh 轮换：只能有一个成功，其余拿到 ErrNotFound。
// 这是 refresh 复用检测的基础。
func TestRotateIsSingleUse(t *testing.T) {
	s, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	user := uniqueUser(t)
	t.Cleanup(func() { _ = s.RevokeUserAll(ctx, user) })

	if err := s.Issue(ctx, "single-use", Entry{UserID: user}); err != nil {
		t.Fatalf("issue: %v", err)
	}

	const n = 8
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			_, err := s.Rotate(ctx, "single-use", "winner-"+string(rune('a'+i)))
			results <- err
		}(i)
	}
	ok := 0
	for i := 0; i < n; i++ {
		if err := <-results; err == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one rotate should succeed, got %d", ok)
	}
}

func TestRevokeByToken(t *testing.T) {
	s, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	user := uniqueUser(t)
	t.Cleanup(func() { _ = s.RevokeUserAll(ctx, user) })

	if err := s.Issue(ctx, "logout-me", Entry{UserID: user}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := s.RevokeByToken(ctx, "logout-me"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.Lookup(ctx, "logout-me"); err == nil {
		t.Fatal("refresh should be gone after revoke")
	}
	// 幂等：重复吊销不报错。
	if err := s.RevokeByToken(ctx, "logout-me"); err != nil {
		t.Fatalf("revoke twice should be idempotent: %v", err)
	}
}

func TestRevokeUserAllExceptKeepsCurrent(t *testing.T) {
	s, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	user := uniqueUser(t)
	t.Cleanup(func() { _ = s.RevokeUserAll(ctx, user) })

	// 三台设备
	for _, r := range []string{"dev-a", "dev-b", "dev-c"} {
		if err := s.Issue(ctx, r, Entry{UserID: user}); err != nil {
			t.Fatalf("issue %s: %v", r, err)
		}
	}
	// 改密：保留 dev-b，踢掉其余。
	if err := s.RevokeUserAllExcept(ctx, user, "dev-b"); err != nil {
		t.Fatalf("revoke except: %v", err)
	}
	if _, err := s.Lookup(ctx, "dev-b"); err != nil {
		t.Fatalf("kept refresh should survive: %v", err)
	}
	for _, r := range []string{"dev-a", "dev-c"} {
		if _, err := s.Lookup(ctx, r); err == nil {
			t.Fatalf("refresh %s should have been revoked", r)
		}
	}
	if n, _ := s.Count(ctx, user); n != 1 {
		t.Fatalf("count after revoke-except = %d, want 1", n)
	}
}

func TestRevokeUserAll(t *testing.T) {
	s, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	user := uniqueUser(t)

	for _, r := range []string{"x1", "x2"} {
		if err := s.Issue(ctx, r, Entry{UserID: user}); err != nil {
			t.Fatalf("issue: %v", err)
		}
	}
	if err := s.RevokeUserAll(ctx, user); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if n, _ := s.Count(ctx, user); n != 0 {
		t.Fatalf("count after revoke-all = %d, want 0", n)
	}
	for _, r := range []string{"x1", "x2"} {
		if _, err := s.Lookup(ctx, r); err == nil {
			t.Fatalf("refresh %s should be revoked", r)
		}
	}
}

func TestRevokeClientAll(t *testing.T) {
	s, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	user := uniqueUser(t)
	t.Cleanup(func() { _ = s.RevokeUserAll(ctx, user) })

	if err := s.Issue(ctx, "cl-1", Entry{UserID: user, ClientID: "app-1"}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := s.Issue(ctx, "cl-2", Entry{UserID: user, ClientID: "app-2"}); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := s.RevokeClientAll(ctx, "app-1"); err != nil {
		t.Fatalf("revoke client: %v", err)
	}
	if _, err := s.Lookup(ctx, "cl-1"); err == nil {
		t.Fatal("app-1 refresh should be revoked")
	}
	if _, err := s.Lookup(ctx, "cl-2"); err != nil {
		t.Fatalf("app-2 refresh should survive: %v", err)
	}
}

// nil store 的方法必须全部安全退化（供只跑 Postgres 的测试使用）。
func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	ctx := context.Background()
	if err := s.Issue(ctx, "r", Entry{UserID: "u"}); err != nil {
		t.Fatalf("nil issue: %v", err)
	}
	if _, err := s.Lookup(ctx, "r"); err != ErrNotFound {
		t.Fatalf("nil lookup want ErrNotFound, got %v", err)
	}
	if _, err := s.Rotate(ctx, "a", "b"); err != ErrNotFound {
		t.Fatalf("nil rotate want ErrNotFound, got %v", err)
	}
	if err := s.RevokeByToken(ctx, "r"); err != nil {
		t.Fatalf("nil revoke: %v", err)
	}
	if err := s.RevokeUserAll(ctx, "u"); err != nil {
		t.Fatalf("nil revoke all: %v", err)
	}
}