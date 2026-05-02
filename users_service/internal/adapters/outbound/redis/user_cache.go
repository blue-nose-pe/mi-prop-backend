package redisadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"users_service/internal/core/domain"
	"users_service/internal/core/ports"

	"github.com/redis/go-redis/v9"
)

type UserCache struct {
	client *redis.Client
	ttl    time.Duration
}

var _ ports.UserCache = (*UserCache)(nil)

func NewUserCache(c *redis.Client, ttl time.Duration) *UserCache {
	return &UserCache{client: c, ttl: ttl}
}

func (c *UserCache) Set(ctx context.Context, u *domain.User) error {
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, userKey(u.ID), data, c.ttl).Err()
}

func (c *UserCache) Get(ctx context.Context, id domain.UserID) (*domain.User, error) {
	data, err := c.client.Get(ctx, userKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrUserNotFound // miss => el core ira al repo
	}
	if err != nil {
		return nil, err
	}
	var u domain.User
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *UserCache) Delete(ctx context.Context, id domain.UserID) error {
	return c.client.Del(ctx, userKey(id)).Err()
}

func userKey(id domain.UserID) string {
	return fmt.Sprintf("users_service:user:%s", id)
}
