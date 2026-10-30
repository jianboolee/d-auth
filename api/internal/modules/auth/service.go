package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/mailer"
	"github.com/jianboolee/d-auth/internal/infra/password"
	"github.com/jianboolee/d-auth/internal/infra/secret"
	"github.com/jianboolee/d-auth/internal/infra/sms"
	"github.com/jianboolee/d-auth/internal/infra/tokens"
	"github.com/jianboolee/d-auth/internal/infra/wechat"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/codes"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/sessionstore"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usercache"
	"github.com/jianboolee/d-auth/internal/normalize"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Service struct {
	cfg      *config.Config
	store    *store.Store
	codes    *codes.Service
	sms      sms.Sender
	mail     mailer.Sender
	jwt      *tokens.Manager
	wechat   wechat.Client
	rdb      *redis.Client
	sessions *sessionstore.Store
	// cache 缓存组装好的用户资料。登录会更新 last_login_at、提升超管会改角色，
	// 两处都要让缓存失效，否则 internal 接口会返回陈旧的「最近登录时间」。
	cache *usercache.Cache
}

func New(cfg *config.Config, st *store.Store, c *codes.Service, smsSender sms.Sender, mailSender mailer.Sender, jwt *tokens.Manager, wx wechat.Client, rdb *redis.Client, sessions *sessionstore.Store, cache *usercache.Cache) *Service {
	return &Service{cfg: cfg, store: st, codes: c, sms: smsSender, mail: mailSender, jwt: jwt, wechat: wx, rdb: rdb, sessions: sessions, cache: cache}
}

type LoginMeta struct {
	IP        string
	UserAgent string
	ClientKey string
}

// 各渠道允许的验证码场景，只列出服务端真正会 Verify 的场景。
//
// scene 会进入验证码 key 与短信模板选择，必须白名单化：否则攻击者可以对同一个
// 号码不断更换 scene，每次拿到一份新的限流配额，而未知 scene 仍会发出真实短信。
//
// 注意：若要新增「短信重置密码」，需把 reset_password 加到 smsScenes，
// 否则会被当成非法场景拒绝。
var (
	smsScenes = map[string]struct{}{
		"login":      {},
		"bind_phone": {},
		// delete_account：注销账号前验证当前手机号。让「手机丢了但登录态还在」
		// 的人无法直接抹掉账号，需要证明自己掌握绑定的号码。
		"delete_account": {},
	}
	emailScenes = map[string]struct{}{
		"register":       {},
		"reset_password": {},
		"bind_email":     {},
	}
)

func sceneAllowed(allowed map[string]struct{}, scene, fallback string) (string, bool) {
	if scene == "" {
		scene = fallback
	}
	if _, ok := allowed[scene]; !ok {
		return "", false
	}
	return scene, true
}

func (s *Service) SendSMS(ctx context.Context, phoneRaw, scene, ip string) (string, error) {
	phone, ok := normalize.Phone(phoneRaw)
	if !ok {
		return "", apperr.New(apperr.ErrInvalidArgument, "INVALID_PHONE", "手机号不正确")
	}
	scene, ok = sceneAllowed(smsScenes, scene, "login")
	if !ok {
		return "", apperr.New(apperr.ErrInvalidArgument, "INVALID_SCENE", "不支持的场景")
	}
	code, err := s.codes.Issue(ctx, codes.IssueInput{Scene: scene, Target: phone, IP: ip, Channel: "sms"})
	if err != nil {
		return "", err
	}
	slog.Info("verification code sent", "channel", "sms", "scene", scene,
		"target", normalize.MaskTarget(phone), "ip", ip)
	s.logDevCode("sms", phone, scene, code)
	if err := s.sms.Send(ctx, phone, code, scene); err != nil {
		return "", apperr.New(err, "SMS_FAILED", "短信发送失败")
	}
	return code, nil
}

// SendSMSForUser 给指定用户「当前绑定」的手机号发验证码。
//
// 号码由服务端解析，前端只传 userID：/v1/me 返回的手机号是脱敏的
// （138****8000），前端拿不到真实号码，也无法把它交给 SendSMS。
// 供 delete_account 这类「证明自己掌握绑定号码」的场景使用。
func (s *Service) SendSMSForUser(ctx context.Context, userID, scene, ip string) (string, error) {
	idents, err := s.store.IdentitiesOf(userID)
	if err != nil {
		return "", err
	}
	phone := ""
	for _, id := range idents {
		if id.Type == models.IdentityPhone {
			phone = id.Identifier
			break
		}
	}
	if phone == "" {
		return "", apperr.New(apperr.ErrInvalidArgument, "NO_PHONE", "未绑定手机号")
	}
	return s.SendSMS(ctx, phone, scene, ip)
}

