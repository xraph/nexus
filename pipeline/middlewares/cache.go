package middlewares

import (
	"context"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/pipeline"
)

// CacheMiddleware checks the cache before calling the provider,
// and stores successful responses in the cache.
type CacheMiddleware struct {
	cache cache.Service
}

// NewCache creates a caching middleware.
func NewCache(c cache.Service) *CacheMiddleware {
	return &CacheMiddleware{cache: c}
}

func (m *CacheMiddleware) Name() string  { return "cache" }
func (m *CacheMiddleware) Priority() int { return 280 } // After transforms, before routing

func (m *CacheMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.cache == nil || req.Completion == nil {
		return next(ctx)
	}

	// Don't cache streaming requests
	if req.Type == pipeline.RequestStream {
		return next(ctx)
	}

	// Generate cache key
	key := cache.CacheKey(req.Completion)

	// Check cache
	cached, err := m.cache.Get(ctx, key)
	if err == nil && cached != nil {
		cached.Cached = true
		ctx = pipeline.WithCacheHit(ctx, true)
		return &pipeline.Response{Completion: cached}, nil
	}

	// Cache miss — continue pipeline
	resp, err := next(ctx)
	if err != nil {
		return resp, err
	}

	// Store successful response
	if resp != nil && resp.Completion != nil {
		_ = m.cache.Set(ctx, key, resp.Completion)
	}

	return resp, nil
}
