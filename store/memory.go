package store

import (
	"context"
	"sync"

	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// memoryStore is an in-memory Store implementation for development and testing.
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

// memoryTenantStore is an in-memory tenant store.
type memoryTenantStore struct {
	mu   sync.RWMutex
	data map[string]*tenant.Tenant
}

func (s *memoryTenantStore) Insert(ctx context.Context, t *tenant.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[t.ID.String()] = t
	return nil
}

func (s *memoryTenantStore) FindByID(ctx context.Context, id string) (*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.data[id]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (s *memoryTenantStore) FindBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.data {
		if t.Slug == slug {
			return t, nil
		}
	}
	return nil, nil
}

func (s *memoryTenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[t.ID.String()] = t
	return nil
}

func (s *memoryTenantStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (s *memoryTenantStore) List(ctx context.Context, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*tenant.Tenant
	for _, t := range s.data {
		if opts != nil && opts.Status != "" && string(t.Status) != opts.Status {
			continue
		}
		result = append(result, t)
	}
	return result, len(result), nil
}

// memoryKeyStore is an in-memory key store.
type memoryKeyStore struct {
	mu   sync.RWMutex
	data map[string]*key.APIKey
}

func (s *memoryKeyStore) Insert(ctx context.Context, k *key.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k.ID.String()] = k
	return nil
}

func (s *memoryKeyStore) FindByID(ctx context.Context, id string) (*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.data[id]
	if !ok {
		return nil, nil
	}
	return k, nil
}

func (s *memoryKeyStore) FindByPrefix(ctx context.Context, prefix string) (*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.data {
		if k.Prefix == prefix {
			return k, nil
		}
	}
	return nil, nil
}

func (s *memoryKeyStore) Update(ctx context.Context, k *key.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k.ID.String()] = k
	return nil
}

func (s *memoryKeyStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (s *memoryKeyStore) ListByTenant(ctx context.Context, tenantID string) ([]*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*key.APIKey
	for _, k := range s.data {
		if k.TenantID.String() == tenantID {
			result = append(result, k)
		}
	}
	return result, nil
}

// memoryUsageStore is an in-memory usage store.
type memoryUsageStore struct {
	mu      sync.RWMutex
	records []*usage.Record
}

func (s *memoryUsageStore) Insert(ctx context.Context, rec *usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

func (s *memoryUsageStore) MonthlySpend(ctx context.Context, tenantID string) (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total float64
	for _, r := range s.records {
		if r.TenantID.String() == tenantID {
			total += r.CostUSD
		}
	}
	return total, nil
}

func (s *memoryUsageStore) DailyRequests(ctx context.Context, tenantID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, r := range s.records {
		if r.TenantID.String() == tenantID {
			count++
		}
	}
	return count, nil
}

func (s *memoryUsageStore) Summary(ctx context.Context, tenantID string, period string) (*usage.Summary, error) {
	return &usage.Summary{TenantID: tenantID, Period: period}, nil
}

func (s *memoryUsageStore) Query(ctx context.Context, opts *usage.QueryOptions) ([]*usage.Record, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.records, len(s.records), nil
}
