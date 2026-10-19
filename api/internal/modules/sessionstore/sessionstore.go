// Package sessionstore 用 Redis 承载「refresh token 白名单」：它只回答一个问题
// ——这枚 refresh token 还能不能换到新的 access token。
//
// 职责边界（与旧 auth_sessions 表的关键差异）：
//
//   - 读路径不碰这里。access token 是自证的 RS256 JWT，验签即信（见 httpx.resolveActor），
//     不查库、不查 Redis。因此 Redis 从「每个请求都要查的会话表」退化为「续期裁决者」，
//     只有登录 / 续期 / 登出 / 改密这些动作才访问它。
//   - 用白名单而非黑名单：key 存在 = 可续期。Redis 丢数据的方向是「所有人都要重登」
//     （烦但安全），而不是「已吊销的 token 复活」（安全问题）。
//   - fail-closed：Redis 不可用时续期/登录直接失败，绝不「查不到就放行」。
//     这与 usercache 的 fail-open 正好相反——那只是可选缓存，这里承载的是会话状态。
//
// key 设计（沿用项目 dauth: 前缀约定）：
//
//	dauth:rt:<sha256(refresh)>   → JSON{user_id, cid, family, created}，TTL = refresh 寿命
//	dauth:urt:<user_id>          → SET(该用户所有有效 refresh 的 hash)，用于按用户批量吊销
//
// refresh token 明文只出现在 Set-Cookie 与响应体里，存储侧一律只留 sha256，
// 与旧 auth_sessions.refresh_token_hash 的取值方式一致。
package sessionstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// keyPrefix 带版本号：会话结构变化时改版本即可让旧键自然过期，
	// 与 usercache 的做法一致。
	keyPrefix = "dauth:rt:v1:"
	// userIndexPrefix 是「某用户名下所有 refresh」的集合前缀。
	userIndexPrefix = "dauth:urt:v1:"
	// opTimeout 限制单次 Redis 操作耗时。与 usercache 不同，这里超时即失败，
	// 不降级回源——没有可回源的地方。
	opTimeout = 3 * time.Second
)

// ErrNotFound 表示 refresh token 不存在、已过期或已被吊销（三者对外无差别，
// 调用方统一按「登录已失效」处理，避免泄露「是否存在但已吊销」这类信息）。
var ErrNotFound = errors.New("sessionstore: refresh token not found")

// Entry 是存进 Redis 的 refresh 记录（不含明文 token）。
type Entry struct {
	UserID  string    `json:"user_id"`
	ClientID string   `json:"cid,omitempty"`
	// Family 标识一次「登录会话」的轮转链：登录时生成，此后每次轮换沿用。
	// 用于将来检测 refresh 复用（旧 token 再次出现即判定盗用，端掉整条链）。
	Family  string    `json:"family"`
	Created time.Time `json:"created"`
}

// Store 是 refresh token 白名单。所有方法都可并发调用。
//
// 与 usercache 一样，所有方法都接受 nil 接收者并退化为「无操作 / 未命中」：
// 只跑 Postgres 的测试（如后台服务测试）无需拉起 Redis，构造时传 nil 即可。
// 生产环境必须传真实 Store——nil 下登录不会登记 refresh（等于发不出可续期的会话）。
type Store struct {
	rdb *redis.Client
	ttl time.Duration
}

// New 构造。rdb 为 nil 时返回 nil，表示「无会话存储」。
func New(rdb *redis.Client, ttl time.Duration) *Store {
	if rdb == nil {
		return nil
	}
	return &Store{rdb: rdb, ttl: ttl}
}

// TTL 暴露 refresh 寿命，便于日志与排查。
func (s *Store) TTL() time.Duration {
	if s == nil {
		return 0
	}
	return s.ttl
}

func refreshKey(hash string) string { return keyPrefix + hash }
func userIndexKey(userID string) string { return userIndexPrefix + userID }

// Hash 计算 refresh token 的存储键。与 store.HashToken 同源（sha256 hex），
// 但这里自带一份以免 sessionstore 反向依赖 store 包。
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func opCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, opTimeout)
}

