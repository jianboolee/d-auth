package auth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/database"
	"github.com/jianboolee/d-auth/internal/infra/password"
	"github.com/jianboolee/d-auth/internal/infra/secret"
	"github.com/jianboolee/d-auth/internal/infra/tokens"
	"github.com/jianboolee/d-auth/internal/infra/wechat"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/codes"
	"github.com/jianboolee/d-auth/internal/modules/sessionstore"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/normalize"
	"github.com/redis/go-redis/v9"
)

// is_new_user 必须表示「本次请求刚建号」，不是「用户很新」。
// 最容易被写错的地方是拿 created_at 去猜，所以这些用例把「先建号、隔天再登录」
// 和「同一请求重复登录」两种情况都跑一遍。
//
// 需要 PG_TEST_DSN + REDIS_TEST_ADDR；缺任一项即跳过。
func newLoginTestService(t *testing.T) (*Service, *codes.Service) {
	t.Helper()
	dsn := os.Getenv("PG_TEST_DSN")
	addr := os.Getenv("REDIS_TEST_ADDR")
	if dsn == "" || addr == "" {
		t.Skip("PG_TEST_DSN / REDIS_TEST_ADDR not set")
	}
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, dir := range []string{"../../../migrations", "migrations", "../../migrations"} {
		if err = database.Migrate(db, dir); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := &config.Config{
		Env:           "dev",
		MasterKey:     "newuser-test-master-key-32-bytes!!!",
		JWTIssuer:     "test",
		JWTSecret:     "test-secret",
		JWTAlg:        tokens.AlgHS256,
		JWTAccessTTL:  15 * time.Minute,
		JWTRefreshTTL: time.Hour,
	}
	jwtMgr, err := tokens.NewManager(tokens.Options{
		Issuer: cfg.JWTIssuer, Alg: cfg.JWTAlg, Secret: cfg.JWTSecret,
		AccessTTL: cfg.JWTAccessTTL, Dev: true,
	})
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	st := store.New(db)
	// 冷却压到 1ms：用例需要在同一号码上连续取两次验证码，
	// 默认 60s 冷却会直接把第二次签发挡掉。
	codeSvc := codes.New(rdb, codes.Options{Cooldown: time.Millisecond})
	svc := &Service{
		cfg:      cfg,
		store:    st,
		codes:    codeSvc,
		jwt:      jwtMgr,
		wechat:   &mockMini{},
		rdb:      rdb,
		sessions: sessionstore.New(rdb, cfg.JWTRefreshTTL),
		// cache 留空：Cache 的 nil 接收者全程降级为「不命中」，测试不关心缓存。
	}
	return svc, codeSvc
}

// mockMini 是只为本文件服务的假微信客户端。
//
// 不用 wechat.Mock：它的 UnionID 固定为 "mock-union"，同一时刻的多次运行会命中
// 上一轮残留的身份记录，导致「首次登录应为新用户」的断言时真时假。
// 这里按 AppID 给出稳定的 openid，并让 UnionID 为空——查找走
// 「appID:openID」这条与用例一一对应的路径。
type mockMini struct{}

func (mockMini) WebQRAuthorizeURL(state string) string { return "https://example.invalid/qr?state=" + state }

func (mockMini) WebCallback(context.Context, string) (wechat.UserInfo, error) {
	return wechat.UserInfo{}, nil
}

func (mockMini) MiniCode2Session(_ context.Context, appID, _ string, jsCode string) (wechat.UserInfo, error) {
	return wechat.UserInfo{OpenID: "openid-" + jsCode, AppID: appID}, nil
}

// randomPhone 生成一个能通过 normalize.Phone 校验的随机中国大陆手机号。
// 用例之间不能复用同一个号：验证码按「目标」限流，重复使用会撞冷却。
func randomPhone() string {
	h := uuid.NewString()
	// 第二位取 3-9，其余 9 位取 0-9。
	second := byte('3' + int(h[0])%7)
	var b strings.Builder
	b.WriteByte('1')
	b.WriteByte(second)
	for i := 1; i < 10; i++ {
		b.WriteByte(byte('0' + int(h[i*2])%10))
	}
	return b.String()
}

// normalizedPhone 返回服务端落库用的号码形态（+86 前缀），
// 验证码 key 与身份标识都用这个形态。
func normalizedPhone(t *testing.T, raw string) string {
	t.Helper()
	p, ok := normalize.Phone(raw)
	if !ok {
		t.Fatalf("test generated an invalid phone: %q", raw)
	}
	return p
}

// cleanupUser 精确回收本次用例造的用户，避免污染开发库。
// 按手机号/邮箱标识定位，而不是按昵称——默认昵称可能与历史用户重名。
func cleanupUser(t *testing.T, st *store.Store, identType, identifier string) {
	t.Cleanup(func() {
		ident, err := st.FindIdentity(identType, identifier)
		if err != nil {
			return
		}
		uid := ident.UserID
		st.DB.Exec("DELETE FROM auth_identities WHERE user_id = ?", uid)
		st.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", uid)
		st.DB.Exec("DELETE FROM auth_users WHERE id = ?", uid)
	})
}

// 同一手机号第一次短信登录是 true，隔一段时间（模拟历史账号）再登录必须是 false。
func TestSMSLoginSetsIsNewUserOnlyOnCreation(t *testing.T) {
	svc, codeSvc := newLoginTestService(t)
	phone := randomPhone()

	code, err := codeSvc.Issue(context.Background(), codes.IssueInput{Scene: "login", Target: normalizedPhone(t, phone), Channel: "sms"})
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}

	first, err := svc.SMSLogin(context.Background(), phone, code, LoginMeta{})
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if !first.IsNewUser {
		t.Fatal("first SMS login created the account, want is_new_user=true")
	}
	cleanupUser(t, svc.store, models.IdentityPhone, normalizedPhone(t, phone))

	// 模拟「老账号」：把创建时间往前推一天再登录。若实现用时间窗判断，
	// 这里会错误地返回 true；用建号那一刻的标记则一定是 false。
	if err := svc.store.DB.Exec(
		"UPDATE auth_users SET created_at = ? WHERE id = (SELECT user_id FROM auth_identities WHERE type = ? AND identifier = ?)",
		time.Now().Add(-48*time.Hour), models.IdentityPhone, normalizedPhone(t, phone),
	).Error; err != nil {
		t.Fatalf("backdate user: %v", err)
	}

	code2, err := codeSvc.Issue(context.Background(), codes.IssueInput{Scene: "login", Target: normalizedPhone(t, phone), Channel: "sms"})
	if err != nil {
		t.Fatalf("issue code 2: %v", err)
	}
	second, err := svc.SMSLogin(context.Background(), phone, code2, LoginMeta{})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.IsNewUser {
		t.Fatal("repeat SMS login must not report is_new_user=true")
	}
}