func (s *Service) SMSLogin(ctx context.Context, phoneRaw, code string, meta LoginMeta) (*dto.TokenResult, error) {
	phone, ok := normalize.Phone(phoneRaw)
	if !ok {
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_PHONE", "手机号不正确")
	}
	if err := s.codes.Verify(ctx, "login", phone, code); err != nil {
		return nil, err
	}
	user, created, err := s.findOrCreateByIdentity(models.IdentityPhone, phone, normalize.MaskPhoneDisplay(phone))
	if err != nil {
		return nil, err
	}
	return s.issueToken(user, meta, created)
}

func (s *Service) SendEmail(ctx context.Context, emailRaw, scene, ip string) (string, error) {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return "", apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	scene, ok = sceneAllowed(emailScenes, scene, "register")
	if !ok {
		return "", apperr.New(apperr.ErrInvalidArgument, "INVALID_SCENE", "不支持的场景")
	}
	code, err := s.codes.Issue(ctx, codes.IssueInput{Scene: scene, Target: email, IP: ip, Channel: "email"})
	if err != nil {
		return "", err
	}
	slog.Info("verification code sent", "channel", "email", "scene", scene,
		"target", normalize.MaskTarget(email), "ip", ip)
	s.logDevCode("email", email, scene, code)
	if err := s.mail.SendCode(ctx, email, code, scene); err != nil {
		return "", apperr.New(err, "MAIL_FAILED", "邮件发送失败")
	}
	return code, nil
}

func (s *Service) EmailRegister(ctx context.Context, emailRaw, code, pass, nickname string, meta LoginMeta) (*dto.TokenResult, error) {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	if !password.ValidPlain(pass) {
		return nil, apperr.New(apperr.ErrInvalidArgument, "WEAK_PASSWORD", "密码至少 8 位")
	}
	if err := s.codes.Verify(ctx, "register", email, code); err != nil {
		return nil, err
	}
	if _, err := s.store.FindIdentity(models.IdentityEmail, email); err == nil {
		return nil, apperr.New(apperr.ErrEmailTaken, "EMAIL_TAKEN", "邮箱已被注册")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := password.Hash(pass)
	if err != nil {
		return nil, err
	}
	if nickname == "" {
		nickname = defaultNickname("用户", email)
	}
	now := time.Now()
	var user models.User
	err = s.store.DB.Transaction(func(tx *gorm.DB) error {
		user = models.User{Nickname: nickname, PasswordHash: &hash, Status: models.StatusActive}
		if err := s.store.CreateUser(tx, &user, models.RoleUser); err != nil {
			return err
		}
		return s.store.AddIdentity(tx, &models.Identity{
			UserID: user.ID, Type: models.IdentityEmail, Identifier: email, VerifiedAt: &now,
		})
	})
	if err != nil {
		return nil, err
	}
	s.maybePromoteAdmin(&user, "", email)
	return s.issueToken(&user, meta, true)
}

// PasswordLogin 用手机号或邮箱 + 密码登录。密码属于账号本身，因此绑定了手机号的
// 账号可以用任一已绑定标识登录。仅短信登录创建的账号没有设置密码，会返回凭据错误。
func (s *Service) PasswordLogin(_ context.Context, identifierRaw, pass string, meta LoginMeta) (*dto.TokenResult, error) {
	typ, identifier, ok := normalize.Identifier(identifierRaw)
	if !ok {
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_IDENTIFIER", "请输入正确的手机号或邮箱")
	}
	return s.passwordLoginByIdentity(typ, identifier, pass, meta)
}

func (s *Service) EmailLogin(_ context.Context, emailRaw, pass string, meta LoginMeta) (*dto.TokenResult, error) {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	return s.passwordLoginByIdentity(models.IdentityEmail, email, pass, meta)
}

// passwordLoginByIdentity 是密码登录的共用实现：按标识找到账号后校验密码。
// 找不到标识与密码错误返回同一种错误，避免暴露某个手机号/邮箱是否已注册。
func (s *Service) passwordLoginByIdentity(typ, identifier, pass string, meta LoginMeta) (*dto.TokenResult, error) {
	invalid := apperr.New(apperr.ErrUnauthorized, "INVALID_CREDENTIALS", "账号或密码不正确")
	ident, err := s.store.FindIdentity(typ, identifier)
	if err != nil {
		return nil, invalid
	}
	user, err := s.store.UserByID(ident.UserID)
	if err != nil {
		return nil, err
	}
	if user.PasswordHash == nil || !password.Verify(pass, *user.PasswordHash) {
		return nil, invalid
	}
	// 密码登录只校验已有账号，绝不建号，因此 is_new_user 恒为 false。
	return s.issueToken(user, meta, false)
}

func (s *Service) ForgotPassword(ctx context.Context, emailRaw, ip string) error {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	if _, err := s.store.FindIdentity(models.IdentityEmail, email); err != nil {
		return nil
	}
	_, err := s.SendEmail(ctx, email, "reset_password", ip)
	return err
}

func (s *Service) ResetPassword(ctx context.Context, emailRaw, code, pass string) error {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	if !password.ValidPlain(pass) {
		return apperr.New(apperr.ErrInvalidArgument, "WEAK_PASSWORD", "密码至少 8 位")
	}
	if err := s.codes.Verify(ctx, "reset_password", email, code); err != nil {
		return err
	}
	ident, err := s.store.FindIdentity(models.IdentityEmail, email)
	if err != nil {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "账号不存在")
	}
	hash, err := password.Hash(pass)
	if err != nil {
		return err
	}
	if err := s.store.DB.Model(&models.User{}).Where("id = ?", ident.UserID).Updates(map[string]any{
		"password_hash": hash, "updated_at": time.Now(),
	}).Error; err != nil {
		return err
	}
	return s.revokeUserSessions(ident.UserID, "")
}

