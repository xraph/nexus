package usage_test

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/usage"
)

func cost(s string) *money.USD { c := money.MustParse(s); return &c }

func TestNormaliseEnforcesTheRecordInvariant(t *testing.T) {
	cases := []struct {
		name        string
		in          usage.Record
		wantErr     bool
		wantCost    *money.USD
		wantStatus  usage.PricingStatus
		wantOutcome usage.Outcome
	}{
		{"nil cost, nothing set", usage.Record{}, false, nil, usage.PricingUnpricedModel, usage.OutcomeOK},
		{"nil cost, claims priced", usage.Record{PricingStatus: usage.PricingPriced}, false, nil, usage.PricingUnpricedModel, usage.OutcomeOK},
		{"cost, no status", usage.Record{CostUSD: cost("0.25")}, false, cost("0.25"), usage.PricingPriced, usage.OutcomeOK},
		{"cost of 0, no status", usage.Record{CostUSD: cost("0")}, false, cost("0"), usage.PricingPriced, usage.OutcomeOK},
		{"cost and priced", usage.Record{CostUSD: cost("1.5"), PricingStatus: usage.PricingPriced, Outcome: usage.OutcomeError}, false, cost("1.5"), usage.PricingPriced, usage.OutcomeError},
		{"cache hit, no cost", usage.Record{Cached: true}, false, cost("0"), usage.PricingCached, usage.OutcomeCached},
		{"cache hit with a stray cost and status", usage.Record{Cached: true, CostUSD: cost("9"), PricingStatus: usage.PricingPriced, Outcome: usage.OutcomeCached}, false, cost("0"), usage.PricingCached, usage.OutcomeCached},
		{"cache hit says unpriced", usage.Record{Cached: true, PricingStatus: usage.PricingUnpricedModel}, false, cost("0"), usage.PricingCached, usage.OutcomeCached},
		{"unpriced with a cost", usage.Record{CostUSD: cost("0.25"), PricingStatus: usage.PricingUnpricedModel}, true, nil, "", ""},
		{"unpriced with a cost of 0", usage.Record{CostUSD: cost("0"), PricingStatus: usage.PricingUnpricedModel}, true, nil, "", ""},
		{"blocked keeps its outcome", usage.Record{Outcome: usage.OutcomeBlocked}, false, nil, usage.PricingUnpricedModel, usage.OutcomeBlocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			got, err := usage.Normalise(&in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalise: %v", err)
			}
			switch {
			case got.CostUSD == nil && c.wantCost == nil:
			case got.CostUSD == nil || c.wantCost == nil || !got.CostUSD.Equal(*c.wantCost):
				t.Errorf("cost = %v, want %v", got.CostUSD, c.wantCost)
			}
			if got.PricingStatus != c.wantStatus || got.Outcome != c.wantOutcome {
				t.Errorf("status/outcome = %s/%s, want %s/%s", got.PricingStatus, got.Outcome, c.wantStatus, c.wantOutcome)
			}
			if in.CostUSD != c.in.CostUSD || in.PricingStatus != c.in.PricingStatus || in.Outcome != c.in.Outcome {
				t.Errorf("the caller's record was changed")
			}
		})
	}
	if _, err := usage.Normalise(nil); err == nil {
		t.Errorf("a nil record should be refused")
	}
}

// A writer that sets no cost and no status must show up as unpriced, never
// as a priced $0.
func TestServiceRecordCountsAnUnsetCostAsUnpriced(t *testing.T) {
	ctx := context.Background()
	svc := usage.NewService(store.NewMemory().Usage())
	if err := svc.Record(ctx, &usage.Record{ID: id.NewUsageID(), Provider: "openai", Model: "gpt-4o", TotalTokens: 10, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := svc.Record(ctx, &usage.Record{ID: id.NewUsageID(), Provider: "openai", Model: "gpt-4o", CostUSD: cost("0.5"), CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("record: %v", err)
	}
	sum, err := svc.Summary(ctx, "", "month")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.TotalRequests != 2 || sum.UnpricedRequests != 1 || sum.TotalCostUSD.String() != "0.5" {
		t.Fatalf("summary = %d requests, %d unpriced, cost %s; want 2, 1, 0.5", sum.TotalRequests, sum.UnpricedRequests, sum.TotalCostUSD)
	}
	err = svc.Record(ctx, &usage.Record{ID: id.NewUsageID(), CostUSD: cost("1"), PricingStatus: usage.PricingUnpricedModel})
	if err == nil {
		t.Fatalf("a record that is unpriced and costs $1 should be refused")
	}
	sum, _ = svc.Summary(ctx, "", "month")
	if sum.TotalRequests != 2 {
		t.Fatalf("the refused record was stored: %d requests", sum.TotalRequests)
	}
}
