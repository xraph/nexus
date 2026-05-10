package stores_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/provider"
)

func TestRedisCache_RoundTrip(t *testing.T) {
	t.Parallel()
	rc := stores.NewRedis(newFakeRedis(), stores.WithRedisTTL(time.Minute))

	resp := &provider.CompletionResponse{
		ID:       "id-1",
		Provider: "test",
		Model:    "m",
		Choices: []provider.Choice{{
			Index:   0,
			Message: provider.Message{Role: "assistant", Content: "hello"},
		}},
		Usage: provider.Usage{TotalTokens: 5},
	}

	if err := rc.Set(context.Background(), "k", resp); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := rc.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected hit, got nil")
	}
	if got.ID != "id-1" || got.Choices[0].Message.Content != "hello" {
		t.Fatalf("round-trip mangled: %+v", got)
	}
}

func TestRedisCache_MissReturnsNil(t *testing.T) {
	t.Parallel()
	rc := stores.NewRedis(newFakeRedis())
	got, err := rc.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil on miss, got %+v", got)
	}
}

func TestRedisCache_RejectsNilSet(t *testing.T) {
	t.Parallel()
	rc := stores.NewRedis(newFakeRedis())
	if err := rc.Set(context.Background(), "k", nil); err == nil {
		t.Fatal("expected error for nil response")
	}
}