// Refresh 用一枚 refresh token 换新的 access token，并轮换 refresh。
//
// 会话是否可续期完全由 Redis 白名单裁决（fail-closed）：命中才发新 token，
// 未命中（不存在/已过期/已吊销）统一返回「登录已失效」。轮换是原子的，
// 并发用同一枚旧 refresh 时只有一个成功。
func (s *Service) Refresh(refreshToken string, meta LoginMeta) (*dto.TokenResult, error) {
	if refreshToken == "" {
		return nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "未登录")
	}
	// 先轮换（旧键删、新键写），拿到旧记录里的归属信息。
	// 轮换失败即视为无效 refresh，不区分原因。
	newRaw, err := secret.RandomToken(32)
	if err != nil {
		return nil, err
	}
	entry, err := s.sessions.Rotate(context.Background(), refreshToken, newRaw)
	if err != nil {
		return nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "登录已失效")
	}
	user, err := s.store.UserByID(entry.UserID)
	if err != nil {
		return nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "登录已失效")
	}
	if user.Status != models.StatusActive {
		return nil, apperr.New(apperr.ErrDisabled, "DISABLED", "账号已停用")
	}
	roles, _ := s.store.RoleCodes(user.ID)
	cid := uuid.Nil
	if entry.ClientID != "" {
		cid, _ = uuid.Parse(entry.ClientID)
	}
	access, _, err := s.jwt.Sign(user.ID, cid, roles)
	if err != nil {
		return nil, err
	}
	// 续期不可能是登录：账号在拿到 refresh 之前就已存在，is_new_user 恒为 false。
	return s.pack(user, roles, access, newRaw, false), nil
}

// RevokeSession 吊销一枚 refresh token（登出）。幂等：token 为空或本就不存在时
// 直接返回 nil。登出只「停止续期」，已签发的 access token 在 TTL 内仍可用。
func (s *Service) RevokeSession(refreshToken string) error {
	return s.sessions.RevokeByToken(context.Background(), refreshToken)
}

func (s *Service) WeChatQRStart(ctx context.Context) (map[string]any, error) {
	ticket := idgen.NewString()
	payload := map[string]any{"status": "pending"}
	b, _ := json.Marshal(payload)
	if err := s.rdb.Set(ctx, "wxqr:"+ticket, b, 10*time.Minute).Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"ticket": ticket,
		"qr_url": s.wechat.WebQRAuthorizeURL(ticket),
	}, nil
}

