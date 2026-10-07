package middlewares_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
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
		{"a miss whose response a cache marked cached", true, false, &pipeline.Response{Completion: &provider.CompletionResponse{Cached: true, Model: "gpt-4o", Usage: served.Usage}}, nil, "gpt-4o", usage.OutcomeOK, usage.PricingPriced, "0.008755", "", ""},
		{"output block of a cache hit", false, true, nil, &guard.BlockedError{Guard: "leak", Phase: guard.PhaseOutput, Usage: &served.Usage, Model: "gpt-4o", Provider: "openai"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingCached, "0", "leak", ""},
		{"input block", false, false, nil, &guard.BlockedError{Guard: "pii", Phase: guard.PhaseInput, Reason: "ssn"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingNotCharged, "0", "pii", ""},
		{"output block", true, false, nil, &guard.BlockedError{Guard: "leak", Phase: guard.PhaseOutput, Usage: &served.Usage, Model: "gpt-4o", Provider: "openai"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingPriced, "0.008755", "leak", ""},
		{"output block without usage", true, false, nil, &guard.BlockedError{Guard: "leak", Phase: guard.PhaseOutput}, "gpt-4o", usage.OutcomeBlocked, usage.PricingUnknown, "", "leak", ""},
		{"output block, response names another provider", true, false, nil, &guard.BlockedError{Guard: "leak", Phase: guard.PhaseOutput, Usage: &served.Usage, Model: "gpt-4o", Provider: "openai-compatible"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingPriced, "0.008755", "leak", ""},
		{"block with no phase", true, false, nil, &guard.BlockedError{Guard: "odd"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingUnknown, "", "odd", ""},
		{"block with no phase, with usage", true, false, nil, &guard.BlockedError{Guard: "odd", Usage: &served.Usage, Model: "gpt-4o", Provider: "openai"}, "gpt-4o", usage.OutcomeBlocked, usage.PricingPriced, "0.008755", "odd", ""},
		{"served, only a total reported", true, false, &pipeline.Response{Completion: &provider.CompletionResponse{Model: "gpt-4o", Usage: provider.Usage{TotalTokens: 1801}}}, nil, "gpt-4o", usage.OutcomeOK, usage.PricingUnknown, "", "", ""},
		{"served, no tokens reported", true, false, &pipeline.Response{Completion: &provider.CompletionResponse{Model: "gpt-4o"}}, nil, "gpt-4o", usage.OutcomeOK, usage.PricingUnknown, "", "", ""},
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

func TestUsageZeroTokensOnAFreeModelCostExactlyZero(t *testing.T) {
	rec := newRecordingUsage()
	free := prices{"local/llama": {Free: true}}
	mw := middlewares.NewUsage(rec, free, nil)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "llama"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		req.State[pipeline.StateProviderName] = "local"
		return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "llama"}}, nil
	})
	_ = mw.Flush(context.Background())
	got := rec.only(t)
	if got.Outcome != usage.OutcomeOK || got.PricingStatus != usage.PricingPriced || got.CostUSD == nil || !got.CostUSD.IsZero() {
		t.Fatalf("free model, no tokens = %s/%s cost %v; want ok/priced/0", got.Outcome, got.PricingStatus, got.CostUSD)
	}
}

func TestUsageRecordsAnEmbeddingThatReportsOnlyATotal(t *testing.T) {
	rec := newRecordingUsage()
	mw := middlewares.NewUsage(rec, prices{"voyageai/voyage-3": {EmbeddingPerMillion: money.MustParse("0.06")}}, nil)
	req := &pipeline.Request{Type: pipeline.RequestEmbedding, Embedding: &provider.EmbeddingRequest{Model: "voyage-3"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		req.State[pipeline.StateProviderName] = "voyageai"
		return &pipeline.Response{Embedding: &provider.EmbeddingResponse{Model: "voyage-3", Usage: provider.Usage{TotalTokens: 5000}}}, nil
	})
	_ = mw.Flush(context.Background())
	got := rec.only(t)
	// 5000 x 0.06 / 1e6
	if got.Outcome != usage.OutcomeOK || got.PricingStatus != usage.PricingPriced || got.CostUSD == nil || got.CostUSD.String() != "0.0003" {
		t.Fatalf("total-only embedding = %s/%s cost %v; want ok/priced/0.0003", got.Outcome, got.PricingStatus, got.CostUSD)
	}
}

func TestUsageZeroTokenEmbeddingIsUnknown(t *testing.T) {
	rec := newRecordingUsage()
	mw := middlewares.NewUsage(rec, prices{"openai/text-embedding-3-small": {EmbeddingPerMillion: money.MustParse("0.02")}}, nil)
	req := &pipeline.Request{Type: pipeline.RequestEmbedding, Embedding: &provider.EmbeddingRequest{Model: "text-embedding-3-small"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		req.State[pipeline.StateProviderName] = "openai"
		return &pipeline.Response{Embedding: &provider.EmbeddingResponse{Model: "text-embedding-3-small"}}, nil
	})
	_ = mw.Flush(context.Background())
	got := rec.only(t)
	if got.Outcome != usage.OutcomeOK || got.PricingStatus != usage.PricingUnknown || got.CostUSD != nil {
		t.Fatalf("embedding, no tokens = %s/%s cost %v; want ok/unknown/nil", got.Outcome, got.PricingStatus, got.CostUSD)
	}
}

// slowUsage stores records slowly and counts the ones that reached it, so a
// test can tell whether Flush returned before they all had.
type slowUsage struct {
	*recordingUsage
	stored atomic.Int64
}

func (s *slowUsage) Record(ctx context.Context, rec *usage.Record) error {
	time.Sleep(time.Duration(1+s.stored.Load()%3) * time.Millisecond)
	s.stored.Add(1)
	return s.recordingUsage.Record(ctx, rec)
}

func TestUsageFlushWaitsForConcurrentRecords(t *testing.T) {
	const requests = 50
	rec := &slowUsage{recordingUsage: newRecordingUsage()}
	rec.done = make(chan struct{}, requests) // never read here
	mw := middlewares.NewUsage(rec, gpt4o, nil)

	var returned atomic.Int64 // Process calls that have returned
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
			_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
				req.State[pipeline.StateProviderName] = "openai"
				return &pipeline.Response{Completion: &provider.CompletionResponse{Model: "gpt-4o", Usage: provider.Usage{PromptTokens: 1, TotalTokens: 1}}}, nil
			})
			returned.Add(1)
		}()
	}

	// Flushes that start while requests are still arriving must not return
	// before the records of every request that had already returned.
	var flushers sync.WaitGroup
	for range 10 {
		flushers.Add(1)
		go func() {
			defer flushers.Done()
			for returned.Load() < requests {
				before := returned.Load()
				if err := mw.Flush(context.Background()); err != nil {
					t.Errorf("flush: %v", err)
					return
				}
				if got := rec.stored.Load(); got < before {
					t.Errorf("flush returned with %d records stored, but %d requests had already finished", got, before)
					return
				}
			}
		}()
	}
	wg.Wait()
	flushers.Wait()
	if err := mw.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := rec.stored.Load(); got != requests {
		t.Fatalf("flush returned with %d of %d records stored", got, requests)
	}
}