// Issue 登记一枚新的 refresh token。raw 为明文（只用于算 hash），family 为
// 轮转链标识——首次登录时由调用方生成，续期轮换时沿用原值。
func (s *Store) Issue(ctx context.Context, raw string, e Entry) error {
	if s == nil {
		return nil
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()

	if e.Created.IsZero() {
		e.Created = time.Now()
	}
	blob, err := json.Marshal(e)
	if err != nil {
		return err
	}
	hash := Hash(raw)
	// 一条 Lua 保证「写 refresh + 加入用户索引」要么都成功要么都不做，
	// 避免索引里有悬空成员（批量吊销时删到不存在的 key 无害，但反过来
	// 有 key 却不在索引里会让该用户的 refresh 逃过批量吊销）。
	return issueScript.Run(ctx, s.rdb,
		[]string{refreshKey(hash), userIndexKey(e.UserID)},
		blob, int(s.ttl.Seconds()), hash,
	).Err()
}

var issueScript = redis.NewScript(`
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
redis.call('SADD', KEYS[2], ARGV[3])
redis.call('EXPIRE', KEYS[2], tonumber(ARGV[2]))
return 1
`)

// Lookup 只读地查一枚 refresh，不轮换。用于「不产生副作用地确认登录态是否还活着」，
// 例如登出时想先确认 token 归属，或未来的会话自检。
func (s *Store) Lookup(ctx context.Context, raw string) (Entry, error) {
	if s == nil {
		return Entry{}, ErrNotFound
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.lookup(ctx, Hash(raw))
}

func (s *Store) lookup(ctx context.Context, hash string) (Entry, error) {
	blob, err := s.rdb.Get(ctx, refreshKey(hash)).Bytes()
	if err == redis.Nil {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(blob, &e); err != nil {
		// 结构不兼容（发布期间新旧并存）：当作失效并清掉坏值。
		_ = s.rdb.Del(ctx, refreshKey(hash)).Err()
		return Entry{}, ErrNotFound
	}
	return e, nil
}

// Rotate 用一枚旧 refresh 换一枚新的：旧键删除、新键写入、用户索引同步替换，
// 并在同一次调用里返回旧记录（调用方据此拿 user_id/cid 去签新 access）。
//
// 轮换是原子的：Lua 脚本先取旧值，取不到直接返回 nil；取到则删旧写新。
// 这样并发用同一枚旧 token 续期时，只有一个能成功——落败方拿到 ErrNotFound，
// 可用于检测 refresh 复用（见 Entry.Family 注释）。
func (s *Store) Rotate(ctx context.Context, oldRaw, newRaw string) (Entry, error) {
	if s == nil {
		return Entry{}, ErrNotFound
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.rotate(ctx, oldRaw, newRaw)
}

func (s *Store) rotate(ctx context.Context, oldRaw, newRaw string) (Entry, error) {
	oldHash := Hash(oldRaw)
	newHash := Hash(newRaw)
	res, err := rotateScript.Run(ctx, s.rdb, []string{refreshKey(oldHash)}, int(s.ttl.Seconds())).Result()
	if err != nil {
		return Entry{}, err
	}
	blob, _ := res.(string)
	if blob == "" {
		return Entry{}, ErrNotFound
	}
	var e Entry
	if err := json.Unmarshal([]byte(blob), &e); err != nil {
		// 取到但解析不出来：数据损坏，删掉旧键后按失效处理。
		_ = s.rdb.Del(ctx, refreshKey(oldHash)).Err()
		return Entry{}, ErrNotFound
	}
	if e.Created.IsZero() {
		e.Created = time.Now()
	}

	next, err := json.Marshal(e)
	if err != nil {
		return Entry{}, err
	}
	indexKey := userIndexKey(e.UserID)
	if err := rotateWriteScript.Run(ctx, s.rdb,
		[]string{refreshKey(newHash), indexKey},
		next, int(s.ttl.Seconds()), newHash, oldHash,
	).Err(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// 先取旧值：取不到返回空串（调用方按未命中处理），存在则删除旧键。
var rotateScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if not v then
  return ''
end
redis.call('DEL', KEYS[1])
return v
`)

// 写新键 + 从用户索引移除旧 hash、加入新 hash。
var rotateWriteScript = redis.NewScript(`
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
redis.call('SREM', KEYS[2], ARGV[4])
redis.call('SADD', KEYS[2], ARGV[3])
redis.call('EXPIRE', KEYS[2], tonumber(ARGV[2]))
return 1
`)

// RevokeByToken 吊销单枚 refresh（登出）。幂等：不存在也返回 nil。
func (s *Store) RevokeByToken(ctx context.Context, raw string) error {
	if raw == "" || s == nil {
		return nil
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	e, err := s.lookup(ctx, Hash(raw))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.revoke(ctx, Hash(raw), e.UserID)
}

var revokeScript = redis.NewScript(`
redis.call('DEL', KEYS[1])
redis.call('SREM', KEYS[2], ARGV[1])
return 1
`)

func (s *Store) revoke(ctx context.Context, hash, userID string) error {
	return revokeScript.Run(ctx, s.rdb,
		[]string{refreshKey(hash), userIndexKey(userID)}, hash,
	).Err()
}

// RevokeUserAll 吊销某用户全部 refresh（停用 / 注销）。幂等。
func (s *Store) RevokeUserAll(ctx context.Context, userID string) error {
	return s.RevokeUserAllExcept(ctx, userID, "")
}

// RevokeUserAllExcept 吊销某用户全部 refresh，但保留 keepRaw（明文）对应的那一枚。
// 改密时用它「踢掉其它设备、留下当前设备」，与旧 auth_sessions 里
// `id <> current` 的语义一致。keepRaw 为空等价于 RevokeUserAll。
//
// 走用户索引一次性定位，避免 SCAN 全库。索引里的成员可能因 TTL 过期而不再有
// 对应的 refresh 键，删到不存在的键无害；索引本身按需重建（保留项留下，否则清掉）。
func (s *Store) RevokeUserAllExcept(ctx context.Context, userID, keepRaw string) error {
	if userID == "" || s == nil {
		return nil
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()

	hashes, err := s.rdb.SMembers(ctx, userIndexKey(userID)).Result()
	if err != nil {
		return err
	}
	keep := ""
	if keepRaw != "" {
		keep = Hash(keepRaw)
	}

	delKeys := make([]string, 0, len(hashes))
	delHashes := make([]interface{}, 0, len(hashes))
	kept := false
	for _, h := range hashes {
		if h == keep {
			kept = true
			continue
		}
		delKeys = append(delKeys, refreshKey(h))
		delHashes = append(delHashes, h)
	}
	if len(delKeys) > 0 {
		if err := s.rdb.Del(ctx, delKeys...).Err(); err != nil {
			return err
		}
	}
	if kept {
		// 保留一枚：索引收缩为只剩它（移除被删的成员，注意集合成员是裸 hash，
		// 不是带前缀的 key），TTL 沿用，由后续轮换再刷新。
		if len(delHashes) == 0 {
			return nil
		}
		return s.rdb.SRem(ctx, userIndexKey(userID), delHashes...).Err()
	}
	return s.rdb.Del(ctx, userIndexKey(userID)).Err()
}

// RevokeClientAll 吊销归属某 client 的全部 refresh（删除应用时）。需要全库扫描，
// 因此仅在低频的管理动作里调用。
//
// 用 SCAN 而非 KEYS：后者会阻塞 Redis。这里逐批读取 refresh 键并比对 cid。
func (s *Store) RevokeClientAll(ctx context.Context, clientID string) error {
	if clientID == "" || s == nil {
		return nil
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()

	var cursor uint64
	for {
		keys, next, err := s.rdb.Scan(ctx, cursor, keyPrefix+"*", 200).Result()
		if err != nil {
			return err
		}
		for _, k := range keys {
			blob, err := s.rdb.Get(ctx, k).Bytes()
			if err != nil {
				continue
			}
			var e Entry
			if json.Unmarshal(blob, &e) != nil || e.ClientID != clientID {
				continue
			}
			// 删 refresh 键，并从属主的用户索引里摘掉该 hash。
			hash := strings.TrimPrefix(k, keyPrefix)
			_ = s.rdb.Del(ctx, k).Err()
			_ = s.rdb.SRem(ctx, userIndexKey(e.UserID), hash).Err()
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Count 返回当前有效 refresh 数量，仅用于诊断/测试。
func (s *Store) Count(ctx context.Context, userID string) (int64, error) {
	if s == nil {
		return 0, nil
	}
	ctx, cancel := opCtx(ctx)
	defer cancel()
	return s.rdb.SCard(ctx, userIndexKey(userID)).Result()
}

func (e Entry) String() string {
	return fmt.Sprintf("sessionstore.Entry{user=%s cid=%s family=%s}", e.UserID, e.ClientID, e.Family)
}