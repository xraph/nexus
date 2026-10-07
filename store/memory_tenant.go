package store

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/paging"
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

func (s *memoryTenantStore) FindByID(_ context.Context, tenantID string) (*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.data[tenantID]
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

func (s *memoryTenantStore) Delete(_ context.Context, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, tenantID)
	return nil
}

func (s *memoryTenantStore) List(_ context.Context, opts *tenant.ListOptions) (*tenant.ListResult, error) {
	if opts == nil {
		opts = &tenant.ListOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixTenant); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	term := strings.ToLower(opts.Search)
	s.mu.RLock()
	var rows []*tenant.Tenant
	for _, t := range s.data {
		switch {
		case opts.Status != "" && string(t.Status) != opts.Status:
		case term != "" && !strings.Contains(strings.ToLower(t.Name), term) && !strings.Contains(strings.ToLower(t.Slug), term):
		case opts.Cursor != "" && t.ID.String() >= opts.Cursor:
		default:
			rows = append(rows, cloneTenant(t))
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(rows, func(a, b *tenant.Tenant) int { return strings.Compare(b.ID.String(), a.ID.String()) })
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	page, next := paging.Trim(rows, limit, func(t *tenant.Tenant) string { return t.ID.String() })
	return &tenant.ListResult{Items: page, NextCursor: next}, nil
}
