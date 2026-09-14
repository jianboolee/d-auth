package codes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/normalize"
	"github.com/redis/go-redis/v9"
)

// Options 是限流参数。默认值面向「一个号码/一个 IP 的正常人」设计：
// 单目标每小时 10 条足以覆盖重发，单 IP 每小时 100 条足以覆盖共享出口下的小团队。
// 注意 IP 维度对 CGNAT / 公司出口不友好，需要按 slog 里的限流日志调参。
type Options struct {
	TTL         time.Duration // 验证码有效期
	Cooldown    time.Duration // 同一目标两次发送的最小间隔
	PerTargetHr int           // 同一目标每小时上限（跨 scene，硬上限）
	PerIPMinute int           // 同一来源 IP 每分钟上限
	PerIPHour   int           // 同一来源 IP 每小时上限
}

func (o Options) withDefaults() Options {
	if o.TTL <= 0 {
		o.TTL = 5 * time.Minute
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 60 * time.Second
	}
	if o.PerTargetHr <= 0 {
		o.PerTargetHr = 10
	}
	if o.PerIPMinute <= 0 {
		o.PerIPMinute = 10
	}
	if o.PerIPHour <= 0 {
		o.PerIPHour = 100
	}
	return o
}

type Service struct {
	rdb  *redis.Client
	opts Options
}

func New(rdb *redis.Client, opts Options) *Service {
	return &Service{rdb: rdb, opts: opts.withDefaults()}
}

// IssueInput 是一次验证码签发请求。
type IssueInput struct {
	Scene  string
	Target string // 手机号或邮箱，已规范化
	IP     string // 来源 IP，用于按 IP 限流；为空则跳过该维度
	// Channel 仅用于日志（"sms" / "email"）。
	Channel string
}

// Issue 检查限流并签发验证码，返回明文验证码供短信/邮件发送方使用。
//
// 限流按「目标」与「来源 IP」两个维度，且都不包含 scene：scene 是客户端可控的，
// 一旦进入限流 key，攻击者只要更换 scene 就能拿到一份新配额，单号上限形同虚设。
// 校验与计数在同一次 Lua 调用里完成，避免并发下「都读到未超限、再一起写」的竞态。
func (s *Service) Issue(ctx context.Context, in IssueInput) (string, error) {
	code := sixDigits()
	sum := hash(in.Scene, in.Target, code)

	codeKey := fmt.Sprintf("code:%s:%s", in.Scene, in.Target)
	buckets := []rateBucket{{
		key:    fmt.Sprintf("rate:cooldown:%s", in.Target),
		limit:  1,
		window: s.opts.Cooldown,
		code:   "SEND_TOO_FAST",
		msg:    "发送过于频繁，请稍后再试",
	}, {
		key:    fmt.Sprintf("rate:target:hour:%s", in.Target),
		limit:  s.opts.PerTargetHr,
		window: time.Hour,
		code:   "SEND_LIMIT",
		msg:    "发送次数已达上限",
	}}
	if in.IP != "" {
		buckets = append(buckets,
			rateBucket{
				key:    fmt.Sprintf("rate:ip:minute:%s", in.IP),
				limit:  s.opts.PerIPMinute,
				window: time.Minute,
				code:   "SEND_TOO_FAST",
				msg:    "发送过于频繁，请稍后再试",
			},
			rateBucket{
				key:    fmt.Sprintf("rate:ip:hour:%s", in.IP),
				limit:  s.opts.PerIPHour,
				window: time.Hour,
				code:   "SEND_LIMIT",
				msg:    "发送次数已达上限",
			},
		)
	}

	allowed, hit, err := s.consume(ctx, buckets, codeKey, sum, s.opts.TTL)
	if err != nil {
		return "", err
	}
	if !allowed {
		// 记录命中的桶，便于发现滥用与按真实流量调参（target 脱敏，不落明文号码）。
		slog.Warn("code send rate limited",
			"channel", in.Channel, "scene", in.Scene,
			"target", normalize.MaskTarget(in.Target), "ip", in.IP, "bucket", hit)
		return "", apperr.New(apperr.ErrRateLimited, hit.code, hit.msg)
	}
	return code, nil
}