func (s *Service) WeChatQRStatus(ctx context.Context, ticket string) (map[string]any, *dto.TokenResult, error) {
	raw, err := s.rdb.Get(ctx, "wxqr:"+ticket).Bytes()
	if err != nil {
		return nil, nil, apperr.New(apperr.ErrNotFound, "QR_EXPIRED", "二维码已过期")
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	status, _ := payload["status"].(string)
	if status != "confirmed" {
		return payload, nil, nil
	}
	userID, _ := payload["user_id"].(string)
	uid, err := idgen.ParseUserID(userID)
	if err != nil {
		return payload, nil, nil
	}
	user, err := s.store.UserByID(uid)
	if err != nil {
		return nil, nil, err
	}
	// 扫码流程里账号是在 WeChatCallback 建的，到这里已经隔了一次请求，
	// 「本次请求是否建号」无从判断。这条路径属于控制台登录、不承载新用户引导，
	// 因此固定为 false，而不是拿创建时间猜。
	tokens, err := s.issueToken(user, LoginMeta{ClientKey: models.ClientKeyConsole}, false)
	if err != nil {
		return nil, nil, err
	}
	_ = s.rdb.Del(ctx, "wxqr:"+ticket).Err()
	return map[string]any{"status": "confirmed"}, tokens, nil
}

func (s *Service) WeChatCallback(ctx context.Context, code, ticket string, meta LoginMeta) error {
	info, err := s.wechat.WebCallback(ctx, code)
	if err != nil {
		return apperr.New(err, "WECHAT_FAILED", "微信授权失败")
	}
	// 扫码登录的建号发生在回调里，签发 token 时已隔一次请求，无法判断，见 WeChatQRStatus。
	user, _, err := s.upsertWechatUser(models.IdentityWechatOpen, info)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"status": "confirmed", "user_id": idgen.FormatUserID(user.ID)})
	return s.rdb.Set(ctx, "wxqr:"+ticket, payload, 5*time.Minute).Err()
}

func (s *Service) WeChatMini(ctx context.Context, clientKey, jsCode string, meta LoginMeta) (*dto.TokenResult, error) {
	cl, err := s.store.ClientByKey(clientKey)
	if err != nil {
		return nil, apperr.New(apperr.ErrInvalidArgument, "UNKNOWN_CLIENT", "未知应用")
	}
	secretPlain, err := secret.Decrypt(s.cfg.MasterKey, cl.WechatAppSecretEnc)
	if err != nil {
		return nil, err
	}
	if secretPlain == "" && s.cfg.IsDev() {
		secretPlain = "mock"
	}
	// 凭据缺失时，请求会带着空 appid/secret 打到微信、失败后统一报「小程序登录
	// 失败」，看不出是没配置还是微信侧报错。非 dev 环境先在这里明确挡下。
	// dev 保留上面的 mock 回退，否则本地不带真凭据就跑不通小程序登录。
	if !s.cfg.IsDev() && (cl.WechatAppID == "" || secretPlain == "") {
		return nil, apperr.New(apperr.ErrInvalidArgument, "WECHAT_NOT_CONFIGURED",
			"该小程序未配置微信凭据（AppID / AppSecret），请先在后台补齐")
	}
	info, err := s.wechat.MiniCode2Session(ctx, cl.WechatAppID, secretPlain, jsCode)
	if err != nil {
		return nil, apperr.New(err, "WECHAT_FAILED", "小程序登录失败")
	}
	info.AppID = cl.WechatAppID
	user, created, err := s.upsertWechatUser(models.IdentityWechatMini, info)
	if err != nil {
		return nil, err
	}
	meta.ClientKey = clientKey
	return s.issueToken(user, meta, created)
}

func (s *Service) MockWeChatConfirm(ctx context.Context, ticket, openID string) error {
	if openID == "" {
		openID = "mock-open-" + ticket[:8]
	}
	info := wechat.UserInfo{OpenID: openID, UnionID: "mock-union", AppID: "mock-web"}
	// 扫码登录的建号发生在回调里，签发 token 时已隔一次请求，无法判断，见 WeChatQRStatus。
	user, _, err := s.upsertWechatUser(models.IdentityWechatOpen, info)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"status": "confirmed", "user_id": idgen.FormatUserID(user.ID)})
	return s.rdb.Set(ctx, "wxqr:"+ticket, payload, 5*time.Minute).Err()
}

func (s *Service) JWKS() map[string]any { return s.jwt.JWKS() }

