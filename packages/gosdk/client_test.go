package dauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dauth "github.com/jianboolee/d-auth/packages/gosdk"
)

// 这个文件用 httptest 起一个假 d-auth，端到端验证客户端真的按预期发请求、
// 解析统一响应体、并把业务错误还原成 *APIError。不依赖真实服务。

func newStub(t *testing.T, handler http.HandlerFunc) (*dauth.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return dauth.New(dauth.Options{BaseURL: srv.URL, APIKey: "sk-test"}), srv
}

func writeOK(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": "OK", "message": "ok", "data": data,
	})
}

func TestInternalUserSendsBearerAndParsesEnvelope(t *testing.T) {
	var gotAuth, gotPath string
	c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		writeOK(t, w, map[string]any{
			"id": "u1", "nickname": "老王", "roles": []string{"user"}, "phone": "13800000000",
		})
	})

	u, err := c.InternalUser(context.Background(), "u1")
	if err != nil {
		t.Fatalf("InternalUser: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
	if gotPath != "/v1/internal/users/u1" {
		t.Errorf("path = %q", gotPath)
	}
	if u.Nickname != "老王" || u.Phone != "13800000000" {
		t.Errorf("parsed user = %+v", u)
	}
}

// PatchUser 必须把指针字段序列化进 JSON，且没设的字段不出现在 body 里
// （否则服务端分不清「改成零值」和「不改」）。
func TestPatchUserMarshalsOnlySetFields(t *testing.T) {
	var body map[string]any
	c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeOK(t, w, map[string]any{"id": "u1", "nickname": "新", "bio": "b", "gender": 2})
	})

	nick := "新"
	gender := int16(2)
	u, err := c.PatchUser(context.Background(), "u1", dauth.PatchUserInput{
		Nickname: &nick, Gender: &gender,
	})
	if err != nil {
		t.Fatalf("PatchUser: %v", err)
	}
	if body["nickname"] != "新" {
		t.Errorf("body nickname = %v", body["nickname"])
	}
	if body["gender"] != float64(2) {
		t.Errorf("body gender = %v", body["gender"])
	}
	if _, present := body["bio"]; present {
		t.Errorf("bio 未设置却出现在 body：%v", body["bio"])
	}
	if _, present := body["status"]; present {
		t.Errorf("status 未设置却出现在 body：%v", body["status"])
	}
	if u.Bio != "b" || u.Gender != 2 {
		t.Errorf("parsed user = %+v", u)
	}
}

func TestAPIErrorSurfacedWithCode(t *testing.T) {
	c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": "PHONE_TAKEN", "message": "手机号已被使用",
		})
	})

	_, err := c.SetPhone(context.Background(), "u1", "13800000000")
	var apiErr *dauth.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Code != "PHONE_TAKEN" || apiErr.Status != http.StatusConflict {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

// 批量查询 ids 用逗号拼接，且匹配不上时返回空切片而非错误。
func TestUsersSkipMissingReturnsEmpty(t *testing.T) {
	var gotIDs string
	c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotIDs = r.URL.Query().Get("ids")
		writeOK(t, w, map[string]any{"items": []any{}, "total": 0})
	})

	list, err := c.Users(context.Background(), []string{"a", "b", "c"}, "", "")
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if gotIDs != "a,b,c" {
		t.Errorf("ids = %q, want a,b,c", gotIDs)
	}
	if len(list) != 0 {
		t.Errorf("want empty, got %d", len(list))
	}
}

// 登录走公开接口：即便客户端配了 API Key，也应带上（无害），但路径必须是
// /v1/auth/sms/login 且解析出 token。
func TestSMSLoginParsesTokens(t *testing.T) {
	var path string
	c, _ := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeOK(t, w, map[string]any{
			"access_token": "a", "refresh_token": "r", "token_type": "Bearer",
			"expires_in": 900, "user": map[string]any{"id": "u1", "nickname": "n"},
		})
	})

	toks, err := c.SMSLogin(context.Background(), "13800000000", "123456")
	if err != nil {
		t.Fatalf("SMSLogin: %v", err)
	}
	if path != "/v1/auth/sms/login" {
		t.Errorf("path = %q", path)
	}
	if toks.AccessToken != "a" || toks.User.ID != "u1" {
		t.Errorf("tokens = %+v", toks)
	}
}

// BaseURL 带 /v1 或尾斜杠时也要归一化，避免拼出 /v1/v1/...。
func TestBaseURLNormalization(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeOK(t, w, []any{})
	}))
	defer srv.Close()

	for _, base := range []string{srv.URL + "/", srv.URL + "/v1", srv.URL + "/v1/"} {
		c := dauth.New(dauth.Options{BaseURL: base, APIKey: "sk-x"})
		if _, err := c.Roles(context.Background()); err != nil {
			t.Fatalf("base %q: %v", base, err)
		}
		if !strings.HasPrefix(path, "/v1/admin/roles") {
			t.Errorf("base %q produced path %q", base, path)
		}
	}
}
