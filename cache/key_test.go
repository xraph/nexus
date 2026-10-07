package cache_test

import (
	"testing"

	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/provider"
)

func TestTenantsDoNotShareCacheKeys(t *testing.T) {
	a := &provider.CompletionRequest{Model: "gpt-4o", TenantID: "tenant_a", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	b := *a
	b.TenantID = "tenant_b"
	if cache.Key(a) == cache.Key(&b) {
		t.Fatalf("two tenants got the same cache key")
	}
	if cache.StreamKey(a) == cache.StreamKey(&b) {
		t.Fatalf("two tenants got the same stream cache key")
	}
}
