package nexus_test

import (
	"context"
	"errors"
	"io"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/usage"
)

type fakeProvider struct {
	name     string
	price    provider.Pricing
	failures int // fail this many Complete calls first
	calls    int
	stream   []*provider.StreamChunk
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true, Streaming: true, Embeddings: true}
}
func (f *fakeProvider) Models(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "gpt-4o", Provider: f.name, Pricing: f.price}, {ID: "embed-small", Provider: f.name, Pricing: provider.Pricing{EmbeddingPerMillion: money.MustParse("0.02")}}}, nil
}
func (f *fakeProvider) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	f.calls++
	if f.calls <= f.failures {
		return nil, errors.New("upstream 502")
	}
	return &provider.CompletionResponse{Provider: f.name, Model: req.Model,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "hello"}}},
		Usage:   provider.Usage{PromptTokens: 1234, CompletionTokens: 567, TotalTokens: 1801}}, nil
}
func (f *fakeProvider) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return &sliceStream{chunks: f.stream}, nil
}
func (f *fakeProvider) Embed(_ context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return &provider.EmbeddingResponse{Provider: f.name, Model: req.Model, Usage: provider.Usage{PromptTokens: 5000, TotalTokens: 5000}}, nil
}
func (f *fakeProvider) Healthy(context.Context) bool { return true }

type sliceStream struct {
	chunks []*provider.StreamChunk
	i      int
}

func (s *sliceStream) Next(context.Context) (*provider.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		return nil, io.EOF
	}
	s.i++
	return s.chunks[s.i-1], nil
}
func (s *sliceStream) Close() error           { return nil }
func (s *sliceStream) Usage() *provider.Usage { return nil }

var listPrice = provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10")}

// gateway builds a real gateway over a memory store, runs fn, shuts it down
// (which flushes usage) and returns every stored record.
func gateway(t *testing.T, fn func(ctx context.Context, gw *nexus.Gateway), opts ...nexus.Option) []*usage.Record {
	t.Helper()
	s := store.NewMemory()
	gw := nexus.New(append([]nexus.Option{nexus.WithDatabase(s)}, opts...)...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	fn(context.Background(), gw)
	if err := gw.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	res, err := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return res.Items
}

func TestACompletionIsRecordedPricedAndAttributed(t *testing.T) {
	tenant, key := id.NewTenantID().String(), id.NewKeyID().String()
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		_, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", TenantID: tenant, KeyID: key,
			Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
	}, nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.CostUSD == nil || r.CostUSD.String() != "0.008755" || r.PricingStatus != usage.PricingPriced || r.Outcome != usage.OutcomeOK {
		t.Fatalf("record = cost %v status %s outcome %s", r.CostUSD, r.PricingStatus, r.Outcome)
	}
	if r.TenantID.String() != tenant || r.KeyID.String() != key || r.RequestID.IsNil() || r.Provider != "openai" || r.Model != "gpt-4o" || r.Latency <= 0 {
		t.Fatalf("attribution = %+v", r)
	}
}

func TestTheSameModelIsPricedAtTheProviderThatServedIt(t *testing.T) {
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		_, _ = gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	}, nexus.WithProvider(&fakeProvider{name: "openrouter", price: provider.Pricing{InputPerMillion: money.MustParse("3"), OutputPerMillion: money.MustParse("12")}}))
	// 1234 x 3 / 1e6 + 567 x 12 / 1e6
	if len(recs) != 1 || recs[0].CostUSD == nil || recs[0].CostUSD.String() != "0.010506" {
		t.Fatalf("records = %+v", recs)
	}
}

func TestACacheHitIsRecordedAndTenantsDoNotShare(t *testing.T) {
	a, b := id.NewTenantID().String(), id.NewTenantID().String()
	p := &fakeProvider{name: "openai", price: listPrice}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		for _, tenant := range []string{a, a, b} {
			_, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", TenantID: tenant, Messages: []provider.Message{{Role: "user", Content: "same"}}})
			if err != nil {
				t.Fatalf("complete: %v", err)
			}
		}
	}, nexus.WithProvider(p), nexus.WithCache(stores.NewMemory()))
	if p.calls != 2 {
		t.Fatalf("provider called %d times; tenant A's second request should hit the cache and tenant B's should not", p.calls)
	}
	var hits int
	for _, r := range recs {
		if r.Outcome == usage.OutcomeCached {
			hits++
			if r.CostUSD == nil || !r.CostUSD.IsZero() || r.TenantID.String() != a {
				t.Fatalf("cache hit record = %+v", r)
			}
		}
	}
	if len(recs) != 3 || hits != 1 {
		t.Fatalf("records %d, cache hits %d", len(recs), hits)
	}
}

