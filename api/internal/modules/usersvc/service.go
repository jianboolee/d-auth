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
	"github.com/jianboolee/d-auth/internal/normalize"
	"gorm.io/gorm"
)

type Service struct {
	cfg   *config.Config
	store *store.Store
	codes *codes.Service
	oss   ossstore.Store
	auth  *auth.Service
}

func New(cfg *config.Config, st *store.Store, c *codes.Service, oss ossstore.Store, a *auth.Service) *Service {
	return &Service{cfg: cfg, store: st, codes: c, oss: oss, auth: a}
}

func (s *Service) Me(userID uuid.UUID) (dto.User, error) {
	return s.auth.PublicUser(userID)
}

type PatchMe struct {
	Nickname *string `json:"nickname"`
	Bio      *string `json:"bio"`
	Gender   *int16  `json:"gender"`
}

func (s *Service) UpdateMe(userID uuid.UUID, p PatchMe) (dto.User, error) {
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
	return s.Me(userID)
}

// ChangePassword 直接设置新密码，不再校验原密码（登录态本身即凭据）。
// 成功后撤销除当前会话外的其它会话，改密码即让其它设备下线。
func (s *Service) ChangePassword(userID uuid.UUID, newPass string, sessionID uuid.UUID) error {
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
	now := time.Now()
	return s.store.DB.Model(&models.Session{}).
		Where("user_id = ? AND id <> ? AND revoked_at IS NULL", userID, sessionID).
		Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
}

// ChangePhone 换绑/首次绑定手机号，只校验新手机号的验证码（bind_phone 场景）。
// 原来没有手机号时就是绑定（add-phone），有手机号时直接替换。
func (s *Service) ChangePhone(ctx context.Context, userID uuid.UUID, newPhoneRaw, newCode string) error {
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
	return err
}

func (s *Service) BindEmail(ctx context.Context, userID uuid.UUID, emailRaw, code string) error {
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
	return err
}

func (s *Service) Identities(userID uuid.UUID) ([]models.Identity, error) {
	return s.store.IdentitiesOf(userID)
}

// DeleteAccountConfirm 是注销时必须原样提交的确认串。
//
// 前端也会要求用户输入同样的文字，但那只是防误触；后端这道校验才是真正的保护：
// 否则一次误发的请求（或脚本调用）就能不可逆地注销一个账号。
const DeleteAccountConfirm = "确认注销"

// DeleteAccount 注销账号，抹掉个人信息并释放手机号/邮箱。
// 这是不可逆操作：调用方（自助接口或后台）负责在此之前完成确认流程。
func (s *Service) DeleteAccount(userID uuid.UUID, confirm string) error {
	if confirm != DeleteAccountConfirm {
		return apperr.New(apperr.ErrInvalidArgument, "CONFIRM_REQUIRED", "请输入确认文字以完成注销")
	}
	if _, err := s.store.UserByID(userID); err != nil {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	// 不允许删除最后一名活跃超管：仓库没有任何 CLI 恢复手段，
	// 删掉之后将无人能进入后台。
	if err := s.guardLastSuperAdmin(userID); err != nil {
		return err
	}
	return s.store.DeleteAccount(userID)
}

// guardLastSuperAdmin 在目标用户是超管时，确认还有其它活跃超管可用。
func (s *Service) guardLastSuperAdmin(userID uuid.UUID) error {
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

func (s *Service) UnbindIdentity(userID, identID uuid.UUID) error {
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
	return nil
}

func (s *Service) Sessions(userID uuid.UUID) ([]models.Session, error) {
	var list []models.Session
	err := s.store.DB.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, time.Now()).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (s *Service) RevokeSession(userID, sessionID uuid.UUID) error {
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

func (s *Service) SignAvatar(userID uuid.UUID, contentType string) (map[string]any, error) {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_TYPE", "仅支持 jpeg/png/webp")
	}
	// 对象键里用对外的用户 ID 形式（uuid 字符串）。sign 与 confirm 走同一个
	// 函数，所以校验必然一致。注意：本次 ID 格式统一后，新上传的路径用的是
	// uuid，而存量文件仍按老的 16 位串目录存放——读取按 key 走，不受影响。
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

func (s *Service) ConfirmAvatar(userID uuid.UUID, objectKey string) (dto.User, error) {
	key := strings.TrimPrefix(strings.TrimSpace(objectKey), "/")
	if key == "" || !strings.HasPrefix(key, s.cfg.OSSRootDir+"/") || !strings.Contains(key, "/"+idgen.FormatUserID(userID)+"/") {
		return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_KEY", "无效的头像 key")
	}
	if err := s.store.DB.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"avatar_key": key, "updated_at": time.Now(),
	}).Error; err != nil {
		return dto.User{}, err
	}
	return s.Me(userID)
}

func (s *Service) SaveAvatarBytes(userID uuid.UUID, objectKey string, data []byte) (dto.User, error) {
	if err := s.oss.SaveLocal(objectKey, data); err != nil {
		return dto.User{}, err
	}
	return s.ConfirmAvatar(userID, objectKey)
}
