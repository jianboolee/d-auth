package ossstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/ossstore"
)

func testCfg(rootDir string) *config.Config {
	return &config.Config{
		OSSRootDir:         rootDir,
		OSSBucket:          "demo-bucket",
		OSSEndpoint:        "oss-cn-hangzhou.aliyuncs.com",
		OSSCDNBase:         "https://cdn.example.com/",
		OSSAvatarSuffix:    "?x-oss-process=image/resize,m_fill,w_360,h_360",
		OSSAccessKeyID:     "key-id",
		OSSAccessKeySecret: "key-secret",
	}
}

func TestSignPutUsesRootDir(t *testing.T) {
	cfg := testCfg("uploads")
	store := ossstore.NewMock(cfg)

	key, upload, public, _, err := store.SignPut("image/png", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "uploads/user-1/") || !strings.HasSuffix(key, ".png") {
		t.Fatalf("unexpected key %q", key)
	}
	if upload != "/v1/me/avatar/upload?key="+key {
		t.Fatalf("unexpected upload url %q", upload)
	}
	// 公开地址统一带图片处理后缀，且 upload_url 不带（PUT 需存原图）。
	if public != "https://cdn.example.com/"+key+"?x-oss-process=image/resize,m_fill,w_360,h_360" {
		t.Fatalf("unexpected public url %q", public)
	}
	if strings.Contains(upload, "x-oss-process") {
		t.Fatalf("upload url should not carry suffix: %q", upload)
	}
}

func TestPublicURLKeepsFullKey(t *testing.T) {
	cfg := testCfg("uploads")
	store := ossstore.NewMock(cfg)

	// 允许读取根目录不同的历史对象键，不应被裁剪。
	want := "https://cdn.example.com/avatars/u/old.png?x-oss-process=image/resize,m_fill,w_360,h_360"
	if got := store.PublicURL("avatars/u/old.png"); got != want {
		t.Fatalf("unexpected legacy url %q", got)
	}
}

func TestPublicURLWithoutSuffix(t *testing.T) {
	cfg := testCfg("uploads")
	cfg.OSSAvatarSuffix = ""
	store := ossstore.NewMock(cfg)

	if got := store.PublicURL("uploads/u/a.png"); got != "https://cdn.example.com/uploads/u/a.png" {
		t.Fatalf("unexpected url %q", got)
	}
}

func TestPublicURLExternalKeyUntouched(t *testing.T) {
	cfg := testCfg("uploads")
	store := ossstore.NewMock(cfg)

	// 已是完整 URL 的键原样返回，不追加后缀。
	ext := "https://other.example.com/avatar.png"
	if got := store.PublicURL(ext); got != ext {
		t.Fatalf("unexpected external url %q", got)
	}
}

func TestSaveLocalKeepsDirsAndRejectsEscape(t *testing.T) {
	cfg := testCfg("uploads")

	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	store := ossstore.NewMock(cfg)
	key := "uploads/user-1/avatar.png"
	if err := store.SaveLocal(key, []byte("data")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("data", "uploads", "user-1", "avatar.png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "data" {
		t.Fatalf("unexpected body %q", body)
	}

	if err := store.SaveLocal("other/user-1/avatar.png", []byte("x")); err == nil {
		t.Fatal("expected rejection for key outside root dir")
	}
	if err := store.SaveLocal("uploads/../../escape.png", []byte("x")); err == nil {
		t.Fatal("expected rejection for escaping key")
	}
}

func TestAliyunSignsFullObjectPath(t *testing.T) {
	cfg := testCfg("uploads")
	store := ossstore.NewAliyun(cfg)

	key, upload, public, _, err := store.SignPut("image/webp", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "uploads/user-1/") || !strings.HasSuffix(key, ".webp") {
		t.Fatalf("unexpected key %q", key)
	}
	if !strings.Contains(upload, "https://demo-bucket.oss-cn-hangzhou.aliyuncs.com/uploads/user-1/") {
		t.Fatalf("unexpected upload url %q", upload)
	}
	if !strings.Contains(upload, "OSSAccessKeyId=key-id") {
		t.Fatalf("missing access key in %q", upload)
	}
	// 上传地址用于存放原图，不带处理后缀；公开地址带后缀。
	if strings.Contains(upload, "x-oss-process") {
		t.Fatalf("upload url should not carry suffix: %q", upload)
	}
	if public != "https://cdn.example.com/"+key+"?x-oss-process=image/resize,m_fill,w_360,h_360" {
		t.Fatalf("unexpected public url %q", public)
	}
}
