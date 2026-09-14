package admin

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/infra/password"
	"github.com/jianboolee/d-auth/internal/infra/secret"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/normalize"
	"gorm.io/gorm"
)

type Service struct {
	cfg   *config.Config
	store *store.Store
}

func New(cfg *config.Config, st *store.Store) *Service {
	return &Service{cfg: cfg, store: st}
}

type UserListQuery struct {
	Q      string
	Status string
	Role   string
	Page   int
	Size   int
}

func (s *Service) ListUsers(q UserListQuery) ([]dto.User, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 || q.Size > 100 {
		q.Size = 20
	}
	tx := s.store.DB.Model(&models.User{}).Where("deleted_at IS NULL")
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.Q != "" {
		like := "%" + q.Q + "%"
		tx = tx.Where(`id::text ILIKE ? OR nickname ILIKE ? OR id IN (
			SELECT user_id FROM auth_identities WHERE identifier ILIKE ?
		)`, like, like, like)
	}
	if q.Role != "" {
		tx = tx.Where(`id IN (SELECT ur.user_id FROM auth_user_roles ur JOIN auth_roles r ON r.id = ur.role_id WHERE r.code = ?)`, q.Role)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []models.User
	if err := tx.Order("created_at DESC").Offset((q.Page - 1) * q.Size).Limit(q.Size).Find(&users).Error; err != nil {
		return nil, 0, err
	}
	out := make([]dto.User, 0, len(users))
	for i := range users {
		roles, _ := s.store.RoleCodes(users[i].ID)
		idents, _ := s.store.IdentitiesOf(users[i].ID)
		out = append(out, dto.FromUser(&users[i], roles, idents, s.cfg.AvatarURL))
	}
	return out, total, nil
}

func (s *Service) GetUser(id uuid.UUID) (dto.User, []models.Identity, []models.Session, error) {
	user, err := s.store.UserByID(id)
	if err != nil {
		return dto.User{}, nil, nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	roles, _ := s.store.RoleCodes(id)
	idents, _ := s.store.IdentitiesOf(id)
	var sess []models.Session
	_ = s.store.DB.Where("user_id = ?", id).Order("created_at DESC").Limit(50).Find(&sess).Error
	return dto.FromUser(user, roles, idents, s.cfg.AvatarURL), idents, sess, nil
}

type CreateUserInput struct {
	Nickname string   `json:"nickname"`
	Phone    string   `json:"phone"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Roles    []string `json:"roles"`
}

func (s *Service) CreateUser(in CreateUserInput) (dto.User, error) {
	if in.Phone == "" && in.Email == "" {
		return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "NEED_IDENTITY", "至少提供手机号或邮箱")
	}
	roles := in.Roles
	if len(roles) == 0 {
		roles = []string{models.RoleUser}
	}
	var user models.User
	err := s.store.DB.Transaction(func(tx *gorm.DB) error {
		user = models.User{Nickname: in.Nickname, Status: models.StatusActive}
		if user.Nickname == "" {
			user.Nickname = "用户"
		}
		if in.Password != "" {
			if !password.ValidPlain(in.Password) {
				return apperr.New(apperr.ErrInvalidArgument, "WEAK_PASSWORD", "密码至少 8 位")
			}
			h, err := password.Hash(in.Password)
			if err != nil {
				return err
			}
			user.PasswordHash = &h
		}
		if err := s.store.CreateUser(tx, &user, roles...); err != nil {
			return err
		}
		now := time.Now()
		if in.Phone != "" {
			phone, ok := normalize.Phone(in.Phone)
			if !ok {
				return apperr.New(apperr.ErrInvalidArgument, "INVALID_PHONE", "手机号不正确")
			}
			if err := s.store.AddIdentity(tx, &models.Identity{UserID: user.ID, Type: models.IdentityPhone, Identifier: phone, VerifiedAt: &now}); err != nil {
				return apperr.New(apperr.ErrPhoneTaken, "PHONE_TAKEN", "手机号已被使用")
			}
		}
		if in.Email != "" {
			email, ok := normalize.Email(in.Email)
			if !ok {
				return apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
			}
			if err := s.store.AddIdentity(tx, &models.Identity{UserID: user.ID, Type: models.IdentityEmail, Identifier: email, VerifiedAt: &now}); err != nil {
				return apperr.New(apperr.ErrEmailTaken, "EMAIL_TAKEN", "邮箱已被使用")
			}
		}
		return nil
	})
	if err != nil {
		return dto.User{}, err
	}
	u, _, _, err := s.GetUser(user.ID)
	return u, err
}

func (s *Service) PatchUser(id uuid.UUID, nickname *string, status *string, roles []string) (dto.User, error) {
	user, err := s.store.UserByID(id)
	if err != nil {
		return dto.User{}, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	updates := map[string]any{"updated_at": time.Now()}
	if nickname != nil {
		updates["nickname"] = strings.TrimSpace(*nickname)
	}
	if status != nil {
		updates["status"] = *status
	}
	if err := s.store.DB.Model(user).Updates(updates).Error; err != nil {
		return dto.User{}, err
	}
	if roles != nil {
		if err := s.store.ReplaceRoles(nil, id, roles); err != nil {
			return dto.User{}, err
		}
	}
	if status != nil && *status == models.StatusDisabled {
		now := time.Now()
		_ = s.store.DB.Model(&models.Session{}).Where("user_id = ? AND revoked_at IS NULL", id).
			Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
	}
	u, _, _, err := s.GetUser(id)
	return u, err
}

func (s *Service) ResetPassword(id uuid.UUID, pass string) error {
	if !password.ValidPlain(pass) {
		return apperr.New(apperr.ErrInvalidArgument, "WEAK_PASSWORD", "密码至少 8 位")
	}
	hash, err := password.Hash(pass)
	if err != nil {
		return err
	}
	if err := s.store.DB.Model(&models.User{}).Where("id = ?", id).Updates(map[string]any{
		"password_hash": hash, "updated_at": time.Now(),
	}).Error; err != nil {
		return err
	}
	now := time.Now()
	return s.store.DB.Model(&models.Session{}).Where("user_id = ? AND revoked_at IS NULL", id).
		Updates(map[string]any{"revoked_at": now, "updated_at": now}).Error
}

func (s *Service) ListRoles() ([]models.Role, error) {
	var roles []models.Role
	err := s.store.DB.Order("code").Find(&roles).Error
	return roles, err
}

func (s *Service) ListClients() ([]models.Client, error) {
	var list []models.Client
	err := s.store.DB.Order("created_at DESC").Find(&list).Error
	return list, err
}

type ClientInput struct {
	ClientKey      string   `json:"client_key"`
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
	LoginMethods   []string `json:"login_methods"`
	WechatAppID    string   `json:"wechat_app_id"`
	WechatSecret   string   `json:"wechat_app_secret"`
	Status         string   `json:"status"`
}

func (s *Service) CreateClient(in ClientInput) (*models.Client, error) {
	if in.ClientKey == "" || in.Name == "" {
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_BODY", "缺少 client_key 或名称")
	}
	if in.Type == "" {
		in.Type = "web"
	}
	if in.Status == "" {
		in.Status = models.StatusActive
	}
	enc, err := secret.Encrypt(s.cfg.MasterKey, in.WechatSecret)
	if err != nil {
		return nil, err
	}
	cl := models.Client{
		ID:                 idgen.New(),
		ClientKey:          in.ClientKey,
		Name:               in.Name,
		Type:               in.Type,
		AllowedOrigins:     in.AllowedOrigins,
		LoginMethods:       in.LoginMethods,
		WechatAppID:        in.WechatAppID,
		WechatAppSecretEnc: enc,
		Status:             in.Status,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := s.store.DB.Create(&cl).Error; err != nil {
		return nil, apperr.New(apperr.ErrConflict, "CLIENT_TAKEN", "client_key 已存在")
	}
	return &cl, nil
}

func (s *Service) UpdateClient(id uuid.UUID, in ClientInput) (*models.Client, error) {
	cl, err := s.store.ClientByID(id)
	if err != nil {
		return nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "应用不存在")
	}
	updates := map[string]any{"updated_at": time.Now()}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.Type != "" {
		updates["type"] = in.Type
	}
	if in.Status != "" {
		updates["status"] = in.Status
	}
	if in.WechatAppID != "" {
		updates["wechat_app_id"] = in.WechatAppID
	}
	if in.AllowedOrigins != nil {
		updates["allowed_origins"] = models.TextArray(in.AllowedOrigins)
	}
	if in.LoginMethods != nil {
		updates["login_methods"] = models.TextArray(in.LoginMethods)
	}
	if in.WechatSecret != "" {
		enc, err := secret.Encrypt(s.cfg.MasterKey, in.WechatSecret)
		if err != nil {
			return nil, err
		}
		updates["wechat_app_secret_enc"] = enc
	}
	if err := s.store.DB.Model(cl).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.store.ClientByID(id)
}

func (s *Service) ListAPIKeys() ([]models.APIKey, error) {
	var list []models.APIKey
	err := s.store.DB.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (s *Service) CreateAPIKey(name string, scopes []string, createdBy uuid.UUID, days int) (*models.APIKey, string, error) {
	raw, err := secret.RandomToken(24)
	if err != nil {
		return nil, "", err
	}
	full := "dak_live_" + raw
	prefix := store.PrefixOfAPIKey(full)
	var exp *time.Time
	if days > 0 {
		t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		exp = &t
	}
	if len(scopes) == 0 {
		scopes = []string{"users:read"}
	}
	k := models.APIKey{
		ID:        idgen.New(),
		Name:      name,
		KeyPrefix: prefix,
		KeyHash:   hashKey(full),
		Scopes:    scopes,
		CreatedBy: &createdBy,
		ExpiresAt: exp,
		Status:    models.StatusActive,
		CreatedAt: time.Now(),
	}
	if err := s.store.DB.Create(&k).Error; err != nil {
		return nil, "", err
	}
	return &k, full, nil
}

func (s *Service) PatchAPIKey(id uuid.UUID, status *string, scopes []string) (*models.APIKey, error) {
	var k models.APIKey
	if err := s.store.DB.Where("id = ?", id).First(&k).Error; err != nil {
		return nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "密钥不存在")
	}
	updates := map[string]any{}
	if status != nil {
		updates["status"] = *status
	}
	if scopes != nil {
		updates["scopes"] = models.TextArray(scopes)
	}
	if len(updates) == 0 {
		return &k, nil
	}
	if err := s.store.DB.Model(&k).Updates(updates).Error; err != nil {
		return nil, err
	}
	_ = s.store.DB.Where("id = ?", id).First(&k).Error
	return &k, nil
}

func (s *Service) ListAudit(q string, page, size int) ([]models.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	tx := s.store.DB.Model(&models.AuditLog{})
	if q != "" {
		tx = tx.Where("action ILIKE ?", "%"+q+"%")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []models.AuditLog
	err := tx.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error
	return list, total, err
}

func (s *Service) FindUserByIdentity(typ, identifier string) (*models.User, error) {
	ident, err := s.store.FindIdentity(typ, identifier)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
		}
		return nil, err
	}
	return s.store.UserByID(ident.UserID)
}

func hashKey(full string) string {
	return store.HashToken(full)
}
