// Package stores provides cache store implementations for Nexus.
package stores

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/xraph/nexus/provider"
)

// RedisCache is a Redis-backed cache implementation.
// It requires a Redis client to be provided at construction time.
//
// Usage:
//
//	import "github.com/redis/go-redis/v9"
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	cache := stores.NewRedis(rdb)
type RedisCache struct {
	client RedisClient
	ttl    time.Duration
	prefix string
}

// RedisClient is the minimal interface required from a Redis client.
// Compatible with github.com/redis/go-redis/v9.
type RedisClient interface {
	Get(ctx context.Context, key string) RedisResult
	Set(ctx context.Context, key string, value any, ttl time.Duration) RedisResult
	Del(ctx context.Context, keys ...string) RedisResult
}

// RedisResult is the minimal result interface from a Redis command.
type RedisResult interface {
	Bytes() ([]byte, error)
	Err() error
}

// RedisOption configures a Redis cache.
type RedisOption func(*RedisCache)

// WithRedisTTL sets the cache TTL.
func WithRedisTTL(ttl time.Duration) RedisOption {
	return func(c *RedisCache) { c.ttl = ttl }
}

// WithRedisPrefix sets a key prefix.
func WithRedisPrefix(prefix string) RedisOption {
	return func(c *RedisCache) { c.prefix = prefix }
}

// NewRedis creates a Redis-backed cache.
func NewRedis(client RedisClient, opts ...RedisOption) *RedisCache {
	c := &RedisCache{
		client: client,
		ttl:    10 * time.Minute,
		prefix: "nexus:",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get retrieves a cached completion response. Returns (nil, nil) on miss.
func (c *RedisCache) Get(ctx context.Context, key string) (*provider.CompletionResponse, error) {
	res := c.client.Get(ctx, c.prefix+key)
	data, err := res.Bytes()
	if err != nil || len(data) == 0 {
		return nil, nil //nolint:nilnil // miss is the empty case
	}
	var resp provider.CompletionResponse
	if uerr := json.Unmarshal(data, &resp); uerr != nil {
		return nil, uerr
	}
	return &resp, nil
}

// Set stores a completion response under key with the configured TTL.
func (c *RedisCache) Set(ctx context.Context, key string, resp *provider.CompletionResponse) error {
	if resp == nil {
		return errors.New("redis: cannot cache nil response")
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	res := c.client.Set(ctx, c.prefix+key, data, c.ttl)
	if res != nil {
		return res.Err()
	}
	return nil
}

// Delete removes a cached entry.
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	res := c.client.Del(ctx, c.prefix+key)
	if res != nil {
		return res.Err()
	}
	return nil
}

// Clear is a no-op — bulk-delete by prefix requires SCAN, which we don't
// expose on the minimal RedisClient interface. Callers needing Clear can
// flush the database out-of-band.
func (c *RedisCache) Clear(_ context.Context) error { return nil }
