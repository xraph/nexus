package stores_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/provider"
)

// fakeRedis is a tiny in-process Redis stand-in implementing the minimal
// surface RedisStreamCache uses (Get / Set with TTL / Del). Avoids pulling
// miniredis as a test dep.
type fakeRedis struct {
	mu      sync.Mutex
	entries map[string]fakeRedisEntry
}

type fakeRedisEntry struct {
	bytes     []byte
	expiresAt time.Time
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{entries: make(map[string]fakeRedisEntry)}
}

func (f *fakeRedis) Get(_ context.Context, key string) stores.RedisResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[key]
	if !ok {
		return &fakeResult{err: errors.New("nil")}
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		delete(f.entries, key)
		return &fakeResult{err: errors.New("nil")}
	}
	return &fakeResult{bytes: e.bytes}
}

func (f *fakeRedis) Set(_ context.Context, key string, value any, ttl time.Duration) stores.RedisResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := value.([]byte)
	if !ok {
		return &fakeResult{err: errors.New("expected []byte value")}
	}
	e := fakeRedisEntry{bytes: append([]byte{}, b...)}
	if ttl > 0 {
		e.expiresAt = time.Now().Add(ttl)
	}
	f.entries[key] = e
	return &fakeResult{}
}

func (f *fakeRedis) Del(_ context.Context, keys ...string) stores.RedisResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.entries, k)
	}
	return &fakeResult{}
}

type fakeResult struct {
	bytes []byte
	err   error
}

func (r *fakeResult) Bytes() ([]byte, error) { return r.bytes, r.err }
func (r *fakeResult) Err() error             { return r.err }

func TestRedisStreamCache_RoundTrip(t *testing.T) {
	t.Parallel()
	rc := stores.NewRedisStream(newFakeRedis())

	frames := []cache.StreamFrame{
		{Chunk: &provider.StreamChunk{Provider: "p", Model: "m", Delta: provider.Delta{Content: "Hello"}}, OffsetMs: 0},
		{Chunk: &provider.StreamChunk{Delta: provider.Delta{Content: " world"}}, OffsetMs: 50},
		{Chunk: &provider.StreamChunk{Kind: provider.EventUsage, Usage: &provider.Usage{TotalTokens: 7}}, OffsetMs: 75},
	}

	if err := rc.SetStream(context.Background(), "k1", frames, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := rc.GetStream(context.Background(), "k1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d frames, want 3", len(got))
	}
	if got[0].Chunk.Delta.Content != "Hello" || got[1].Chunk.Delta.Content != " world" {
		t.Fatalf("text round-trip: %+v", got)
	}
	if got[2].Chunk.Usage == nil || got[2].Chunk.Usage.TotalTokens != 7 {
		t.Fatalf("usage round-trip: %+v", got[2].Chunk)
	}
}

func TestRedisStreamCache_MissReturnsNil(t *testing.T) {
	t.Parallel()
	rc := stores.NewRedisStream(newFakeRedis())
	got, err := rc.GetStream(context.Background(), "missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil on miss, got %d frames", len(got))
	}
}

func TestRedisStreamCache_Delete(t *testing.T) {
	t.Parallel()
	rc := stores.NewRedisStream(newFakeRedis())
	frames := []cache.StreamFrame{{Chunk: &provider.StreamChunk{Delta: provider.Delta{Content: "a"}}}}
	if err := rc.SetStream(context.Background(), "k", frames, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := rc.DeleteStream(context.Background(), "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := rc.GetStream(context.Background(), "k")
	if got != nil {
		t.Fatalf("expected nil after delete, got %d", len(got))
	}
}
