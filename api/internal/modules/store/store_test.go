package store

import (
	"os"
	"testing"

	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
func newTestStore(t *testing.T) *Store {
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
	return New(db)
}

// RoleCodes 在没有角色时必须返回空切片而不是 nil。
//
// nil 会序列化成 JSON 的 null，而客户端把 roles 当数组用（后台用户列表的角色列
// 直接 .map）。一旦出现 null，渲染函数抛错，router-view 会永久停在上一个页面：
// 地址栏和标题都变了、内容不变、切任何路由都没反应。开发库上已经因为一条没有
// 角色的历史记录触发过一次。
func TestRoleCodesReturnsEmptySliceNotNil(t *testing.T) {
	st := newTestStore(t)

	// 故意绕过 CreateUser：它会默认补上 user 角色，构造不出「无角色」的用户。
	u := &models.User{ID: idgen.NewUserID(), Nickname: "无角色用户", Status: models.StatusActive}
	if err := st.DB.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u.ID) })

	codes, err := st.RoleCodes(u.ID)
	if err != nil {
		t.Fatalf("RoleCodes: %v", err)
	}
	if codes == nil {
		t.Fatal("RoleCodes returned nil; it must return an empty slice so JSON encodes [] not null")
	}
	if len(codes) != 0 {
		t.Fatalf("expected no roles, got %v", codes)
	}
}

// 有角色时照常返回，确认上面的归一没有把正常路径改坏。
func TestRoleCodesReturnsAssignedRoles(t *testing.T) {
	st := newTestStore(t)

	u := &models.User{Nickname: "有角色用户", Status: models.StatusActive}
	if err := st.CreateUser(nil, u, models.RoleUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", u.ID)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", u.ID)
	})

	codes, err := st.RoleCodes(u.ID)
	if err != nil {
		t.Fatalf("RoleCodes: %v", err)
	}
	if len(codes) != 1 || codes[0] != models.RoleUser {
		t.Fatalf("want [%s], got %v", models.RoleUser, codes)
	}
}
