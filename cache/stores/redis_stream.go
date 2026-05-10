package stores

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"time"

	"github.com/xraph/nexus/cache"
)

// RedisStreamCache stores streamed responses (ordered StreamFrames) under
// a single Redis key per cache entry. The blob is gob-encoded — fast,
// type-safe, and handles the multi-modal byte payloads (audio/image)
// without further escaping.
//
// Compatible with the same RedisClient interface as RedisCache.
type RedisStreamCache struct {
	client RedisClient
	prefix string
}

// RedisStreamOption configures the Redis stream cache.
type RedisStreamOption func(*RedisStreamCache)

// WithRedisStreamPrefix sets a key prefix (default "nexus:stream:").
func WithRedisStreamPrefix(prefix string) RedisStreamOption {
	return func(c *RedisStreamCache) { c.prefix = prefix }
}

// NewRedisStream creates a Redis-backed stream cache.
func NewRedisStream(client RedisClient, opts ...RedisStreamOption) *RedisStreamCache {
	c := &RedisStreamCache{
		client: client,
		prefix: "nexus:stream:",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetStream returns cached frames for key, or nil on miss.
func (c *RedisStreamCache) GetStream(ctx context.Context, key string) ([]cache.StreamFrame, error) {
	res := c.client.Get(ctx, c.prefix+key)
	data, err := res.Bytes()
	if err != nil || len(data) == 0 {
		return nil, nil //nolint:nilnil // miss is the empty case, not an error
	}
	var frames []cache.StreamFrame
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&frames); err != nil {
		return nil, fmt.Errorf("redisstream: decode: %w", err)
	}
	return frames, nil
}

// SetStream stores frames under key with the given TTL. ttl=0 means no
// explicit expiry (server default applies).
func (c *RedisStreamCache) SetStream(ctx context.Context, key string, frames []cache.StreamFrame, ttl time.Duration) error {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(frames); err != nil {
		return fmt.Errorf("redisstream: encode: %w", err)
	}
	res := c.client.Set(ctx, c.prefix+key, buf.Bytes(), ttl)
	if res != nil {
		if err := res.Err(); err != nil {
			return err
		}
	}
	return nil
}

// DeleteStream removes a cached stream.
func (c *RedisStreamCache) DeleteStream(ctx context.Context, key string) error {
	res := c.client.Del(ctx, c.prefix+key)
	if res != nil {
		if err := res.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Compile-time check.
var _ cache.StreamCache = (*RedisStreamCache)(nil)
