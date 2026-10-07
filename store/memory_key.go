package store

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/paging"
)

type memoryKeyStore struct {
	mu   sync.RWMutex
	data map[string]*key.APIKey
}

func (s *memoryKeyStore) Insert(_ context.Context, k *key.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k.ID.String()] = cloneKey(k)
	return nil
}

func (s *memoryKeyStore) FindByID(_ context.Context, keyID string) (*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.data[keyID]
	if !ok {
		return nil, key.ErrNotFound
	}
	return cloneKey(k), nil
}

func (s *memoryKeyStore) FindByPrefix(_ context.Context, prefix string) ([]*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*key.APIKey{}
	for _, k := range s.data {
		if k.Prefix == prefix {
			out = append(out, cloneKey(k))
		}
	}
	return out, nil
}

func (s *memoryKeyStore) TouchLastUsed(_ context.Context, keyID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.data[keyID]
	if !ok {
		return key.ErrNotFound
	}
	t := at
	k.LastUsedAt = &t
	return nil
}

func (s *memoryKeyStore) Update(_ context.Context, k *key.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[k.ID.String()]; !ok {
		return key.ErrNotFound
	}
	s.data[k.ID.String()] = cloneKey(k)
	return nil
}

func (s *memoryKeyStore) Delete(_ context.Context, keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, keyID)
	return nil
}

func (s *memoryKeyStore) ListByTenant(_ context.Context, tenantID string) ([]*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*key.APIKey
	for _, k := range s.data {
		if k.TenantID.String() == tenantID {
			result = append(result, cloneKey(k))
		}
	}
	return result, nil
}

func (s *memoryKeyStore) List(_ context.Context, opts *key.ListOptions) (*key.ListResult, error) {
	if opts == nil {
		opts = &key.ListOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixKey); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	s.mu.RLock()
	var rows []*key.APIKey
	for _, k := range s.data {
		switch {
		case opts.TenantID != "" && k.TenantID.String() != opts.TenantID:
		case opts.Status != "" && k.Status != opts.Status:
		case opts.Cursor != "" && k.ID.String() >= opts.Cursor:
		default:
			rows = append(rows, cloneKey(k))
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(rows, func(a, b *key.APIKey) int { return strings.Compare(b.ID.String(), a.ID.String()) })
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	page, next := paging.Trim(rows, limit, func(k *key.APIKey) string { return k.ID.String() })
	return &key.ListResult{Items: page, NextCursor: next}, nil
}
