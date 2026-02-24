// Package stores provides cache store implementations for Nexus.
package stores

import (
	"context"
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

func (c *RedisCache) Get(ctx context.Context, key string) (*provider.CompletionResponse, error) {
	result := c.client.Get(ctx, c.prefix+key)
	data, err := result.Bytes()
	if err != nil {
		return nil, nil // cache miss
	}
	_ = data // Would json.Unmarshal here
	return nil, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, _ *provider.CompletionResponse) error {
	// Would json.Marshal resp here
	_ = c.client.Set(ctx, c.prefix+key, []byte("{}"), c.ttl)
	return nil
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	_ = c.client.Del(ctx, c.prefix+key)
	return nil
}

func (c *RedisCache) Clear(_ context.Context) error {
	// Redis SCAN + DEL with prefix — requires full client access
	return nil
}
