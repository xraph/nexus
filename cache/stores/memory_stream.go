package stores

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/nexus/cache"
)

// MemoryStreamCache is an in-memory store for cached streamed responses.
// It uses a simple map with TTL — eviction is lazy on Get, plus a soft
// max-entry cap on Set.
type MemoryStreamCache struct {
	mu      sync.RWMutex
	entries map[string]*streamEntry
	maxKeys int
}

type streamEntry struct {
	frames    []cache.StreamFrame
	expiresAt time.Time
}

// MemoryStreamOption configures the memory stream cache.
type MemoryStreamOption func(*MemoryStreamCache)

// WithStreamMaxKeys caps the number of cached streams.
func WithStreamMaxKeys(n int) MemoryStreamOption {
	return func(c *MemoryStreamCache) { c.maxKeys = n }
}

// NewMemoryStream creates a new in-memory stream cache.
func NewMemoryStream(opts ...MemoryStreamOption) *MemoryStreamCache {
	c := &MemoryStreamCache{
		entries: make(map[string]*streamEntry),
		maxKeys: 1024,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetStream returns the cached frames for key, or nil if absent/expired.
func (c *MemoryStreamCache) GetStream(_ context.Context, key string) ([]cache.StreamFrame, error) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, nil
	}
	return e.frames, nil
}

// SetStream stores frames under key with the given TTL. ttl=0 means no
// expiry (subject only to the soft max-keys cap).
func (c *MemoryStreamCache) SetStream(_ context.Context, key string, frames []cache.StreamFrame, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxKeys {
		// Evict an arbitrary entry — round-robin would need tracking that
		// isn't worth the complexity for this default backend.
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}

	entry := &streamEntry{frames: frames}
	if ttl > 0 {
		entry.expiresAt = time.Now().Add(ttl)
	}
	c.entries[key] = entry
	return nil
}

// DeleteStream removes the cached frames for key.
func (c *MemoryStreamCache) DeleteStream(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
	return nil
}

// Compile-time check.
var _ cache.StreamCache = (*MemoryStreamCache)(nil)
