package cache

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// SemanticMatcher finds semantically similar cache keys.
// This enables cache hits even when the exact prompt differs
// but the meaning is equivalent.
type SemanticMatcher interface {
	// Match returns the best matching cache key and a similarity score.
	// A score of 1.0 is an exact match; 0.0 is no match.
	// Returns ("", 0, nil) if no similar entry is found.
	Match(ctx context.Context, key string, threshold float64) (matchedKey string, score float64, err error)

	// Index adds a key to the semantic index.
	Index(ctx context.Context, key string) error

	// Remove deletes a key from the semantic index.
	Remove(ctx context.Context, key string) error
}

// SemanticCache wraps a Cache with semantic matching capabilities.
type SemanticCache struct {
	cache     Cache
	matcher   SemanticMatcher
	threshold float64
}

// NewSemanticCache creates a cache that falls back to semantic matching
// when exact key lookup misses. The threshold (0.0-1.0) controls the
// minimum similarity score for a semantic match.
func NewSemanticCache(cache Cache, matcher SemanticMatcher, threshold float64) *SemanticCache {
	if threshold <= 0 {
		threshold = 0.85
	}
	return &SemanticCache{
		cache:     cache,
		matcher:   matcher,
		threshold: threshold,
	}
}

// Compile-time check.
var _ Cache = (*SemanticCache)(nil)

func (sc *SemanticCache) Get(ctx context.Context, key string) (*provider.CompletionResponse, error) {
	// Try exact match first
	resp, err := sc.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if resp != nil {
		return resp, nil
	}

	// Fall back to semantic matching
	matchedKey, score, err := sc.matcher.Match(ctx, key, sc.threshold)
	if err != nil || matchedKey == "" || score < sc.threshold {
		return nil, nil
	}

	return sc.cache.Get(ctx, matchedKey)
}

func (sc *SemanticCache) Set(ctx context.Context, key string, resp *provider.CompletionResponse) error {
	if err := sc.matcher.Index(ctx, key); err != nil {
		// Non-fatal: cache still works without semantic index
		_ = err
	}
	return sc.cache.Set(ctx, key, resp)
}

func (sc *SemanticCache) Delete(ctx context.Context, key string) error {
	if err := sc.matcher.Remove(ctx, key); err != nil {
		// best-effort: semantic index removal is non-fatal
		_ = err
	}
	return sc.cache.Delete(ctx, key)
}

func (sc *SemanticCache) Clear(ctx context.Context) error {
	return sc.cache.Clear(ctx)
}
