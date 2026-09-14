package codes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	rdb *redis.Client
}

func New(rdb *redis.Client) *Service { return &Service{rdb: rdb} }

func (s *Service) Issue(ctx context.Context, scene, target string) (string, error) {
	if err := s.checkRate(ctx, scene, target); err != nil {
		return "", err
	}
	code := sixDigits()
	sum := hash(scene, target, code)
	key := fmt.Sprintf("code:%s:%s", scene, target)
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, key, map[string]any{"hash": sum, "attempts": 0})
	pipe.Expire(ctx, key, 5*time.Minute)
	pipe.Set(ctx, fmt.Sprintf("rate:send:%s:%s", scene, target), "1", 60*time.Second)
	pipe.Incr(ctx, fmt.Sprintf("rate:hour:%s:%s", scene, target))
	pipe.Expire(ctx, fmt.Sprintf("rate:hour:%s:%s", scene, target), time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
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

func (s *Service) checkRate(ctx context.Context, scene, target string) error {
	n, err := s.rdb.Exists(ctx, fmt.Sprintf("rate:send:%s:%s", scene, target)).Result()
	if err != nil {
		return err
	}
	if n > 0 {
		return apperr.New(apperr.ErrRateLimited, "SEND_TOO_FAST", "发送过于频繁，请稍后再试")
	}
	hour, err := s.rdb.Get(ctx, fmt.Sprintf("rate:hour:%s:%s", scene, target)).Int()
	if err != nil && err != redis.Nil {
		return err
	}
	if hour >= 10 {
		return apperr.New(apperr.ErrRateLimited, "SEND_LIMIT", "发送次数已达上限")
	}
	return nil
}

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
