package ossstore_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestSaveKeepsDirsAndRejectsEscape(t *testing.T) {
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
	if err := store.Save(key, []byte("data")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join("data", "uploads", "user-1", "avatar.png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "data" {
		t.Fatalf("unexpected body %q", body)
	}

	if err := store.Save("other/user-1/avatar.png", []byte("x")); err == nil {
		t.Fatal("expected rejection for key outside root dir")
	}
	if err := store.Save("uploads/../../escape.png", []byte("x")); err == nil {
		t.Fatal("expected rejection for escaping key")
	}
}

func TestAliyunSignPutRelaysThroughServer(t *testing.T) {
	cfg := testCfg("uploads")
	store := ossstore.NewAliyun(cfg)

	key, upload, public, headers, err := store.SignPut("image/webp", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "uploads/user-1/") || !strings.HasSuffix(key, ".webp") {
		t.Fatalf("unexpected key %q", key)
	}
	// 中转模式：上传地址必须是同源的 /v1 路由，不再出现 OSS 域名或签名参数。
	if want := "/v1/me/avatar/upload?key=" + key; upload != want {
		t.Fatalf("unexpected upload url %q want %q", upload, want)
	}
	if strings.Contains(upload, "OSSAccessKeyId") || strings.Contains(upload, "aliyuncs") {
		t.Fatalf("upload url must not be a presigned oss url: %q", upload)
	}
	if headers["Content-Type"] != "image/webp" {
		t.Fatalf("unexpected headers %v", headers)
	}
	// 公开地址带处理后缀；上传地址不带。
	if public != "https://cdn.example.com/"+key+"?x-oss-process=image/resize,m_fill,w_360,h_360" {
		t.Fatalf("unexpected public url %q", public)
	}
	if strings.Contains(public, key) && strings.Contains(upload, "x-oss-process") {
		t.Fatalf("upload url should not carry suffix: %q", upload)
	}
}

// Save 必须用 OSS V1 Header 签名发起 PUT：
// StringToSign = "PUT\n\n{Content-Type}\n{Date}\n/{bucket}/{key}"，
// 且 Date 与请求头、Authorization 完全一致。
func TestAliyunSavePutsObjectWithV1HeaderAuth(t *testing.T) {
	cfg := testCfg("uploads")
	var gotAuth, gotDate, gotCT, gotPath, gotBody string

	// endpoint 用 httptest 的 host:port，virtualHost 会拼成 bucket.host:port，
	// 再用 transport 把请求强行送回测试服务器。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		gotDate = r.Header.Get("Date")
		gotCT = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	ep := strings.TrimPrefix(ts.URL, "http://") // host:port
	cfg.OSSEndpoint = ep
	store := ossstore.NewAliyun(cfg).WithTransport(rewriteToTestServer(ts))

	key := "uploads/user-1/abc.png"
	if err := store.Save(key, []byte("PNGDATA")); err != nil {
		t.Fatalf("save: %v", err)
	}

	if gotPath != "/"+key {
		t.Fatalf("path = %q want /%q", gotPath, key)
	}
	if gotBody != "PNGDATA" {
		t.Fatalf("body = %q", gotBody)
	}
	wantCT := "image/png" // 由扩展名反推
	if gotCT != wantCT {
		t.Fatalf("content-type = %q want %q", gotCT, wantCT)
	}

	wantSigStart := "OSS key-id:"
	if !strings.HasPrefix(gotAuth, wantSigStart) {
		t.Fatalf("authorization = %q want prefix %q", gotAuth, wantSigStart)
	}
	resource := "/" + cfg.OSSBucket + "/" + key
	stringToSign := "PUT\n\n" + wantCT + "\n" + gotDate + "\n" + resource
	mac := hmac.New(sha1.New, []byte(cfg.OSSAccessKeySecret))
	mac.Write([]byte(stringToSign))
	want := wantSigStart + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if gotAuth != want {
		t.Fatalf("signature mismatch\n got %q\nwant %q\nstringToSign=%q", gotAuth, want, stringToSign)
	}
}

func TestAliyunSaveRejectsBadKeys(t *testing.T) {
	cfg := testCfg("uploads")
	store := ossstore.NewAliyun(cfg)
	for _, key := range []string{"", "other/user-1/a.png", "uploads/../../escape.png"} {
		if err := store.Save(key, []byte("x")); err == nil {
			t.Fatalf("expected rejection for key %q", key)
		}
	}
}

func TestAliyunSaveSurfacesErrorBody(t *testing.T) {
	cfg := testCfg("uploads")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "<Error><Code>SignatureDoesNotMatch</Code></Error>")
	}))
	t.Cleanup(ts.Close)
	cfg.OSSEndpoint = strings.TrimPrefix(ts.URL, "http://")
	store := ossstore.NewAliyun(cfg).WithTransport(rewriteToTestServer(ts))

	err := store.Save("uploads/user-1/a.png", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Fatalf("expected wrapped 403 error, got %v", err)
	}
}

// rewriteToTestServer 把发往 bucket.<endpoint> 的请求改写到测试服务器，
// 保留原始 Host/路径/头，使签名校验与真实行为一致。
func rewriteToTestServer(ts *httptest.Server) http.RoundTripper {
	return roundTripFunc(func(req *http.Request) (*http.Response, error) {
		saved := req.URL.Host
		req.URL.Scheme = "http"
		req.URL.Host = strings.TrimPrefix(ts.URL, "http://")
		req.Host = saved // 虚机机 host 留给断言
		return http.DefaultTransport.RoundTrip(req)
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// 嵌套根目录（如 uploads/avatars）在 mock 模式下必须与 Save 的落盘位置一致，
// 否则 /static/ 路由会因重复前缀而 404。
func TestSaveNestedRootDir(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	cfg := testCfg("uploads/avatars")
	store := ossstore.NewMock(cfg)
	key, _, public, _, err := store.SignPut("image/png", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "uploads/avatars/user-1/") {
		t.Fatalf("unexpected key %q", key)
	}
	if err := store.Save(key, []byte("DATA")); err != nil {
		t.Fatal(err)
	}
	// 落盘路径 = DataRoot + 完整对象键
	onDisk := filepath.Join(ossstore.DataRoot(), filepath.FromSlash(key))
	body, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read %s: %v", onDisk, err)
	}
	if string(body) != "DATA" {
		t.Fatalf("body = %q", body)
	}
	// 公开地址去掉 CDN 前缀后，应等于 / + 完整对象键（静态路由据此定位文件）
	want := "https://cdn.example.com/" + key + "?x-oss-process=image/resize,m_fill,w_360,h_360"
	if public != want {
		t.Fatalf("public = %q want %q", public, want)
	}
}
