package app

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/httpx"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/infra/mailer"
	"github.com/jianboolee/d-auth/internal/infra/ossstore"
	"github.com/jianboolee/d-auth/internal/infra/rdb"
	"github.com/jianboolee/d-auth/internal/infra/sms"
	"github.com/jianboolee/d-auth/internal/infra/tokens"
	"github.com/jianboolee/d-auth/internal/infra/wechat"
	"github.com/jianboolee/d-auth/internal/modules/admin"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/codes"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usersvc"
)

func New(cfg *config.Config) (*gin.Engine, error) {
	db, err := database.Open(cfg.PostgresDSN)
	if err != nil {
		return nil, err
	}
	migDir := findMigrations()
	if err := database.Migrate(db, migDir); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	redis, err := rdb.Open(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		return nil, err
	}
	jwt, err := tokens.NewManager(tokens.Options{
		Issuer:    cfg.JWTIssuer,
		Alg:       cfg.JWTAlg,
		Secret:    cfg.JWTSecret,
		KeyPath:   cfg.JWTPrivateKeyPath,
		AccessTTL: cfg.JWTAccessTTL,
		Dev:       cfg.IsDev(),
	})
	if err != nil {
		return nil, err
	}
	slog.Info("jwt signer ready", "alg", jwt.Alg())

	var smsSender sms.Sender = &sms.Mock{}
	if !cfg.UseMockSMS() {
		smsSender = sms.NewAliyun(cfg)
	} else {
		slog.Warn("using mock SMS sender")
	}
	var mailSender mailer.Sender = &mailer.Mock{}
	if !cfg.UseMockMail() {
		mailSender = mailer.NewAliyun(cfg)
	} else {
		slog.Warn("using mock mail sender")
	}
	var oss ossstore.Store = ossstore.NewMock(cfg)
	if !cfg.UseMockOSS() {
		oss = ossstore.NewAliyun(cfg)
	} else {
		slog.Warn("using local mock OSS")
	}
	var wx wechat.Client = &wechat.Mock{AppID: "mock-web"}
	if cfg.WeChatWebAppID != "" && cfg.WeChatWebAppSecret != "" {
		wx = &wechat.Live{AppID: cfg.WeChatWebAppID, AppSecret: cfg.WeChatWebAppSecret, RedirectURL: cfg.WeChatRedirectURL}
	} else {
		slog.Warn("using mock wechat client")
	}

	st := store.New(db)
	codeSvc := codes.New(redis, cfg.CodeTTL)
	authSvc := auth.New(cfg, st, codeSvc, smsSender, mailSender, jwt, wx, redis)
	userSvc := usersvc.New(cfg, st, codeSvc, oss, authSvc)
	adminSvc := admin.New(cfg, st)
	return httpx.NewRouter(cfg, authSvc, userSvc, adminSvc), nil
}

func findMigrations() string {
	candidates := []string{
		"migrations",
		filepath.Join("api", "migrations"),
		filepath.Join("..", "migrations"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return "migrations"
}
