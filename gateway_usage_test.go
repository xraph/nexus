package nexus_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router/strategies"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

type fakeProvider struct {
	name        string
	price       provider.Pricing
	failures    int // fail this many Complete calls first
	calls       int
	streamCalls int
	stream      []*provider.StreamChunk
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
	f.streamCalls++
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
	return gatewayOn(t, store.NewMemory(), fn, opts...)
}

// gatewayOn is gateway over a store the test holds, so it can look at the
// store before shutdown.
func gatewayOn(t *testing.T, s store.Store, fn func(ctx context.Context, gw *nexus.Gateway), opts ...nexus.Option) []*usage.Record {
	t.Helper()
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

// tenantAndKey creates a real tenant and a key for it, because the access
// stage refuses a tenant or key the store does not know.
func tenantAndKey(t *testing.T, gw *nexus.Gateway) (tenantID, keyID string) {
	t.Helper()
	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: "Acme", Slug: "acme-" + strings.ToLower(id.NewTenantID().String())})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	k, _, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "k"})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	return tn.ID.String(), k.ID.String()
}

func TestACompletionIsRecordedPricedAndAttributed(t *testing.T) {
	var tenant, key string
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		tenant, key = tenantAndKey(t, gw)
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
	if r.TenantID.String() != tenant || r.KeyID.String() != key || r.RequestID.IsNil() || r.Provider != "openai" || r.Model != "gpt-4o" || r.Latency < 0 {
		t.Fatalf("attribution = %+v", r)
	}
}

func TestTheSameModelIsPricedAtTheProviderThatServedIt(t *testing.T) {
	// Both providers list gpt-4o, at different prices. The first registered
	// is not the one that serves: the priority router picks the second. A
	// price lookup that ignored the serving provider would find openai first.
	first := &fakeProvider{name: "openai", price: listPrice}
	second := &fakeProvider{name: "openrouter", price: provider.Pricing{InputPerMillion: money.MustParse("3"), OutputPerMillion: money.MustParse("12")}}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		if _, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}}); err != nil {
			t.Fatalf("complete: %v", err)
		}
	}, nexus.WithProvider(first), nexus.WithProvider(second), nexus.WithRouter(strategies.NewPriority("openrouter")))
	if first.calls != 0 || second.calls != 1 {
		t.Fatalf("calls: openai %d, openrouter %d; the router should have picked openrouter", first.calls, second.calls)
	}
	// 1234 x 3 / 1e6 + 567 x 12 / 1e6
	if len(recs) != 1 || recs[0].Provider != "openrouter" || recs[0].CostUSD == nil || recs[0].CostUSD.String() != "0.010506" {
		t.Fatalf("records = %+v", recs)
	}
}

func TestACacheHitIsRecordedAndTenantsDoNotShare(t *testing.T) {
	var a, b string
	p := &fakeProvider{name: "openai", price: listPrice}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		a, _ = tenantAndKey(t, gw)
		b, _ = tenantAndKey(t, gw)
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

func TestABlockedCacheHitIsNotCharged(t *testing.T) {
	p := &fakeProvider{name: "openai", price: listPrice}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		for range 2 {
			_, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "same"}}})
			var blocked *guard.BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("err = %v, want a block", err)
			}
		}
	}, nexus.WithProvider(p), nexus.WithCache(stores.NewMemory()), nexus.WithGuard(blockAll{guard.PhaseOutput}))
	if p.calls != 1 {
		t.Fatalf("provider called %d times; the second request should be a cache hit", p.calls)
	}
	var priced, replayed int
	for _, r := range recs {
		if r.Outcome != usage.OutcomeBlocked || r.BlockedBy != "policy" {
			t.Fatalf("record = %+v", r)
		}
		switch {
		case r.PricingStatus == usage.PricingPriced && r.CostUSD != nil && r.CostUSD.String() == "0.008755":
			priced++
		case r.PricingStatus == usage.PricingCached && r.Cached && r.CostUSD != nil && r.CostUSD.IsZero():
			replayed++
		default:
			t.Fatalf("record = %s cost %v cached %v", r.PricingStatus, r.CostUSD, r.Cached)
		}
	}
	if len(recs) != 2 || priced != 1 || replayed != 1 {
		t.Fatalf("records %d: priced %d, cached %d; want 2: 1 and 1", len(recs), priced, replayed)
	}
}

