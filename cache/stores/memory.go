// Package stores provides cache implementations for Nexus.
package stores

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/xraph/nexus/provider"
)

// MemoryCache is an in-memory LRU cache with TTL support.
type MemoryCache struct {
	mu       sync.RWMutex
	items    map[string]*list.Element
	eviction *list.List
	maxSize  int
	ttl      time.Duration
}

type memoryCacheEntry struct {
	key       string
	value     *provider.CompletionResponse
	expiresAt time.Time
}

// MemoryOption configures the memory cache.
type MemoryOption func(*MemoryCache)

// WithMaxSize sets the maximum number of entries.
func WithMaxSize(size int) MemoryOption {
	return func(c *MemoryCache) { c.maxSize = size }
}

// WithTTL sets the time-to-live for cache entries.
func WithTTL(ttl time.Duration) MemoryOption {
	return func(c *MemoryCache) { c.ttl = ttl }
}

// NewMemory creates an in-memory LRU cache.
func NewMemory(opts ...MemoryOption) *MemoryCache {
	c := &MemoryCache{
		items:    make(map[string]*list.Element),
		eviction: list.New(),
		maxSize:  1000,
		ttl:      10 * time.Minute,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *MemoryCache) Get(_ context.Context, key string) (*provider.CompletionResponse, error) {
	c.mu.RLock()
	elem, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		return nil, nil
	}

	entry := elem.Value.(*memoryCacheEntry)

	// Check TTL
	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		c.removeElement(elem)
		c.mu.Unlock()
		return nil, nil
	}

	// Move to front (most recently used)
	c.mu.Lock()
	c.eviction.MoveToFront(elem)
	c.mu.Unlock()

	return entry.value, nil
}

func (c *MemoryCache) Set(_ context.Context, key string, resp *provider.CompletionResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, update it
	if elem, ok := c.items[key]; ok {
		c.eviction.MoveToFront(elem)
		entry := elem.Value.(*memoryCacheEntry)
		entry.value = resp
		entry.expiresAt = time.Now().Add(c.ttl)
		return nil
	}

	// Evict if at capacity
	if c.eviction.Len() >= c.maxSize {
		c.evictOldest()
	}

	entry := &memoryCacheEntry{
		key:       key,
		value:     resp,
		expiresAt: time.Now().Add(c.ttl),
	}
	elem := c.eviction.PushFront(entry)
	c.items[key] = elem

	return nil
}

func (c *MemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
	return nil
}

func (c *MemoryCache) Clear(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.eviction.Init()
	return nil
}

func (c *MemoryCache) evictOldest() {
	elem := c.eviction.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

func (c *MemoryCache) removeElement(elem *list.Element) {
	c.eviction.Remove(elem)
	entry := elem.Value.(*memoryCacheEntry)
	delete(c.items, entry.key)
}
