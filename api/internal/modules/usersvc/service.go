package usersvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/ossstore"
	"github.com/jianboolee/d-auth/internal/infra/password"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/codes"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usercache"
	"github.com/jianboolee/d-auth/internal/normalize"
	"gorm.io/gorm"
)

// CodeVerifier 是 usersvc 依赖的验证码校验能力。
//
// 定义成窄接口而非直接依赖 *codes.Service，是为了让只跑 Postgres 的测试
// （如注销流程）注入假实现——真实现需要 Redis，不该把两套外部依赖绑在一起。
// 生产环境注入的就是 *codes.Service，它天然满足这个接口。
type CodeVerifier interface {
	Verify(ctx context.Context, scene, target, code string) error
}

type Service struct {
	cfg   *config.Config
	store *store.Store
	codes CodeVerifier
	oss   ossstore.Store
	auth  *auth.Service
	// cache 缓存组装好的用户资料。自助接口的每个写操作（改资料、改密码、
	// 换绑手机/邮箱、改头像、注销）都会让资料变化，因此每次写完都要 Del。
	cache *usercache.Cache
}

func New(cfg *config.Config, st *store.Store, c *codes.Service, oss ossstore.Store, a *auth.Service, cache *usercache.Cache) *Service {
	return &Service{cfg: cfg, store: st, codes: c, oss: oss, auth: a, cache: cache}
}

// invalidate 让某个用户的资料缓存立即作废。所有会改动用户资料的自助写操作
// 都应调用它；漏掉只会在 TTL 内返回旧数据，不会报错。
func (s *Service) invalidate(userID string) {
	s.cache.Del(context.Background(), userID)
}

func (s *Service) Me(userID string) (dto.User, error) {
	return s.auth.PublicUser(userID)
}

type PatchMe struct {
	Nickname *string `json:"nickname"`
	Bio      *string `json:"bio"`
	Gender   *int16  `json:"gender"`
}

func (s *Service) UpdateMe(userID string, p PatchMe) (dto.User, error) {
	updates := map[string]any{"updated_at": time.Now()}
	if p.Nickname != nil {
		updates["nickname"] = strings.TrimSpace(*p.Nickname)
	}
	if p.Bio != nil {
		updates["bio"] = *p.Bio
	}
	if p.Gender != nil {
		updates["gender"] = *p.Gender
	}
	if err := s.store.DB.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return dto.User{}, err
	}
	s.invalidate(userID)
	return s.Me(userID)
}

// ChangePassword 直接设置新密码，不再校验原密码（登录态本身即凭据）。
// 成功后撤销除当前会话外的其它会话，改密码即让其它设备下线。
func (s *Service) ChangePassword(userID string, newPass string, sessionID uuid.UUID) error {
	if !password.ValidPlain(newPass) {
		return apperr.New(apperr.ErrInvalidArgument, "WEAK_PASSWORD", "密码至少 8 位")
	}
	hash, err := password.Hash(newPass)
	if err != nil {
		return err
	}
	if err := s.store.DB.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash": hash, "updated_at": time.Now(),
	}).Error; err != nil {
		return err
	}
	// has_password 会出现在资料里。
	s.invalidate(userID)
	now := time.Now()
	return s.store.DB.Model(&models.Session{}).
		Where("user_id = ? AND id <> ? AND revoked_at IS NULL", userID, sessionID).
		Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
}