// slowProvider answers after a delay and counts its calls safely, so many
// requests can be in flight at once.
type slowProvider struct {
	*fakeProvider
	delay time.Duration
	n     atomic.Int64
}

func (s *slowProvider) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	s.n.Add(1)
	time.Sleep(s.delay)
	return &provider.CompletionResponse{Provider: s.name, Model: req.Model,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "my card is 4111"}}},
		Usage:   provider.Usage{PromptTokens: 1234, CompletionTokens: 567, TotalTokens: 1801}}, nil
}

// slowRedactor is an output guard that takes its time and rewrites every
// message, the way a PII redactor does. While it runs, other requests for
// the same prompt can hit the cache.
type slowRedactor struct{ delay time.Duration }

func (slowRedactor) Name() string       { return "redact" }
func (slowRedactor) Phase() guard.Phase { return guard.PhaseOutput }
func (g slowRedactor) Check(_ context.Context, in *guard.CheckInput) (*guard.CheckResult, error) {
	time.Sleep(g.delay)
	out := make([]provider.Message, len(in.Messages))
	for i, m := range in.Messages {
		out[i] = provider.Message{Role: m.Role, Content: "my card is [redacted]"}
	}
	return &guard.CheckResult{Passed: true, Action: guard.ActionRedact, Modified: true, Messages: out}, nil
}

func TestConcurrentRequestsAreChargedOncePerProviderCall(t *testing.T) {
	// The provider answers in 5ms and the output guard takes 40ms, so while
	// the first request is still in the guard, the ones that start after it
	// hit the cache. A hit must never mark or rewrite the response the first
	// request is still carrying, and must never read as a charge.
	const requests = 20
	p := &slowProvider{fakeProvider: &fakeProvider{name: "openai", price: listPrice}, delay: 5 * time.Millisecond}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		tenant, _ := tenantAndKey(t, gw)
		var wg sync.WaitGroup
		for i := range requests {
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(time.Duration(i) * 2 * time.Millisecond)
				resp, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", TenantID: tenant, Messages: []provider.Message{{Role: "user", Content: "same"}}})
				if err != nil {
					t.Errorf("complete: %v", err)
					return
				}
				if got := resp.Choices[0].Message.Content; got != "my card is [redacted]" {
					t.Errorf("content = %v, want it redacted", got)
				}
			}()
		}
		wg.Wait()
	}, nexus.WithProvider(p), nexus.WithCache(stores.NewMemory()), nexus.WithGuard(slowRedactor{40 * time.Millisecond}))

	calls := int(p.n.Load())
	var priced, hits int
	for _, r := range recs {
		switch {
		case r.PricingStatus == usage.PricingPriced && r.Outcome == usage.OutcomeOK && r.CostUSD != nil && r.CostUSD.String() == "0.008755":
			priced++
		case r.PricingStatus == usage.PricingCached && r.Outcome == usage.OutcomeCached && r.CostUSD != nil && r.CostUSD.IsZero():
			hits++
		default:
			t.Fatalf("record = %s/%s cost %v", r.Outcome, r.PricingStatus, r.CostUSD)
		}
	}
	if len(recs) != requests || priced != calls || hits != requests-calls {
		t.Fatalf("%d records, %d priced, %d cached; the provider was called %d times", len(recs), priced, hits, calls)
	}
	if hits == 0 {
		t.Fatalf("no request hit the cache, so the test proved nothing")
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
	s := store.NewMemory()
	recs := gatewayOn(t, s, func(ctx context.Context, gw *nexus.Gateway) {
		st, err := gw.Engine().CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		for {
			if _, err := st.Next(ctx); err != nil {
				break
			}
		}
		// The stream has been read to its end but not closed: nothing may be
		// recorded yet, because the record is written on Close.
		before, qerr := s.Usage().Query(ctx, &usage.QueryOptions{Limit: 100})
		if qerr != nil {
			t.Fatalf("query: %v", qerr)
		}
		if len(before.Items) != 0 {
			t.Fatalf("%d records before Close, want 0", len(before.Items))
		}
		_ = st.Close()
	}, nexus.WithProvider(p))
	if len(recs) != 1 || recs[0].CostUSD == nil || recs[0].CostUSD.String() != "0.00045" || recs[0].TotalTokens != 120 {
		t.Fatalf("stream record = %+v", recs)
	}
	if r := recs[0]; r.Outcome != usage.OutcomeOK || r.PricingStatus != usage.PricingPriced || r.Provider != "openai" {
		t.Fatalf("stream record = outcome %s status %s provider %s", r.Outcome, r.PricingStatus, r.Provider)
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
	if r := recs[0]; r.Outcome != usage.OutcomeOK || r.PricingStatus != usage.PricingPriced || r.Provider != "openai" {
		t.Fatalf("embedding record = outcome %s status %s provider %s", r.Outcome, r.PricingStatus, r.Provider)
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

func TestShutdownWaitsForAStreamThatIsStillOpen(t *testing.T) {
	u := &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	p := &fakeProvider{name: "openai", price: listPrice, stream: []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "hi"}}, {Kind: provider.EventUsage, Usage: u},
	}}
	// The stream is still open when Shutdown starts. Another goroutine reads
	// and closes it a little later; its record must land before Shutdown
	// returns.
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		st, err := gw.Engine().CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			for {
				if _, err := st.Next(ctx); err != nil {
					break
				}
			}
			_ = st.Close()
		}()
	}, nexus.WithProvider(p))
	if len(recs) != 1 || recs[0].Outcome != usage.OutcomeOK || recs[0].CostUSD == nil || recs[0].CostUSD.String() != "0.00045" {
		t.Fatalf("records when Shutdown returned = %+v", recs)
	}
}

