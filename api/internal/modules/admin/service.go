package admin

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
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
	"github.com/jianboolee/d-auth/internal/modules/sessionstore"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usercache"
	"github.com/jianboolee/d-auth/internal/normalize"
	"gorm.io/gorm"
)

type Service struct {
	cfg   *config.Config
	store *store.Store
	// sessions 是 refresh token 白名单，后台停用/改密用户时用它吊销其全部会话。
	sessions *sessionstore.Store
	// cache 缓存组装好的用户资料。后台的每个写操作都会改动资料、角色或身份，
	// 因此写完都要 Del，否则业务端最多在 TTL 内看到旧数据。
	cache *usercache.Cache
}

func New(cfg *config.Config, st *store.Store, sessions *sessionstore.Store, cache *usercache.Cache) *Service {
	return &Service{cfg: cfg, store: st, sessions: sessions, cache: cache}
}

type UserListQuery struct {
	Q      string
	Status string
	Role   string
	Page   int
	Size   int
	// 注册时间范围，RFC3339。为空表示该端不限。
	CreatedFrom string
	CreatedTo   string
	// 排序字段与方向，取值见 userListSortColumns。
	Sort  string
	Order string

	// 解析后的时间边界，由 parseUserListQuery 填充，不对外暴露。
	createdFrom *time.Time
	createdTo   *time.Time
}

// userListSortColumns 是允许排序的字段白名单。列名会拼进 ORDER BY，
// 不能拿请求参数直接拼 SQL，因此只认这张表里的键。
var userListSortColumns = map[string]string{
	"created_at":    "created_at",
	"last_login_at": "last_login_at",
	"nickname":      "nickname",
}

// 时间筛选只作用于注册时间，字段名与排序用的 created_at 保持一致。
func parseUserListQuery(q *UserListQuery) error {
	if q.CreatedFrom != "" {
		t, err := time.Parse(time.RFC3339, q.CreatedFrom)
		if err != nil {
			return apperr.New(apperr.ErrInvalidArgument, "INVALID_TIME", "created_from 需为 RFC3339 时间")
		}
		q.createdFrom = &t
	}
	if q.CreatedTo != "" {
		t, err := time.Parse(time.RFC3339, q.CreatedTo)
		if err != nil {
			return apperr.New(apperr.ErrInvalidArgument, "INVALID_TIME", "created_to 需为 RFC3339 时间")
		}
		q.createdTo = &t
	}
	if q.createdFrom != nil && q.createdTo != nil && q.createdTo.Before(*q.createdFrom) {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_TIME_RANGE", "结束时间不能早于开始时间")
	}
	if q.Sort == "" {
		q.Sort = "created_at"
	}
	if _, ok := userListSortColumns[q.Sort]; !ok {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_SORT", "不支持按该字段排序")
	}
	if q.Order == "" {
		// 默认与原来一致：新注册的排在前面
		q.Order = "desc"
	}
	if q.Order != "asc" && q.Order != "desc" {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_ORDER", "排序方向只能是 asc 或 desc")
	}
	return nil
}

// invalidate 让某个用户的资料缓存立即作废。后台写操作改的都是会出现在
// 用户资料里的字段，写完必须调用它；漏掉只会在 TTL 内返回旧数据。
func (s *Service) invalidate(id string) {
	s.cache.Del(context.Background(), id)
}

