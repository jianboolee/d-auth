package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
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
		u.ID = idgen.NewUserID()
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

// RoleCodes 读取用户的角色代码。
//
// 没有角色时返回空切片而不是 nil：nil 序列化成 JSON 的 null，而客户端按数组
// 处理（dto.User.Roles 是 []string），后台用户列表会因此在渲染角色列时崩掉。
// TextArray.Scan 已经做了同样的归一，这里保持一致。
func (s *Store) RoleCodes(userID uuid.UUID) ([]string, error) {
	var codes []string
	err := s.DB.Raw(`SELECT r.code FROM auth_roles r JOIN auth_user_roles ur ON ur.role_id = r.id WHERE ur.user_id = ?`, userID).Scan(&codes).Error
	if codes == nil {
		codes = []string{}
	}
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

// ReplaceIdentity 把用户某类身份整体替换为一个新值：先删掉该类已有记录，再写入新的。
// 手机号/邮箱在业务上是一对一的，(type, identifier) 上有唯一索引兜底并发。
func (s *Store) ReplaceIdentity(tx *gorm.DB, userID uuid.UUID, typ, identifier string) error {
	if tx == nil {
		tx = s.DB
	}
	if err := s.DeleteIdentities(tx, userID, typ); err != nil {
		return err
	}
	now := time.Now()
	return s.AddIdentity(tx, &models.Identity{
		UserID: userID, Type: typ, Identifier: identifier, VerifiedAt: &now,
	})
}

// DeleteIdentities 解绑用户某类身份（删除该类下的所有记录）。
func (s *Store) DeleteIdentities(tx *gorm.DB, userID uuid.UUID, typ string) error {
	if tx == nil {
		tx = s.DB
	}
	return tx.Where("user_id = ? AND type = ?", userID, typ).Delete(&models.Identity{}).Error
}

// CountActiveSuperAdmins 统计仍可登录的超管数量，用于阻止删除最后一名超管
// （仓库里没有任何 CLI 恢复手段，删掉之后无人能进后台）。
func (s *Store) CountActiveSuperAdmins() (int64, error) {
	var n int64
	err := s.DB.Model(&models.User{}).
		Where(`deleted_at IS NULL AND status = ? AND id IN (
			SELECT ur.user_id FROM auth_user_roles ur
			JOIN auth_roles r ON r.id = ur.role_id
			WHERE r.code = ?)`, models.StatusActive, models.RoleSuperAdmin).
		Count(&n).Error
	return n, err
}

// DeleteAccount 注销账号：把用户行改写成不含个人信息的「墓碑」，并释放它占用的资源。
//
// 为什么保留这一行而不是物理删除：
//   - 审计日志的 target_user_id 需要一个仍然存在的 id 才有意义；
//   - 外键约束（identities / sessions 是 RESTRICT）要求逐表处理，保留墓碑比级联删除更可控。
//
// 抹掉的字段与保留的字段：
//   - 抹掉：昵称、头像 key、简介、性别、密码哈希、最近登录 IP；
//   - 释放：全部 identity 行（否则 (type, identifier) 唯一索引会永久占用该手机号/邮箱）、
//     全部 session 行（含 IP 与 User-Agent）、把用户创建的 API Key 的 created_by 置空；
//   - 保留：id 与 created_at，仅作为墓碑。
//
// 访问控制交给 status=deleted + deleted_at：resolveActor 两条路径都要求 status == active，
// UserByID/ListUsers 都过滤 deleted_at IS NULL，因此墓碑既不能登录也不会出现在后台列表。
// 整个过程在一个事务里完成，避免中途失败留下「身份已释放但账号仍能登录」的中间态。
func (s *Store) DeleteAccount(userID uuid.UUID) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.Identity{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&models.Session{}).Error; err != nil {
			return err
		}
		// created_by 是 RESTRICT 外键且可空：解绑而不是删掉密钥，
		// 否则会把可能仍在生产环境使用的密钥一并废掉。
		if err := tx.Model(&models.APIKey{}).Where("created_by = ?", userID).
			Update("created_by", nil).Error; err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
			"nickname":      "已注销",
			"avatar_key":    "",
			"bio":           "",
			"gender":        0,
			"password_hash": nil,
			"last_login_ip": "",
			"status":        models.StatusDeleted,
			"deleted_at":    now,
			"updated_at":    now,
		}).Error
	})
}

// IsUniqueViolation 判断错误是否为唯一约束冲突（Postgres SQLSTATE 23505）。
// 用于把并发写入撞唯一索引的情况归为「已被占用」，而不是当作 500。
// 这里不启用 gorm 的 TranslateError：它是全局行为，会改变所有查询的错误值。
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

// HasIdentity 判断用户是否绑定了某类身份，用于「是否已绑手机号」这类轻量检查，
// 避免为了一个布尔值把整个 identities 列表读出来。
func (s *Store) HasIdentity(userID uuid.UUID, typ string) (bool, error) {
	var n int64
	err := s.DB.Model(&models.Identity{}).
		Where("user_id = ? AND type = ?", userID, typ).
		Count(&n).Error
	return n > 0, err
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
