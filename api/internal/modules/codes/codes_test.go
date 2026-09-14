package codes

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/redis/go-redis/v9"
)

// 需要真实 redis：通过 REDIS_TEST_ADDR 提供地址，否则跳过。
// 限流的正确性完全取决于 Redis 里的原子脚本，用真 redis 验证才有意义。
func newTestService(t *testing.T, opts Options) (*Service, *redis.Client) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR not set")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, opts), rdb
}

// 每个用例用独立 target，避免相互干扰。
func target(name string) string {
	return fmt.Sprintf("1380000%04d", uniqueSuffix(name))
}

var (
	suffixMu sync.Mutex
	suffix   int
)

func uniqueSuffix(name string) int {
	suffixMu.Lock()
	defer suffixMu.Unlock()
	suffix++
	// 用名字长度做点区分，配合自增保证不重复
	return (len(name)*7 + suffix) % 10000
}

func issue(s *Service, scene, tgt, ip string) error {
	_, err := s.Issue(context.Background(), IssueInput{Scene: scene, Target: tgt, IP: ip, Channel: "test"})
	return err
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	if c, _, ok := apperr.CodeOf(err); ok {
		return c
	}
	return "UNC0DED"
}

// 核心回归：换 scene 不能绕过单目标上限。
// 修复前限流 key 含 scene，攻击者可对同一号码换 scene 拿到新配额。
func TestTargetLimitIsNotBypassableByScene(t *testing.T) {
	const perTarget = 3
	svc, _ := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: perTarget, PerIPMinute: 1000, PerIPHour: 1000})
	tgt := target("scene-bypass")

	scenes := []string{"login", "bind_phone", "login", "bind_phone", "login"}
	var blocked int
	for i, sc := range scenes {
		err := issue(svc, sc, tgt, "203.0.113.10")
		if err != nil {
			blocked++
			if codeOf(err) != "SEND_LIMIT" {
				t.Fatalf("attempt %d (scene=%s): got %s, want SEND_LIMIT", i, sc, codeOf(err))
			}
		}
	}
	if blocked == 0 {
		t.Fatal("changing the scene bypassed the per-target limit: no attempt was blocked")
	}
	// 前 perTarget 次必须放行，之后一律拦截
	allowed := len(scenes) - blocked
	if allowed != perTarget {
		t.Fatalf("allowed %d sends, want exactly %d (per-target hour cap)", allowed, perTarget)
	}
}

// 冷却：同一目标在窗口内第二次发送应被 SEND_TOO_FAST 拦下。
func TestCooldownPerTarget(t *testing.T) {
	svc, _ := newTestService(t, Options{Cooldown: time.Minute, PerTargetHr: 100, PerIPMinute: 1000, PerIPHour: 1000})
	tgt := target("cooldown")

	if err := issue(svc, "login", tgt, "203.0.113.11"); err != nil {
		t.Fatalf("first send should pass, got %v", err)
	}
	err := issue(svc, "login", tgt, "203.0.113.11")
	if codeOf(err) != "SEND_TOO_FAST" {
		t.Fatalf("second send: got %q, want SEND_TOO_FAST", codeOf(err))
	}
	// 换 scene 也一样受冷却约束（冷却也按 target）
	err = issue(svc, "bind_phone", tgt, "203.0.113.11")
	if codeOf(err) != "SEND_TOO_FAST" {
		t.Fatalf("second send with a different scene: got %q, want SEND_TOO_FAST", codeOf(err))
	}
}

// IP 维度：不同号码、同一 IP，达到每分钟上限后应被拦（挡批量枚举）。
func TestPerIPMinuteLimit(t *testing.T) {
	const perIP = 2
	svc, _ := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: 100, PerIPMinute: perIP, PerIPHour: 1000})
	ip := "203.0.113.20"

	for i := 0; i < perIP; i++ {
		if err := issue(svc, "login", target(fmt.Sprintf("ip-ok-%d", i)), ip); err != nil {
			t.Fatalf("send %d should pass, got %v", i, err)
		}
	}
	err := issue(svc, "login", target("ip-blocked"), ip)
	if codeOf(err) != "SEND_TOO_FAST" {
		t.Fatalf("after %d sends from one IP: got %q, want SEND_TOO_FAST", perIP, codeOf(err))
	}
}

