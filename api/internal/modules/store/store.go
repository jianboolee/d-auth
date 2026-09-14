package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Store struct {
	DB *gorm.DB
}

func New(db *gorm.DB) *Store { return &Store{DB: db} }

func (s *Store) CreateUser(tx *gorm.DB, u *models.User, roleCodes ...string) error {
	if tx == nil {
		tx = s.DB
	}
	if u.ID == uuid.Nil {
		u.ID = idgen.New()
	}
	now := time.Now()
	u.CreatedAt = now
	u.UpdatedAt = now
	if u.Status == "" {
		u.Status = models.StatusActive
	}
	if err := tx.Create(u).Error; err != nil {
		return err
	}
	if len(roleCodes) == 0 {
		roleCodes = []string{models.RoleUser}
	}
	return s.AddRoles(tx, u.ID, roleCodes...)
}

func (s *Store) AddRoles(tx *gorm.DB, userID uuid.UUID, codes ...string) error {
	if tx == nil {
		tx = s.DB
	}
	var roles []models.Role
	if err := tx.Where("code IN ?", codes).Find(&roles).Error; err != nil {
		return err
	}
	now := time.Now()
	for _, r := range roles {
		ur := models.UserRole{UserID: userID, RoleID: r.ID, CreatedAt: now}
		if err := tx.Where("user_id = ? AND role_id = ?", userID, r.ID).FirstOrCreate(&ur).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceRoles(tx *gorm.DB, userID uuid.UUID, codes []string) error {
	if tx == nil {
		tx = s.DB
	}
	if err := tx.Where("user_id = ?", userID).Delete(&models.UserRole{}).Error; err != nil {
		return err
	}
	return s.AddRoles(tx, userID, codes...)
}

func (s *Store) RoleCodes(userID uuid.UUID) ([]string, error) {
	var codes []string
	err := s.DB.Raw(`SELECT r.code FROM auth_roles r JOIN auth_user_roles ur ON ur.role_id = r.id WHERE ur.user_id = ?`, userID).Scan(&codes).Error
	return codes, err
}

func (s *Store) FindIdentity(typ, identifier string) (*models.Identity, error) {
	var ident models.Identity
	err := s.DB.Where("type = ? AND identifier = ?", typ, identifier).First(&ident).Error
	if err != nil {
		return nil, err
	}
	return &ident, nil
}

func (s *Store) FindWechat(unionID, appID, openID string) (*models.Identity, error) {
	if unionID != "" {
		var ident models.Identity
		err := s.DB.Where("(type = ? OR type = ?) AND extra->>'union_id' = ?", models.IdentityWechatOpen, models.IdentityWechatMini, unionID).First(&ident).Error
		if err == nil {
			return &ident, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}
	var ident models.Identity
	key := appID + ":" + openID
	if appID == "" {
		key = openID
	}
	err := s.DB.Where("identifier = ? AND (type = ? OR type = ?)", key, models.IdentityWechatOpen, models.IdentityWechatMini).First(&ident).Error
	if err != nil {
		return nil, err
	}
	return &ident, nil
}

func (s *Store) UserByID(id uuid.UUID) (*models.User, error) {
	var u models.User
	err := s.DB.Where("id = ? AND deleted_at IS NULL", id).First(&u).Error
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) AddIdentity(tx *gorm.DB, ident *models.Identity) error {
	if tx == nil {
		tx = s.DB
	}
	if ident.ID == uuid.Nil {
		ident.ID = idgen.New()
	}
	if ident.CreatedAt.IsZero() {
		ident.CreatedAt = time.Now()
	}
	if ident.Extra == nil {
		ident.Extra = models.JSONMap{}
	}
	return tx.Create(ident).Error
}

func (s *Store) IdentitiesOf(userID uuid.UUID) ([]models.Identity, error) {
	var list []models.Identity
	err := s.DB.Where("user_id = ?", userID).Order("created_at").Find(&list).Error
	return list, err
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (s *Store) ClientByKey(key string) (*models.Client, error) {
	var c models.Client
	err := s.DB.Where("client_key = ? AND status = ?", key, models.StatusActive).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ClientByID(id uuid.UUID) (*models.Client, error) {
	var c models.Client
	err := s.DB.Where("id = ?", id).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ClientByWechatAppID(appID string) (*models.Client, error) {
	var c models.Client
	err := s.DB.Where("wechat_app_id = ? AND status = ?", appID, models.StatusActive).First(&c).Error
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) FindAPIKeyByPrefix(prefix string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.DB.Where("key_prefix = ? AND status = ?", prefix, models.StatusActive).First(&k).Error
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func PrefixOfAPIKey(full string) string {
	if len(full) <= 16 {
		return full
	}
	return full[:16]
}

func (s *Store) WriteAudit(actorUser, apiKey, target *uuid.UUID, action, ip, ua string, meta models.JSONMap) {
	if meta == nil {
		meta = models.JSONMap{}
	}
	_ = s.DB.Create(&models.AuditLog{
		ID:           idgen.New(),
		ActorUserID:  actorUser,
		APIKeyID:     apiKey,
		Action:       action,
		TargetUserID: target,
		IP:           ip,
		UserAgent:    ua,
		Metadata:     meta,
		CreatedAt:    time.Now(),
	}).Error
}

func ContainsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}