func (s *Service) ParseAccess(token string) (*tokens.Claims, error) {
	return s.jwt.Parse(token)
}

// findOrCreateByIdentity 按「标识类型 + 标识」找到账号，找不到就建号。
// 第二个返回值表示本次调用是否真的建了号——调用方要拿它填 is_new_user，
// 不能用创建时间之类的间接指标去猜。
func (s *Service) findOrCreateByIdentity(typ, identifier, nickname string) (*models.User, bool, error) {
	ident, err := s.store.FindIdentity(typ, identifier)
	if err == nil {
		user, uerr := s.store.UserByID(ident.UserID)
		return user, false, uerr
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	now := time.Now()
	var user models.User
	err = s.store.DB.Transaction(func(tx *gorm.DB) error {
		user = models.User{Nickname: nickname, Status: models.StatusActive}
		if err := s.store.CreateUser(tx, &user, models.RoleUser); err != nil {
			return err
		}
		return s.store.AddIdentity(tx, &models.Identity{
			UserID: user.ID, Type: typ, Identifier: identifier, VerifiedAt: &now,
		})
	})
	if err != nil {
		return nil, false, err
	}
	phone, email := "", ""
	if typ == models.IdentityPhone {
		phone = identifier
	}
	if typ == models.IdentityEmail {
		email = identifier
	}
	s.maybePromoteAdmin(&user, phone, email)
	return &user, true, nil
}

// upsertWechatUser 同上：第二个返回值是「本次是否建号」。
func (s *Service) upsertWechatUser(typ string, info wechat.UserInfo) (*models.User, bool, error) {
	ident, err := s.store.FindWechat(info.UnionID, info.AppID, info.OpenID)
	if err == nil {
		user, uerr := s.store.UserByID(ident.UserID)
		return user, false, uerr
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	now := time.Now()
	identifier := info.OpenID
	if info.AppID != "" {
		identifier = info.AppID + ":" + info.OpenID
	}
	extra := models.JSONMap{"union_id": info.UnionID, "app_id": info.AppID}
	var user models.User
	err = s.store.DB.Transaction(func(tx *gorm.DB) error {
		user = models.User{Nickname: "微信用户", Status: models.StatusActive}
		if err := s.store.CreateUser(tx, &user, models.RoleUser); err != nil {
			return err
		}
		return s.store.AddIdentity(tx, &models.Identity{
			UserID: user.ID, Type: typ, Identifier: identifier, Extra: extra, VerifiedAt: &now,
		})
	})
	if err != nil {
		return nil, false, err
	}
	return &user, true, nil
}

// issueToken 完成一次登录：签发 access JWT 并把 refresh token 登记进 Redis 白名单。
// 不再写任何会话表——会话状态只存在于 Redis。
//
// isNewUser 由建号那一步给出（见 findOrCreateByIdentity / upsertWechatUser），
// 这里只负责透传，不要在这里用创建时间反推。
func (s *Service) issueToken(user *models.User, meta LoginMeta, isNewUser bool) (*dto.TokenResult, error) {
	if user.Status != models.StatusActive {
		return nil, apperr.New(apperr.ErrDisabled, "DISABLED", "账号已停用")
	}
	// 没有会话存储就无法登记 refresh，等于发不出可续期的会话。生产环境必须配好
	// Redis；这里显式挡下（非 coded 错误 → 500），而不是静默发一枚永远续不了期的
	// access token。
	if s.sessions == nil {
		return nil, fmt.Errorf("sessionstore not configured")
	}
	if meta.ClientKey == "" {
		meta.ClientKey = models.ClientKeyConsole
	}
	var clientID *uuid.UUID
	if cl, err := s.store.ClientByKey(meta.ClientKey); err == nil {
		id := cl.ID
		clientID = &id
	}
	refreshRaw, err := secret.RandomToken(32)
	if err != nil {
		return nil, err
	}
	cid := uuid.Nil
	entry := sessionstore.Entry{UserID: user.ID, Family: idgen.NewString()}
	if clientID != nil {
		cid = *clientID
		entry.ClientID = cid.String()
	}
	// 先登记 refresh 再签 access：若 Redis 不可用，这里直接失败（非 coded 错误 → 500），
	// 不会发出一枚「永远续不了期」的 access（fail-closed）。
	if err := s.sessions.Issue(context.Background(), refreshRaw, entry); err != nil {
		return nil, fmt.Errorf("session store issue: %w", err)
	}
	roles, _ := s.store.RoleCodes(user.ID)
	access, _, err := s.jwt.Sign(user.ID, cid, roles)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_ = s.store.DB.Model(user).Updates(map[string]any{"last_login_at": now, "last_login_ip": meta.IP, "updated_at": now}).Error
	user.LastLoginAt = &now
	// last_login_at 会出现在用户资料里，登录后必须让缓存失效。
	s.cache.Del(context.Background(), user.ID)
	return s.pack(user, roles, access, refreshRaw, isNewUser), nil
}

// pack 组装响应。isNewUser 由调用方给出：只有登录路径可能为 true，
// 续期（Refresh）永远是 false。
func (s *Service) pack(user *models.User, roles []string, access, refresh string, isNewUser bool) *dto.TokenResult {
	idents, _ := s.store.IdentitiesOf(user.ID)
	return &dto.TokenResult{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.cfg.JWTAccessTTL.Seconds()),
		IsNewUser:    isNewUser,
		// 登录响应属于自助接口，手机号/邮箱脱敏后再返回。
		User: dto.Masked(dto.FromUser(user, roles, idents, s.cfg.AvatarURL)),
	}
}

// PublicUser 返回「本人视角」的用户信息，手机号/邮箱脱敏。
// 后台与 internal 接口不用它——那些走 admin.GetUser，保留明文供管理员核对与编辑。
func (s *Service) PublicUser(userID string) (dto.User, error) {
	user, err := s.store.UserByID(userID)
	if err != nil {
		return dto.User{}, err
	}
	roles, _ := s.store.RoleCodes(userID)
	idents, _ := s.store.IdentitiesOf(userID)
	return dto.Masked(dto.FromUser(user, roles, idents, s.cfg.AvatarURL)), nil
}

func (s *Service) maybePromoteAdmin(user *models.User, phone, email string) {
	wantPhone := s.cfg.InitAdminPhone
	wantEmail := s.cfg.InitAdminEmail
	if wantPhone == "" && wantEmail == "" {
		return
	}
	match := (wantPhone != "" && phone == wantPhone) || (wantEmail != "" && email == wantEmail)
	if !match {
		return
	}
	var n int64
	s.store.DB.Raw(`SELECT COUNT(1) FROM auth_user_roles ur JOIN auth_roles r ON r.id = ur.role_id WHERE r.code = 'superadmin'`).Scan(&n)
	if n > 0 {
		return
	}
	_ = s.store.AddRoles(nil, user.ID, models.RoleSuperAdmin, models.RoleAdmin)
	// 角色变了，缓存里的 is_admin / roles 立即作废。
	s.cache.Del(context.Background(), user.ID)
}

// RevokeUserSessions 供其它模块（改密、停用、注销）吊销某用户全部 refresh token。
// keepRaw 非空时保留该枚，用于「改密但留在当前设备」。
func (s *Service) RevokeUserSessions(userID, keepRaw string) error {
	return s.revokeUserSessions(userID, keepRaw)
}

// revokeUserSessions 吊销某用户的全部 refresh token（改密 / 重置密码 / 停用 / 注销），
// 即「停止续期」。keepRaw 非空时保留该枚（改密场景留下当前设备）。
// access token 在 TTL 内仍可用，这是纯验签方案的既定窗口。
func (s *Service) revokeUserSessions(userID, keepRaw string) error {
	return s.sessions.RevokeUserAllExcept(context.Background(), userID, keepRaw)
}

func defaultNickname(prefix, ident string) string {
	if len(ident) >= 4 {
		return prefix + ident[len(ident)-4:]
	}
	return prefix
}

func HashAPIKey(full string) string {
	sum := sha256.Sum256([]byte(full))
	return hex.EncodeToString(sum[:])
}

func (s *Service) Store() *store.Store { return s.store }

func ClientKeyFrom(header string) string {
	if header == "" {
		return models.ClientKeyConsole
	}
	return strings.TrimSpace(header)
}

func (s *Service) DebugCodeEnabled() bool { return s.cfg.IsDev() }

func (s *Service) logDevCode(channel, target, scene, code string) {
	if !s.DebugCodeEnabled() {
		return
	}
	slog.Info("dev verification code", "channel", channel, "target", target, "scene", scene, "code", code)
}

func DebugHashHint(scene, target, code string) string {
	return fmt.Sprintf("%s/%s", scene, target)
}
