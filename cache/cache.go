// Package cache defines the caching interface for Nexus.
package cache

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Cache is the core caching interface.
type Cache interface {
	Get(ctx context.Context, key string) (*provider.CompletionResponse, error)
	Set(ctx context.Context, key string, resp *provider.CompletionResponse) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
}

// Service wraps a Cache with higher-level operations.
type Service interface {
	Cache
	Stats(ctx context.Context) (*Stats, error)
}

// Stats reports cache performance.
type Stats struct {
	Hits    int64   `json:"hits"`
	Misses  int64   `json:"misses"`
	Size    int64   `json:"size"`  // entries
	Bytes   int64   `json:"bytes"` // memory usage
	HitRate float64 `json:"hit_rate"`
}

// NewService wraps a Cache as a Service.
func NewService(c Cache) Service {
	return &cacheService{cache: c}
}

type cacheService struct {
	cache  Cache
	hits   int64
	misses int64
}

func (s *cacheService) Get(ctx context.Context, key string) (*provider.CompletionResponse, error) {
	resp, err := s.cache.Get(ctx, key)
	if err != nil || resp == nil {
		s.misses++
		return resp, err
	}
	s.hits++
	return resp, nil
}

func (s *cacheService) Set(ctx context.Context, key string, resp *provider.CompletionResponse) error {
	return s.cache.Set(ctx, key, resp)
}

func (s *cacheService) Delete(ctx context.Context, key string) error {
	return s.cache.Delete(ctx, key)
}

func (s *cacheService) Clear(ctx context.Context) error {
	return s.cache.Clear(ctx)
}

func (s *cacheService) Stats(_ context.Context) (*Stats, error) {
	total := s.hits + s.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(s.hits) / float64(total)
	}
	return &Stats{
		Hits:    s.hits,
		Misses:  s.misses,
		HitRate: hitRate,
	}, nil
}
