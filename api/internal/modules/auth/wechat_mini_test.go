package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/infra/secret"
	"github.com/jianboolee/d-auth/internal/infra/tokens"
	"github.com/jianboolee/d-auth/internal/infra/wechat"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/sessionstore"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/redis/go-redis/v9"
)

// 需要真实 postgres：通过 PG_TEST_DSN 提供连接串，否则跳过。
// 这里验证的是「微信凭据没配时报什么错」，所以要真的落一条 client 行。
func newWechatTestService(t *testing.T, env string) (*Service, *store.Store) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 测试的工作目录是本包目录，迁移在 api/migrations；兼容从 api 或仓库根跑。
	var mErr error
	for _, dir := range []string{"../../../migrations", "migrations", "../../migrations"} {
		if mErr = database.Migrate(db, dir); mErr == nil {
			break
		}
	}
	if mErr != nil {
		t.Fatalf("migrate: %v", mErr)
	}

	cfg := &config.Config{
		Env:           env,
		MasterKey:     "wechat-test-master-key-32-bytes!!!!",
		JWTIssuer:     "test",
		JWTSecret:     "test-secret",
		JWTAlg:        tokens.AlgHS256,
		JWTRefreshTTL: time.Hour,
	}
	jwtMgr, err := tokens.NewManager(tokens.Options{
		Issuer: cfg.JWTIssuer, Alg: cfg.JWTAlg, Secret: cfg.JWTSecret,
		AccessTTL: time.Minute, Dev: true,
	})
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	st := store.New(db)
	// 会话白名单（refresh token）现在存 Redis。测试用 REDIS_TEST_ADDR，缺省 Redis
	// 不可用——但本文件只验证「凭据缺失时提前报错」这类早退路径，真正的
	// issueToken 由 TestWeChatMiniProceedsWithCredentials 覆盖，它需要 Redis。
	// 因此这里允许 sessions 为 nil：早退用例不会走到 issueToken。
	svc := &Service{
		cfg:    cfg,
		store:  st,
		jwt:    jwtMgr,
		// 这里的用例只验证「凭据缺失时提前报错」等早退路径，
		// 不会真的调到微信 —— 给一个固定 Resolver 即可。
		wxResolver: wechat.NewFixedResolver(&wechat.Mock{}),
	}
	if addr := os.Getenv("REDIS_TEST_ADDR"); addr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		svc.rdb = rdb
		svc.sessions = sessionstore.New(rdb, cfg.JWTRefreshTTL)
	}
	return svc, st
}

