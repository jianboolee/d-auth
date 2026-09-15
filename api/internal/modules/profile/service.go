// Package profile 读取用户资料：缓存优先，未命中回源数据库并回填。
//
// 它同时服务两条路径：
//   - 公开资料接口 /v1/users：输出前经 dto.FromUserProfile 脱敏并把 roles 折叠成 is_admin；
//   - internal 接口 /v1/internal/users：返回明文 User（含完整 roles）。
//
// 两条路径共用同一份缓存条目（缓存的就是组装好的 dto.User），所以后者也能吃到缓存。
package profile

import (
	"context"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/modules/usercache"
)

type Service struct {
	cfg   *config.Config
	store *store.Store
	cache *usercache.Cache
}

// New 构造读取服务。cache 可以为 nil（关闭缓存或 Redis 缺失），此时每次都回源数据库。
func New(cfg *config.Config, st *store.Store, cache *usercache.Cache) *Service {
	return &Service{cfg: cfg, store: st, cache: cache}
}

func notFound() error { return apperr.New(apperr.ErrNotFound, "NOT_FOUND", "用户不存在") }

// UserByID 返回单个用户的明文资料（含完整 roles），供 internal 接口使用。
func (s *Service) UserByID(ctx context.Context, id uuid.UUID) (dto.User, error) {
	if id == uuid.Nil {
		return dto.User{}, notFound()
	}
	return s.load(ctx, id)
}

// ProfileByID 返回脱敏后的公开资料。
func (s *Service) ProfileByID(ctx context.Context, id uuid.UUID) (dto.Profile, error) {
	u, err := s.UserByID(ctx, id)
	if err != nil {
		return dto.Profile{}, err
	}
	return dto.FromUserProfile(u), nil
}

// UserByIdentity 按 (类型, 标识) 精确匹配，类型只应是 phone 或 email。
// 先走 FindIdentity（(type, identifier) 上有唯一索引，1 次查询），命中后再复用
// 按 id 的缓存路径。
func (s *Service) UserByIdentity(ctx context.Context, typ, identifier string) (dto.User, error) {
	ident, err := s.store.FindIdentity(typ, identifier)
	if err != nil {
		return dto.User{}, notFound()
	}
	return s.UserByID(ctx, ident.UserID)
}

// ProfileByIdentity 是 UserByIdentity 的脱敏版本。
func (s *Service) ProfileByIdentity(ctx context.Context, typ, identifier string) (dto.Profile, error) {
	u, err := s.UserByIdentity(ctx, typ, identifier)
	if err != nil {
		return dto.Profile{}, err
	}
	return dto.FromUserProfile(u), nil
}

// Profiles 批量取脱敏资料，返回值顺序与入参 ids 一致，查不到的 id 直接跳过
// （批量查询的语义是「尽量给」，不是「一个缺失就整体失败」）。
func (s *Service) Profiles(ctx context.Context, ids []uuid.UUID) ([]dto.Profile, error) {
	m, err := s.loadMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]dto.Profile, 0, len(m))
	for _, id := range ids {
		if u, ok := m[id]; ok {
			out = append(out, dto.FromUserProfile(u))
		}
	}
	return out, nil
}

// load 读单个用户：缓存优先，未命中则打库（用户 + 角色 + 身份共 3 次查询）并回填。
func (s *Service) load(ctx context.Context, id uuid.UUID) (dto.User, error) {
	if u, ok := s.cache.Get(ctx, id); ok {
		return u, nil
	}
	user, err := s.store.UserByID(id)
	if err != nil {
		return dto.User{}, notFound()
	}
	roles, _ := s.store.RoleCodes(id)
	idents, _ := s.store.IdentitiesOf(id)
	u := dto.FromUser(user, roles, idents, s.cfg.AvatarURL)
	s.cache.Put(ctx, u)
	return u, nil
}

// loadMany 批量读：先 MGET 拿命中，再对缺失部分用 3 条 IN 查询补齐（避免 N+1），
// 最后把补齐的结果回填。不存在的 id 不会出现在返回值里。
func (s *Service) loadMany(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]dto.User, error) {
	out := make(map[uuid.UUID]dto.User, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for id, u := range s.cache.MGet(ctx, ids) {
		out[id] = u
	}
	var missing []uuid.UUID
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	users, err := s.store.UsersByIDs(missing)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return out, nil
	}
	found := make([]uuid.UUID, 0, len(users))
	for i := range users {
		found = append(found, users[i].ID)
	}
	rolesByID, err := s.store.RoleCodesByUserIDs(found)
	if err != nil {
		return nil, err
	}
	identsByID, err := s.store.IdentitiesByUserIDs(found)
	if err != nil {
		return nil, err
	}
	fill := make([]dto.User, 0, len(users))
	for i := range users {
		roles := rolesByID[users[i].ID]
		if roles == nil {
			// 与 store.RoleCodes 一致：没有角色时给空切片而不是 nil，
			// 否则序列化成 null，前端按数组渲染会崩。
			roles = []string{}
		}
		u := dto.FromUser(&users[i], roles, identsByID[users[i].ID], s.cfg.AvatarURL)
		out[users[i].ID] = u
		fill = append(fill, u)
	}
	s.cache.PutMany(ctx, fill)
	return out, nil
}
