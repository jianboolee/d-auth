package config_test

import (
	"strings"
	"testing"

	"github.com/jianboolee/d-auth/internal/config"
)

// 用 t.Setenv 显式占位，godotenv.Load 不会覆盖已存在的环境变量，
// 因此这些断言不会被仓库根的 .env 影响。

func TestOSSRootDirDefault(t *testing.T) {
	t.Setenv("DAUTH_OSS_ROOT_DIR", "")
	t.Setenv("DAUTH_OSS_PREFIX", "")

	if got := config.Load().OSSRootDir; got != "uploads" {
		t.Fatalf("got %q want uploads", got)
	}
}

func TestOSSRootDirLegacyPrefixFallback(t *testing.T) {
	t.Setenv("DAUTH_OSS_ROOT_DIR", "")
	t.Setenv("DAUTH_OSS_PREFIX", "avatars")

	if got := config.Load().OSSRootDir; got != "avatars" {
		t.Fatalf("got %q want avatars", got)
	}
}

func TestOSSRootDirTakesPrecedenceOverPrefix(t *testing.T) {
	t.Setenv("DAUTH_OSS_ROOT_DIR", "uploads")
	t.Setenv("DAUTH_OSS_PREFIX", "avatars")

	if got := config.Load().OSSRootDir; got != "uploads" {
		t.Fatalf("got %q want uploads", got)
	}
}

func TestOSSRootDirTrimsSlashes(t *testing.T) {
	t.Setenv("DAUTH_OSS_ROOT_DIR", "/general/uploads/")
	t.Setenv("DAUTH_OSS_PREFIX", "")

	if got := config.Load().OSSRootDir; got != "general/uploads" {
		t.Fatalf("got %q want general/uploads", got)
	}
}

func TestServiceKeysFallBackToCommonKey(t *testing.T) {
	t.Setenv("DAUTH_ALIYUN_ACCESS_KEY_ID", "common-id")
	t.Setenv("DAUTH_ALIYUN_ACCESS_KEY_SECRET", "common-secret")
	t.Setenv("DAUTH_ALIYUN_SMS_ACCESS_KEY_ID", "")
	t.Setenv("DAUTH_ALIYUN_SMS_ACCESS_KEY_SECRET", "")
	t.Setenv("DAUTH_ALIYUN_DM_ACCESS_KEY_ID", "")
	t.Setenv("DAUTH_ALIYUN_DM_ACCESS_KEY_SECRET", "")
	t.Setenv("DAUTH_OSS_ACCESS_KEY_ID", "")
	t.Setenv("DAUTH_OSS_ACCESS_KEY_SECRET", "")

	cfg := config.Load()
	if cfg.AliyunSMSAccessKeyID != "common-id" || cfg.AliyunSMSAccessKeySecret != "common-secret" {
		t.Fatalf("sms got %q/%q", cfg.AliyunSMSAccessKeyID, cfg.AliyunSMSAccessKeySecret)
	}
	if cfg.AliyunDMAccessKeyID != "common-id" || cfg.AliyunDMAccessKeySecret != "common-secret" {
		t.Fatalf("dm got %q/%q", cfg.AliyunDMAccessKeyID, cfg.AliyunDMAccessKeySecret)
	}
	if cfg.OSSAccessKeyID != "common-id" || cfg.OSSAccessKeySecret != "common-secret" {
		t.Fatalf("oss got %q/%q", cfg.OSSAccessKeyID, cfg.OSSAccessKeySecret)
	}
}

func TestServiceKeysOverrideIndependently(t *testing.T) {
	t.Setenv("DAUTH_ALIYUN_ACCESS_KEY_ID", "common-id")
	t.Setenv("DAUTH_ALIYUN_ACCESS_KEY_SECRET", "common-secret")
	t.Setenv("DAUTH_ALIYUN_SMS_ACCESS_KEY_ID", "sms-id")
	t.Setenv("DAUTH_ALIYUN_SMS_ACCESS_KEY_SECRET", "sms-secret")
	t.Setenv("DAUTH_ALIYUN_DM_ACCESS_KEY_ID", "dm-id")
	t.Setenv("DAUTH_ALIYUN_DM_ACCESS_KEY_SECRET", "dm-secret")
	t.Setenv("DAUTH_OSS_ACCESS_KEY_ID", "oss-id")
	t.Setenv("DAUTH_OSS_ACCESS_KEY_SECRET", "oss-secret")

	cfg := config.Load()
	if cfg.AliyunSMSAccessKeyID != "sms-id" || cfg.AliyunSMSAccessKeySecret != "sms-secret" {
		t.Fatalf("sms got %q/%q", cfg.AliyunSMSAccessKeyID, cfg.AliyunSMSAccessKeySecret)
	}
	if cfg.AliyunDMAccessKeyID != "dm-id" || cfg.AliyunDMAccessKeySecret != "dm-secret" {
		t.Fatalf("dm got %q/%q", cfg.AliyunDMAccessKeyID, cfg.AliyunDMAccessKeySecret)
	}
	if cfg.OSSAccessKeyID != "oss-id" || cfg.OSSAccessKeySecret != "oss-secret" {
		t.Fatalf("oss got %q/%q", cfg.OSSAccessKeyID, cfg.OSSAccessKeySecret)
	}
	// 通用密钥本身保持不变。
	if cfg.AliyunAccessKeyID != "common-id" || cfg.AliyunAccessKeySecret != "common-secret" {
		t.Fatalf("common got %q/%q", cfg.AliyunAccessKeyID, cfg.AliyunAccessKeySecret)
	}
}

