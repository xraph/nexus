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

func TestProvidersDoNotShareCacheKeys(t *testing.T) {
	base := provider.CompletionRequest{Model: "gpt-4o", TenantID: "tenant_a", Messages: []provider.Message{{Role: "user", Content: "hi"}}}
	keys := map[string]string{}
	streamKeys := map[string]string{}
	for _, name := range []string{"", "openai", "openrouter"} {
		req := base
		req.Provider = name
		if other, ok := keys[cache.Key(&req)]; ok {
			t.Fatalf("provider %q got the same cache key as provider %q", name, other)
		}
		if other, ok := streamKeys[cache.StreamKey(&req)]; ok {
			t.Fatalf("provider %q got the same stream cache key as provider %q", name, other)
		}
		keys[cache.Key(&req)] = name
		streamKeys[cache.StreamKey(&req)] = name

		same := req
		if cache.Key(&req) != cache.Key(&same) || cache.StreamKey(&req) != cache.StreamKey(&same) {
			t.Fatalf("identical requests naming provider %q got different keys", name)
		}
	}
}
