package usersvc

import (
	"strings"
	"testing"

	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/ossstore"
)

// 这两个用例只碰签名/校验逻辑，不需要数据库，因此不依赖 PG_TEST_DSN。

func newSignService(t *testing.T) *Service {
	t.Helper()
	cfg := &config.Config{OSSRootDir: "uploads"}
	return &Service{cfg: cfg, oss: ossstore.NewMock(cfg)}
}

// 后台代客换头像必须复用到独立上传地址：若漏改，前端会把文件 PUT 到 /v1/me/...
// 那条按「当前登录者」鉴权的路由上，管理员自己的头像反被改动。
func TestSignAvatarForAdmin_RoutesUploadToAdminPath(t *testing.T) {
	s := newSignService(t)
	targetID := idgen.NewString()
	res, err := s.SignAvatarForAdmin(targetID, "image/png")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	upload, _ := res["upload_url"].(string)
	objectKey, _ := res["object_key"].(string)
	wantPrefix := "/v1/admin/users/" + idgen.FormatUserID(targetID) + "/avatar/upload?key="
	if !strings.HasPrefix(upload, wantPrefix) {
		t.Fatalf("upload_url = %q, want prefix %q", upload, wantPrefix)
	}
	if !strings.HasSuffix(upload, objectKey) {
		t.Fatalf("upload_url %q should carry object_key %q", upload, objectKey)
	}
}

// 自助端签名返回的仍是 /v1/me 那条路由（不受后台改造影响）。
func TestSignAvatar_SelfKeepsMePath(t *testing.T) {
	s := newSignService(t)
	res, err := s.SignAvatar(idgen.NewString(), "image/jpeg")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	upload, _ := res["upload_url"].(string)
	if !strings.HasPrefix(upload, "/v1/me/avatar/upload?key=") {
		t.Fatalf("upload_url = %q, want self /v1/me path", upload)
	}
}

// validateAvatarKey 是后台上传接口唯一的越权防线：key 必须落在目标用户目录下。
// 这里锁死跨用户 key 会被拒绝，否则管理员传别人的 key 就能改写他人头像。
func TestValidateAvatarKey_RejectsForeignUserDirectory(t *testing.T) {
	s := newSignService(t)
	ownerID := idgen.NewString()
	otherID := idgen.NewString()
	key := "uploads/" + idgen.FormatUserID(otherID) + "/abc.png"
	if _, err := s.validateAvatarKey(ownerID, key); err == nil {
		t.Fatalf("expected foreign-user key %q to be rejected for owner %q", key, ownerID)
	}
	own := "uploads/" + idgen.FormatUserID(ownerID) + "/abc.png"
	got, err := s.validateAvatarKey(ownerID, own)
	if err != nil {
		t.Fatalf("own key rejected: %v", err)
	}
	if got != own {
		t.Fatalf("key = %q, want %q", got, own)
	}
}

// 非法类型必须在签名阶段拒绝，后台与自助共用同一份白名单。
func TestSignAvatarForAdmin_RejectsBadType(t *testing.T) {
	s := newSignService(t)
	if _, err := s.SignAvatarForAdmin(idgen.NewString(), "image/gif"); err == nil {
		t.Fatal("expected gif to be rejected")
	}
}
