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
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usercache"
	"github.com/jianboolee/d-auth/internal/normalize"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Service struct {
	cfg    *config.Config
	store  *store.Store
	codes  *codes.Service
	sms    sms.Sender
	mail   mailer.Sender
	jwt    *tokens.Manager
	wechat wechat.Client
	rdb    *redis.Client
	// cache 缓存组装好的用户资料。登录会更新 last_login_at、提升超管会改角色，
	// 两处都要让缓存失效，否则 internal 接口会返回陈旧的「最近登录时间」。
	cache *usercache.Cache
}

func New(cfg *config.Config, st *store.Store, c *codes.Service, smsSender sms.Sender, mailSender mailer.Sender, jwt *tokens.Manager, wx wechat.Client, rdb *redis.Client, cache *usercache.Cache) *Service {
	return &Service{cfg: cfg, store: st, codes: c, sms: smsSender, mail: mailSender, jwt: jwt, wechat: wx, rdb: rdb, cache: cache}
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

func (s *Service) SMSLogin(ctx context.Context, phoneRaw, code string, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	phone, ok := normalize.Phone(phoneRaw)
	if !ok {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_PHONE", "手机号不正确")
	}
	if err := s.codes.Verify(ctx, "login", phone, code); err != nil {
		return nil, nil, err
	}
	user, err := s.findOrCreateByIdentity(models.IdentityPhone, phone, normalize.MaskPhoneDisplay(phone))
	if err != nil {
		return nil, nil, err
	}
	return s.issueSession(user, meta)
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

func (s *Service) EmailRegister(ctx context.Context, emailRaw, code, pass, nickname string, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	if !password.ValidPlain(pass) {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "WEAK_PASSWORD", "密码至少 8 位")
	}
	if err := s.codes.Verify(ctx, "register", email, code); err != nil {
		return nil, nil, err
	}
	if _, err := s.store.FindIdentity(models.IdentityEmail, email); err == nil {
		return nil, nil, apperr.New(apperr.ErrEmailTaken, "EMAIL_TAKEN", "邮箱已被注册")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	hash, err := password.Hash(pass)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, err
	}
	s.maybePromoteAdmin(&user, "", email)
	return s.issueSession(&user, meta)
}

// PasswordLogin 用手机号或邮箱 + 密码登录。密码属于账号本身，因此绑定了手机号的
// 账号可以用任一已绑定标识登录。仅短信登录创建的账号没有设置密码，会返回凭据错误。
func (s *Service) PasswordLogin(_ context.Context, identifierRaw, pass string, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	typ, identifier, ok := normalize.Identifier(identifierRaw)
	if !ok {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_IDENTIFIER", "请输入正确的手机号或邮箱")
	}
	return s.passwordLoginByIdentity(typ, identifier, pass, meta)
}

func (s *Service) EmailLogin(_ context.Context, emailRaw, pass string, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	return s.passwordLoginByIdentity(models.IdentityEmail, email, pass, meta)
}

// passwordLoginByIdentity 是密码登录的共用实现：按标识找到账号后校验密码。
// 找不到标识与密码错误返回同一种错误，避免暴露某个手机号/邮箱是否已注册。
func (s *Service) passwordLoginByIdentity(typ, identifier, pass string, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	invalid := apperr.New(apperr.ErrUnauthorized, "INVALID_CREDENTIALS", "账号或密码不正确")
	ident, err := s.store.FindIdentity(typ, identifier)
	if err != nil {
		return nil, nil, invalid
	}
	user, err := s.store.UserByID(ident.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user.PasswordHash == nil || !password.Verify(pass, *user.PasswordHash) {
		return nil, nil, invalid
	}
	return s.issueSession(user, meta)
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
	return s.revokeUserSessions(ident.UserID, uuid.Nil)
}

func (s *Service) Refresh(refreshToken string, sessionID uuid.UUID, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	var sess models.Session
	q := s.store.DB.Where("revoked_at IS NULL AND expires_at > ?", time.Now())
	if refreshToken != "" {
		q = q.Where("refresh_token_hash = ?", store.HashToken(refreshToken))
	} else if sessionID != uuid.Nil {
		q = q.Where("id = ?", sessionID)
	} else {
		return nil, nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "未登录")
	}
	if err := q.First(&sess).Error; err != nil {
		return nil, nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "登录已失效")
	}
	user, err := s.store.UserByID(sess.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != models.StatusActive {
		return nil, nil, apperr.New(apperr.ErrDisabled, "DISABLED", "账号已停用")
	}
	newRaw, err := secret.RandomToken(32)
	if err != nil {
		return nil, nil, err
	}
	roles, _ := s.store.RoleCodes(user.ID)
	cid := uuid.Nil
	if sess.ClientID != nil {
		cid = *sess.ClientID
	}
	access, jti, err := s.jwt.Sign(user.ID, sess.ID, cid, roles)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	if err := s.store.DB.Model(&sess).Updates(map[string]any{
		"refresh_token_hash": store.HashToken(newRaw),
		"access_jti":         jti,
		"updated_at":         now,
		"expires_at":         now.Add(s.cfg.JWTRefreshTTL),
		"ip":                 meta.IP,
	}).Error; err != nil {
		return nil, nil, err
	}
	sess.RefreshTokenHash = store.HashToken(newRaw)
	return s.pack(user, roles, access, newRaw), &sess, nil
}

// RevokeSession 吊销一次登录会话。会话有两条定位路径：session ID（cookie 凭据）
// 与 refresh token（token 凭据，即使 access token 已过期也能定位）。
// 两者都没有表示调用方本来就没登录，按幂等处理直接返回 nil；
// 已吊销的会话重复调用同样返回 nil。
func (s *Service) RevokeSession(sessionID uuid.UUID, refreshToken string) error {
	q := s.store.DB.Model(&models.Session{}).Where("revoked_at IS NULL")
	switch {
	case refreshToken != "":
		q = q.Where("refresh_token_hash = ?", store.HashToken(refreshToken))
	case sessionID != uuid.Nil:
		q = q.Where("id = ?", sessionID)
	default:
		return nil
	}
	now := time.Now()
	return q.Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
}

func (s *Service) SessionByID(id uuid.UUID) (*models.Session, error) {
	var sess models.Session
	err := s.store.DB.Where("id = ? AND revoked_at IS NULL AND expires_at > ?", id, time.Now()).First(&sess).Error
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// SlidingRenew 把会话的过期时间顺延为「现在 + JWTRefreshTTL」，实现滑动过期：
// 只要持续使用就不会掉线，只有连续 TTL 时间不活跃才失效。
//
// 不是每个请求都写库，而是用一条带条件的原子 UPDATE 做节流：仅当
// expires_at 已经落后于 (now + TTL - interval) 时才更新，等价于「距上次延长
// 已超过 interval」。这样活跃会话最多每 interval 写一次库，同时也避免了
// 先读后写带来的竞态。
//
// 返回是否真的延长了。配置非法（interval <= 0 或 >= TTL）时不做任何事，
// 退回固定过期 —— 否则会把每个请求都变成一次写入。
//
// WHERE 里带 expires_at > now 是必要的：没有它，一个已过期的会话会被下一次
// 带该 cookie 的请求「复活」。当前调用方 SessionByID 已经先挡了一层，
// 但把这条约束放在这里才让方法自身安全。
func (s *Service) SlidingRenew(id uuid.UUID) (bool, error) {
	interval := s.cfg.SessionRenewInterval
	ttl := s.cfg.JWTRefreshTTL
	if interval <= 0 || interval >= ttl {
		return false, nil
	}
	now := time.Now()
	res := s.store.DB.Model(&models.Session{}).
		Where("id = ? AND revoked_at IS NULL AND expires_at > ? AND expires_at < ?",
			id, now, now.Add(ttl-interval)).
		Updates(map[string]any{
			"expires_at": now.Add(ttl),
			"updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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

func (s *Service) WeChatQRStatus(ctx context.Context, ticket string) (map[string]any, *dto.TokenResult, *models.Session, error) {
	raw, err := s.rdb.Get(ctx, "wxqr:"+ticket).Bytes()
	if err != nil {
		return nil, nil, nil, apperr.New(apperr.ErrNotFound, "QR_EXPIRED", "二维码已过期")
	}
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	status, _ := payload["status"].(string)
	if status != "confirmed" {
		return payload, nil, nil, nil
	}
	userID, _ := payload["user_id"].(string)
	uid, err := idgen.ParseUserID(userID)
	if err != nil {
		return payload, nil, nil, nil
	}
	user, err := s.store.UserByID(uid)
	if err != nil {
		return nil, nil, nil, err
	}
	tokens, sess, err := s.issueSession(user, LoginMeta{ClientKey: models.ClientKeyConsole})
	if err != nil {
		return nil, nil, nil, err
	}
	_ = s.rdb.Del(ctx, "wxqr:"+ticket).Err()
	return map[string]any{"status": "confirmed"}, tokens, sess, nil
}

func (s *Service) WeChatCallback(ctx context.Context, code, ticket string, meta LoginMeta) error {
	info, err := s.wechat.WebCallback(ctx, code)
	if err != nil {
		return apperr.New(err, "WECHAT_FAILED", "微信授权失败")
	}
	user, err := s.upsertWechatUser(models.IdentityWechatOpen, info)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"status": "confirmed", "user_id": idgen.FormatUserID(user.ID)})
	return s.rdb.Set(ctx, "wxqr:"+ticket, payload, 5*time.Minute).Err()
}

func (s *Service) WeChatMini(ctx context.Context, clientKey, jsCode string, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	cl, err := s.store.ClientByKey(clientKey)
	if err != nil {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "UNKNOWN_CLIENT", "未知应用")
	}
	secretPlain, err := secret.Decrypt(s.cfg.MasterKey, cl.WechatAppSecretEnc)
	if err != nil {
		return nil, nil, err
	}
	if secretPlain == "" && s.cfg.IsDev() {
		secretPlain = "mock"
	}
	// 凭据缺失时，请求会带着空 appid/secret 打到微信、失败后统一报「小程序登录
	// 失败」，看不出是没配置还是微信侧报错。非 dev 环境先在这里明确挡下。
	// dev 保留上面的 mock 回退，否则本地不带真凭据就跑不通小程序登录。
	if !s.cfg.IsDev() && (cl.WechatAppID == "" || secretPlain == "") {
		return nil, nil, apperr.New(apperr.ErrInvalidArgument, "WECHAT_NOT_CONFIGURED",
			"该小程序未配置微信凭据（AppID / AppSecret），请先在后台补齐")
	}
	info, err := s.wechat.MiniCode2Session(ctx, cl.WechatAppID, secretPlain, jsCode)
	if err != nil {
		return nil, nil, apperr.New(err, "WECHAT_FAILED", "小程序登录失败")
	}
	info.AppID = cl.WechatAppID
	user, err := s.upsertWechatUser(models.IdentityWechatMini, info)
	if err != nil {
		return nil, nil, err
	}
	meta.ClientKey = clientKey
	return s.issueSession(user, meta)
}

func (s *Service) MockWeChatConfirm(ctx context.Context, ticket, openID string) error {
	if openID == "" {
		openID = "mock-open-" + ticket[:8]
	}
	info := wechat.UserInfo{OpenID: openID, UnionID: "mock-union", AppID: "mock-web"}
	user, err := s.upsertWechatUser(models.IdentityWechatOpen, info)
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

func (s *Service) findOrCreateByIdentity(typ, identifier, nickname string) (*models.User, error) {
	ident, err := s.store.FindIdentity(typ, identifier)
	if err == nil {
		return s.store.UserByID(ident.UserID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
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
		return nil, err
	}
	phone, email := "", ""
	if typ == models.IdentityPhone {
		phone = identifier
	}
	if typ == models.IdentityEmail {
		email = identifier
	}
	s.maybePromoteAdmin(&user, phone, email)
	return &user, nil
}

func (s *Service) upsertWechatUser(typ string, info wechat.UserInfo) (*models.User, error) {
	ident, err := s.store.FindWechat(info.UnionID, info.AppID, info.OpenID)
	if err == nil {
		return s.store.UserByID(ident.UserID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
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
	return &user, err
}

func (s *Service) issueSession(user *models.User, meta LoginMeta) (*dto.TokenResult, *models.Session, error) {
	if user.Status != models.StatusActive {
		return nil, nil, apperr.New(apperr.ErrDisabled, "DISABLED", "账号已停用")
	}
	var clientID *uuid.UUID
	if meta.ClientKey == "" {
		meta.ClientKey = models.ClientKeyConsole
	}
	if cl, err := s.store.ClientByKey(meta.ClientKey); err == nil {
		id := cl.ID
		clientID = &id
	}
	refreshRaw, err := secret.RandomToken(32)
	if err != nil {
		return nil, nil, err
	}
	sess := models.Session{
		ID:               idgen.New(),
		UserID:           user.ID,
		ClientID:         clientID,
		RefreshTokenHash: store.HashToken(refreshRaw),
		IP:               meta.IP,
		UserAgent:        meta.UserAgent,
		ExpiresAt:        time.Now().Add(s.cfg.JWTRefreshTTL),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	cid := uuid.Nil
	if clientID != nil {
		cid = *clientID
	}
	roles, _ := s.store.RoleCodes(user.ID)
	access, jti, err := s.jwt.Sign(user.ID, sess.ID, cid, roles)
	if err != nil {
		return nil, nil, err
	}
	sess.AccessJTI = jti
	if err := s.store.DB.Create(&sess).Error; err != nil {
		return nil, nil, err
	}
	now := time.Now()
	_ = s.store.DB.Model(user).Updates(map[string]any{"last_login_at": now, "last_login_ip": meta.IP, "updated_at": now}).Error
	user.LastLoginAt = &now
	// last_login_at 会出现在用户资料里，登录后必须让缓存失效。
	s.cache.Del(context.Background(), user.ID)
	return s.pack(user, roles, access, refreshRaw), &sess, nil
}

func (s *Service) pack(user *models.User, roles []string, access, refresh string) *dto.TokenResult {
	idents, _ := s.store.IdentitiesOf(user.ID)
	return &dto.TokenResult{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.cfg.JWTAccessTTL.Seconds()),
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

func (s *Service) revokeUserSessions(userID string, except uuid.UUID) error {
	now := time.Now()
	q := s.store.DB.Model(&models.Session{}).Where("user_id = ? AND revoked_at IS NULL", userID)
	if except != uuid.Nil {
		q = q.Where("id <> ?", except)
	}
	return q.Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
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