func TestShutdownWithAnExpiredContextAndNothingPendingIsClean(t *testing.T) {
	// Flush used to select between "done" and ctx.Done() even with nothing
	// pending, so an expired context failed about half the time.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for i := range 200 {
		gw := nexus.New(nexus.WithDatabase(store.NewMemory()))
		if err := gw.Initialize(context.Background()); err != nil {
			t.Fatalf("initialize: %v", err)
		}
		if err := gw.Shutdown(ctx); err != nil {
			t.Fatalf("run %d: shutdown with nothing pending = %v", i, err)
		}
	}
}

func TestARecordAfterShutdownIsCountedAndNotStored(t *testing.T) {
	s := store.NewMemory()
	gw := nexus.New(nexus.WithDatabase(s), nexus.WithProvider(&fakeProvider{name: "openai", price: listPrice}))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := gw.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{Model: "gpt-4o"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	res, err := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 100})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Items) != 0 || gw.UsageInsertErrors() != 1 {
		t.Fatalf("%d records stored, %d counted as lost; want 0 and 1", len(res.Items), gw.UsageInsertErrors())
	}
}

func TestShutdownReturnsTheFlushErrorWhenAnInsertIsStuck(t *testing.T) {
	g := newGatedUsage()
	gw := nexus.New(nexus.WithUsageService(g))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	oneRequest(t, gw, g)
	defer close(g.release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := gw.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown = %v, want the flush's deadline error", err)
	}
}

func TestAMismatchedIdentityIsRefusedAndChargedToNeitherTenant(t *testing.T) {
	p := &fakeProvider{name: "openai", price: listPrice}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		a, _ := tenantAndKey(t, gw)
		b, _ := tenantAndKey(t, gw)
		ctx = pipeline.WithTenantID(ctx, a)
		_, err := gw.Engine().Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", TenantID: b, Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		var refused pipeline.Refusal
		if !errors.Is(err, middlewares.ErrInvalidIdentity) || !errors.As(err, &refused) || refused.StatusCode() != 400 {
			t.Fatalf("err = %v, want an invalid_request refusal", err)
		}
	}, nexus.WithProvider(p))
	if p.calls != 0 || len(recs) != 1 {
		t.Fatalf("provider calls %d, records %d; want 0 and 1", p.calls, len(recs))
	}
	r := recs[0]
	if r.Outcome != usage.OutcomeRefused || r.RefusalCode != "invalid_request" || r.StatusCode != 400 {
		t.Fatalf("record = %s code %q status %d; want refused, invalid_request, 400", r.Outcome, r.RefusalCode, r.StatusCode)
	}
	if !r.TenantID.IsNil() || !r.KeyID.IsNil() {
		t.Fatalf("record attributed to %s / %s; a mismatched identity is charged to neither", r.TenantID, r.KeyID)
	}
	if r.PricingStatus != usage.PricingNotCharged || r.CostUSD == nil || !r.CostUSD.IsZero() {
		t.Fatalf("record = %s cost %v; want not_charged at $0", r.PricingStatus, r.CostUSD)
	}
}

