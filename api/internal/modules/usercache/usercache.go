// Package usercache 缓存「组装完成的用户资料」（dto.User），供公开资料接口与
// internal 接口共享。
//
// 缓存里存的是明文（含完整手机号/邮箱与角色）：两条读取路径任意一条命中都能
// 省掉数据库往返，而脱敏只在输出前做（dto.FromUserProfile）。Redis 与 Postgres
// 属同一信任边界，缓存明文不额外扩大暴露面。
//
// 失效策略是「写时显式删除 + TTL 兜底」：所有写路径调用 Del，遗漏的由 TTL 收敛。
// 任何 Redis 故障都只降级为回源数据库，不影响请求成败——缓存是可选优化，
// 不能把它的可用性变成接口的可用性。
package usercache

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/redis/go-redis/v9"
)

const (
	// keyPrefix 带版本号：缓存结构变化时改版本即可让旧键自然过期，
	// 不必写一次扫键的迁移。
	//
	// v2：dto.User 新增 updated_at。旧 v1 条目不含该字段，反序列化只会得到
	// 零值时间（0001-01-01），而 /v1/internal/users 恰好是从缓存取 dto.User
	// 返回的，会把零值透给调用方。改版本让旧条目不再被读到，比等 TTL 过期稳。
	keyPrefix = "dauth:user:v2:"
	// opTimeout 限制单次 Redis 操作的耗时。缓存是可选优化，Redis 变慢时
	// 应当立刻放弃并回源，而不是拖着请求一起等。
	opTimeout = 300 * time.Millisecond
)

// Cache 是用户资料缓存。请用 New 构造：未启用时 New 返回 nil，而所有方法都
// 接受 nil 接收者并退化为「从不命中 / 不写入」，调用方无需判空。
type Cache struct {
	rdb *redis.Client
	ttl time.Duration
}

// New 构造缓存。rdb 为 nil 或 ttl <= 0 时返回 nil，表示关闭缓存。
// 返回 nil 而不是一个空实现，是为了让「关掉缓存」在调用方看起来和没有缓存一样。
func New(rdb *redis.Client, ttl time.Duration) *Cache {
	if rdb == nil || ttl <= 0 {
		return nil
	}
	return &Cache{rdb: rdb, ttl: ttl}
}

func (c *Cache) Enabled() bool { return c != nil }

// TTL 暴露缓存有效期，便于日志与排查。
func (c *Cache) TTL() time.Duration {
	if c == nil {
		return 0
	}
	return c.ttl
}

func cacheKey(id uuid.UUID) string { return keyPrefix + id.String() }

func opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, opTimeout)
}

// Get 读单个用户。未命中或 Redis 出错都返回 false，由调用方回源。
func (c *Cache) Get(ctx context.Context, id uuid.UUID) (dto.User, bool) {
	if c == nil || id == uuid.Nil {
		return dto.User{}, false
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	raw, err := c.rdb.Get(ctx, cacheKey(id)).Bytes()
	if err != nil {
		// redis.Nil 是正常的未命中，不值得记日志。
		if err != redis.Nil {
			slog.Warn("user cache get failed", "err", err, "user_id", id)
		}
		return dto.User{}, false
	}
	var u dto.User
	if err := json.Unmarshal(raw, &u); err != nil {
		// 结构不兼容（例如发布期间新旧版本共存）：当作未命中并删掉坏值，
		// 否则每次读都要再解析一次同样的垃圾。
		slog.Warn("user cache decode failed", "err", err, "user_id", id)
		_ = c.rdb.Del(ctx, cacheKey(id)).Err()
		return dto.User{}, false
	}
	return u, true
}

// MGet 批量读，返回命中的部分。Redis 出错时返回空 map（视作全部未命中），
// 由调用方统一回源。
func (c *Cache) MGet(ctx context.Context, ids []uuid.UUID) map[uuid.UUID]dto.User {
	out := make(map[uuid.UUID]dto.User, len(ids))
	if c == nil || len(ids) == 0 {
		return out
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = cacheKey(id)
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		slog.Warn("user cache mget failed", "err", err, "count", len(ids))
		return out
	}
	for i, v := range vals {
		if i >= len(ids) || v == nil {
			continue
		}
		raw, ok := v.(string)
		if !ok {
			continue
		}
		var u dto.User
		if err := json.Unmarshal([]byte(raw), &u); err != nil {
			slog.Warn("user cache decode failed", "err", err, "user_id", ids[i])
			continue
		}
		out[ids[i]] = u
	}
	return out
}

// Put 写入单个用户。ID 为空或非法就跳过（写不进去不影响正确性，只是下次不命中）。
func (c *Cache) Put(ctx context.Context, u dto.User) {
	if c == nil {
		return
	}
	raw, err := json.Marshal(u)
	if err != nil {
		slog.Warn("user cache encode failed", "err", err, "user_id", u.ID)
		return
	}
	id, err := uuid.Parse(u.ID)
	if err != nil {
		return
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	if err := c.rdb.Set(ctx, cacheKey(id), raw, c.ttl).Err(); err != nil {
		slog.Warn("user cache put failed", "err", err, "user_id", id)
	}
}

// PutMany 批量写入，用 pipeline 把 N 次往返压成 1 次。
func (c *Cache) PutMany(ctx context.Context, users []dto.User) {
	if c == nil || len(users) == 0 {
		return
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	pipe := c.rdb.Pipeline()
	n := 0
	for _, u := range users {
		id, err := uuid.Parse(u.ID)
		if err != nil {
			continue
		}
		raw, err := json.Marshal(u)
		if err != nil {
			slog.Warn("user cache encode failed", "err", err, "user_id", u.ID)
			continue
		}
		pipe.Set(ctx, cacheKey(id), raw, c.ttl)
		n++
	}
	if n == 0 {
		return
	}
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Warn("user cache put many failed", "err", err, "count", n)
	}
}

// Del 删除若干用户的缓存。写路径在改动用户资料后调用它，让下一次读取回源。
func (c *Cache) Del(ctx context.Context, ids ...uuid.UUID) {
	if c == nil || len(ids) == 0 {
		return
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		keys = append(keys, cacheKey(id))
	}
	if len(keys) == 0 {
		return
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		slog.Warn("user cache del failed", "err", err, "count", len(keys))
	}
}
