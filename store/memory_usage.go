package store

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/paging"
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

func (s *memoryUsageStore) Series(_ context.Context, opts *usage.SeriesOptions) ([]usage.SeriesPoint, error) {
	if _, err := usage.FillSeries(opts, nil); err != nil {
		return nil, err
	}
	from := usage.BucketStart(opts.Start, opts.Bucket)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var points []usage.SeriesPoint
	for _, r := range s.records {
		if opts.TenantID != "" && r.TenantID.String() != opts.TenantID {
			continue
		}
		if r.CreatedAt.Before(from) || !r.CreatedAt.Before(opts.End) {
			continue
		}
		points = append(points, pointOf(r))
	}
	return usage.FillSeries(opts, points)
}

func pointOf(r *usage.Record) usage.SeriesPoint {
	p := usage.SeriesPoint{Start: r.CreatedAt, Requests: 1, Tokens: r.TotalTokens}
	if r.CostUSD != nil {
		p.CostUSD = *r.CostUSD
	}
	if r.PricingStatus == usage.PricingUnpricedModel {
		p.Unpriced = 1
	}
	return p
}

func (s *memoryUsageStore) Query(_ context.Context, opts *usage.QueryOptions) (*usage.QueryResult, error) {
	if opts == nil {
		opts = &usage.QueryOptions{}
	}
	if err := paging.CheckCursor(opts.Cursor, id.PrefixUsage); err != nil {
		return nil, err
	}
	limit := paging.Limit(opts.Limit)
	s.mu.RLock()
	var rows []*usage.Record
	for _, r := range s.records {
		switch {
		case opts.TenantID != "" && r.TenantID.String() != opts.TenantID:
		case opts.KeyID != "" && r.KeyID.String() != opts.KeyID:
		case opts.Provider != "" && r.Provider != opts.Provider:
		case opts.Model != "" && r.Model != opts.Model:
		case opts.Outcome != "" && r.Outcome != opts.Outcome:
		case !opts.StartTime.IsZero() && r.CreatedAt.Before(opts.StartTime):
		case !opts.EndTime.IsZero() && !r.CreatedAt.Before(opts.EndTime):
		case opts.Cursor != "" && r.ID.String() >= opts.Cursor:
		default:
			rows = append(rows, cloneRecord(r))
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(rows, func(a, b *usage.Record) int { return strings.Compare(b.ID.String(), a.ID.String()) })
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	page, next := paging.Trim(rows, limit, func(r *usage.Record) string { return r.ID.String() })
	return &usage.QueryResult{Items: page, NextCursor: next}, nil
}
