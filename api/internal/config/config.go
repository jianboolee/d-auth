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
	JWTAlg            string
	JWTSecret         string
	JWTPrivateKeyPath string
	JWTAccessTTL      time.Duration
	// JWTRefreshTTL 是会话寿命：cookie 模式下表示「最后活跃后多久过期」，
	// token 模式下每次 refresh 都会以此重置（见 SessionRenewInterval）。
	JWTRefreshTTL time.Duration
	// SessionRenewInterval 是 cookie 会话滑动续期的节流间隔：活跃请求最多每
	// 这么久延长一次 expires_at，避免每个请求都写库。必须大于 0 且远小于
	// JWTRefreshTTL；非法时会关闭滑动续期（退回固定过期，而不是每请求写库）。
	SessionRenewInterval time.Duration

	// UserCacheTTL 是用户资料在 Redis 中的缓存有效期。读取用户资料的接口
	// （公开资料 / internal）命中缓存可省掉数据库往返，写路径会显式失效，
	// TTL 只是兜底。配 0 即关闭缓存（回源数据库，功能不受影响）。
	UserCacheTTL time.Duration

	InitAdminPhone string
	InitAdminEmail string
	// CodeTTL 是短信/邮件验证码的有效期，同时用于短信模板里的「X 分钟内有效」。
	CodeTTL time.Duration
	// 验证码发送限流。按「目标」（手机号/邮箱）与「来源 IP」两个维度各自限流：
	// 目标维度是硬上限（换 scene 也会共用同一配额），IP 维度用于挡住批量枚举。
	CodeCooldown    time.Duration
	CodePerTargetHr int
	CodePerIPMinute int
	CodePerIPHour   int
	// TrustedProxies 是可信代理地址，决定 gin 是否采信 X-Forwarded-For。
	// 只有来自这些地址的请求，ClientIP() 才会取 XFF，否则用直连地址——
	// 配错会让所有请求塌缩成同一个 IP，IP 限流会误封所有人。
	TrustedProxies []string
	// MasterKey 用于加密数据库中保存的各应用微信 AppSecret（AES-256-GCM，密钥为 SHA256(MasterKey)）。
	// 上线后不应更改，否则已加密的 AppSecret 将无法解密。
	MasterKey string

	// 通用密钥，作为下列各服务未单独配置时的兜底。
	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
	// 短信、邮件、OSS 可各自使用不同的 AccessKey；留空则回退到通用密钥。
	AliyunSMSAccessKeyID     string
	AliyunSMSAccessKeySecret string
	AliyunDMAccessKeyID      string
	AliyunDMAccessKeySecret  string
	OSSAccessKeyID           string
	OSSAccessKeySecret       string
	SMSSignName              string
	SMSTemplateLogin         string
	SMSTemplateBind          string
	SMSTemplateReset         string
	DMAccount                string
	DMFromAlias              string
	DMRegion                 string

	OSSEndpoint string
	OSSBucket   string
	OSSCDNBase  string
	// OSSRootDir 是对象键在 bucket 中的根目录，例如 uploads。
	OSSRootDir string
	// OSSAvatarSuffix 是头像 URL 末尾统一追加的图片处理后缀，例如阿里云 360x360 正方形裁剪。
	OSSAvatarSuffix string

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
		"postgres://dauth:dauth@localhost:5432/dauth?sslmode=disable",
	)
	redisAddr := firstNonEmpty(env("DAUTH_REDIS_ADDR", ""), env("REDIS_ADDR", ""), "localhost:6379")

	// 通用密钥；短信、邮件、OSS 可用各自的 ACCESS_KEY 覆盖，未配置时回退到这里。
	aliyunKeyID := env("DAUTH_ALIYUN_ACCESS_KEY_ID", "")
	aliyunKeySecret := env("DAUTH_ALIYUN_ACCESS_KEY_SECRET", "")
	smsKeyID := firstNonEmpty(env("DAUTH_ALIYUN_SMS_ACCESS_KEY_ID", ""), aliyunKeyID)
	smsKeySecret := firstNonEmpty(env("DAUTH_ALIYUN_SMS_ACCESS_KEY_SECRET", ""), aliyunKeySecret)
	dmKeyID := firstNonEmpty(env("DAUTH_ALIYUN_DM_ACCESS_KEY_ID", ""), aliyunKeyID)
	dmKeySecret := firstNonEmpty(env("DAUTH_ALIYUN_DM_ACCESS_KEY_SECRET", ""), aliyunKeySecret)
	ossKeyID := firstNonEmpty(env("DAUTH_OSS_ACCESS_KEY_ID", ""), aliyunKeyID)
	ossKeySecret := firstNonEmpty(env("DAUTH_OSS_ACCESS_KEY_SECRET", ""), aliyunKeySecret)
	// DAUTH_OSS_PREFIX 是旧名字，保留兼容；新名字是 DAUTH_OSS_ROOT_DIR。
	ossRootDir := strings.Trim(
		firstNonEmpty(env("DAUTH_OSS_ROOT_DIR", ""), env("DAUTH_OSS_PREFIX", ""), "uploads"),
		"/",
	)
	if ossRootDir == "" {
		ossRootDir = "uploads"
	}
	// 头像统一追加的图片处理后缀，默认阿里云 360x360 正方形裁剪。
	ossAvatarSuffix := strings.TrimSpace(env("DAUTH_OSS_AVATAR_SUFFIX", "?x-oss-process=image/resize,m_fill,w_360,h_360"))
	if ossAvatarSuffix != "" && !strings.HasPrefix(ossAvatarSuffix, "?") {
		ossAvatarSuffix = "?" + ossAvatarSuffix
	}

	return &Config{
		Env:                      firstNonEmpty(env("DAUTH_ENV", ""), env("APP_ENV", ""), "dev"),
		HTTPAddr:                 httpAddr,
		PublicURL:                strings.TrimRight(publicURL, "/"),
		WebOrigin:                webOrigin,
		PostgresDSN:              dsn,
		RedisAddr:                redisAddr,
		RedisPassword:            env("DAUTH_REDIS_PASSWORD", env("REDIS_PASSWORD", "")),
		RedisDB:                  envInt("DAUTH_REDIS_DB", envInt("REDIS_DB", 0)),
		CookieDomain:             env("DAUTH_COOKIE_DOMAIN", ""),
		CookieSecure:             envBool("DAUTH_COOKIE_SECURE", false),
		CookieName:               env("DAUTH_COOKIE_NAME", "dauth_session"),
		AllowedOrigins:           origins,
		JWTIssuer:                firstNonEmpty(env("DAUTH_JWT_ISSUER", ""), strings.TrimRight(publicURL, "/")),
		JWTAlg:                   env("DAUTH_JWT_ALG", env("JWT_ALG", "")),
		JWTSecret:                firstNonEmpty(env("DAUTH_JWT_SECRET", ""), env("JWT_SECRET", "")),
		JWTPrivateKeyPath:        env("DAUTH_JWT_PRIVATE_KEY_PATH", ""),
		JWTAccessTTL:             envDuration("DAUTH_JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:            envDuration("DAUTH_JWT_REFRESH_TTL", 30*24*time.Hour),
		SessionRenewInterval:     envDuration("DAUTH_SESSION_RENEW_INTERVAL", 24*time.Hour),
		UserCacheTTL:             envDuration("DAUTH_USER_CACHE_TTL", 60*time.Second),
		InitAdminPhone:           env("DAUTH_INIT_ADMIN_PHONE", ""),
		InitAdminEmail:           strings.ToLower(strings.TrimSpace(env("DAUTH_INIT_ADMIN_EMAIL", ""))),
		CodeTTL:                  envDuration("DAUTH_CODE_TTL", 5*time.Minute),
		CodeCooldown:             envDuration("DAUTH_CODE_COOLDOWN", 60*time.Second),
		CodePerTargetHr:          envInt("DAUTH_CODE_PER_TARGET_HOUR", 10),
		CodePerIPMinute:          envInt("DAUTH_CODE_PER_IP_MINUTE", 10),
		CodePerIPHour:            envInt("DAUTH_CODE_PER_IP_HOUR", 100),
		TrustedProxies:           splitCSV(env("DAUTH_TRUSTED_PROXIES", "127.0.0.1,::1")),
		MasterKey:                env("DAUTH_MASTER_KEY", "dev-master-key-change-me-32b!!!!!!!!"),
		AliyunAccessKeyID:        aliyunKeyID,
		AliyunAccessKeySecret:    aliyunKeySecret,
		AliyunSMSAccessKeyID:     smsKeyID,
		AliyunSMSAccessKeySecret: smsKeySecret,
		AliyunDMAccessKeyID:      dmKeyID,
		AliyunDMAccessKeySecret:  dmKeySecret,
		OSSAccessKeyID:           ossKeyID,
		OSSAccessKeySecret:       ossKeySecret,
		SMSSignName:              env("DAUTH_ALIYUN_SMS_SIGN_NAME", ""),
		SMSTemplateLogin:         env("DAUTH_ALIYUN_SMS_TEMPLATE_LOGIN", ""),
		SMSTemplateBind:          env("DAUTH_ALIYUN_SMS_TEMPLATE_BIND", ""),
		SMSTemplateReset:         env("DAUTH_ALIYUN_SMS_TEMPLATE_RESET", ""),
		DMAccount:                env("DAUTH_ALIYUN_DM_ACCOUNT", ""),
		DMFromAlias:              env("DAUTH_ALIYUN_DM_FROM_ALIAS", "d-auth"),
		DMRegion:                 env("DAUTH_ALIYUN_DM_REGION", "cn-hangzhou"),
		OSSEndpoint:              env("DAUTH_OSS_ENDPOINT", ""),
		OSSBucket:                env("DAUTH_OSS_BUCKET", ""),
		OSSCDNBase:               strings.TrimRight(env("DAUTH_OSS_CDN_BASE", "http://localhost:8682/static"), "/"),
		OSSRootDir:               ossRootDir,
		OSSAvatarSuffix:          ossAvatarSuffix,
		WeChatWebAppID:           env("DAUTH_WECHAT_WEB_APP_ID", ""),
		WeChatWebAppSecret:       env("DAUTH_WECHAT_WEB_APP_SECRET", ""),
		WeChatRedirectURL:        env("DAUTH_WECHAT_REDIRECT_URL", strings.TrimRight(publicURL, "/")+"/v1/auth/wechat/callback"),
	}
}

func (c *Config) IsDev() bool { return c.Env != "prod" && c.Env != "production" }

func (c *Config) UseMockSMS() bool {
	return c.AliyunSMSAccessKeyID == "" || c.SMSSignName == ""
}

func (c *Config) UseMockMail() bool {
	return c.AliyunDMAccessKeyID == "" || c.DMAccount == ""
}

func (c *Config) UseMockOSS() bool {
	return c.OSSEndpoint == "" || c.OSSBucket == "" || c.OSSAccessKeyID == ""
}

// AvatarURL 返回头像的公开访问地址，并按 OSSAvatarSuffix 追加图片处理后缀。
// objectKey 为空返回空串；已是完整 URL 的键原样返回（不追加后缀，避免影响外部地址）。
func (c *Config) AvatarURL(objectKey string) string {
	if objectKey == "" {
		return ""
	}
	if strings.HasPrefix(objectKey, "http://") || strings.HasPrefix(objectKey, "https://") {
		return objectKey
	}
	u := strings.TrimRight(c.OSSCDNBase, "/") + "/" + strings.TrimPrefix(objectKey, "/")
	if c.OSSAvatarSuffix != "" {
		u += c.OSSAvatarSuffix
	}
	return u
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
