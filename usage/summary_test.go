package usage_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/usage"
)

func TestPeriodStart(t *testing.T) {
	now := time.Date(2026, 10, 7, 15, 4, 5, 0, time.FixedZone("CDT", -5*3600)) // 20:04:05 UTC
	cases := map[string]time.Time{
		"day":   time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC),
		"week":  time.Date(2026, 9, 30, 20, 4, 5, 0, time.UTC),
		"month": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	for period, want := range cases {
		got, err := usage.PeriodStart(period, now)
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("PeriodStart(%s) = %s, %v; want %s UTC", period, got, err, want)
		}
	}
	if _, err := usage.PeriodStart("year", now); !errors.Is(err, usage.ErrInvalidPeriod) {
		t.Fatalf("an unknown period should be ErrInvalidPeriod, got %v", err)
	}
}

func TestBuildSummary(t *testing.T) {
	rows := []usage.SummaryRow{
		{Provider: "openai", Model: "gpt-4o", Outcome: usage.OutcomeOK, PricingStatus: usage.PricingPriced, Requests: 3, Tokens: 300, LatencyNs: int64(3 * time.Second), Cost: money.MustParse("0.00000045")},
		{Provider: "openai", Model: "gpt-4o", Outcome: usage.OutcomeCached, PricingStatus: usage.PricingCached, Cached: true, Requests: 1, Tokens: 100, LatencyNs: int64(time.Second)},
		{Provider: "local", Model: "llama", Outcome: usage.OutcomeOK, PricingStatus: usage.PricingUnpricedModel, Requests: 1, Tokens: 50, LatencyNs: int64(time.Second)},
	}
	s := usage.BuildSummary("", "month", rows)
	if s.TotalRequests != 5 || s.TotalTokens != 450 || s.UnpricedRequests != 1 {
		t.Fatalf("totals = %+v", s)
	}
	if s.TotalCostUSD.String() != "0.00000045" {
		t.Fatalf("total cost = %s", s.TotalCostUSD)
	}
	if s.CacheHitRate != 0.2 || s.AvgLatency != time.Second {
		t.Fatalf("cache hit rate %v, avg latency %s", s.CacheHitRate, s.AvgLatency)
	}
	if s.ByOutcome[usage.OutcomeOK] != 4 || s.ByOutcome[usage.OutcomeCached] != 1 {
		t.Fatalf("by outcome = %v", s.ByOutcome)
	}
	if m := s.ByModel["llama"]; m == nil || m.Unpriced != 1 || !m.CostUSD.IsZero() {
		t.Fatalf("llama = %+v", m)
	}
	if p := s.ByProvider["openai"]; p == nil || p.Requests != 4 || p.CostUSD.String() != "0.00000045" {
		t.Fatalf("openai = %+v", p)
	}
	if empty := usage.BuildSummary("t", "day", nil); empty.CacheHitRate != 0 || empty.AvgLatency != 0 || empty.ByModel == nil {
		t.Fatalf("an empty summary should have zero rates and non-nil maps, got %+v", empty)
	}
}