// 不同 IP 各自计数，互不影响（避免把所有人算作一个 IP）。
func TestDifferentIPsHaveSeparateBudgets(t *testing.T) {
	svc, _ := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: 100, PerIPMinute: 1, PerIPHour: 1000})

	if err := issue(svc, "login", target("ip-a"), "203.0.113.31"); err != nil {
		t.Fatalf("ip A first send: %v", err)
	}
	if err := issue(svc, "login", target("ip-b"), "203.0.113.32"); err != nil {
		t.Fatalf("ip B should have its own budget, got %v", err)
	}
}

// IP 为空时跳过 IP 维度，不应误判（用于内部调用等没有来源 IP 的场景）。
func TestEmptyIPSkipsIPBuckets(t *testing.T) {
	svc, _ := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: 100, PerIPMinute: 1, PerIPHour: 1})

	for i := 0; i < 5; i++ {
		if err := issue(svc, "login", target(fmt.Sprintf("noip-%d", i)), ""); err != nil {
			t.Fatalf("send %d without IP should pass, got %v", i, err)
		}
	}
}

// 原子性：并发打同一目标，放行次数不得超过上限，且验证码 key 只应存在一份状态。
func TestConcurrentSendsRespectLimit(t *testing.T) {
	const perTarget = 3
	svc, _ := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: perTarget, PerIPMinute: 1000, PerIPHour: 1000})
	tgt := target("concurrent")

	const goroutines = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	passed := 0
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := issue(svc, "login", tgt, "203.0.113.40"); err == nil {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if passed > perTarget {
		t.Fatalf("concurrent sends exceeded the limit: %d passed, cap is %d", passed, perTarget)
	}
	if passed == 0 {
		t.Fatal("no concurrent send passed; expected the limit to allow up to the cap")
	}
}

// 超限时不应写入验证码（避免攻击者通过被拒的请求覆盖受害者已有的有效验证码）。
func TestBlockedSendDoesNotOverwriteExistingCode(t *testing.T) {
	svc, rdb := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: 1, PerIPMinute: 1000, PerIPHour: 1000})
	tgt := target("nooverwrite")
	ctx := context.Background()

	if err := issue(svc, "login", tgt, "203.0.113.50"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	key := "code:login:" + tgt
	before, err := rdb.HGet(ctx, key, "hash").Result()
	if err != nil {
		t.Fatalf("read stored hash: %v", err)
	}

	if err := issue(svc, "login", tgt, "203.0.113.50"); codeOf(err) != "SEND_LIMIT" {
		t.Fatalf("second send should be blocked, got %q", codeOf(err))
	}
	after, err := rdb.HGet(ctx, key, "hash").Result()
	if err != nil {
		t.Fatalf("read stored hash after block: %v", err)
	}
	if before != after {
		t.Fatal("a blocked send overwrote the existing verification code")
	}
}

// 验证码校验的既有行为不能被破坏：正确码通过、错误码失败、用后即弃。
func TestVerifyRoundTrip(t *testing.T) {
	svc, _ := newTestService(t, Options{Cooldown: time.Millisecond, PerTargetHr: 100, PerIPMinute: 1000, PerIPHour: 1000})
	ctx := context.Background()
	tgt := target("verify")

	code, err := svc.Issue(ctx, IssueInput{Scene: "login", Target: tgt, IP: "203.0.113.60", Channel: "test"})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := svc.Verify(ctx, "login", tgt, "000000"); err == nil && code != "000000" {
		t.Fatal("a wrong code should not verify")
	}
	if err := svc.Verify(ctx, "login", tgt, code); err != nil {
		t.Fatalf("correct code should verify, got %v", err)
	}
	// 单次使用
	if err := svc.Verify(ctx, "login", tgt, code); codeOf(err) != "INVALID_CODE" {
		t.Fatalf("code should be single-use, got %q", codeOf(err))
	}
}