// ChangePhone 换绑/首次绑定手机号，只校验新手机号的验证码（bind_phone 场景）。
// 原来没有手机号时就是绑定（add-phone），有手机号时直接替换。
func (s *Service) ChangePhone(ctx context.Context, userID string, newPhoneRaw, newCode string) error {
	newPhone, ok := normalize.Phone(newPhoneRaw)
	if !ok {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_PHONE", "手机号不正确")
	}
	if err := s.codes.Verify(ctx, "bind_phone", newPhone, newCode); err != nil {
		return err
	}
	if existing, err := s.store.FindIdentity(models.IdentityPhone, newPhone); err == nil && existing.UserID != userID {
		return apperr.New(apperr.ErrPhoneTaken, "PHONE_TAKEN", "手机号已被使用")
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err := s.store.DB.Transaction(func(tx *gorm.DB) error {
		return s.store.ReplaceIdentity(tx, userID, models.IdentityPhone, newPhone)
	})
	if store.IsUniqueViolation(err) {
		return apperr.New(apperr.ErrPhoneTaken, "PHONE_TAKEN", "手机号已被使用")
	}
	if err != nil {
		return err
	}
	s.invalidate(userID)
	return nil
}

func (s *Service) BindEmail(ctx context.Context, userID string, emailRaw, code string) error {
	email, ok := normalize.Email(emailRaw)
	if !ok {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
	}
	if err := s.codes.Verify(ctx, "bind_email", email, code); err != nil {
		return err
	}
	if existing, err := s.store.FindIdentity(models.IdentityEmail, email); err == nil && existing.UserID != userID {
		return apperr.New(apperr.ErrEmailTaken, "EMAIL_TAKEN", "邮箱已被使用")
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err := s.store.DB.Transaction(func(tx *gorm.DB) error {
		return s.store.ReplaceIdentity(tx, userID, models.IdentityEmail, email)
	})
	if store.IsUniqueViolation(err) {
		return apperr.New(apperr.ErrEmailTaken, "EMAIL_TAKEN", "邮箱已被使用")
	}
	if err != nil {
		return err
	}
	s.invalidate(userID)
	return nil
}

func (s *Service) Identities(userID string) ([]models.Identity, error) {
	return s.store.IdentitiesOf(userID)
}

// DeleteAccountConfirm 是注销时必须原样提交的确认串。
//
// 前端也会要求用户输入同样的文字，但那只是防误触；后端这道校验才是真正的保护：
// 否则一次误发的请求（或脚本调用）就能不可逆地注销一个账号。
const DeleteAccountConfirm = "确认注销"

// DeleteAccount 注销账号，抹掉个人信息并释放手机号/邮箱。
// 这是不可逆操作：调用方（自助接口或后台）负责在此之前完成确认流程。
//
// 自助注销走 DeleteSelf：后台管理员拿不到目标用户的短信验证码，只能靠确认串，
// 所以这里保留「只校验确认串」的语义给后台使用。
func (s *Service) DeleteAccount(userID string, confirm string) error {
	if err := checkDeleteConfirm(confirm); err != nil {
		return err
	}
	return s.delete(userID)
}

// DeleteSelf 是用户自助注销：除确认串外，已绑定手机号的用户还必须提交该号码
// 收到的验证码（delete_account 场景）。绑了手机号就必须证明自己掌握它，避免
// 登录态被他人短暂接管后账号被直接抹掉。
//
// 未绑定手机号的用户仍只看确认串：/v1/me 的注销路由刻意不要求已绑手机号，
// 手机号不可用的人也应该能注销自己的账号。
func (s *Service) DeleteSelf(ctx context.Context, userID, confirm, code string) error {
	// 先校验确认串再验验证码：验证码是一次性的，确认串写错却把码消耗掉，
	// 用户就得重新发一条短信才能重试。
	if err := checkDeleteConfirm(confirm); err != nil {
		return err
	}
	phone, hasPhone, err := s.phoneOf(userID)
	if err != nil {
		return err
	}
	if hasPhone {
		if strings.TrimSpace(code) == "" {
			return apperr.New(apperr.ErrInvalidArgument, "CODE_REQUIRED", "请输入手机号验证码")
		}
		if err := s.codes.Verify(ctx, "delete_account", phone, strings.TrimSpace(code)); err != nil {
			return err
		}
	}
	return s.delete(userID)
}

// phoneOf 返回用户当前绑定的手机号（未绑定时 hasPhone=false）。
func (s *Service) phoneOf(userID string) (string, bool, error) {
	idents, err := s.store.IdentitiesOf(userID)
	if err != nil {
		return "", false, err
	}
	for _, id := range idents {
		if id.Type == models.IdentityPhone {
			return id.Identifier, true, nil
		}
	}
	return "", false, nil
}

// checkDeleteConfirm 校验注销确认串。
func checkDeleteConfirm(confirm string) error {
	if confirm != DeleteAccountConfirm {
		return apperr.New(apperr.ErrInvalidArgument, "CONFIRM_REQUIRED", "请输入确认文字以完成注销")
	}
	return nil
}

// delete 是确认串已通过后的实际删除：墓碑式抹除 + 释放身份 + 缓存失效。
func (s *Service) delete(userID string) error {
	if _, err := s.store.UserByID(userID); err != nil {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	// 不允许删除最后一名活跃超管：仓库没有任何 CLI 恢复手段，
	// 删掉之后将无人能进入后台。
	if err := s.guardLastSuperAdmin(userID); err != nil {
		return err
	}
	if err := s.store.DeleteAccount(userID); err != nil {
		return err
	}
	s.invalidate(userID)
	return nil
}

// guardLastSuperAdmin 在目标用户是超管时，确认还有其它活跃超管可用。
func (s *Service) guardLastSuperAdmin(userID string) error {
	roles, err := s.store.RoleCodes(userID)
	if err != nil {
		return err
	}
	isSuper := false
	for _, r := range roles {
		if r == models.RoleSuperAdmin {
			isSuper = true
			break
		}
	}
	if !isSuper {
		return nil
	}
	n, err := s.store.CountActiveSuperAdmins()
	if err != nil {
		return err
	}
	if n <= 1 {
		return apperr.New(apperr.ErrInvalidArgument, "LAST_SUPERADMIN", "不能注销最后一个超级管理员")
	}
	return nil
}

func (s *Service) UnbindIdentity(userID string, identID uuid.UUID) error {
	list, err := s.store.IdentitiesOf(userID)
	if err != nil {
		return err
	}
	if len(list) <= 1 {
		return apperr.New(apperr.ErrInvalidArgument, "LAST_IDENTITY", "至少保留一种登录方式")
	}
	res := s.store.DB.Where("id = ? AND user_id = ?", identID, userID).Delete(&models.Identity{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "标识不存在")
	}
	s.invalidate(userID)
	return nil
}

func (s *Service) Sessions(userID string) ([]models.Session, error) {
	var list []models.Session
	err := s.store.DB.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (s *Service) RevokeSession(userID string, sessionID uuid.UUID) error {
	now := time.Now()
	res := s.store.DB.Model(&models.Session{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", sessionID, userID).
		Updates(map[string]any{"revoked_at": now, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "会话不存在")
	}
	return nil
}

func (s *Service) SignAvatar(userID string, contentType string) (map[string]any, error) {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_TYPE", "仅支持 jpeg/png/webp")
	}
	// 对象键里用对外的用户 ID 形式（22 位字母数字串）。sign 与 confirm 走同一个
	// 函数，所以校验必然一致；历史上按 16 位串目录存放的旧文件仍按原 key 读取，
	// 不受影响。
	key, upload, pub, headers, err := s.oss.SignPut(contentType, idgen.FormatUserID(userID))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"object_key": key,
		"upload_url": upload,
		"public_url": pub,
		"headers":    headers,
	}, nil
}

func (s *Service) ConfirmAvatar(userID string, objectKey string) (dto.User, error) {
	key := strings.TrimPrefix(strings.TrimSpace(objectKey), "/")
	if key == "" || !strings.HasPrefix(key, s.cfg.OSSRootDir+"/") || !strings.Contains(key, "/"+idgen.FormatUserID(userID)+"/") {
		return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_KEY", "无效的头像 key")
	}
	if err := s.store.DB.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"avatar_key": key, "updated_at": time.Now(),
	}).Error; err != nil {
		return dto.User{}, err
	}
	s.invalidate(userID)
	return s.Me(userID)
}

func (s *Service) SaveAvatarBytes(userID string, objectKey string, data []byte) (dto.User, error) {
	if err := s.oss.SaveLocal(objectKey, data); err != nil {
		return dto.User{}, err
	}
	return s.ConfirmAvatar(userID, objectKey)
}