func TestAStreamCacheReplayIsRecordedAsCached(t *testing.T) {
	u := &provider.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	p := &fakeProvider{name: "openai", price: listPrice, stream: []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "he"}}, {Delta: provider.Delta{Content: "llo"}}, {Kind: provider.EventUsage, Usage: u},
	}}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		for range 2 {
			st, err := gw.Engine().CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true, Messages: []provider.Message{{Role: "user", Content: "same"}}})
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			for {
				if _, err := st.Next(ctx); err != nil {
					break
				}
			}
			_ = st.Close()
		}
	}, nexus.WithProvider(p), nexus.WithStreamCache(stores.NewMemoryStream(), cache.StreamCacheOptions{}))
	if p.streamCalls != 1 {
		t.Fatalf("provider streamed %d times; the second request should replay from the cache", p.streamCalls)
	}
	var priced, replayed int
	for _, r := range recs {
		switch {
		case r.Outcome == usage.OutcomeOK && r.PricingStatus == usage.PricingPriced && r.CostUSD != nil && r.CostUSD.String() == "0.00045":
			priced++
		case r.Outcome == usage.OutcomeCached && r.PricingStatus == usage.PricingCached && r.Cached && r.CostUSD != nil && r.CostUSD.IsZero() && r.TotalTokens == 120:
			replayed++
		default:
			t.Fatalf("record = %s/%s cost %v tokens %d", r.Outcome, r.PricingStatus, r.CostUSD, r.TotalTokens)
		}
	}
	if len(recs) != 2 || priced != 1 || replayed != 1 {
		t.Fatalf("records %d: priced %d, replayed %d; want 2: 1 and 1", len(recs), priced, replayed)
	}
}

func TestATenantNamedOnlyInTheContextIsRecordedAndCachedApart(t *testing.T) {
	var a, b string
	p := &fakeProvider{name: "openai", price: listPrice}
	recs := gateway(t, func(ctx context.Context, gw *nexus.Gateway) {
		a, _ = tenantAndKey(t, gw)
		b, _ = tenantAndKey(t, gw)
		for _, tenant := range []string{a, a, b} {
			tctx := pipeline.WithTenantID(ctx, tenant)
			if _, err := gw.Engine().Complete(tctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "same"}}}); err != nil {
				t.Fatalf("complete: %v", err)
			}
		}
	}, nexus.WithProvider(p), nexus.WithCache(stores.NewMemory()))
	if p.calls != 2 {
		t.Fatalf("provider called %d times; tenant A's repeat should hit and tenant B's should not", p.calls)
	}
	byTenant := map[string]int{}
	var hits int
	for _, r := range recs {
		byTenant[r.TenantID.String()]++
		if r.Outcome == usage.OutcomeCached {
			hits++
			if r.TenantID.String() != a {
				t.Fatalf("cache hit recorded under %s, want tenant A", r.TenantID)
			}
		}
	}
	if len(recs) != 3 || byTenant[a] != 2 || byTenant[b] != 1 || hits != 1 {
		t.Fatalf("records %d by tenant %v, hits %d; want A twice, B once, one hit", len(recs), byTenant, hits)
	}
}

// gateProvider blocks every Complete until release is closed, and signals on
// entered when a call arrives.
type gateProvider struct {
	fakeProvider
	entered chan struct{}
	release chan struct{}
}

func (g *gateProvider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	g.entered <- struct{}{}
	<-g.release
	return g.fakeProvider.Complete(ctx, req)
}

func TestShutdownWaitsForACompletionStillWithTheProvider(t *testing.T) {
	s := store.NewMemory()
	p := &gateProvider{fakeProvider: fakeProvider{name: "openai", price: listPrice}, entered: make(chan struct{}, 1), release: make(chan struct{})}
	gw := nexus.New(nexus.WithDatabase(s), nexus.WithProvider(p))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := gw.Engine().Complete(context.Background(), &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		done <- err
	}()
	<-p.entered
	shut := make(chan error, 1)
	go func() { shut <- gw.Shutdown(context.Background()) }()
	// Shutdown must still be waiting: the completion is with the provider.
	select {
	case err := <-shut:
		t.Fatalf("Shutdown returned %v while a completion was in flight", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := <-shut; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	res, err := s.Usage().Query(context.Background(), &usage.QueryOptions{Limit: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Outcome != usage.OutcomeOK {
		t.Fatalf("records = %+v; the in-flight completion's record must be stored", res.Items)
	}
	if gw.UsageInsertErrors() != 0 {
		t.Fatalf("insert errors = %d, want 0", gw.UsageInsertErrors())
	}
}
