package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
)

// 解绑守卫的核心断言：账号解绑后必须仍能登录。
// 三种情形——有其它标识、无其它标识但有密码、两者都没有（应拒绝）。
func TestUnbindUserIdentity_Guard(t *testing.T) {
	svc, st := newTestService(t)

	// 造一个用户
	uid := idgen.NewUserID()
	if err := st.DB.Create(&models.User{
		ID: uid, Nickname: "unbind-" + uid, Status: models.StatusActive,
	}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	addIdent := func(typ, ident string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := st.DB.Create(&models.Identity{
			ID: id, UserID: uid, Type: typ, Identifier: ident,
			Extra: models.JSONMap{},
		}).Error; err != nil {
			t.Fatalf("create identity: %v", err)
		}
		return id
	}

	phoneID := addIdent(models.IdentityPhone, "+8613900000001")
	wechatID := addIdent(models.IdentityWechatMini, "wxapp:openid-1")

	// 1) 有两条标识时，解绑其中一条应成功
	if _, err := svc.UnbindUserIdentity(uid, wechatID); err != nil {
		t.Fatalf("解绑微信（仍有手机号）应成功，实际报错: %v", err)
	}

	// 2) 只剩手机号且无密码 —— 解绑应被拒（LAST_IDENTITY）
	_, err := svc.UnbindUserIdentity(uid, phoneID)
	if err == nil {
		t.Fatal("解绑最后一种登录方式且无密码，应被拒绝")
	}
	if code, _, ok := apperr.CodeOf(err); !ok || code != "LAST_IDENTITY" {
		t.Fatalf("期望错误码 LAST_IDENTITY，实际 %q (ok=%v)", code, ok)
	}

	// 3) 给用户设密码后，解绑最后一条标识应成功（仍可用密码登录）
	hash := "fake-bcrypt-hash"
	if err := st.DB.Model(&models.User{}).Where("id = ?", uid).
		Update("password_hash", hash).Error; err != nil {
		t.Fatalf("set password: %v", err)
	}
	if _, err := svc.UnbindUserIdentity(uid, phoneID); err != nil {
		t.Fatalf("已设置密码时解绑应成功，实际报错: %v", err)
	}

	// 4) 不属于该用户的标识应返回 NOT_FOUND
	otherUID := idgen.NewUserID()
	if err := st.DB.Create(&models.User{
		ID: otherUID, Nickname: "other-" + otherUID, Status: models.StatusActive,
	}).Error; err != nil {
		t.Fatalf("create other user: %v", err)
	}
	otherIdent := uuid.New()
	if err := st.DB.Create(&models.Identity{
		ID: otherIdent, UserID: otherUID, Type: models.IdentityPhone, Identifier: "+8613900000002",
		Extra: models.JSONMap{},
	}).Error; err != nil {
		t.Fatalf("create other identity: %v", err)
	}
	_, err = svc.UnbindUserIdentity(uid, otherIdent)
	if err == nil {
		t.Fatal("解绑他人标识应被拒绝")
	}
	if code, _, ok := apperr.CodeOf(err); !ok || code != "NOT_FOUND" {
		t.Fatalf("期望 NOT_FOUND，实际 %q (ok=%v)", code, ok)
	}
}