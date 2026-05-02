// Package cacheadapter implementa ports.Cache contra Redis.
// Serializa con encoding/json — overhead aceptable para los dashboards
// que cachea (~5KB cada uno, TTL 5 min).
package cacheadapter

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"analytics_service/internal/core/ports"
)

type RedisCache struct {
	client *redis.Client
}

var _ ports.Cache = (*RedisCache)(nil)

func NewRedis(client *redis.Client) *RedisCache { return &RedisCache{client: client} }

func (c *RedisCache) Get(ctx context.Context, key string, dst any) (bool, error) {
	raw, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		// Cache corrupto → mejor invalidar y dejar que el caller recompute.
		_ = c.client.Del(ctx, key).Err()
		return false, nil
	}
	return true, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, raw, ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}
