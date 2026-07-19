// Package cache wires up the Redis client used for caching, sessions, OTPs and
// rate limiting.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/redis/go-redis/v9"
)

// Connect creates a Redis client and verifies connectivity with a ping.
func Connect(cfg config.Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return client, nil
}