func TestUsageFlushHonoursItsContext(t *testing.T) {
	block := make(chan struct{})
	rec := &blockedUsage{recordingUsage: newRecordingUsage(), block: block}
	mw := middlewares.NewUsage(rec, gpt4o, nil)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := mw.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("flush = %v, want deadline exceeded", err)
	}
	close(block)
	if err := mw.Flush(context.Background()); err != nil {
		t.Fatalf("flush after release: %v", err)
	}
}

type blockedUsage struct {
	*recordingUsage
	block chan struct{}
}

func (b *blockedUsage) Record(ctx context.Context, rec *usage.Record) error {
	<-b.block
	return b.recordingUsage.Record(ctx, rec)
}

type panickingUsage struct{ *recordingUsage }

func (panickingUsage) Record(context.Context, *usage.Record) error { panic("store is nil") }

func TestUsageSurvivesAPanickingUsageService(t *testing.T) {
	log := &errLog{}
	mw := middlewares.NewUsage(panickingUsage{newRecordingUsage()}, gpt4o, log)
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{}}, nil
	})
	if err := mw.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if mw.InsertErrors() != 1 || log.n != 1 {
		t.Fatalf("insert errors %d, logged %d; want 1 and 1", mw.InsertErrors(), log.n)
	}
}

func TestUsageAfterCloseDropsNewRecordsButKeepsOpenStreams(t *testing.T) {
	rec := newRecordingUsage()
	log := &errLog{}
	mw := middlewares.NewUsage(rec, gpt4o, log)
	stream := func() *pipeline.Response {
		req := &pipeline.Request{Type: pipeline.RequestStream, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{pipeline.StateProviderName: "openai"}}
		resp, _ := mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
			return &pipeline.Response{Stream: testutil.NewFakeStream([]*provider.StreamChunk{usageChunk(100, 20)}, nil)}, nil
		})
		return resp
	}
	open := stream() // opened before Close
	mw.Close()
	if got := mw.Pending(); got != 1 {
		t.Fatalf("pending = %d, want the open stream", got)
	}

	// New work after Close is refused: a completion and a stream opened late.
	req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o"}, State: map[string]any{}}
	_, _ = mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
		return &pipeline.Response{Completion: &provider.CompletionResponse{}}, nil
	})
	late := stream()
	_ = late.Stream.Close()

	// The stream opened before Close still records while Flush waits.
	flushed := make(chan error, 1)
	go func() { flushed <- mw.Flush(context.Background()) }()
	select {
	case err := <-flushed:
		t.Fatalf("Flush returned (%v) while a stream opened before Close was still open", err)
	case <-time.After(50 * time.Millisecond):
	}
	for {
		if _, err := open.Stream.Next(context.Background()); err != nil {
			break
		}
	}
	_ = open.Stream.Close()
	select {
	case err := <-flushed:
		if err != nil {
			t.Fatalf("flush: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Flush did not return after the stream closed")
	}
	got := rec.only(t)
	wantRecord(t, got, usage.OutcomeOK, usage.PricingPriced, "0.00045")
	if mw.InsertErrors() != 2 || log.n != 2 {
		t.Fatalf("insert errors %d, logged %d; want 2 dropped records", mw.InsertErrors(), log.n)
	}
}

