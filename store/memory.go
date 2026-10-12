package store

import (
	"maps"
	"slices"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// memoryStore is an in-memory Store for development and tests. It stores
// and returns copies, so a caller changing a row it was handed cannot change
// the store behind Update's back, which no database backend allows either.
type memoryStore struct {
	tenants *memoryTenantStore
	keys    *memoryKeyStore
	usage   *memoryUsageStore
}

// NewMemory creates an in-memory store.
func NewMemory() Store {
	return &memoryStore{
		tenants: &memoryTenantStore{data: make(map[string]*tenant.Tenant)},
		keys:    &memoryKeyStore{data: make(map[string]*key.APIKey)},
		usage:   &memoryUsageStore{},
	}
}

func (s *memoryStore) Tenants() tenant.Store { return s.tenants }
func (s *memoryStore) Keys() key.Store       { return s.keys }
func (s *memoryStore) Usage() usage.Store    { return s.usage }
func (s *memoryStore) Migrate() error        { return nil }
func (s *memoryStore) Close() error          { return nil }

func cloneTenant(t *tenant.Tenant) *tenant.Tenant {
	c := *t
	c.Metadata = maps.Clone(t.Metadata)
	c.Config.AllowedModels = slices.Clone(t.Config.AllowedModels)
	c.Config.BlockedModels = slices.Clone(t.Config.BlockedModels)
	c.Config.Metadata = maps.Clone(t.Config.Metadata)
	if t.Config.CacheEnabled != nil {
		v := *t.Config.CacheEnabled
		c.Config.CacheEnabled = &v
	}
	return &c
}

func cloneKey(k *key.APIKey) *key.APIKey {
	c := *k
	c.Scopes = slices.Clone(k.Scopes)
	c.Metadata = maps.Clone(k.Metadata)
	if k.ExpiresAt != nil {
		v := *k.ExpiresAt
		c.ExpiresAt = &v
	}
	if k.LastUsedAt != nil {
		v := *k.LastUsedAt
		c.LastUsedAt = &v
	}
	return &c
}

func cloneRecord(r *usage.Record) *usage.Record {
	c := *r
	if r.CostUSD != nil {
		v := *r.CostUSD
		c.CostUSD = &v
	}
	return &c
}