func (s *Service) ListUsers(q UserListQuery) ([]dto.User, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size < 1 || q.Size > 100 {
		q.Size = 20
	}
	if err := parseUserListQuery(&q); err != nil {
		return nil, 0, err
	}
	tx := s.store.DB.Model(&models.User{}).Where("deleted_at IS NULL")
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.Q != "" {
		like := "%" + q.Q + "%"
		tx = tx.Where(`id ILIKE ? OR nickname ILIKE ? OR id IN (
			SELECT user_id FROM auth_identities WHERE identifier ILIKE ?
		)`, like, like, like)
	}
	if q.Role != "" {
		tx = tx.Where(`id IN (SELECT ur.user_id FROM auth_user_roles ur JOIN auth_roles r ON r.id = ur.role_id WHERE r.code = ?)`, q.Role)
	}
	// 半开区间 [from, to]：前端把「结束日期」算成次日 0 点传入，这样同一天
	// 内任意时刻都落进范围，不用去猜用户想不想包含到那天的 23:59:59。
	if q.createdFrom != nil {
		tx = tx.Where("created_at >= ?", *q.createdFrom)
	}
	if q.createdTo != nil {
		tx = tx.Where("created_at < ?", *q.createdTo)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	order := userListSortColumns[q.Sort] + " " + strings.ToUpper(q.Order)
	// last_login_at 可为 NULL（从未登录）。不指定时 Postgres 在 DESC 下会把
	// NULL 排在最前，翻页第一屏全是空值，所以固定把空值压在最后。
	if q.Sort == "last_login_at" {
		order += " NULLS LAST"
	}
	// 再按 id 兜底排序：排序字段可能大量重复（如同一秒注册），没有稳定次序时
	// 翻页会出现重复行或漏行。用户 ID 现在是随机串，这个兜底只保证「次序稳定」，
	// 不再代表创建先后（时间序一律看 created_at）。
	order += ", id DESC"
	var users []models.User
	if err := tx.Order(order).Offset((q.Page - 1) * q.Size).Limit(q.Size).Find(&users).Error; err != nil {
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

// GetUser 只组装详情页需要的基本信息与身份。会话不再返回：会话状态已归
// Redis 白名单，后台也不再有会话列表入口。
func (s *Service) GetUser(id string) (dto.User, []models.Identity, error) {
	user, err := s.store.UserByID(id)
	if err != nil {
		return dto.User{}, nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	roles, _ := s.store.RoleCodes(id)
	idents, _ := s.store.IdentitiesOf(id)
	return dto.FromUser(user, roles, idents, s.cfg.AvatarURL), idents, nil
}

// StatsView 是仪表盘的聚合结果。不直接复用 models.User：这些是聚合值，不是
// 某一行的字段。环比百分比不在这里算——后端只给原始计数，展示口径由前端决定。
type StatsView struct {
	Total     int64 `json:"total"`
	Active    int64 `json:"active"`
	Today     int64 `json:"today"`
	Yesterday int64 `json:"yesterday"`
}

// Stats 汇总仪表盘需要的计数：用户总数、活跃用户、今日与昨日注册。
//
// 「今日/昨日」按服务端本地时区切分（生产容器设了 TZ=Asia/Shanghai），用半开
// 区间 [start, end)，与 ListUsers 的 created_from/created_to 约定一致：这样同一
// 天内任意时刻都落进范围，不用去猜要不要包含那天的 23:59:59。
//
// 全部计数排除注销用户（deleted_at IS NOT NULL）：用户是墓碑式软删，DeletedAt
// 不是 gorm.DeletedAt，GORM 不会自动过滤，必须显式写，见 CountActiveSuperAdmins。
// 四个计数用一条条件聚合语句查完，而不是四次 Count——一次往返，且几个数字来自
// 同一个数据库快照，不会因为查询间隙里有人注册而彼此对不上。
func (s *Service) Stats() (StatsView, error) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	yesterdayStart := todayStart.AddDate(0, 0, -1)

	var v StatsView
	err := s.store.DB.Model(&models.User{}).
		Select(`COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = ?) AS active,
			COUNT(*) FILTER (WHERE created_at >= ? AND created_at < ?) AS today,
			COUNT(*) FILTER (WHERE created_at >= ? AND created_at < ?) AS yesterday`,
			models.StatusActive, todayStart, tomorrowStart, yesterdayStart, todayStart).
		Where("deleted_at IS NULL").
		Scan(&v).Error
	return v, err
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
			// 后台建号走 AdminPhone：允许 120 开头的虚拟号等非真实号段。
			phone, ok := normalize.AdminPhone(in.Phone)
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
	s.invalidate(user.ID)
	u, _, err := s.GetUser(user.ID)
	return u, err
}

// PatchUserInput 是后台改用户的可选字段集合。指针为 nil 表示不改该字段
// （与 usersvc.PatchMe 同一约定），Roles 为 nil 表示不动角色。
//
// Bio / Gender 与自助端的 PATCH /v1/me 一致，不做取值校验：业务端代客改资料
// 与用户改自己应保持同一语义。Status 只应是 active / disabled。
type PatchUserInput struct {
	Nickname *string
	Bio      *string
	Gender   *int16
	Status   *string
	Roles    []string
}

func (s *Service) PatchUser(id string, in PatchUserInput) (dto.User, error) {
	user, err := s.store.UserByID(id)
	if err != nil {
		return dto.User{}, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	updates := map[string]any{"updated_at": time.Now()}
	if in.Nickname != nil {
		updates["nickname"] = strings.TrimSpace(*in.Nickname)
	}
	if in.Bio != nil {
		updates["bio"] = *in.Bio
	}
	if in.Gender != nil {
		updates["gender"] = *in.Gender
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if err := s.store.DB.Model(user).Updates(updates).Error; err != nil {
		return dto.User{}, err
	}
	if in.Roles != nil {
		if err := s.store.ReplaceRoles(nil, id, in.Roles); err != nil {
			return dto.User{}, err
		}
	}
	if in.Status != nil && *in.Status == models.StatusDisabled {
		// 停用即吊销全部 refresh：该用户不能再续期。已签发的 access token
		// 在 TTL 内仍可验签通过（纯验签方案的既定窗口）。
		_ = s.sessions.RevokeUserAll(context.Background(), id)
	}
	s.invalidate(id)
	u, _, err := s.GetUser(id)
	return u, err
}

// SetUserIdentity 直接设置用户的手机号或邮箱，供后台管理使用（不校验验证码，
// 与用户自助改绑需要收验证码不同：管理员操作以管理员身份为凭据）。
// 只做设置/替换，不支持解绑——手机号是主要登录能力，解绑会让账号失去登录方式。
// 唯一性由 (type, identifier) 唯一索引保证，这里先查一次给出友好错误，再靠索引兜底并发。
func (s *Service) SetUserIdentity(id string, typ, raw string) (dto.User, error) {
	if _, err := s.store.UserByID(id); err != nil {
		return dto.User{}, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}
	if strings.TrimSpace(raw) == "" {
		code, msg := "INVALID_PHONE", "手机号不能为空"
		if typ == models.IdentityEmail {
			code, msg = "INVALID_EMAIL", "邮箱不能为空"
		}
		return dto.User{}, apperr.New(apperr.ErrInvalidArgument, code, msg)
	}

	var (
		identifier string
		ok         bool
		takenErr   error
	)
	switch typ {
	case models.IdentityPhone:
		// 与 CreateUser 一致：后台改绑同样允许虚拟号等非真实号段。
		identifier, ok = normalize.AdminPhone(raw)
		if !ok {
			return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_PHONE", "手机号不正确")
		}
		takenErr = apperr.New(apperr.ErrPhoneTaken, "PHONE_TAKEN", "手机号已被使用")
	case models.IdentityEmail:
		identifier, ok = normalize.Email(raw)
		if !ok {
			return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_EMAIL", "邮箱不正确")
		}
		takenErr = apperr.New(apperr.ErrEmailTaken, "EMAIL_TAKEN", "邮箱已被使用")
	default:
		return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_TYPE", "不支持的身份类型")
	}

	// 已属于别人 → 拒绝；已属于本人 → 视为无变化（幂等）。
	if existing, err := s.store.FindIdentity(typ, identifier); err == nil {
		if existing.UserID != id {
			return dto.User{}, takenErr
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return dto.User{}, err
	}

	err := s.store.DB.Transaction(func(tx *gorm.DB) error {
		return s.store.ReplaceIdentity(tx, id, typ, identifier)
	})
	if err != nil {
		if store.IsUniqueViolation(err) {
			return dto.User{}, takenErr
		}
		return dto.User{}, err
	}
	s.invalidate(id)
	u, _, err := s.GetUser(id)
	return u, err
}

// UnbindUserIdentity 解绑用户的某一条登录标识，供后台管理使用。
//
// 与自助端 usersvc.UnbindIdentity 的区别：那边按 identID 删自己名下的记录，
// 这里由管理员按 identID 删指定用户名下的记录，且要额外守住「账号还能不能登录」。
// 自助端只数 identity 行数，不看其余行是否真的可用；后台场景更需要这道检查，
// 因为管理员可能一次解绑掉用户唯一的手机号却没意识到对方没有密码。
//
// 守卫规则——解绑后必须仍存在至少一种「可用」的登录能力：
//  1. 剩余 identity 至少一条；或
//  2. 用户设置了密码（可用密码登录，identifier 为手机号/邮箱）。
//
// 两者都不满足时拒绝。这样既能解绑小程序/微信标识，也不会把账号锁死。
func (s *Service) UnbindUserIdentity(id string, identID uuid.UUID) (dto.User, error) {
	u, err := s.store.UserByID(id)
	if err != nil {
		return dto.User{}, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在")
	}

	list, err := s.store.IdentitiesOf(id)
	if err != nil {
		return dto.User{}, err
	}

	// 确认这条标识确实属于本人，避免误删他人的记录；同时数出解绑后还剩几条。
	found := false
	remaining := 0
	for i := range list {
		if list[i].ID == identID {
			found = true
			continue
		}
		remaining++
	}
	if !found {
		return dto.User{}, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "标识不存在或不属于该用户")
	}

	hasPassword := u.PasswordHash != nil && strings.TrimSpace(*u.PasswordHash) != ""
	if remaining == 0 && !hasPassword {
		return dto.User{}, apperr.New(apperr.ErrInvalidArgument, "LAST_IDENTITY",
			"这是该用户最后的登录方式且未设置密码，解绑后将无法登录")
	}

	// 只做删除，不做「自动补一个虚拟号」之类的隐式补偿——那会让管理员
	// 误以为账号仍有登录方式。
	if err := s.store.DB.Where("id = ? AND user_id = ?", identID, id).
		Delete(&models.Identity{}).Error; err != nil {
		return dto.User{}, err
	}

	// 解绑后账号仍可登录，因此不吊销 refresh 白名单（与「停用」不同，
	// 那里才需要全量踢下线）；只需让资料缓存失效。
	s.invalidate(id)

	newUser, _, err := s.GetUser(id)
	if err != nil {
		return dto.User{}, err
	}
	return newUser, nil
}

// 虚拟号形态：120 开头 + 8 位随机数字，共 11 位。120 不是真实号段，
// normalize.Phone 会拒绝它，因此只可能出现在后台建号／改绑的宽松校验下。
const (
	virtualPhonePrefix    = "120"
	virtualPhoneRandomLen = 8
	// 抽到已占用号码就重抽的次数上限。号码空间 1e8，正常一次就能命中。
	virtualPhoneAttempts = 8
)

// newVirtualPhone 生成一个 120 开头的 11 位虚拟号，不做唯一性检查。
func newVirtualPhone() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return "", err
	}
	return virtualPhonePrefix + fmt.Sprintf("%0*d", virtualPhoneRandomLen, n.Int64()), nil
}

// GenerateVirtualPhone 生成一个未被占用的虚拟手机号，供后台建号表单自动填入。
//
// 号码本身不具备任何真实性：它只是一个占位标识，登录、注册、短信发送路径的
// 严格校验不会接受它。函数不写库——先查 auth_identities 避开已占用的号码，
// 真正的唯一性仍由 (type, identifier) 唯一索引在写入时兜底。
func (s *Service) GenerateVirtualPhone() (string, error) {
	for i := 0; i < virtualPhoneAttempts; i++ {
		phone, err := newVirtualPhone()
		if err != nil {
			return "", err
		}
		// CreateUser/SetUserIdentity 用 AdminPhone 规范化，虚拟号原样入库，
		// 所以这里按原值比对即可。
		switch _, err := s.store.FindIdentity(models.IdentityPhone, phone); {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return phone, nil
		case err != nil:
			return "", err
		}
	}
	return "", apperr.New(apperr.ErrConflict, "PHONE_GENERATE_FAILED", "生成虚拟号失败，请重试")
}

func (s *Service) ResetPassword(id string, pass string) error {
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
	// has_password 会出现在资料里（dto.User.HasPassword）。
	s.invalidate(id)
	// 管理员重置密码后吊销该用户全部 refresh：其它设备无法再续期。
	return s.sessions.RevokeUserAll(context.Background(), id)
}

// RoleView 是后台角色列表的一行。带 user_count 便于判断某个角色是否还在被使用，
// 不直接复用 models.Role：user_count 是聚合值，放进模型会让其它按角色查询的
// SELECT 也带上这个并不存在的列。
type RoleView struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	UserCount   int64     `json:"user_count"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Service) ListRoles() ([]RoleView, error) {
	var out []RoleView
	err := s.store.DB.Model(&models.Role{}).
		Select(`auth_roles.id, auth_roles.code, auth_roles.name, auth_roles.description,
			auth_roles.is_system, auth_roles.created_at,
			(SELECT COUNT(*) FROM auth_user_roles ur WHERE ur.role_id = auth_roles.id) AS user_count`).
		Order("auth_roles.code").
		Scan(&out).Error
	return out, err
}

type RoleInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// roleCodeRe 限制角色代码：小写字母开头，后接小写字母/数字/下划线/连字符。
// 代码是鉴权依据（JWT 的 roles claim、HasRole 比较都用它），也是大小写敏感的，
// 所以只接受小写，避免出现 Admin 与 admin 两个「同名不同权」的角色。
var roleCodeRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

func (s *Service) CreateRole(in RoleInput) (RoleView, error) {
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.Name)
	if !roleCodeRe.MatchString(code) {
		return RoleView{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_ROLE_CODE",
			"角色代码需为 2-32 位小写字母、数字、下划线或连字符，且以字母开头")
	}
	if name == "" {
		return RoleView{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_ROLE_NAME", "角色名称不能为空")
	}
	role := models.Role{
		ID:          idgen.New(),
		Code:        code,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		IsSystem:    false,
		CreatedAt:   time.Now(),
	}
	if err := s.store.DB.Create(&role).Error; err != nil {
		if store.IsUniqueViolation(err) {
			return RoleView{}, apperr.New(apperr.ErrConflict, "ROLE_TAKEN", "角色代码已存在")
		}
		return RoleView{}, err
	}
	return RoleView{
		ID: role.ID, Code: role.Code, Name: role.Name, Description: role.Description,
		IsSystem: role.IsSystem, CreatedAt: role.CreatedAt,
	}, nil
}

// UpdateRole 只允许改名称和说明。code 是业务系统与 JWT 引用的稳定标识，
// 改名会让外部鉴权失效；需要换标识时应新建角色并迁移用户。
func (s *Service) UpdateRole(id uuid.UUID, name, description *string) (RoleView, error) {
	var role models.Role
	if err := s.store.DB.Where("id = ?", id).First(&role).Error; err != nil {
		return RoleView{}, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "角色不存在")
	}
	updates := map[string]any{}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return RoleView{}, apperr.New(apperr.ErrInvalidArgument, "INVALID_ROLE_NAME", "角色名称不能为空")
		}
		updates["name"] = n
	}
	if description != nil {
		updates["description"] = strings.TrimSpace(*description)
	}
	if len(updates) == 0 {
		return s.getRoleView(id)
	}
	if err := s.store.DB.Model(&role).Updates(updates).Error; err != nil {
		return RoleView{}, err
	}
	return s.getRoleView(id)
}

// DeleteRole 删除自定义角色。系统内置角色（superadmin/admin/user）不允许删除。
// 该角色与用户的关联记录由外键 ON DELETE CASCADE 一并清除，调用方应先确认影响范围
// （列表里的 user_count 即为受影响人数）。
func (s *Service) DeleteRole(id uuid.UUID) error {
	var role models.Role
	if err := s.store.DB.Where("id = ?", id).First(&role).Error; err != nil {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "角色不存在")
	}
	if role.IsSystem {
		return apperr.New(apperr.ErrInvalidArgument, "SYSTEM_ROLE", "系统内置角色不允许删除")
	}
	return s.store.DB.Delete(&models.Role{}, "id = ?", id).Error
}

func (s *Service) getRoleView(id uuid.UUID) (RoleView, error) {
	var out RoleView
	err := s.store.DB.Model(&models.Role{}).
		Select(`auth_roles.id, auth_roles.code, auth_roles.name, auth_roles.description,
			auth_roles.is_system, auth_roles.created_at,
			(SELECT COUNT(*) FROM auth_user_roles ur WHERE ur.role_id = auth_roles.id) AS user_count`).
		Where("auth_roles.id = ?", id).
		Scan(&out).Error
	return out, err
}

func (s *Service) ListClients() ([]models.Client, error) {
	var list []models.Client
	err := s.store.DB.Order("created_at DESC").Find(&list).Error
	return list, err
}

type ClientInput struct {
	ClientKey    string `json:"client_key"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	WechatAppID  string `json:"wechat_app_id"`
	WechatSecret string `json:"wechat_app_secret"`
	Status       string `json:"status"`
}

// clientKeyRe 限制应用标识：小写字母开头，后接小写字母/数字/下划线/连字符。
// 这个值要作为 X-DAuth-Client 请求头在客户端与服务端之间往返，含空白或换行
// 本身就是隐患；与角色代码用同一套规则，现有标识（web_console 等）都符合。
var clientKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

func errInvalidClientKey() error {
	return apperr.New(apperr.ErrInvalidArgument, "INVALID_CLIENT_KEY",
		"应用标识需以小写字母开头，可含小写字母、数字、下划线和连字符，共 2-32 位")
}

func (s *Service) CreateClient(in ClientInput) (*models.Client, error) {
	if in.ClientKey == "" || in.Name == "" {
		return nil, apperr.New(apperr.ErrInvalidArgument, "INVALID_BODY", "缺少 client_key 或名称")
	}
	if !clientKeyRe.MatchString(in.ClientKey) {
		return nil, errInvalidClientKey()
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
		WechatAppID:        in.WechatAppID,
		WechatAppSecretEnc: enc,
		Status:             in.Status,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := s.store.DB.Create(&cl).Error; err != nil {
		// 只有唯一索引冲突才算「已存在」；其它库错误如实抛出，
		// 不要一律说成重名，否则真实故障会被误导成命名问题。
		if store.IsUniqueViolation(err) {
			return nil, apperr.New(apperr.ErrConflict, "CLIENT_TAKEN", "client_key 已存在")
		}
		return nil, err
	}
	return &cl, nil
}

func (s *Service) UpdateClient(id uuid.UUID, in ClientInput) (*models.Client, error) {
	cl, err := s.store.ClientByID(id)
	if err != nil {
		return nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "应用不存在")
	}
	updates := map[string]any{"updated_at": time.Now()}
	// 应用标识可变，但控制台自己依赖 web_console：前端硬编码了它，登录流程也
	// 会回退到它。改名后控制台发出的请求对不上应用、会话不再归属任何应用，
	// 且要改代码重新部署才能恢复，所以这一行禁止改名。
	if in.ClientKey != "" && in.ClientKey != cl.ClientKey {
		if !clientKeyRe.MatchString(in.ClientKey) {
			return nil, errInvalidClientKey()
		}
		if cl.ClientKey == models.ClientKeyConsole {
			return nil, apperr.New(apperr.ErrInvalidArgument, "CLIENT_PROTECTED",
				"控制台应用的标识不允许修改")
		}
		updates["client_key"] = in.ClientKey
	}
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
	if in.WechatSecret != "" {
		enc, err := secret.Encrypt(s.cfg.MasterKey, in.WechatSecret)
		if err != nil {
			return nil, err
		}
		updates["wechat_app_secret_enc"] = enc
	}
	if err := s.store.DB.Model(cl).Updates(updates).Error; err != nil {
		if store.IsUniqueViolation(err) {
			return nil, apperr.New(apperr.ErrConflict, "CLIENT_TAKEN", "client_key 已存在")
		}
		return nil, err
	}
	return s.store.ClientByID(id)
}

// DeleteClient 物理删除应用。
//
// 删除应用时同步吊销归属它的全部 refresh。会话归属信息现在存在 Redis 里
// （refresh 记录的 cid 字段），Postgres 侧不再有 auth_sessions 外键约束，
// 因此直接删应用即可，无外键冲突。
func (s *Service) DeleteClient(id uuid.UUID) error {
	cl, err := s.store.ClientByID(id)
	if err != nil {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "应用不存在")
	}
	if cl.ClientKey == models.ClientKeyConsole {
		return apperr.New(apperr.ErrInvalidArgument, "CLIENT_PROTECTED",
			"控制台应用不允许删除")
	}
	if err := s.sessions.RevokeClientAll(context.Background(), id.String()); err != nil {
		return err
	}
	return s.store.DB.Delete(&models.Client{}, "id = ?", id).Error
}

func (s *Service) ListAPIKeys() ([]models.APIKey, error) {
	var list []models.APIKey
	err := s.store.DB.Order("created_at DESC").Find(&list).Error
	return list, err
}

// APIKeyDefaultScopes 是新建 API Key 固定的权限：读写用户。控制台不提供按密钥
// 收窄权限的入口，任何一把密钥拿到就是完整的用户读写能力。
var APIKeyDefaultScopes = []string{"users:read", "users:write"}

func (s *Service) CreateAPIKey(name string, createdBy string, days int) (*models.APIKey, string, error) {
	body, err := secret.RandomAlnum(32)
	if err != nil {
		return nil, "", err
	}
	full := "sk-" + body
	prefix := store.PrefixOfAPIKey(full)
	var exp *time.Time
	if days > 0 {
		t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		exp = &t
	}
	k := models.APIKey{
		ID:        idgen.New(),
		Name:      name,
		KeyPrefix: prefix,
		KeySuffix: store.SuffixOfAPIKey(full),
		KeyHash:   hashKey(full),
		Scopes:    models.TextArray(APIKeyDefaultScopes),
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

func (s *Service) PatchAPIKey(id uuid.UUID, name, status *string) (*models.APIKey, error) {
	var k models.APIKey
	if err := s.store.DB.Where("id = ?", id).First(&k).Error; err != nil {
		return nil, apperr.New(apperr.ErrNotFound, "NOT_FOUND", "密钥不存在")
	}
	updates := map[string]any{}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n != "" {
			updates["name"] = n
		}
	}
	if status != nil {
		updates["status"] = *status
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

// DeleteAPIKey 删除 API Key。这里做物理删除：密钥的 key_hash 是唯一凭据，
// 删掉即彻底失效，符合「删除」的直觉；列表里也不再显示，避免被误当成仍可用。
// 审计日志里的 api_key_id 不受影响（auth_audit_logs.api_key_id 没有外键），
// 仍保留历史记录，只是该 id 不再对应到任何密钥。
func (s *Service) DeleteAPIKey(id uuid.UUID) error {
	var k models.APIKey
	if err := s.store.DB.Where("id = ?", id).First(&k).Error; err != nil {
		return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "密钥不存在")
	}
	return s.store.DB.Delete(&models.APIKey{}, "id = ?", id).Error
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

func hashKey(full string) string {
	return store.HashToken(full)
}