// 同一个新号连登两次（未做时间回填）第二次也必须是 false。
func TestSMSLoginSecondRequestNotNew(t *testing.T) {
	svc, codeSvc := newLoginTestService(t)
	phone := randomPhone()
	defer cleanupUser(t, svc.store, models.IdentityPhone, normalizedPhone(t, phone))

	for i := 0; i < 2; i++ {
		code, err := codeSvc.Issue(context.Background(), codes.IssueInput{Scene: "login", Target: normalizedPhone(t, phone), Channel: "sms"})
		if err != nil {
			t.Fatalf("issue code %d: %v", i, err)
		}
		res, err := svc.SMSLogin(context.Background(), phone, code, LoginMeta{})
		if err != nil {
			t.Fatalf("login %d: %v", i, err)
		}
		want := i == 0
		if res.IsNewUser != want {
			t.Fatalf("login #%d is_new_user = %v, want %v", i+1, res.IsNewUser, want)
		}
	}
}

// 小程序登录首次建号 true，第二次 false。
func TestWeChatMiniSetsIsNewUserOnlyOnCreation(t *testing.T) {
	svc, _ := newLoginTestService(t)

	enc, err := secret.Encrypt(svc.cfg.MasterKey, "real-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	appID := "wx" + uuid.NewString()[:16]
	cl := &models.Client{
		ID:                 idgen.New(),
		ClientKey:          "mini_" + uuid.NewString()[:8],
		Name:               "新用户标记测试",
		Type:               "miniprogram",
		WechatAppID:        appID,
		WechatAppSecretEnc: enc,
		Status:             models.StatusActive,
	}
	if err := svc.store.DB.Create(cl).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	t.Cleanup(func() {
		var userIDs []string
		svc.store.DB.Raw("SELECT DISTINCT user_id FROM auth_identities WHERE identifier LIKE ?", appID+":%").
			Scan(&userIDs)
		for _, uid := range userIDs {
			svc.store.DB.Exec("DELETE FROM auth_identities WHERE user_id = ?", uid)
			svc.store.DB.Exec("DELETE FROM auth_user_roles WHERE user_id = ?", uid)
			svc.store.DB.Exec("DELETE FROM auth_users WHERE id = ?", uid)
		}
		svc.store.DB.Exec("DELETE FROM auth_clients WHERE id = ?", cl.ID)
	})

	first, err := svc.WeChatMini(context.Background(), cl.ClientKey, "js-code", LoginMeta{})
	if err != nil {
		t.Fatalf("first mini login: %v", err)
	}
	if !first.IsNewUser {
		t.Fatal("first mini login created the account, want is_new_user=true")
	}
	second, err := svc.WeChatMini(context.Background(), cl.ClientKey, "js-code", LoginMeta{})
	if err != nil {
		t.Fatalf("second mini login: %v", err)
	}
	if second.IsNewUser {
		t.Fatal("repeat mini login must not report is_new_user=true")
	}
}

// 密码登录永远不建号；续期也不可能是新用户。两者都必须显式为 false。
func TestPasswordLoginAndRefreshNeverNew(t *testing.T) {
	svc, _ := newLoginTestService(t)

	email := "u" + uuid.NewString()[:8] + "@example.com"
	hash, err := password.Hash("pass12345")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := time.Now()
	user := models.User{Nickname: "密码用户", PasswordHash: &hash, Status: models.StatusActive}
	if err := svc.store.CreateUser(nil, &user, models.RoleUser); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := svc.store.AddIdentity(nil, &models.Identity{
		UserID: user.ID, Type: models.IdentityEmail, Identifier: email, VerifiedAt: &now,
	}); err != nil {
		t.Fatalf("add identity: %v", err)
	}
	cleanupUser(t, svc.store, models.IdentityEmail, email)

	res, err := svc.PasswordLogin(context.Background(), email, "pass12345", LoginMeta{})
	if err != nil {
		t.Fatalf("password login: %v", err)
	}
	if res.IsNewUser {
		t.Fatal("password login never creates accounts, want is_new_user=false")
	}

	refreshed, err := svc.Refresh(res.RefreshToken, LoginMeta{})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.IsNewUser {
		t.Fatal("refresh must not report is_new_user=true")
	}
}