package store

import (
	"context"
	"sync"

	"github.com/xraph/nexus/usage"
)

// memoryUsageStore is an in-memory usage store.
type memoryUsageStore struct {
	mu      sync.RWMutex
	records []*usage.Record
}

func (s *memoryUsageStore) Insert(_ context.Context, rec *usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
	return nil
}

func (s *memoryUsageStore) MonthlySpend(_ context.Context, tenantID string) (float64, error) {
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

func (s *memoryUsageStore) DailyRequests(_ context.Context, tenantID string) (int, error) {
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

func (s *memoryUsageStore) Summary(_ context.Context, tenantID, period string) (*usage.Summary, error) {
	return &usage.Summary{TenantID: tenantID, Period: period}, nil
}

func (s *memoryUsageStore) Query(_ context.Context, _ *usage.QueryOptions) ([]*usage.Record, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.records, len(s.records), nil
}
