package rdb

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func Open(addr, password string, db int) (*redis.Client, error) {
	c := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}
	return c, nil
}
