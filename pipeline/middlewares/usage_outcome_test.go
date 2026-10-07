package middlewares_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

func TestUsageMiddleware_SettlesOutcomeAndPricing(t *testing.T) {
	t.Parallel()

	cost := money.MustParse("0.000024975")
	tests := []struct {
		name        string
		resp        *pipeline.Response
		err         error
		wantOutcome usage.Outcome
		wantStatus  usage.PricingStatus
		wantCost    string // "" means nil
	}{
		{"error", nil, errors.New("upstream"), usage.OutcomeError, usage.PricingUnpricedModel, ""},
		{"priced", &pipeline.Response{Completion: &provider.CompletionResponse{Cost: &cost}}, nil, usage.OutcomeOK, usage.PricingPriced, "0.000024975"},
		{"unpriced", &pipeline.Response{Completion: &provider.CompletionResponse{}}, nil, usage.OutcomeOK, usage.PricingUnpricedModel, ""},
		{"cached", &pipeline.Response{Completion: &provider.CompletionResponse{Cached: true}}, nil, usage.OutcomeCached, usage.PricingCached, "0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := newRecordingUsage()
			mw := middlewares.NewUsage(rec)
			req := &pipeline.Request{Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
			_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
				return tc.resp, tc.err
			})
			<-rec.done
			rec.mu.Lock()
			defer rec.mu.Unlock()
			got := rec.records[0]
			if got.Outcome != tc.wantOutcome || got.PricingStatus != tc.wantStatus {
				t.Fatalf("outcome %s, status %s; want %s, %s", got.Outcome, got.PricingStatus, tc.wantOutcome, tc.wantStatus)
			}
			switch {
			case tc.wantCost == "" && got.CostUSD != nil:
				t.Fatalf("cost = %s, want nil", got.CostUSD)
			case tc.wantCost != "" && (got.CostUSD == nil || got.CostUSD.String() != tc.wantCost):
				t.Fatalf("cost = %v, want %s", got.CostUSD, tc.wantCost)
			}
		})
	}
}
