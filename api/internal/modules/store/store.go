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
	if u.ID == "" {
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

func (s *Store) AddRoles(tx *gorm.DB, userID string, codes ...string) error {
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

func (s *Store) ReplaceRoles(tx *gorm.DB, userID string, codes []string) error {
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
func (s *Store) RoleCodes(userID string) ([]string, error) {
	var codes []string
	err := s.DB.Raw(`SELECT r.code FROM auth_roles r JOIN auth_user_roles ur ON ur.role_id = r.id WHERE ur.user_id = ?`, userID).Scan(&codes).Error
	if codes == nil {
		codes = []string{}
	}
	return codes, err
}

// UsersByIDs 批量读用户，一次 IN 查询。缺失的 id 不会出现在结果里，
// 调用方据此发现「查不到」，而不是拿到零值 user。
func (s *Store) UsersByIDs(ids []string) ([]models.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []models.User
	err := s.DB.Where("id IN ? AND deleted_at IS NULL", ids).Find(&users).Error
	return users, err
}

// userRoleRow 是 RoleCodesByUserIDs 的扫描目标：一条 (user_id, code) 记录。
type userRoleRow struct {
	UserID string `gorm:"column:user_id"`
	Code   string `gorm:"column:code"`
}

// RoleCodesByUserIDs 批量读用户角色，返回 user_id → 角色代码。
// 没有角色的用户不会出现在 map 里（而不是空切片）：调用方用它填充结果时
// 需要区分「查不到该用户」与「用户没有角色」，后者由调用方自行补空切片。
func (s *Store) RoleCodesByUserIDs(ids []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []userRoleRow
	if err := s.DB.Raw(
		`SELECT ur.user_id, r.code FROM auth_roles r JOIN auth_user_roles ur ON ur.role_id = r.id WHERE ur.user_id IN ?`,
		ids,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.UserID] = append(out[row.UserID], row.Code)
	}
	return out, nil
}

// IdentitiesByUserIDs 批量读身份，返回 user_id → 身份列表（按 created_at 升序，
// 与 IdentitiesOf 一致，保证手机号/邮箱的取值顺序稳定）。
func (s *Store) IdentitiesByUserIDs(ids []string) (map[string][]models.Identity, error) {
	out := make(map[string][]models.Identity, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var list []models.Identity
	if err := s.DB.Where("user_id IN ?", ids).Order("created_at").Find(&list).Error; err != nil {
		return nil, err
	}
	for _, ident := range list {
		out[ident.UserID] = append(out[ident.UserID], ident)
	}
	return out, nil
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

func (s *Store) UserByID(id string) (*models.User, error) {
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

func (s *Store) IdentitiesOf(userID string) ([]models.Identity, error) {
	var list []models.Identity
	err := s.DB.Where("user_id = ?", userID).Order("created_at").Find(&list).Error
	return list, err
}

// ReplaceIdentity 把用户某类身份整体替换为一个新值：先删掉该类已有记录，再写入新的。
// 手机号/邮箱在业务上是一对一的，(type, identifier) 上有唯一索引兜底并发。
func (s *Store) ReplaceIdentity(tx *gorm.DB, userID string, typ, identifier string) error {
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
func (s *Store) DeleteIdentities(tx *gorm.DB, userID string, typ string) error {
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
//     把用户创建的 API Key 的 created_by 置空；
//   - 保留：id 与 created_at，仅作为墓碑。
//
// 访问控制交给 status=deleted + deleted_at：resolveActor 验签后不查库，注销的即时性
// 靠调用方在事务后吊销该用户的 refresh token（见 usersvc.delete → auth.RevokeUserSessions）；
// UserByID/ListUsers 都过滤 deleted_at IS NULL，因此墓碑既不能再续期也不会出现在后台列表。
// 整个过程在一个事务里完成，避免中途失败留下「身份已释放但账号仍能登录」的中间态。
func (s *Store) DeleteAccount(userID string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.Identity{}).Error; err != nil {
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
func (s *Store) HasIdentity(userID string, typ string) (bool, error) {
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

// SuffixOfAPIKey 取 API Key 的后 4 位，存下来用于列表展示（掩码尾部）。
// 只作展示，不参与鉴权；老密钥没有这个值，为空即可。
func SuffixOfAPIKey(full string) string {
	if len(full) <= 4 {
		return full
	}
	return full[len(full)-4:]
}

// WriteAudit 写审计日志。actorUser 是用户 ID（可为 nil）；apiKey 是 API Key 的
// uuid（可为 nil）；target 是被操作的用户 ID（可为 nil）。
func (s *Store) WriteAudit(actorUser *string, apiKey *uuid.UUID, target *string, action, ip, ua string, meta models.JSONMap) {
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