func TestUsageAttributesFromItsContextFirst(t *testing.T) {
	ctxTenant, ctxKey := id.NewTenantID().String(), id.NewKeyID().String()
	reqTenant, reqKey := id.NewTenantID().String(), id.NewKeyID().String()
	for _, c := range []struct {
		name                string
		ctxT, ctxK          string
		reqT, reqK          string
		wantTenant, wantKey string
	}{
		{"both set: the context wins", ctxTenant, ctxKey, reqTenant, reqKey, ctxTenant, ctxKey},
		{"only the context", ctxTenant, ctxKey, "", "", ctxTenant, ctxKey},
		{"only the request", "", "", reqTenant, reqKey, reqTenant, reqKey},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := newRecordingUsage()
			mw := middlewares.NewUsage(rec, gpt4o, nil)
			req := &pipeline.Request{Type: pipeline.RequestCompletion, Completion: &provider.CompletionRequest{Model: "gpt-4o", TenantID: c.reqT, KeyID: c.reqK}, State: map[string]any{}}
			ctx := pipeline.WithKeyID(pipeline.WithTenantID(context.Background(), c.ctxT), c.ctxK)
			_, _ = mw.Process(ctx, req, func(context.Context) (*pipeline.Response, error) {
				return nil, refusal{}
			})
			_ = mw.Flush(context.Background())
			got := rec.only(t)
			if got.TenantID.String() != c.wantTenant || got.KeyID.String() != c.wantKey {
				t.Fatalf("attributed to %s / %s, want %s / %s", got.TenantID, got.KeyID, c.wantTenant, c.wantKey)
			}
		})
	}
}
