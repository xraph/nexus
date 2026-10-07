package store

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/nexus/money"
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
	s.records = append(s.records, cloneRecord(rec))
	return nil
}

func (s *memoryUsageStore) inWindow(tenantID string, since time.Time) []*usage.Record {
	var out []*usage.Record
	for _, r := range s.records {
		if tenantID != "" && r.TenantID.String() != tenantID {
			continue
		}
		if r.CreatedAt.Before(since) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func (s *memoryUsageStore) MonthlySpend(_ context.Context, tenantID string) (money.USD, error) {
	since, err := usage.PeriodStart("month", time.Now())
	if err != nil {
		return money.Zero, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := money.Zero
	for _, r := range s.inWindow(tenantID, since) {
		if r.CostUSD != nil {
			total = total.Add(*r.CostUSD)
		}
	}
	return total, nil
}

func (s *memoryUsageStore) DailyRequests(_ context.Context, tenantID string) (int, error) {
	since, err := usage.PeriodStart("day", time.Now())
	if err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.inWindow(tenantID, since)), nil
}

func (s *memoryUsageStore) Summary(_ context.Context, tenantID, period string) (*usage.Summary, error) {
	since, err := usage.PeriodStart(period, time.Now())
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := s.inWindow(tenantID, since)
	rows := make([]usage.SummaryRow, 0, len(records))
	for _, r := range records {
		rows = append(rows, rowOf(r))
	}
	return usage.BuildSummary(tenantID, period, rows), nil
}

// rowOf is one record as a summary row, for backends that cannot group.
func rowOf(r *usage.Record) usage.SummaryRow {
	row := usage.SummaryRow{
		Provider: r.Provider, Model: r.Model, Outcome: r.Outcome, PricingStatus: r.PricingStatus,
		Cached: r.Cached, Requests: 1, Tokens: r.TotalTokens, LatencyNs: r.Latency.Nanoseconds(),
	}
	if r.CostUSD != nil {
		row.Cost = *r.CostUSD
	}
	return row
}

func (s *memoryUsageStore) Query(_ context.Context, _ *usage.QueryOptions) ([]*usage.Record, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*usage.Record, len(s.records))
	for i, r := range s.records {
		out[i] = cloneRecord(r)
	}
	return out, len(out), nil
}
