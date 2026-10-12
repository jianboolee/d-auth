package profile

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usercache"
	"github.com/redis/go-redis/v9"
)

// 需要真实 postgres（+ 可选的真实 redis）：通过 PG_TEST_DSN / REDIS_TEST_ADDR 提供，
// 否则跳过。缓存命中与失效无法用假对象验证——本包的整套逻辑就是「命中就跳过数据库」，
// 只有打真库真 redis 才能观察出来。
func newTestService(t *testing.T, withCache bool) (*Service, *store.Store, *redis.Client) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
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
	st := store.New(db)
	cfg := &config.Config{OSSCDNBase: "https://cdn.example.com"}

	var cache *usercache.Cache
	var rdb *redis.Client
	if withCache {
		addr := os.Getenv("REDIS_TEST_ADDR")
		if addr == "" {
			t.Skip("REDIS_TEST_ADDR not set")
		}
		rdb = redis.NewClient(&redis.Options{Addr: addr})
		cache = usercache.New(rdb, time.Minute)
		if !cache.Enabled() {
			t.Fatal("expected cache to be enabled")
		}
	}
	return New(cfg, st, cache), st, rdb
}

// uniquePhone 生成一个 (type, identifier) 上不冲突的手机号。
// auth_identities 对 (type, identifier) 有唯一索引，同一测试里建多个用户时
// 用同一个号码会直接撞索引——这也正是生产里的真实约束。
func uniquePhone(t *testing.T) string {
	t.Helper()
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return fmt.Sprintf("+8613%09d", binary.BigEndian.Uint32(b[:])%1_000_000_000)
}

// makeUser 建一个带手机号的用户并注册清理，同时返回手机号以便断言脱敏结果。
// cache 可为 nil；非 nil 时一并删掉该用户的缓存键——测试与开发共用同一个 Redis，
// 不该留下指向已删除用户的条目。
func makeUser(t *testing.T, st *store.Store, cache *usercache.Cache, nickname string, roles ...string) (string, string) {
	t.Helper()
	if len(roles) == 0 {
		roles = []string{models.RoleUser}
	}
	u := &models.User{Nickname: nickname, Status: models.StatusActive}
	if err := st.CreateUser(nil, u, roles...); err != nil {
		t.Fatalf("create user: %v", err)
	}
	phone := uniquePhone(t)
	now := time.Now()
	if err := st.AddIdentity(nil, &models.Identity{
		UserID: u.ID, Type: models.IdentityPhone, Identifier: phone, VerifiedAt: &now,
	}); err != nil {
		t.Fatalf("add identity: %v", err)
	}
	t.Cleanup(func() {
		cache.Del(context.Background(), u.ID)
		st.DB.Exec("DELETE FROM auth_identities WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u.ID)
	})
	return u.ID, phone
}

// 改昵称（绕过服务，模拟「别的写路径」）后，第二次读取若返回旧昵称，
// 就证明第一次读取的结果确实来自缓存而不是数据库。
func TestCacheHitSkipsDatabase(t *testing.T) {
	svc, st, rdb := newTestService(t, true)
	t.Cleanup(func() { rdb.Close() })
	ctx := context.Background()
	id, _ := makeUser(t, st, svc.cache, "原名")

	first, err := svc.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if first.Nickname != "原名" {
		t.Fatalf("first read nickname = %q", first.Nickname)
	}

	// 直接改库，不经过任何会调用 cache.Del 的写路径。
	if err := st.DB.Model(&models.User{}).Where("id = ?", id).Update("nickname", "新名").Error; err != nil {
		t.Fatalf("update: %v", err)
	}

	second, err := svc.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if second.Nickname != "原名" {
		t.Fatalf("second read = %q; want the cached 原名 (a miss would have returned 新名)", second.Nickname)
	}
}

// 显式失效后必须回源，拿到新值。
func TestDelInvalidates(t *testing.T) {
	svc, st, rdb := newTestService(t, true)
	t.Cleanup(func() { rdb.Close() })
	ctx := context.Background()
	id, _ := makeUser(t, st, svc.cache, "原名")

	if _, err := svc.UserByID(ctx, id); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	if err := st.DB.Model(&models.User{}).Where("id = ?", id).Update("nickname", "新名").Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	svc.cache.Del(ctx, id)

	got, err := svc.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("read after del: %v", err)
	}
	if got.Nickname != "新名" {
		t.Fatalf("after Del nickname = %q, want 新名", got.Nickname)
	}
}

// 关掉缓存（cache=nil）时仍然可用，且每次都读新值。
func TestWorksWithoutCache(t *testing.T) {
	svc, st, _ := newTestService(t, false)
	ctx := context.Background()
	id, _ := makeUser(t, st, nil, "原名")

	if _, err := svc.UserByID(ctx, id); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := st.DB.Model(&models.User{}).Where("id = ?", id).Update("nickname", "新名").Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := svc.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Nickname != "新名" {
		t.Fatalf("without cache nickname = %q, want 新名", got.Nickname)
	}
}