type blockAll struct{ phase guard.Phase }

func (b blockAll) Name() string       { return "policy" }
func (b blockAll) Phase() guard.Phase { return b.phase }
func (b blockAll) Check(context.Context, *guard.CheckInput) (*guard.CheckResult, error) {
	return &guard.CheckResult{Blocked: true, Action: guard.ActionBlock, Reason: "not allowed"}, nil
}

func TestGuardBlocksAreRecordedWithTheGuardsName(t *testing.T) {
	for _, c := range []struct {
		phase    guard.Phase
		status   usage.PricingStatus
		wantCost string
	}{
		{guard.PhaseInput, usage.PricingNotCharged, "0"},
		{guard.PhaseOutput, usage.PricingPriced, "0.008755"},
	} {
		recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
			_, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
			var blocked *guard.BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("%s: err = %v", c.phase, err)
			}
		}, nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}), nexus.WithGuard(blockAll{c.phase}))
		if len(recs) != 1 || recs[0].Outcome != usage.OutcomeBlocked || recs[0].BlockedBy != "policy" || recs[0].PricingStatus != c.status || recs[0].CostUSD == nil || recs[0].CostUSD.String() != c.wantCost {
			t.Fatalf("%s: records = %+v", c.phase, recs)
		}
	}
}

func TestARetriedRequestIsRecordedOnce(t *testing.T) {
	p := &fakeProvider{name: "openai", price: listPrice, failures: 2}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		if _, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}}); err != nil {
			t.Fatalf("complete after retries: %v", err)
		}
	}, nexus.WithProvider(p), nexus.WithMaxRetries(2))
	if p.calls != 3 || len(recs) != 1 || recs[0].Outcome != usage.OutcomeOK {
		t.Fatalf("calls %d, records %+v", p.calls, recs)
	}
}

func TestFailuresSayWhetherAProviderWasCalled(t *testing.T) {
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		_, _ = gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o"})
	}, nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice, failures: 99}), nexus.WithMaxRetries(0))
	if len(recs) != 1 || recs[0].Outcome != usage.OutcomeError || recs[0].PricingStatus != usage.PricingUnknown || recs[0].CostUSD != nil {
		t.Fatalf("provider failure = %+v", recs)
	}
	recs = gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		_, _ = gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o"})
	})
	if len(recs) != 1 || recs[0].PricingStatus != usage.PricingNotCharged || recs[0].CostUSD == nil || !recs[0].CostUSD.IsZero() {
		t.Fatalf("no provider registered = %+v", recs)
	}
}

func TestAStreamIsRecordedWhenClosed(t *testing.T) {
	u := &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	p := &fakeProvider{name: "openai", price: listPrice, stream: []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "he"}}, {Delta: provider.Delta{Content: "llo"}}, {Kind: provider.EventUsage, Usage: u},
	}}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		st, err := gw.Engine().CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		for {
			if _, err := st.Next(ctx); err != nil {
				break
			}
		}
		_ = st.Close()
	}, nexus.WithProvider(p))
	if len(recs) != 1 || recs[0].CostUSD == nil || recs[0].CostUSD.String() != "0.00045" || recs[0].TotalTokens != 120 {
		t.Fatalf("stream record = %+v", recs)
	}
}

func TestAnEmbeddingIsRecorded(t *testing.T) {
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		if _, err := gw.Engine().Embed(ctx, &provider.EmbeddingRequest{Model: "embed-small", Input: []string{"x"}}); err != nil {
			t.Fatalf("embed: %v", err)
		}
	}, nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	if len(recs) != 1 || recs[0].CostUSD == nil || recs[0].CostUSD.String() != "0.0001" {
		t.Fatalf("embedding record = %+v", recs)
	}
}

type countingMW struct{ n *int }

func (countingMW) Name() string  { return "custom" }
func (countingMW) Priority() int { return 400 }
func (c countingMW) Process(ctx context.Context, _ *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	*c.n++
	return next(ctx)
}

func TestCustomMiddlewareAboveTheCallRunsPerAttempt(t *testing.T) {
	var n int
	gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		_, _ = gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o"})
	}, nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice, failures: 1}), nexus.WithMaxRetries(1), nexus.WithMiddleware(countingMW{&n}))
	if n != 2 {
		t.Fatalf("custom middleware at 400 ran %d times; it must run once per attempt (it was dead before)", n)
	}
}
