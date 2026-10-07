package store

import (
	"context"
	"sync"

	"github.com/xraph/nexus/tenant"
)

type memoryTenantStore struct {
	mu   sync.RWMutex
	data map[string]*tenant.Tenant
}

func (s *memoryTenantStore) Insert(_ context.Context, t *tenant.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[t.ID.String()] = cloneTenant(t)
	return nil
}

func (s *memoryTenantStore) FindByID(_ context.Context, id string) (*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.data[id]
	if !ok {
		return nil, tenant.ErrNotFound
	}
	return cloneTenant(t), nil
}

func (s *memoryTenantStore) FindBySlug(_ context.Context, slug string) (*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.data {
		if t.Slug == slug {
			return cloneTenant(t), nil
		}
	}
	return nil, tenant.ErrNotFound
}

func (s *memoryTenantStore) Update(_ context.Context, t *tenant.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[t.ID.String()]; !ok {
		return tenant.ErrNotFound
	}
	s.data[t.ID.String()] = cloneTenant(t)
	return nil
}

func (s *memoryTenantStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

func (s *memoryTenantStore) List(_ context.Context, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*tenant.Tenant
	for _, t := range s.data {
		if opts != nil && opts.Status != "" && string(t.Status) != opts.Status {
			continue
		}
		result = append(result, cloneTenant(t))
	}
	return result, len(result), nil
}