// 批量：顺序与入参 ids 一致，缺失的 id 静默跳过。
func TestProfilesOrderAndSkipsMissing(t *testing.T) {
	svc, st, _ := newTestService(t, false)
	ctx := context.Background()
	a, _ := makeUser(t, st, nil, "甲")
	b, _ := makeUser(t, st, nil, "乙")
	missing := idgen.NewUserID()

	got, err := svc.Profiles(ctx, []string{b, missing, a})
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d profiles, want 2 (missing id should be skipped)", len(got))
	}
	if got[0].Nickname != "乙" || got[1].Nickname != "甲" {
		t.Fatalf("order not preserved: %q %q", got[0].Nickname, got[1].Nickname)
	}
}

// 批量在缓存开启时也要能返回完整结果（命中 + 回填混合路径）。
func TestProfilesWithCache(t *testing.T) {
	svc, st, rdb := newTestService(t, true)
	t.Cleanup(func() { rdb.Close() })
	ctx := context.Background()
	a, _ := makeUser(t, st, svc.cache, "甲")
	b, _ := makeUser(t, st, svc.cache, "乙")

	// 先只预热 a，让这次批量查询同时走「命中」与「回源补齐」两条分支。
	if _, err := svc.UserByID(ctx, a); err != nil {
		t.Fatalf("warm a: %v", err)
	}
	got, err := svc.Profiles(ctx, []string{a, b})
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d profiles, want 2", len(got))
	}
	for _, p := range got {
		if p.Nickname != "甲" && p.Nickname != "乙" {
			t.Fatalf("unexpected nickname %q", p.Nickname)
		}
	}
	// b 应已被回填：改库后仍返回旧值即证明命中了缓存。
	if err := st.DB.Model(&models.User{}).Where("id = ?", b).Update("nickname", "乙改").Error; err != nil {
		t.Fatalf("update: %v", err)
	}
	again, err := svc.Profiles(ctx, []string{b})
	if err != nil {
		t.Fatalf("second profiles: %v", err)
	}
	if again[0].Nickname != "乙" {
		t.Fatalf("filled entry = %q, want cached 乙", again[0].Nickname)
	}
}

// 公开资料必须脱敏、且不含角色明细（is_admin 只反映是否管理角色）。
func TestProfileIsMaskedAndRolesCollapsed(t *testing.T) {
	svc, st, _ := newTestService(t, false)
	ctx := context.Background()
	id, phone := makeUser(t, st, nil, "管理员", models.RoleUser, models.RoleAdmin)

	p, err := svc.ProfileByID(ctx, id)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	// 脱敏后保留末 4 位、遮住中间，且绝不等于明文。
	if p.Phone == phone || !strings.Contains(p.Phone, "****") {
		t.Fatalf("phone = %q, want a masked form of %q", p.Phone, phone)
	}
	if !strings.HasSuffix(p.Phone, phone[len(phone)-4:]) {
		t.Fatalf("phone = %q should keep the last 4 digits of %q", p.Phone, phone)
	}
	if !p.IsAdmin {
		t.Fatal("expected is_admin true for an admin user")
	}
	if !p.HasPhone {
		t.Fatal("expected has_phone true")
	}

	plain, err := svc.UserByID(ctx, id)
	if err != nil {
		t.Fatalf("internal read: %v", err)
	}
	if plain.Phone != phone {
		t.Fatalf("internal read phone = %q, want plaintext %q (internal is the trusted path)", plain.Phone, phone)
	}
}

// 查不到的用户返回 NOT_FOUND，而不是零值资料。
func TestNotFound(t *testing.T) {
	svc, _, _ := newTestService(t, false)
	ctx := context.Background()

	_, err := svc.UserByID(ctx, idgen.NewUserID())
	if code, _, ok := apperr.CodeOf(err); !ok || code != "NOT_FOUND" {
		t.Fatalf("UserByID err = %v (code %q), want NOT_FOUND", err, code)
	}
	_, err = svc.ProfileByIdentity(ctx, models.IdentityPhone, "+8613900000000")
	if code, _, ok := apperr.CodeOf(err); !ok || code != "NOT_FOUND" {
		t.Fatalf("ProfileByIdentity err = %v (code %q), want NOT_FOUND", err, code)
	}
}

// 按手机号/邮箱匹配：命中返回本人，未绑定该类型则不匹配。
func TestByIdentity(t *testing.T) {
	svc, st, _ := newTestService(t, false)
	ctx := context.Background()
	id, phone := makeUser(t, st, nil, "甲")

	p, err := svc.ProfileByIdentity(ctx, models.IdentityPhone, phone)
	if err != nil {
		t.Fatalf("match phone: %v", err)
	}
	if p.ID != id {
		t.Fatalf("matched id = %q, want %q", p.ID, id)
	}
	// 该用户没有邮箱身份，按邮箱不应匹配到它。
	if _, err := svc.ProfileByIdentity(ctx, models.IdentityEmail, "nobody@example.com"); err == nil {
		t.Fatal("expected NOT_FOUND for an unbound email")
	}
	// 未注册的手机号同样匹配不到。
	if _, err := svc.ProfileByIdentity(ctx, models.IdentityPhone, "+8613999999999"); err == nil {
		t.Fatal("expected NOT_FOUND for an unregistered phone")
	}
}

// 空 ID 直接当作不存在，不要拿空串去查库。
func TestEmptyIDIsNotFound(t *testing.T) {
	svc, _, _ := newTestService(t, false)
	if _, err := svc.UserByID(context.Background(), ""); err == nil {
		t.Fatal("expected NOT_FOUND for an empty id")
	}
}