func TestUseMockSwitchesOnServiceKey(t *testing.T) {
	t.Setenv("DAUTH_ALIYUN_ACCESS_KEY_ID", "")
	t.Setenv("DAUTH_ALIYUN_ACCESS_KEY_SECRET", "")
	t.Setenv("DAUTH_ALIYUN_SMS_ACCESS_KEY_ID", "sms-id")
	t.Setenv("DAUTH_ALIYUN_SMS_ACCESS_KEY_SECRET", "sms-secret")
	t.Setenv("DAUTH_ALIYUN_SMS_SIGN_NAME", "sign")
	t.Setenv("DAUTH_ALIYUN_DM_ACCOUNT", "")

	cfg := config.Load()
	// 只配置短信专用密钥时，短信应为真实发送，邮件仍为 mock。
	if cfg.UseMockSMS() {
		t.Fatal("expected real SMS sender with dedicated key")
	}
	if !cfg.UseMockMail() {
		t.Fatal("expected mock mail sender without DM account")
	}
}

func TestOSSAvatarSuffixDefault(t *testing.T) {
	t.Setenv("DAUTH_OSS_AVATAR_SUFFIX", "")

	want := "?x-oss-process=image/resize,m_fill,w_360,h_360"
	if got := config.Load().OSSAvatarSuffix; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOSSAvatarSuffixNormalized(t *testing.T) {
	// 不带 ? 前缀时自动补上。
	t.Setenv("DAUTH_OSS_AVATAR_SUFFIX", "x-oss-process=image/resize,w_200")

	want := "?x-oss-process=image/resize,w_200"
	if got := config.Load().OSSAvatarSuffix; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAvatarURL(t *testing.T) {
	t.Setenv("DAUTH_OSS_CDN_BASE", "https://cdn.example.com/")

	cfg := config.Load()
	cfg.OSSCDNBase = "https://cdn.example.com/"
	cfg.OSSAvatarSuffix = "?x-oss-process=image/resize,m_fill,w_360,h_360"

	if got := cfg.AvatarURL(""); got != "" {
		t.Fatalf("empty key should yield empty url, got %q", got)
	}
	want := "https://cdn.example.com/uploads/u/a.png?x-oss-process=image/resize,m_fill,w_360,h_360"
	if got := cfg.AvatarURL("uploads/u/a.png"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// 外部完整 URL 原样返回。
	ext := "https://other.example.com/a.png"
	if got := cfg.AvatarURL(ext); got != ext {
		t.Fatalf("got %q want %q", got, ext)
	}
	// 前缀斜杠不应产生双斜杠。
	if got := cfg.AvatarURL("/uploads/u/a.png"); !strings.HasPrefix(got, "https://cdn.example.com/uploads/") {
		t.Fatalf("unexpected url %q", got)
	}
}

func TestAvatarURLNoSuffix(t *testing.T) {
	cfg := &config.Config{OSSCDNBase: "https://cdn.example.com", OSSAvatarSuffix: ""}
	if got := cfg.AvatarURL("uploads/u/a.png"); got != "https://cdn.example.com/uploads/u/a.png" {
		t.Fatalf("unexpected url %q", got)
	}
}

// 本地 Postgres / Redis 用默认端口，不再分配独立端口（8683/8684）。
func TestDatabaseAndRedisDefaultsUseStandardPorts(t *testing.T) {
	t.Setenv("DAUTH_POSTGRES_DSN", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DAUTH_REDIS_ADDR", "")
	t.Setenv("REDIS_ADDR", "")

	cfg := config.Load()
	if got := cfg.PostgresDSN; !strings.Contains(got, "@localhost:5432/") {
		t.Fatalf("default postgres dsn should use 5432, got %q", got)
	}
	if got := cfg.RedisAddr; got != "localhost:6379" {
		t.Fatalf("default redis addr = %q, want localhost:6379", got)
	}
}
