package usage

import (
	"time"

	"github.com/xraph/nexus/money"
)

// SummaryRow is one group of records a backend aggregated: requests sharing
// provider, model, outcome, pricing status and cache flag. A backend that
// cannot group (SQLite, memory) passes one row per record with Requests 1.
// Cost sums priced records only; it is zero for an unpriced group.
type SummaryRow struct {
	Provider      string
	Model         string
	Outcome       Outcome
	PricingStatus PricingStatus
	Cached        bool
	Requests      int
	Tokens        int
	LatencyNs     int64
	Cost          money.USD
}

// BuildSummary turns grouped rows into a Summary. Every backend calls it, so
// the rules for what counts as unpriced, cached or average live in one place.
func BuildSummary(tenantID, period string, rows []SummaryRow) *Summary {
	s := &Summary{
		TenantID:   tenantID,
		Period:     period,
		ByProvider: make(map[string]*ProviderUsage),
		ByModel:    make(map[string]*ModelUsage),
		ByOutcome:  make(map[Outcome]int),
	}
	var latency int64
	var cached int
	for _, r := range rows {
		s.TotalRequests += r.Requests
		s.TotalTokens += r.Tokens
		s.TotalCostUSD = s.TotalCostUSD.Add(r.Cost)
		s.ByOutcome[r.Outcome] += r.Requests
		latency += r.LatencyNs
		unpriced := 0
		if r.PricingStatus == PricingUnpricedModel {
			unpriced = r.Requests
			s.UnpricedRequests += r.Requests
		}
		if r.Cached {
			cached += r.Requests
		}

		p := s.ByProvider[r.Provider]
		if p == nil {
			p = &ProviderUsage{}
			s.ByProvider[r.Provider] = p
		}
		p.Requests += r.Requests
		p.Tokens += r.Tokens
		p.CostUSD = p.CostUSD.Add(r.Cost)
		p.Unpriced += unpriced

		m := s.ByModel[r.Model]
		if m == nil {
			m = &ModelUsage{}
			s.ByModel[r.Model] = m
		}
		m.Requests += r.Requests
		m.Tokens += r.Tokens
		m.CostUSD = m.CostUSD.Add(r.Cost)
		m.Unpriced += unpriced
	}
	if s.TotalRequests > 0 {
		s.CacheHitRate = float64(cached) / float64(s.TotalRequests)
		s.AvgLatency = time.Duration(latency / int64(s.TotalRequests))
	}
	return s
}