// makeMiniClient 建一个小程序应用，凭据按参数写入（空字符串表示不配置）。
func makeMiniClient(t *testing.T, svc *Service, st *store.Store, appID, appSecret string) *models.Client {
	t.Helper()
	enc, err := secret.Encrypt(svc.cfg.MasterKey, appSecret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// client_key 用随机 UUID 片段：idgen.New() 是时间有序的，同一毫秒内连续调用
	// 前 8 位相同，子用例之间会撞唯一索引。
	cl := &models.Client{
		ID:                 idgen.New(),
		ClientKey:          "mini_" + uuid.NewString()[:8],
		Name:               "小程序测试",
		Type:               "miniprogram",
		WechatAppID:        appID,
		WechatAppSecretEnc: enc,
		Status:             models.StatusActive,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := st.DB.Create(cl).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	t.Cleanup(func() {
		// 只删本用例真的造出来的行。
		//
		// 这里不能按 nickname = '微信用户' 之类去删：开发库里可能存在同名的历史
		// 用户，用昵称当条件会把它们的身份一起删掉（已踩过一次）。按 client_id
		// 定位会话、再顺着 user_id 精确回收，才是安全的。
		var userIDs []string
		st.DB.Raw("SELECT DISTINCT user_id FROM auth_sessions WHERE client_id = ?", cl.ID).
			Scan(&userIDs)
		st.DB.Exec("DELETE FROM auth_sessions WHERE client_id = ?", cl.ID)
		for _, uid := range userIDs {
			st.DB.Exec("DELETE FROM auth_identities WHERE user_id = ?", uid)
			st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", uid)
			st.DB.Exec("DELETE FROM auth_users WHERE id = ?", uid)
		}
		st.DB.Exec("DELETE FROM auth_clients WHERE id = ?", cl.ID)
	})
	return cl
}

// 非 dev 环境下凭据缺失要明确报「未配置」，而不是把空 appid 打到微信再报
// 一句笼统的「小程序登录失败」——那样分不清是配置问题还是微信侧报错。
// 凭据缺失时**降级到 Mock**，不报错（2026-10-31 改）。
//
// 语义变了：原来这里是「非 dev 环境 + 凭据缺失 → WECHAT_NOT_CONFIGURED」。
// 凭据搬进 auth_clients 之后，Mock 变成**每个 client 的降级路径**而不是
// "全局部署模式"，所以没配凭据就该降级。
//
// 这个改动修掉了一个真实的生产故障：生产 APP_ENV=production、而 mp_dcamp
// 没配凭据，旧逻辑直接 400，整条小程序登录路径不可用。
func TestWeChatMiniFallsBackToMockWhenCredentialsMissing(t *testing.T) {
	// 关键：用 production 环境跑 —— 这正是线上配置。
	svc, st := newWechatTestService(t, "production")

	cases := []struct {
		name      string
		appID     string
		appSecret string
	}{
		{"both missing", "", ""},
		{"missing appid", "", "some-secret"},
		{"missing secret", "wx1234567890abcdef", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cl := makeMiniClient(t, svc, st, tc.appID, tc.appSecret)
			_, err := svc.WeChatMini(context.Background(), cl.ClientKey, "js-code", LoginMeta{})
			// 凭据不全 → Mock 返回身份 → 走到 issueToken。
			// sessions 为 nil 时会在那里报错（本用例不需要 Redis），
			// 但**绝不能**是 WECHAT_NOT_CONFIGURED —— 那说明还在走旧逻辑。
			if code := wechatCodeOf(err); code == "WECHAT_NOT_CONFIGURED" {
				t.Fatalf("凭据缺失应降级到 Mock，不该报 WECHAT_NOT_CONFIGURED (err=%v)", err)
			}
		})
	}
}

// dev 环境同样降级（这条行为没变，但原因变了：
// 不再依赖 IsDev 判断，而是统一按 client 凭据说话）。
func TestWeChatMiniAllowsMissingCredentialsInDev(t *testing.T) {
	svc, st := newWechatTestService(t, "dev")
	cl := makeMiniClient(t, svc, st, "", "")

	_, err := svc.WeChatMini(context.Background(), cl.ClientKey, "js-code", LoginMeta{})
	if code := wechatCodeOf(err); code == "WECHAT_NOT_CONFIGURED" {
		t.Fatalf("dev should fall back to the mock, got WECHAT_NOT_CONFIGURED (err=%v)", err)
	}
}

// 凭据齐全时应越过这道检查，继续走到签发 token（由 mock 客户端返回成功）。
// 签发会登记 refresh 到 Redis，因此需要 REDIS_TEST_ADDR。
func TestWeChatMiniProceedsWithCredentials(t *testing.T) {
	svc, st := newWechatTestService(t, "production")
	if svc.sessions == nil {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	cl := makeMiniClient(t, svc, st, "wx1234567890abcdef", "real-secret")

	res, err := svc.WeChatMini(context.Background(), cl.ClientKey, "js-code", LoginMeta{})
	if code := wechatCodeOf(err); code == "WECHAT_NOT_CONFIGURED" {
		t.Fatalf("credentials were set but guard still rejected: %v", err)
	}
	if err != nil {
		t.Fatalf("mini login: %v", err)
	}
	if res == nil || res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens to be issued")
	}
}

// 未知应用仍应报 UNKNOWN_CLIENT，不受新检查影响。
func TestWeChatMiniUnknownClient(t *testing.T) {
	svc, _ := newWechatTestService(t, "production")
	_, err := svc.WeChatMini(context.Background(), "nope_"+uuid.NewString()[:8], "js-code", LoginMeta{})
	if code := wechatCodeOf(err); code != "UNKNOWN_CLIENT" {
		t.Fatalf("want UNKNOWN_CLIENT, got %q (err=%v)", code, err)
	}
}

func wechatCodeOf(err error) string {
	if err == nil {
		return ""
	}
	if c, _, ok := apperr.CodeOf(err); ok {
		return c
	}
	return "UNCODED"
}
