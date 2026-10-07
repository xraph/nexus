package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
)

// Now is a timestamp every backend stores exactly: UTC, whole seconds.
// SQLite keeps tenant and key times as RFC3339 text with no fraction, and
// Mongo keeps milliseconds.
func Now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// Tenant builds a tenant with every field filled, so a backend that drops a
// field fails the round trip instead of passing it by omission.
func Tenant(slug string) *tenant.Tenant {
	now := Now()
	cache := true
	return &tenant.Tenant{
		ID:     id.NewTenantID(),
		Name:   "Tenant " + slug,
		Slug:   slug,
		Status: tenant.StatusActive,
		Quota: tenant.Quota{
			RPM:               60,
			TPM:               90_000,
			DailyRequests:     1_000,
			MaxTokensPerReq:   4_096,
			MaxStreamDuration: 90 * time.Second,
			MaxStreamTokens:   8_192,
		},
		Config: tenant.Config{
			AllowedModels:   []string{"gpt-4o", "claude-sonnet-4-5"},
			BlockedModels:   []string{"o1"},
			DefaultModel:    "gpt-4o-mini",
			RoutingStrategy: "priority",
			GuardrailPolicy: "strict",
			CacheEnabled:    &cache,
			Metadata:        map[string]string{"tier": "gold"},
		},
		Metadata:  map[string]string{"owner": "billing"},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Key builds a key with every field filled.
func Key(tenantID id.TenantID, name string) *key.APIKey {
	now := Now()
	expires, used := now.Add(30*24*time.Hour), now.Add(-time.Hour)
	return &key.APIKey{
		ID:         id.NewKeyID(),
		TenantID:   tenantID,
		Name:       name,
		Prefix:     "nxs_" + randomHex8(),
		Hash:       "hash-of-" + name,
		Scopes:     []string{"completions", "models"},
		Status:     key.KeyActive,
		ExpiresAt:  &expires,
		LastUsedAt: &used,
		Metadata:   map[string]string{"team": "search"},
		CreatedAt:  now,
	}
}

// InsertTenant stores a fresh tenant with a unique slug and returns it.
func InsertTenant(t *testing.T, s store.Store) *tenant.Tenant {
	t.Helper()
	tn := Tenant("t-" + randomHex8())
	if err := s.Tenants().Insert(context.Background(), tn); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tn
}

// SameTenant fails the test unless got and want match field by field.
func SameTenant(t *testing.T, got, want *tenant.Tenant) {
	t.Helper()
	if got == nil {
		t.Fatalf("tenant is nil")
	}
	g, w := *got, *want
	if g.ID.String() != w.ID.String() {
		t.Errorf("tenant id = %s, want %s", g.ID, w.ID)
	}
	if !g.CreatedAt.Equal(w.CreatedAt) || !g.UpdatedAt.Equal(w.UpdatedAt) {
		t.Errorf("tenant times = %s/%s, want %s/%s", g.CreatedAt, g.UpdatedAt, w.CreatedAt, w.UpdatedAt)
	}
	g.ID, w.ID = id.Nil, id.Nil
	g.CreatedAt, w.CreatedAt, g.UpdatedAt, w.UpdatedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("tenant round trip:\n got %+v\nwant %+v", g, w)
	}
}

// SameKey fails the test unless got and want match field by field.
func SameKey(t *testing.T, got, want *key.APIKey) {
	t.Helper()
	if got == nil {
		t.Fatalf("key is nil")
	}
	g, w := *got, *want
	if g.ID.String() != w.ID.String() || g.TenantID.String() != w.TenantID.String() {
		t.Errorf("key ids = %s/%s, want %s/%s", g.ID, g.TenantID, w.ID, w.TenantID)
	}
	sameTimePtr(t, "expires_at", g.ExpiresAt, w.ExpiresAt)
	sameTimePtr(t, "last_used_at", g.LastUsedAt, w.LastUsedAt)
	if !g.CreatedAt.Equal(w.CreatedAt) {
		t.Errorf("key created_at = %s, want %s", g.CreatedAt, w.CreatedAt)
	}
	g.ID, w.ID, g.TenantID, w.TenantID = id.Nil, id.Nil, id.Nil, id.Nil
	g.ExpiresAt, w.ExpiresAt, g.LastUsedAt, w.LastUsedAt = nil, nil, nil, nil
	g.CreatedAt, w.CreatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("key round trip:\n got %+v\nwant %+v", g, w)
	}
}

func sameTimePtr(t *testing.T, field string, got, want *time.Time) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", field, got, want)
	case !got.Equal(*want):
		t.Errorf("%s = %s, want %s", field, got, want)
	}
}

func randomHex8() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
