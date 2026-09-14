package legal

import (
	"os"
	"strings"
	"testing"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
func newTestService(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 测试工作目录是本包目录；兼容从 api 或仓库根跑
	var mErr error
	for _, dir := range []string{"../../../migrations", "migrations", "../../migrations"} {
		if mErr = database.Migrate(db, dir); mErr == nil {
			break
		}
	}
	if mErr != nil {
		t.Fatalf("migrate: %v", mErr)
	}
	svc := New(store.New(db))
	// 用例会改写全局单例的两行，跑完恢复为「未设置」，避免影响手动验证。
	t.Cleanup(func() {
		for _, k := range models.LegalKinds {
			if _, err := svc.Upsert(k, "", ""); err != nil {
				t.Logf("cleanup %s: %v", k, err)
			}
		}
	})
	return svc
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	if c, _, ok := apperr.CodeOf(err); ok {
		return c
	}
	return "UNC0DED"
}

// 迁移建表并预置了 terms/privacy 两行，All 必须始终能给出这两个 key。
func TestAllIncludesBothKinds(t *testing.T) {
	svc := newTestService(t)
	all, err := svc.All()
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	for _, k := range models.LegalKinds {
		if _, ok := all[k]; !ok {
			t.Fatalf("All() is missing kind %q", k)
		}
	}
}

func TestUpsertAndGetRoundTrip(t *testing.T) {
	svc := newTestService(t)

	got, err := svc.Upsert(models.LegalKindTerms, "https://example.com/terms", "# 用户协议\n\n正文")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got.URL != "https://example.com/terms" {
		t.Fatalf("url = %q", got.URL)
	}

	loaded, err := svc.Get(models.LegalKindTerms)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.Content != "# 用户协议\n\n正文" {
		t.Fatalf("content = %q", loaded.Content)
	}

	// 覆盖写：url 清空、只留正文（即「不跳转、渲染正文」状态）
	if _, err := svc.Upsert(models.LegalKindTerms, "", "only content"); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	loaded, _ = svc.Get(models.LegalKindTerms)
	if loaded.URL != "" || loaded.Content != "only content" {
		t.Fatalf("after overwrite: url=%q content=%q", loaded.URL, loaded.Content)
	}
}

// 两者都传空即回到「未设置」，前端应显示占位页。
func TestUpsertClearsBoth(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Upsert(models.LegalKindPrivacy, "https://example.com/p", "text"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.Upsert(models.LegalKindPrivacy, "  ", "  "); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ := svc.Get(models.LegalKindPrivacy)
	if got.URL != "" || got.Content != "" {
		t.Fatalf("expected cleared doc, got url=%q content=%q", got.URL, got.Content)
	}
}

func TestRejectsUnknownKind(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Upsert("faq", "", "x"); codeOf(err) != "INVALID_KIND" {
		t.Fatalf("upsert unknown kind: got %q, want INVALID_KIND", codeOf(err))
	}
	if _, err := svc.Get("faq"); codeOf(err) != "INVALID_KIND" {
		t.Fatalf("get unknown kind: got %q, want INVALID_KIND", codeOf(err))
	}
}

// 安全边界：url 会被前端交给 window.location.replace()，伪协议必须拒绝。
func TestRejectsUnsafeURL(t *testing.T) {
	svc := newTestService(t)
	bad := []string{
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"//example.com/terms",
		"/terms",
		"ftp://example.com/x",
	}
	for _, u := range bad {
		if _, err := svc.Upsert(models.LegalKindTerms, u, ""); codeOf(err) != "INVALID_URL" {
			t.Fatalf("upsert url %q: got %q, want INVALID_URL", u, codeOf(err))
		}
	}
	// 合法的大小写混合也应放行
	for _, u := range []string{"http://example.com/a", "HTTPS://Example.com/a"} {
		if _, err := svc.Upsert(models.LegalKindTerms, u, ""); err != nil {
			t.Fatalf("upsert url %q should pass, got %v", u, err)
		}
	}
}

func TestRejectsOversizedContent(t *testing.T) {
	svc := newTestService(t)
	tooLong := strings.Repeat("a", MaxContentBytes+1)
	if _, err := svc.Upsert(models.LegalKindTerms, "", tooLong); codeOf(err) != "CONTENT_TOO_LONG" {
		t.Fatalf("got %q, want CONTENT_TOO_LONG", codeOf(err))
	}
	// 边界值应放行
	if _, err := svc.Upsert(models.LegalKindTerms, "", strings.Repeat("a", MaxContentBytes)); err != nil {
		t.Fatalf("content at limit should pass, got %v", err)
	}
}

func TestRejectsOversizedURL(t *testing.T) {
	svc := newTestService(t)
	long := "https://example.com/" + strings.Repeat("a", MaxURLBytes)
	if _, err := svc.Upsert(models.LegalKindTerms, long, ""); codeOf(err) != "URL_TOO_LONG" {
		t.Fatalf("got %q, want URL_TOO_LONG", codeOf(err))
	}
}
