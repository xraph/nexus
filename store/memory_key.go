package store

import (
	"context"
	"sync"

	"github.com/xraph/nexus/key"
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

func (s *memoryKeyStore) FindByID(_ context.Context, id string) (*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.data[id]
	if !ok {
		return nil, key.ErrNotFound
	}
	return cloneKey(k), nil
}

func (s *memoryKeyStore) FindByPrefix(_ context.Context, prefix string) (*key.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.data {
		if k.Prefix == prefix {
			return cloneKey(k), nil
		}
	}
	return nil, key.ErrNotFound
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

func (s *memoryKeyStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
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
