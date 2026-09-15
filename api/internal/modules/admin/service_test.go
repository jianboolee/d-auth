package admin

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
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
	return New(cfg, st), st
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
// 刻意不传 allowed_origins / login_methods：这两列是 NOT NULL，省略字段是
// SDK 允许的用法，也是回归用例要守住的场景（见 TestCreateClientWithoutOptionalFields）。
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

// 建应用时省略可选字段不应失败。allowed_origins / login_methods 是 NOT NULL 列，
// nil slice 会被写成 NULL 而撞非空约束——控制台总是发 []，所以这个坑只在直接调
// API/SDK（ClientInput 字段可选）时暴露，且原先会被误报成「client_key 已存在」。
func TestCreateClientWithoutOptionalFields(t *testing.T) {
	svc, _ := newTestService(t)

	cl, err := svc.CreateClient(ClientInput{ClientKey: uniqueKey("bare"), Name: "仅必填"})
	if err != nil {
		t.Fatalf("create with omitted optional fields: %v", err)
	}
	if cl.AllowedOrigins == nil || cl.LoginMethods == nil {
		t.Errorf("arrays should come back as empty, got origins=%v methods=%v",
			cl.AllowedOrigins, cl.LoginMethods)
	}
	t.Cleanup(func() { _ = svc.DeleteClient(cl.ID) })
}

// 删除不存在的应用应报 NotFound，而不是静默成功。
func TestDeleteClientMissing(t *testing.T) {
	svc, _ := newTestService(t)
	if err := svc.DeleteClient(uuid.New()); codeOf(err) != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND, got %q (err=%v)", codeOf(err), err)
	}
}
