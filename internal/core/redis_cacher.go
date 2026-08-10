package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisCacher struct {
	client *redis.Client
}

func NewRedisCacher(client *redis.Client) Cacher {
	return &redisCacher{client: client}
}

func (r *redisCacher) Set(
	ctx context.Context,
	key string,
	value any,
	ttl time.Duration,
) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, b, ttl).Err()
}

func (r *redisCacher) SetNX(
	ctx context.Context,
	key string,
	value any,
	ttl time.Duration,
) (bool, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return false, err
	}

	// SetArgs with Mode "NX" rather than the deprecated SetNX helper. Redis
	// replies with a nil bulk string (redis.Nil here) when the key already
	// existed and nothing was written — that is the "lock not acquired" case,
	// not an error.
	err = r.client.SetArgs(ctx, key, b, redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Err()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *redisCacher) Get(ctx context.Context, key string, dest any) error {
	val, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrCacheMiss
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(val, dest)
}

func (r *redisCacher) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *redisCacher) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	return n > 0, err
}
