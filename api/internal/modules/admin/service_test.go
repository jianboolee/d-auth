package admin

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/normalize"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
// 应用改名/删除涉及唯一索引与实际的外键约束，只有打真库才能验证：
// auth_sessions.client_id 没有 ON DELETE，直接删会不会撞外键是这次的核心疑问。
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
	st := store.New(db)
	cfg := &config.Config{MasterKey: "test-master-key-please-change-32b!!"}
	// 传 nil 缓存：这些用例只关心客户端生命周期，不涉及用户资料读取；
	// usercache 的所有方法都接受 nil 接收者。
	return New(cfg, st, nil), st
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	if c, _, ok := apperr.CodeOf(err); ok {
		return c
	}
	return "UNCODED"
}

// makeClient 建一个应用；key 用随机后缀保证同一库上重复跑不撞唯一索引。
func makeClient(t *testing.T, svc *Service, key string) *models.Client {
	t.Helper()
	cl, err := svc.CreateClient(ClientInput{
		ClientKey: key,
		Name:      "测试应用",
		Type:      "web",
	})
	if err != nil {
		t.Fatalf("create client %s: %v", key, err)
	}
	t.Cleanup(func() {
		// 用例可能已把它删掉；这里只兜底清理，忽略 NotFound。
		_ = svc.DeleteClient(cl.ID)
	})
	return cl
}

func uniqueKey(prefix string) string {
	return prefix + "_" + uuid.NewString()[:8]
}

// 改名后新标识能查到、旧标识查不到——证明 UpdateClient 真的把 client_key 写下去了，
// 而不像改动前那样静默忽略。
func TestUpdateClientRenamesKey(t *testing.T) {
	svc, st := newTestService(t)
	oldKey := uniqueKey("rename_from")
	cl := makeClient(t, svc, oldKey)
	newKey := uniqueKey("rename_to")

	if _, err := svc.UpdateClient(cl.ID, ClientInput{ClientKey: newKey}); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if _, err := st.ClientByKey(newKey); err != nil {
		t.Errorf("new key not found after rename: %v", err)
	}
	if _, err := st.ClientByKey(oldKey); err == nil {
		t.Errorf("old key %q still resolvable after rename", oldKey)
	}
}

// 改成已被占用的标识应归为冲突，而不是 500。
func TestUpdateClientRejectsDuplicateKey(t *testing.T) {
	svc, _ := newTestService(t)
	other := makeClient(t, svc, uniqueKey("taken"))
	target := makeClient(t, svc, uniqueKey("mover"))

	_, err := svc.UpdateClient(target.ID, ClientInput{ClientKey: other.ClientKey})
	if got := codeOf(err); got != "CLIENT_TAKEN" {
		t.Fatalf("want CLIENT_TAKEN, got %q (err=%v)", got, err)
	}
}

// 非法标识要挡在写入之前：这个值会作为请求头往返，空白/换行本身就是隐患。
func TestUpdateClientRejectsInvalidKey(t *testing.T) {
	svc, _ := newTestService(t)
	cl := makeClient(t, svc, uniqueKey("valid"))

	for _, bad := range []string{"Has Space", "UPPER", "-leading", "a", "中文"} {
		_, err := svc.UpdateClient(cl.ID, ClientInput{ClientKey: bad})
		if got := codeOf(err); got != "INVALID_CLIENT_KEY" {
			t.Errorf("key %q: want INVALID_CLIENT_KEY, got %q (err=%v)", bad, got, err)
		}
	}
}

// 建应用时同样校验标识格式。
func TestCreateClientRejectsInvalidKey(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.CreateClient(ClientInput{ClientKey: "Bad Key", Name: "x"})
	if got := codeOf(err); got != "INVALID_CLIENT_KEY" {
		t.Fatalf("want INVALID_CLIENT_KEY, got %q (err=%v)", got, err)
	}
}

// 重复的 client_key 应报冲突。这条同时守住 CreateClient 的一个既有缺陷：
// 原先把任何库错误都说成「已存在」。
func TestCreateClientRejectsDuplicateKey(t *testing.T) {
	svc, _ := newTestService(t)
	first := makeClient(t, svc, uniqueKey("dup"))

	_, err := svc.CreateClient(ClientInput{ClientKey: first.ClientKey, Name: "又一个"})
	if got := codeOf(err); got != "CLIENT_TAKEN" {
		t.Fatalf("want CLIENT_TAKEN, got %q (err=%v)", got, err)
	}
}