func (s *Service) Verify(ctx context.Context, scene, target, code string) error {
	key := fmt.Sprintf("code:%s:%s", scene, target)
	data, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil || len(data) == 0 {
		return apperr.New(apperr.ErrInvalidCode, "INVALID_CODE", "验证码不正确或已过期")
	}
	attempts, _ := strconv.Atoi(data["attempts"])
	if attempts >= 5 {
		_ = s.rdb.Del(ctx, key).Err()
		return apperr.New(apperr.ErrInvalidCode, "CODE_LOCKED", "验证码尝试次数过多")
	}
	if data["hash"] != hash(scene, target, code) {
		_ = s.rdb.HIncrBy(ctx, key, "attempts", 1).Err()
		return apperr.New(apperr.ErrInvalidCode, "INVALID_CODE", "验证码不正确或已过期")
	}
	_ = s.rdb.Del(ctx, key).Err()
	return nil
}

// rateBucket 是一个固定窗口计数桶：窗口内计数达到 limit 即拒绝。
// limit 为 1 时等价于「窗口内只能来一次」的冷却。
type rateBucket struct {
	key    string
	limit  int
	window time.Duration
	code   string
	msg    string
}

// consume 在 Redis 里原子地「检查所有桶 + 写入验证码 + 递增所有桶」。
// 返回 allowed=false 时 hit 是命中的桶（用于决定错误码与日志）。
func (s *Service) consume(ctx context.Context, buckets []rateBucket, codeKey, codeHash string, ttl time.Duration) (bool, rateBucket, error) {
	keys := make([]string, 0, len(buckets))
	argv := make([]any, 0, len(buckets)*2+3)
	// KEYS = [bucket1..bucketN, codeKey]
	// ARGV = [n, limit1,window1, ..., limitN,windowN, codeHash, ttlSeconds]
	argv = append(argv, len(buckets))
	for _, b := range buckets {
		keys = append(keys, b.key)
		argv = append(argv, b.limit, int(b.window.Seconds()))
	}
	keys = append(keys, codeKey)
	argv = append(argv, codeHash, int(ttl.Seconds()))

	res, err := consumeScript.Run(ctx, s.rdb, keys, argv...).Int64()
	if err != nil {
		return false, rateBucket{}, err
	}
	if res >= 0 {
		b := buckets[res]
		return false, b, nil
	}
	return true, rateBucket{}, nil
}

// consumeScript 返回 -1 表示放行；返回 i>=0 表示第 i 个桶超限（不做任何写入）。
// 先全部检查、再全部写入，保证超限时不产生副作用（不会只递增了一部分）。
var consumeScript = redis.NewScript(`
local n = tonumber(ARGV[1])
local bucketCount = n
local codeKeyIdx = bucketCount + 1
local hashArg = ARGV[1 + bucketCount * 2 + 1]
local ttlArg  = ARGV[1 + bucketCount * 2 + 2]

-- 1) 检查所有桶
for i = 1, bucketCount do
  local limit = tonumber(ARGV[1 + (i - 1) * 2 + 1])
  local count = tonumber(redis.call('GET', KEYS[i]) or '0')
  if count >= limit then
    return i - 1
  end
end

-- 2) 写入验证码
redis.call('HSET', KEYS[codeKeyIdx], 'hash', hashArg, 'attempts', 0)
redis.call('EXPIRE', KEYS[codeKeyIdx], ttlArg)

-- 3) 递增所有桶（首次写入时设置过期）
for i = 1, bucketCount do
  local window = tonumber(ARGV[1 + (i - 1) * 2 + 2])
  local c = redis.call('INCR', KEYS[i])
  if c == 1 then
    redis.call('EXPIRE', KEYS[i], window)
  end
end

return -1
`)

func sixDigits() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%06d", n%1000000)
}

func hash(scene, target, code string) string {
	sum := sha256.Sum256([]byte(scene + ":" + target + ":" + code))
	return hex.EncodeToString(sum[:])
}
