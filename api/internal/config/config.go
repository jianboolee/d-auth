package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env       string
	HTTPAddr  string
	PublicURL string
	WebOrigin string

	PostgresDSN string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	CookieDomain   string
	CookieSecure   bool
	CookieName     string
	AllowedOrigins []string

	JWTIssuer         string
	JWTPrivateKeyPath string
	JWTAccessTTL      time.Duration
	JWTRefreshTTL     time.Duration

	InitAdminPhone string
	InitAdminEmail string
	MasterKey      string

	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
	SMSSignName           string
	SMSTemplateLogin      string
	SMSTemplateBind       string
	SMSTemplateReset      string
	DMAccount             string
	DMFromAlias           string
	DMRegion              string

	OSSEndpoint string
	OSSBucket   string
	OSSCDNBase  string
	OSSPrefix   string

	WeChatWebAppID     string
	WeChatWebAppSecret string
	WeChatRedirectURL  string
}

func Load() *Config {
	_ = loadDotEnv()

	httpAddr := env("DAUTH_HTTP_ADDR", "")
	if httpAddr == "" {
		httpAddr = ":" + env("HTTP_PORT", "8681")
	}

	publicURL := firstNonEmpty(
		env("DAUTH_PUBLIC_URL", ""),
		env("PUBLIC_URL", ""),
		"http://localhost:8681",
	)
	webOrigin := env("DAUTH_WEB_ORIGIN", "http://localhost:8682")
	origins := splitCSV(env("DAUTH_ALLOWED_ORIGINS", "http://localhost:8682,http://127.0.0.1:8682,http://localhost:8680,http://127.0.0.1:8680"))

	dsn := firstNonEmpty(
		env("DAUTH_POSTGRES_DSN", ""),
		env("DATABASE_URL", ""),
		"postgres://dauth:dauth@localhost:8683/dauth?sslmode=disable",
	)
	redisAddr := firstNonEmpty(env("DAUTH_REDIS_ADDR", ""), env("REDIS_ADDR", ""), "localhost:8684")

	return &Config{
		Env:            firstNonEmpty(env("DAUTH_ENV", ""), env("APP_ENV", ""), "dev"),
		HTTPAddr:       httpAddr,
		PublicURL:      strings.TrimRight(publicURL, "/"),
		WebOrigin:      webOrigin,
		PostgresDSN:    dsn,
		RedisAddr:      redisAddr,
		RedisPassword:  env("DAUTH_REDIS_PASSWORD", env("REDIS_PASSWORD", "")),
		RedisDB:        envInt("DAUTH_REDIS_DB", envInt("REDIS_DB", 0)),
		CookieDomain:   env("DAUTH_COOKIE_DOMAIN", ""),
		CookieSecure:   envBool("DAUTH_COOKIE_SECURE", false),
		CookieName:     env("DAUTH_COOKIE_NAME", "dauth_session"),
		AllowedOrigins: origins,
		JWTIssuer:         firstNonEmpty(env("DAUTH_JWT_ISSUER", ""), strings.TrimRight(publicURL, "/")),
		JWTPrivateKeyPath: env("DAUTH_JWT_PRIVATE_KEY_PATH", ""),
		JWTAccessTTL:      envDuration("DAUTH_JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:     envDuration("DAUTH_JWT_REFRESH_TTL", 30*24*time.Hour),
		InitAdminPhone: env("DAUTH_INIT_ADMIN_PHONE", ""),
		InitAdminEmail: strings.ToLower(strings.TrimSpace(env("DAUTH_INIT_ADMIN_EMAIL", ""))),
		MasterKey:      env("DAUTH_MASTER_KEY", "dev-master-key-change-me-32b!!!!!!!!"),
		AliyunAccessKeyID:     env("DAUTH_ALIYUN_ACCESS_KEY_ID", ""),
		AliyunAccessKeySecret: env("DAUTH_ALIYUN_ACCESS_KEY_SECRET", ""),
		SMSSignName:           env("DAUTH_ALIYUN_SMS_SIGN_NAME", ""),
		SMSTemplateLogin:      env("DAUTH_ALIYUN_SMS_TEMPLATE_LOGIN", ""),
		SMSTemplateBind:       env("DAUTH_ALIYUN_SMS_TEMPLATE_BIND", ""),
		SMSTemplateReset:      env("DAUTH_ALIYUN_SMS_TEMPLATE_RESET", ""),
		DMAccount:             env("DAUTH_ALIYUN_DM_ACCOUNT", ""),
		DMFromAlias:           env("DAUTH_ALIYUN_DM_FROM_ALIAS", "d-auth"),
		DMRegion:              env("DAUTH_ALIYUN_DM_REGION", "cn-hangzhou"),
		OSSEndpoint: env("DAUTH_OSS_ENDPOINT", ""),
		OSSBucket:   env("DAUTH_OSS_BUCKET", ""),
		OSSCDNBase:  strings.TrimRight(env("DAUTH_OSS_CDN_BASE", "http://localhost:8682/static/avatars"), "/"),
		OSSPrefix:   strings.Trim(env("DAUTH_OSS_PREFIX", "avatars"), "/"),
		WeChatWebAppID:     env("DAUTH_WECHAT_WEB_APP_ID", ""),
		WeChatWebAppSecret: env("DAUTH_WECHAT_WEB_APP_SECRET", ""),
		WeChatRedirectURL:  env("DAUTH_WECHAT_REDIRECT_URL", strings.TrimRight(publicURL, "/")+"/v1/auth/wechat/callback"),
	}
}

func (c *Config) IsDev() bool { return c.Env != "prod" && c.Env != "production" }

func (c *Config) UseMockSMS() bool {
	return c.AliyunAccessKeyID == "" || c.SMSSignName == ""
}

func (c *Config) UseMockMail() bool {
	return c.AliyunAccessKeyID == "" || c.DMAccount == ""
}

func (c *Config) UseMockOSS() bool {
	return c.OSSEndpoint == "" || c.OSSBucket == ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes"
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadDotEnv() error {
	candidates := []string{".env", "../.env", "../../.env"}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return godotenv.Load(p)
		}
	}
	return nil
}