// 停用是这次「下线应用」的主要手段，所以要验证它真的生效：ClientByKey 只认
// active，停用后按标识就查不到了（小程序换会话会因此报「未知应用」）。
func TestUpdateClientDisableStopsLookup(t *testing.T) {
	svc, st := newTestService(t)
	cl := makeClient(t, svc, uniqueKey("toggle"))

	if _, err := svc.UpdateClient(cl.ID, ClientInput{Status: models.StatusDisabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := st.ClientByKey(cl.ClientKey); err == nil {
		t.Errorf("disabled client %q is still resolvable by key", cl.ClientKey)
	}

	// 重新启用后应恢复
	if _, err := svc.UpdateClient(cl.ID, ClientInput{Status: models.StatusActive}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if _, err := st.ClientByKey(cl.ClientKey); err != nil {
		t.Errorf("re-enabled client not resolvable: %v", err)
	}
}

// 删除应用时，引用它的会话必须存活且 client_id 被置空。
// 这是整个改动的关键点：auth_sessions.client_id 没有 ON DELETE 级联，若不先解绑，
// DELETE 会直接撞外键失败。
func TestDeleteClientKeepsSessionsAndNullsClientID(t *testing.T) {
	svc, st := newTestService(t)
	cl := makeClient(t, svc, uniqueKey("del"))

	user := &models.User{Nickname: "会话归属测试", Status: models.StatusActive}
	if err := st.CreateUser(nil, user, models.RoleUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sess := &models.Session{
		ID:               uuid.New(),
		UserID:           user.ID,
		ClientID:         &cl.ID,
		RefreshTokenHash: "hash",
		ExpiresAt:        time.Now().Add(time.Hour),
	}
	if err := st.DB.Create(sess).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := svc.DeleteClient(cl.ID); err != nil {
		t.Fatalf("delete client: %v", err)
	}

	if _, err := st.ClientByID(cl.ID); err == nil {
		t.Errorf("client row still present after delete")
	}
	var got models.Session
	if err := st.DB.Where("id = ?", sess.ID).First(&got).Error; err != nil {
		t.Fatalf("session was deleted along with the client: %v", err)
	}
	if got.ClientID != nil {
		t.Errorf("session client_id = %v, want NULL", *got.ClientID)
	}

	// 清理会话，避免同一库上反复跑时堆积
	t.Cleanup(func() {
		st.DB.Where("user_id = ?", user.ID).Delete(&models.Session{})
		st.DB.Where("id = ?", user.ID).Delete(&models.User{})
	})
}

// 控制台自身的应用禁止改名与删除：前端硬编码了 web_console，改动后要重新部署才能恢复。
func TestConsoleClientIsProtected(t *testing.T) {
	svc, st := newTestService(t)

	// 迁移里 seed 了 web_console，确保它存在并拿到 id
	console, err := st.ClientByKey(models.ClientKeyConsole)
	if err != nil {
		t.Fatalf("seeded console client missing: %v", err)
	}

	if _, err := svc.UpdateClient(console.ID, ClientInput{ClientKey: uniqueKey("hijack")}); codeOf(err) != "CLIENT_PROTECTED" {
		t.Errorf("rename console: want CLIENT_PROTECTED, got %q (err=%v)", codeOf(err), err)
	}
	if err := svc.DeleteClient(console.ID); codeOf(err) != "CLIENT_PROTECTED" {
		t.Errorf("delete console: want CLIENT_PROTECTED, got %q (err=%v)", codeOf(err), err)
	}

	// 保护只针对标识与删除；名称等字段仍应可改
	if _, err := svc.UpdateClient(console.ID, ClientInput{Name: "控制台"}); err != nil {
		t.Errorf("renaming the console's display name should be allowed: %v", err)
	}
	// 用例改了 seed 行的名称，跑完恢复，避免影响手动验证
	t.Cleanup(func() {
		svc.UpdateClient(console.ID, ClientInput{Name: "d-auth 控制台"})
	})
}

// virtualPhoneSeq 保证同一进程内每次调用都拿到不同的后缀，避免仅靠时间戳时的偶然重复。
var virtualPhoneSeq atomic.Int64

// uniqueVirtualPhone 造一个 120 开头的虚拟号，避免同一库上重复跑时撞唯一索引。
// 必须全是数字：AdminPhone 对非真实号段只接受纯数字，用 uuid 片段会带上十六进制字母。
func uniqueVirtualPhone() string {
	n := time.Now().UnixNano() + virtualPhoneSeq.Add(1)
	return "120" + fmt.Sprintf("%08d", n%100000000)
}

// makeTimedUser 建一个用户并把 created_at 改成指定时间。
// CreateUser 内部写死 now，时间筛选的用例必须能控制这个值才能断言区间边界。
func makeTimedUser(t *testing.T, svc *Service, st *store.Store, nickname string, createdAt time.Time, lastLogin *time.Time) *models.User {
	t.Helper()
	u := &models.User{Nickname: nickname, Status: models.StatusActive}
	if err := st.CreateUser(nil, u, models.RoleUser); err != nil {
		t.Fatalf("create user %s: %v", nickname, err)
	}
	if err := st.DB.Model(&models.User{}).Where("id = ?", u.ID).
		Updates(map[string]any{"created_at": createdAt, "last_login_at": lastLogin}).Error; err != nil {
		t.Fatalf("set times on %s: %v", nickname, err)
	}
	t.Cleanup(func() {
		// 标识没有级联，先删标识与角色再删用户
		st.DB.Where("user_id = ?", u.ID).Delete(&models.Identity{})
		st.DB.Where("user_id = ?", u.ID).Delete(&models.UserRole{})
		st.DB.Where("id = ?", u.ID).Delete(&models.User{})
	})
	return u
}

// pickNicknames 从结果里挑出指定昵称的出现顺序，便于断言排序结果。
func pickNicknames(users []dto.User, want map[string]bool) []string {
	var got []string
	for _, u := range users {
		if want[u.Nickname] {
			got = append(got, u.Nickname)
		}
	}
	return got
}

// 排序字段是拼进 ORDER BY 的，必须走白名单：直接拼请求参数就是注入。
// 非法字段、非法方向、非法时间格式都要给出可读的 400，而不是 500 或静默忽略。
func TestListUsersRejectsInvalidQuery(t *testing.T) {
	svc, _ := newTestService(t)
	cases := []struct {
		name string
		q    UserListQuery
		want string
	}{
		{"sort injection", UserListQuery{Sort: "created_at; DROP TABLE auth_users"}, "INVALID_SORT"},
		{"unknown sort column", UserListQuery{Sort: "password_hash"}, "INVALID_SORT"},
		{"bad order", UserListQuery{Sort: "created_at", Order: "sideways"}, "INVALID_ORDER"},
		{"bad created_from", UserListQuery{CreatedFrom: "2026-01-01"}, "INVALID_TIME"},
		{"bad created_to", UserListQuery{CreatedTo: "yesterday"}, "INVALID_TIME"},
		{
			"reversed range",
			UserListQuery{CreatedFrom: "2026-02-01T00:00:00Z", CreatedTo: "2026-01-01T00:00:00Z"},
			"INVALID_TIME_RANGE",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := svc.ListUsers(c.q); codeOf(err) != c.want {
				t.Fatalf("want %s, got %q (err=%v)", c.want, codeOf(err), err)
			}
		})
	}
}

// 时间筛选按半开区间 [from, to) 过滤注册时间。
func TestListUsersFiltersByCreatedRange(t *testing.T) {
	svc, st := newTestService(t)

	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	inRange := makeTimedUser(t, svc, st, "range-in", base, nil)
	before := makeTimedUser(t, svc, st, "range-before", base.Add(-72*time.Hour), nil)
	after := makeTimedUser(t, svc, st, "range-after", base.Add(72*time.Hour), nil)
	want := map[string]bool{inRange.Nickname: true, before.Nickname: true, after.Nickname: true}

	list, _, err := svc.ListUsers(UserListQuery{
		CreatedFrom: base.Add(-24 * time.Hour).Format(time.RFC3339),
		CreatedTo:   base.Add(24 * time.Hour).Format(time.RFC3339),
		Size:        100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := pickNicknames(list, want)
	if len(got) != 1 || got[0] != inRange.Nickname {
		t.Fatalf("range filter picked %v, want only %q", got, inRange.Nickname)
	}

	// 端点语义是「含左不含右」：恰好在 to 上的记录应被排除。
	exact, _, err := svc.ListUsers(UserListQuery{
		CreatedFrom: base.Add(-24 * time.Hour).Format(time.RFC3339),
		CreatedTo:   base.Format(time.RFC3339),
		Size:        100,
	})
	if err != nil {
		t.Fatalf("list boundary: %v", err)
	}
	if got := pickNicknames(exact, want); len(got) != 0 {
		t.Fatalf("exclusive right bound leaked rows: %v", got)
	}
}

// 注册时间与昵称都能双向排序。
func TestListUsersSorts(t *testing.T) {
	svc, st := newTestService(t)
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	// 昵称顺序与时间顺序刻意相反，两个维度的断言才会互相独立
	older := makeTimedUser(t, svc, st, "sort-zeta", base, nil)
	newer := makeTimedUser(t, svc, st, "sort-alpha", base.Add(time.Hour), nil)
	want := map[string]bool{older.Nickname: true, newer.Nickname: true}

	list, _, err := svc.ListUsers(UserListQuery{Sort: "created_at", Order: "asc", Size: 100})
	if err != nil {
		t.Fatalf("asc: %v", err)
	}
	if got := pickNicknames(list, want); len(got) != 2 || got[0] != older.Nickname {
		t.Fatalf("created_at asc = %v, want [%s %s]", got, older.Nickname, newer.Nickname)
	}

	list, _, err = svc.ListUsers(UserListQuery{Sort: "created_at", Order: "desc", Size: 100})
	if err != nil {
		t.Fatalf("desc: %v", err)
	}
	if got := pickNicknames(list, want); len(got) != 2 || got[0] != newer.Nickname {
		t.Fatalf("created_at desc = %v, want [%s %s]", got, newer.Nickname, older.Nickname)
	}

	// 昵称升序应把 sort-alpha 放前面
	list, _, err = svc.ListUsers(UserListQuery{Sort: "nickname", Order: "asc", Size: 100})
	if err != nil {
		t.Fatalf("by nickname: %v", err)
	}
	if got := pickNicknames(list, want); len(got) != 2 || got[0] != newer.Nickname {
		t.Fatalf("nickname asc = %v, want [%s %s]", got, newer.Nickname, older.Nickname)
	}
}

// 从未登录的用户 last_login_at 为 NULL。不特殊处理时 Postgres 在 DESC 下把
// NULL 排在最前，第一屏会全是「—」，所以要求空值固定在末尾。
func TestListUsersSortByLastLoginPutsNullsLast(t *testing.T) {
	svc, st := newTestService(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	loggedIn := base.Add(time.Hour)
	never := makeTimedUser(t, svc, st, "zzz-never-login", base, nil)
	did := makeTimedUser(t, svc, st, "sort-logged-in", base, &loggedIn)
	want := map[string]bool{never.Nickname: true, did.Nickname: true}

	for _, order := range []string{"desc", "asc"} {
		list, _, err := svc.ListUsers(UserListQuery{Sort: "last_login_at", Order: order, Size: 100})
		if err != nil {
			t.Fatalf("%s: %v", order, err)
		}
		got := pickNicknames(list, want)
		if len(got) != 2 {
			t.Fatalf("%s: got %v, want both rows", order, got)
		}
		if got[len(got)-1] != never.Nickname {
			t.Errorf("%s: NULL last_login_at should sort last, got %v", order, got)
		}
	}
}

// 默认查询（不带 sort/order）要保持原行为：按注册时间倒序。
func TestListUsersDefaultOrderIsNewestFirst(t *testing.T) {
	svc, st := newTestService(t)
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	older := makeTimedUser(t, svc, st, "default-older", base, nil)
	newer := makeTimedUser(t, svc, st, "default-newer", base.Add(time.Hour), nil)
	want := map[string]bool{older.Nickname: true, newer.Nickname: true}

	list, _, err := svc.ListUsers(UserListQuery{Size: 100})
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if got := pickNicknames(list, want); len(got) != 2 || got[0] != newer.Nickname {
		t.Fatalf("default order = %v, want newest first [%s %s]", got, newer.Nickname, older.Nickname)
	}
}

// 排序字段大量重复（同一秒注册）时，必须靠 id 兜底才能保证翻页稳定；
// 否则同一个用户可能在两页各出现一次、另一个用户完全不出现。
func TestListUsersPaginationIsStableWithTiedSortKeys(t *testing.T) {
	svc, st := newTestService(t)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	want := map[string]bool{}
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("tie-%d", i)
		// 全部落在同一时刻，制造完全相同的排序键
		makeTimedUser(t, svc, st, name, base, nil)
		want[name] = true
	}

	seen := map[string]int{}
	for page := 1; page <= 2; page++ {
		list, _, err := svc.ListUsers(UserListQuery{Sort: "created_at", Order: "desc", Page: page, Size: 3})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, u := range pickNicknames(list, want) {
			seen[u]++
		}
	}
	if len(seen) != 6 {
		t.Fatalf("two pages covered %d of 6 tied users: %v", len(seen), seen)
	}
	for name, n := range seen {
		if n != 1 {
			t.Errorf("user %s appeared %d times across pages", name, n)
		}
	}
}

// 后台建号允许 120 开头的虚拟号：通过 Phone 会被拒，走 CreateUser 应该能建出来，
// 且标识按原值存下来（不被套上 +86）。
func TestCreateUserWithVirtualPhone(t *testing.T) {
	svc, st := newTestService(t)
	phone := uniqueVirtualPhone()
	// 先用严格校验确认它确实不是「真实号码」——这是整个改动的前提。
	if _, ok := normalize.Phone(phone); ok {
		t.Fatalf("test phone %q unexpectedly passes strict normalize.Phone", phone)
	}

	u, err := svc.CreateUser(CreateUserInput{Nickname: "虚拟号用户", Phone: phone})
	if err != nil {
		t.Fatalf("create user with virtual phone: %v", err)
	}
	if u.Phone != phone {
		t.Errorf("stored phone = %q, want %q (原值保留)", u.Phone, phone)
	}

	// 登录标识里的值应可直接反查，且严格校验仍然拒绝用它登录。
	if _, err := st.FindIdentity(models.IdentityPhone, phone); err != nil {
		t.Errorf("virtual phone identity not persisted: %v", err)
	}
	t.Cleanup(func() {
		id, perr := idgen.ParseUserID(u.ID)
		if perr != nil {
			return
		}
		// auth_identities.user_id 没有级联，先删标识再删用户
		st.DB.Where("user_id = ?", id).Delete(&models.Identity{})
		st.DB.Where("user_id = ?", id).Delete(&models.UserRole{})
		st.DB.Where("id = ?", id).Delete(&models.User{})
	})
}

// 虚拟号同样是唯一标识：重复建号要报 PHONE_TAKEN，而不是撞唯一索引变 500。
func TestCreateUserRejectsDuplicateVirtualPhone(t *testing.T) {
	svc, st := newTestService(t)
	phone := uniqueVirtualPhone()

	first, err := svc.CreateUser(CreateUserInput{Nickname: "先占用", Phone: phone})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	t.Cleanup(func() {
		id, perr := idgen.ParseUserID(first.ID)
		if perr != nil {
			return
		}
		st.DB.Where("user_id = ?", id).Delete(&models.Identity{})
		st.DB.Where("user_id = ?", id).Delete(&models.UserRole{})
		st.DB.Where("id = ?", id).Delete(&models.User{})
	})

	_, err = svc.CreateUser(CreateUserInput{Nickname: "重复占用", Phone: phone})
	if got := codeOf(err); got != "PHONE_TAKEN" {
		t.Fatalf("want PHONE_TAKEN, got %q (err=%v)", got, err)
	}
}

// 生成器要给出 120 开头的 11 位号码，且不能返回已被占用的号。
func TestGenerateVirtualPhone(t *testing.T) {
	svc, st := newTestService(t)

	phone, err := svc.GenerateVirtualPhone()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(phone) != 11 || phone[:3] != "120" {
		t.Fatalf("generated %q, want 11 digits starting with 120", phone)
	}
	if _, ok := normalize.AdminPhone(phone); !ok {
		t.Errorf("generated %q is not accepted by normalize.AdminPhone", phone)
	}

	// 把这个号占用掉，再生成若干次都不应该撞上它。
	u, err := svc.CreateUser(CreateUserInput{Nickname: "占位", Phone: phone})
	if err != nil {
		t.Fatalf("occupy generated phone: %v", err)
	}
	t.Cleanup(func() {
		id, perr := idgen.ParseUserID(u.ID)
		if perr != nil {
			return
		}
		st.DB.Where("user_id = ?", id).Delete(&models.Identity{})
		st.DB.Where("user_id = ?", id).Delete(&models.UserRole{})
		st.DB.Where("id = ?", id).Delete(&models.User{})
	})

	for i := 0; i < 20; i++ {
		next, err := svc.GenerateVirtualPhone()
		if err != nil {
			t.Fatalf("generate #%d: %v", i, err)
		}
		if next == phone {
			t.Fatalf("generator returned the occupied phone %q", phone)
		}
	}
}

// 后台详情页展示 updated_at，所以「哪些操作会推进它」需要是可预期的：
// 改昵称/状态、重置密码会推进；改绑手机号写的是 auth_identities，不推进
// auth_users.updated_at（这是仓库既有约定，自助改绑也一样）。
// 这里把语义钉住，避免以后有人以为它覆盖了所有变更。
func TestUpdatedAtAdvancesOnUserRowWrites(t *testing.T) {
	svc, st := newTestService(t)
	u := makeTimedUser(t, svc, st, "touch-me", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)

	readUpdated := func() time.Time {
		var got models.User
		if err := st.DB.Where("id = ?", u.ID).First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		return got.UpdatedAt
	}
	// 把时间戳压回过去，方便断言「确实被推到更新」
	rewind := func() time.Time {
		past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := st.DB.Model(&models.User{}).Where("id = ?", u.ID).
			Update("updated_at", past).Error; err != nil {
			t.Fatalf("rewind: %v", err)
		}
		return past
	}

	// 改昵称应推进
	before := rewind()
	nick := "touched-nick"
	if _, err := svc.PatchUser(u.ID, PatchUserInput{Nickname: &nick}); err != nil {
		t.Fatalf("patch nickname: %v", err)
	}
	if after := readUpdated(); !after.After(before) {
		t.Errorf("nickname change should advance updated_at: %v -> %v", before, after)
	}

	// 重置密码应推进
	before = rewind()
	if err := svc.ResetPassword(u.ID, "newpassword123"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if after := readUpdated(); !after.After(before) {
		t.Errorf("password reset should advance updated_at: %v -> %v", before, after)
	}

	// 改绑手机号只写身份表，约定上不推进——守住这条以免语义漂移
	before = rewind()
	if _, err := svc.SetUserIdentity(u.ID, models.IdentityPhone, uniqueVirtualPhone()); err != nil {
		t.Fatalf("set phone: %v", err)
	}
	if after := readUpdated(); !after.Equal(before) {
		t.Errorf("identity rebind writes auth_identities and should not bump auth_users.updated_at: %v -> %v", before, after)
	}
}

// 后台 patch 现在也能改 bio / gender（供业务端代客改资料，与自助端 PATCH /me
// 同语义）。这里钉住：字段确实落库，且未传的字段（指针为 nil）保持原值——
// 否则一次只想改昵称的调用会把 bio/gender 抹成零值。
func TestPatchUserUpdatesBioAndGenderWithoutClobberingOthers(t *testing.T) {
	svc, st := newTestService(t)
	u := makeTimedUser(t, svc, st, "patch-bio", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)

	// 先写入初始 bio / gender，再验证 patch 只动传入的字段。
	seedBio := "seed-bio"
	seedGender := int16(1)
	if _, err := svc.PatchUser(u.ID, PatchUserInput{Bio: &seedBio, Gender: &seedGender}); err != nil {
		t.Fatalf("seed bio/gender: %v", err)
	}

	// 只改 bio，gender 不应被零值覆盖。
	newBio := "updated-bio"
	got, err := svc.PatchUser(u.ID, PatchUserInput{Bio: &newBio})
	if err != nil {
		t.Fatalf("patch bio: %v", err)
	}
	if got.Bio != newBio {
		t.Errorf("bio = %q, want %q", got.Bio, newBio)
	}
	if got.Gender != seedGender {
		t.Errorf("gender = %d, want %d (untouched field must not be zeroed)", got.Gender, seedGender)
	}

	// 只改 gender，bio 应保留。
	newGender := int16(2)
	got, err = svc.PatchUser(u.ID, PatchUserInput{Gender: &newGender})
	if err != nil {
		t.Fatalf("patch gender: %v", err)
	}
	if got.Gender != newGender {
		t.Errorf("gender = %d, want %d", got.Gender, newGender)
	}
	if got.Bio != newBio {
		t.Errorf("bio = %q, want %q (untouched field must not be zeroed)", got.Bio, newBio)
	}
}

// makeSession 为指定用户建一条会话，便于验证会话分页。
func makeSession(t *testing.T, st *store.Store, userID string, ip string, createdAt time.Time, revoked bool) *models.Session {
	t.Helper()
	s := &models.Session{
		ID:               uuid.New(),
		UserID:           userID,
		RefreshTokenHash: uuid.NewString(),
		IP:               ip,
		UserAgent:        "test-agent",
		ExpiresAt:        createdAt.Add(24 * time.Hour),
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
	if revoked {
		rev := createdAt.Add(time.Minute)
		s.RevokedAt = &rev
	}
	if err := st.DB.Create(s).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	return s
}

// 会话分页：按创建时间倒序，total 是全部条数而不是当前页条数。
func TestListUserSessionsPaginates(t *testing.T) {
	svc, st := newTestService(t)
	u := makeTimedUser(t, svc, st, "sessions-paging", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	t.Cleanup(func() { st.DB.Where("user_id = ?", u.ID).Delete(&models.Session{}) })

	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	// 5 条会话，创建时间依次递增，倒序后顺序可预期
	for i := 0; i < 5; i++ {
		makeSession(t, st, u.ID, fmt.Sprintf("10.0.0.%d", i), base.Add(time.Duration(i)*time.Minute), false)
	}

	page1, total, err := svc.ListUserSessions(u.ID, 1, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5 (all rows, not just this page)", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 size = %d, want 2", len(page1))
	}
	// 倒序：最新一条在前
	if page1[0].IP != "10.0.0.4" || page1[1].IP != "10.0.0.3" {
		t.Fatalf("page 1 = %s,%s want 10.0.0.4,10.0.0.3", page1[0].IP, page1[1].IP)
	}

	page3, total3, err := svc.ListUserSessions(u.ID, 3, 2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if total3 != 5 {
		t.Fatalf("page 3 total = %d, want 5", total3)
	}
	if len(page3) != 1 || page3[0].IP != "10.0.0.0" {
		t.Fatalf("last page = %v, want the single oldest row 10.0.0.0", page3)
	}
}

// 后台是审计视角：已失效/已下线的会话同样要返回，否则查不到「为什么被踢」。
func TestListUserSessionsIncludesRevoked(t *testing.T) {
	svc, st := newTestService(t)
	u := makeTimedUser(t, svc, st, "sessions-revoked", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	t.Cleanup(func() { st.DB.Where("user_id = ?", u.ID).Delete(&models.Session{}) })

	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	live := makeSession(t, st, u.ID, "10.0.1.1", base, false)
	dead := makeSession(t, st, u.ID, "10.0.1.2", base.Add(time.Minute), true)

	list, total, err := svc.ListUserSessions(u.ID, 1, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (revoked rows are part of the audit view)", total)
	}
	seen := map[uuid.UUID]bool{}
	for i := range list {
		seen[list[i].ID] = true
	}
	if !seen[live.ID] || !seen[dead.ID] {
		t.Fatalf("expected both live and revoked sessions, got %d rows", len(list))
	}
}

// 会话分页只返回目标用户的会话，不能串到别人那里。
func TestListUserSessionsScopedToUser(t *testing.T) {
	svc, st := newTestService(t)
	mine := makeTimedUser(t, svc, st, "sessions-mine", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	other := makeTimedUser(t, svc, st, "sessions-other", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	t.Cleanup(func() {
		st.DB.Where("user_id IN ?", []string{mine.ID, other.ID}).Delete(&models.Session{})
	})

	base := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	makeSession(t, st, mine.ID, "10.0.2.1", base, false)
	makeSession(t, st, other.ID, "10.0.2.2", base, false)
	makeSession(t, st, other.ID, "10.0.2.3", base.Add(time.Minute), false)

	list, total, err := svc.ListUserSessions(mine.ID, 1, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].IP != "10.0.2.1" {
		t.Fatalf("expected only my session, got total=%d list=%v", total, list)
	}
}

// 用户不存在要报 NOT_FOUND，而不是返回空列表（否则前端无法区分「没会话」和「没这个人」）。
func TestListUserSessionsMissingUser(t *testing.T) {
	svc, _ := newTestService(t)
	if _, _, err := svc.ListUserSessions(idgen.NewUserID(), 1, 10); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND, got %q (err=%v)", codeOf(err), err)
	}
}

// 详情不再返回会话：它现在只给基本信息和身份，会话走独立分页接口。
func TestGetUserNoLongerReturnsSessions(t *testing.T) {
	svc, st := newTestService(t)
	u := makeTimedUser(t, svc, st, "detail-no-sessions", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	t.Cleanup(func() { st.DB.Where("user_id = ?", u.ID).Delete(&models.Session{}) })
	makeSession(t, st, u.ID, "10.0.3.1", time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC), false)

	got, _, err := svc.GetUser(u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.ID == "" {
		t.Fatal("detail should still return the user")
	}
	// 会话仍应能通过独立接口取到（证明不是把数据弄丢了）
	_, total, err := svc.ListUserSessions(u.ID, 1, 10)
	if err != nil || total != 1 {
		t.Fatalf("sessions should still be reachable via the dedicated endpoint: total=%d err=%v", total, err)
	}
}

// 删除不存在的应用应报 NotFound，而不是静默成功。
func TestDeleteClientMissing(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.DeleteClient(uuid.New()); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND, got %q (err=%v)", codeOf(err), err)
	}
}

// 仪表盘统计：总数 / 活跃 / 今日 / 昨日，按服务端本地时区的自然日切分。
//
// 断言用「造数前后的差值」而不是绝对数：测试库可能已有其它用例残留的行，
// 只关心本次新增的用户如何落到四个桶里。
func TestStatsCounts(t *testing.T) {
	svc, st := newTestService(t)

	before, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats before: %v", err)
	}

	// 以服务端本地时区算出今日 0 点，再精确造出「今日」与「昨日」的用户，
	// 并各造一个刚好跨在边界上的（昨日 23:59:59 与今日 00:00:00）。
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// 活跃用户：created_at 落在今日，且 status=active。
	makeTimedUser(t, svc, st, "stat-today-active", todayStart.Add(10*time.Hour), nil)
	// 今日注册但已被停用：计入 today，不计入 active。
	disabled := makeTimedUser(t, svc, st, "stat-today-disabled", todayStart.Add(11*time.Hour), nil)
	if err := st.DB.Model(&models.User{}).Where("id = ?", disabled.ID).
		Update("status", models.StatusDisabled).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	// 昨日注册，落在昨日区间内（昨日 23:59:59）。
	makeTimedUser(t, svc, st, "stat-yesterday", todayStart.Add(-time.Second), nil)
	// 恰好在今日 0 点：半开区间应把它算进「今日」，而不是「昨日」。
	makeTimedUser(t, svc, st, "stat-today-start", todayStart, nil)

	after, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats after: %v", err)
	}

	const newUsers = 4
	if after.Total-before.Total != newUsers {
		t.Fatalf("total delta = %d, want %d", after.Total-before.Total, newUsers)
	}
	// 三个新用户是 active，一个刚被停用。
	if after.Active-before.Active != 3 {
		t.Fatalf("active delta = %d, want 3", after.Active-before.Active)
	}
	// 今日：10 点、11 点、以及正好 0 点，共 3 个。
	if after.Today-before.Today != 3 {
		t.Fatalf("today delta = %d, want 3", after.Today-before.Today)
	}
	// 昨日：昨天 23:59:59 的那一个。
	if after.Yesterday-before.Yesterday != 1 {
		t.Fatalf("yesterday delta = %d, want 1", after.Yesterday-before.Yesterday)
	}
}

// 注销用户是墓碑式软删（deleted_at 非空），不应计入任何一项统计。
func TestStatsExcludesDeletedUsers(t *testing.T) {
	svc, st := newTestService(t)

	before, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats before: %v", err)
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	u := makeTimedUser(t, svc, st, "stat-deleted", todayStart.Add(9*time.Hour), nil)
	// 直接打上墓碑标记，模拟已注销（不跑完整注销流程，避免牵扯会话/标识）。
	if err := st.DB.Model(&models.User{}).Where("id = ?", u.ID).
		Updates(map[string]any{"status": models.StatusDeleted, "deleted_at": now}).Error; err != nil {
		t.Fatalf("tombstone user: %v", err)
	}

	after, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats after: %v", err)
	}
	if after.Total != before.Total {
		t.Fatalf("deleted user leaked into total: %d -> %d", before.Total, after.Total)
	}
	if after.Today != before.Today {
		t.Fatalf("deleted user leaked into today: %d -> %d", before.Today, after.Today)
	}
	if after.Active != before.Active {
		t.Fatalf("deleted user leaked into active: %d -> %d", before.Active, after.Active)
	}
}
