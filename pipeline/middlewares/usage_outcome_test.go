package middlewares_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

type prices map[string]provider.Pricing // "provider/model"

func (p prices) Price(_ context.Context, prov string, models ...string) (provider.Pricing, bool) {
	for _, m := range models {
		if price, ok := p[prov+"/"+m]; ok {
			return price, true
		}
	}
	return provider.Pricing{}, false
}

var gpt4o = prices{"openai/gpt-4o": {InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10")}}

type refusal struct{}

func (refusal) Error() string       { return "budget exceeded" }
func (refusal) RefusalCode() string { return "budget_exceeded" }
func (refusal) StatusCode() int     { return 429 }

func TestUsageClassifiesEveryOutcome(t *testing.T) {
	served := &provider.CompletionResponse{Model: "gpt-4o", Usage: provider.Usage{PromptTokens: 1234, CompletionTokens: 567, TotalTokens: 1801}}
	tenant, key := id.NewTenantID().String(), id.NewKeyID().String()
	cases := []struct {
		name        string
		called      bool // the provider name is in State
		cacheHit    bool
		resp        *pipeline.Response
		err         error
		model       string
		wantOutcome usage.Outcome
		wantStatus  usage.PricingStatus
		wantCost    string // "" means nil
		wantBy      string
		wantCode    string
	}{
		{"served and priced", true, false, &pipeline.Response{Completion: served}, nil, "gpt-4o", usage.OutcomeOK, usage.PricingPriced, "0.008755", "", ""},
		{"served, no price", true, false, &pipeline.Response{Completion: &provider.CompletionResponse{Model: "o9", Usage: served.Usage}}, nil, "o9", usage.OutcomeOK, usage.PricingUnpricedModel, "", "", ""},
		{"cache hit", false, true, &pipeline.Response{Completion: &provider.CompletionResponse{Cached: true, Usage: served.Usage}}, nil, "gpt-4o", usage.OutcomeCached, usage.PricingCached, "0", "", ""},
		{"input block", false, false, nil, &guard.BlockedError{Guard: "pii", Phase: guard.PhaseInput, Reason: "ssn"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingNotCharged, "0", "pii", ""},
		{"output block", true, false, nil, &guard.BlockedError{Guard: "leak", Phase: guard.PhaseOutput, Usage: &served.Usage, Model: "gpt-4o", Provider: "openai"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingPriced, "0.008755", "leak", ""},
		{"output block without usage", true, false, nil, &guard.BlockedError{Guard: "leak", Phase: guard.PhaseOutput}, "gpt-4o", usage.OutcomeBlocked, usage.PricingUnknown, "", "leak", ""},
		{"refused", false, false, nil, refusal{}, "gpt-4o", usage.OutcomeRefused, usage.PricingNotCharged, "0", "", "budget_exceeded"},
		{"failed before a provider", false, false, nil, errors.New("no providers registered"), "gpt-4o", usage.OutcomeError, usage.PricingNotCharged, "0", "", ""},
		{"provider failed", true, false, nil, errors.New("upstream 502"), "gpt-4o", usage.OutcomeError, usage.PricingUnknown, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := newRecordingUsage()
			mw := middlewares.NewUsage(rec, gpt4o, nil)
			req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: c.model, TenantID: tenant, KeyID: key}, State: map[string]any{}}
			ctx := pipeline.WithRequestID(context.Background(), id.NewRequestID().String())
			_, _ = mw.Process(ctx, req, func(context.Context) (*pipeline.Response, error) {
				if c.called {
					req.State[pipeline.StateProviderName] = "openai"
				}
				if c.cacheHit {
					req.State[pipeline.StateCacheHit] = true
				}
				return c.resp, c.err
			})
			if err := mw.Flush(context.Background()); err != nil {
				t.Fatalf("flush: %v", err)
			}
			got := rec.only(t)
			if got.Outcome != c.wantOutcome || got.PricingStatus != c.wantStatus || got.BlockedBy != c.wantBy || got.RefusalCode != c.wantCode {
				t.Fatalf("outcome %s/%s by %q code %q", got.Outcome, got.PricingStatus, got.BlockedBy, got.RefusalCode)
			}
			switch {
			case c.wantCost == "" && got.CostUSD != nil:
				t.Fatalf("cost = %s, want nil", got.CostUSD)
			case c.wantCost != "" && (got.CostUSD == nil || got.CostUSD.String() != c.wantCost):
				t.Fatalf("cost = %v, want %s", got.CostUSD, c.wantCost)
			}
			if got.TenantID.String() != tenant || got.KeyID.String() != key || got.RequestID.IsNil() {
				t.Fatalf("attribution = %s / %s / %s", got.TenantID, got.KeyID, got.RequestID)
			}
			if got.Latency <= 0 || time.Since(got.CreatedAt) > time.Minute {
				t.Fatalf("latency %s, created %s", got.Latency, got.CreatedAt)
			}
		})
	}
}

func TestUsageRecordsEmbeddings(t *testing.T) {
	rec := newRecordingUsage()
	mw := middlewares.NewUsage(rec, prices{"openai/text-embedding-3-small": {EmbeddingPerMillion: money.MustParse("0.02")}}, nil)
	req := &pipeline.Request{Type: pipeline.RequestEmbedding, Embedding: &provider.EmbeddingRequest{Model: "text-embedding-3-small"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		req.State[pipeline.StateProviderName] = "openai"
		return &pipeline.Response{Embedding: &provider.EmbeddingResponse{Model: "text-embedding-3-small", Usage: provider.Usage{PromptTokens: 5000, TotalTokens: 5000}}}, nil
	})
	_ = mw.Flush(context.Background())
	got := rec.only(t)
	if got.Outcome != usage.OutcomeOK || got.CostUSD == nil || got.CostUSD.String() != "0.0001" || got.TotalTokens != 5000 {
		t.Fatalf("embedding record = %+v", got)
	}
}

func TestUsageCountsFailedInserts(t *testing.T) {
	rec := newRecordingUsage()
	rec.fail = errors.New("database down")
	log := &errLog{}
	mw := middlewares.NewUsage(rec, gpt4o, log)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{}}, nil
	})
	_ = mw.Flush(context.Background())
	if mw.InsertErrors() != 1 || log.n != 1 {
		t.Fatalf("insert errors %d, logged %d; want 1 and 1", mw.InsertErrors(), log.n)
	}
}

type errLog struct{ n int }

func (l *errLog) Error(string, ...any) { l.n++ }
