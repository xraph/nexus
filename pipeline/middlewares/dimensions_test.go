package middlewares_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
	"github.com/xraph/nexus/router/strategies"
	"github.com/xraph/nexus/usage"
)

// embedder answers every embedding with vectors width numbers long and
// counts its calls. It does not implement provider.DimensionsAware, like
// voyageai or cohere.
type embedder struct {
	name  string
	width int
	calls atomic.Int32
}

func (e *embedder) Name() string { return e.name }
func (e *embedder) Capabilities() provider.Capabilities {
	return provider.Capabilities{Embeddings: true}
}
func (e *embedder) Models(context.Context) ([]provider.Model, error) { return nil, nil }
func (e *embedder) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("embedder does not chat")
}
func (e *embedder) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, errors.New("embedder does not chat")
}
func (e *embedder) Embed(_ context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	e.calls.Add(1)
	out := make([][]float64, len(req.Input))
	for i := range out {
		out[i] = make([]float64, e.width)
	}
	return &provider.EmbeddingResponse{
		Provider: e.name, Model: req.Model, Embeddings: out,
		Usage: provider.Usage{PromptTokens: 1000, TotalTokens: 1000},
	}, nil
}
func (e *embedder) Healthy(context.Context) bool { return true }

// sizedEmbedder is an embedder that implements provider.DimensionsAware
// with supports.
type sizedEmbedder struct {
	*embedder
	supports func(model string) bool
}

func (s sizedEmbedder) SupportsDimensions(model string) bool { return s.supports(model) }

func providerCallFor(ps ...provider.Provider) *middlewares.ProviderCallMiddleware {
	reg := provider.NewRegistry()
	for _, p := range ps {
		reg.Register(p)
	}
	return middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), reg)
}

func embeddingWith(providerName, modelID string, dims int) *pipeline.Request {
	req := routedEmbedding(modelID, providerName)
	req.Embedding.Dimensions = dims
	return req
}

func TestDimensionsRefusedWhereUnsupported(t *testing.T) {
	voyage := &embedder{name: "voyageai", width: 1024}
	// OpenAI on its own base URL answers true only for text-embedding-3-*.
	openai := sizedEmbedder{&embedder{name: "openai", width: 512}, func(m string) bool { return m == "text-embedding-3-small" }}

	cases := map[string]struct {
		prov  *embedder
		req   *pipeline.Request
		mw    *middlewares.ProviderCallMiddleware
		named string
	}{
		"provider is not DimensionsAware": {voyage, embeddingWith("voyageai", "voyage-3", 1024), providerCallFor(voyage), "voyageai"},
		"provider answers false":          {openai.embedder, embeddingWith("openai", "text-embedding-ada-002", 512), providerCallFor(openai), "openai"},
		"routed, not named":               {voyage, embeddingWith("", "voyage-3", 1024), providerCallFor(voyage), "voyageai"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			attempts, err := callOnceThroughRetry(t, tc.mw, tc.req)
			if !errors.Is(err, middlewares.ErrDimensionsUnsupported) || !errors.Is(err, provider.ErrNotSupported) {
				t.Fatalf("err = %v, want ErrDimensionsUnsupported wrapping provider.ErrNotSupported", err)
			}
			if status, code := pipeline.HTTPStatus(err); status != 400 || code != pipeline.CodeInvalidRequest {
				t.Fatalf("refusal = %d %s, want 400 invalid_request", status, code)
			}
			if attempts != 1 || tc.prov.calls.Load() != 0 {
				t.Fatalf("attempts %d, provider calls %d; want one refused attempt and no call", attempts, tc.prov.calls.Load())
			}
			if _, ok := tc.req.State[pipeline.StateProviderName]; ok {
				t.Fatal("a refused request must not record a provider name")
			}
		})
	}

	t.Run("no dimensions asked", func(t *testing.T) {
		if _, err := providerCallFor(voyage).Process(context.Background(), embeddingWith("voyageai", "voyage-3", 0), nil); err != nil {
			t.Fatalf("Dimensions 0 must reach any embedder: %v", err)
		}
	})
	t.Run("provider answers true", func(t *testing.T) {
		if _, err := providerCallFor(openai).Process(context.Background(), embeddingWith("openai", "text-embedding-3-small", 512), nil); err != nil {
			t.Fatalf("text-embedding-3-small at 512: %v", err)
		}
	})
}

func TestDimensionsLengthIsChecked(t *testing.T) {
	compat := sizedEmbedder{&embedder{name: "compat", width: 768}, func(string) bool { return true }}
	mw := providerCallFor(compat)

	attempts, err := callOnceThroughRetry(t, mw, embeddingWith("compat", "nomic-embed", 1024))
	if err == nil || err.Error() != "nexus: provider compat returned 768 dimensions, want 1024" {
		t.Fatalf("err = %v, want the 768 vs 1024 mismatch", err)
	}
	if attempts != 1 || compat.calls.Load() != 1 {
		t.Fatalf("attempts %d, calls %d; a wrong width will not change on retry", attempts, compat.calls.Load())
	}

	compat.width = 1024
	resp, err := mw.Process(context.Background(), embeddingWith("compat", "nomic-embed", 1024), nil)
	if err != nil || len(resp.Embedding.Embeddings[0]) != 1024 {
		t.Fatalf("Process = %+v, %v; want a 1024-long vector", resp, err)
	}
}

func TestDimensionsOutcomesAreRecorded(t *testing.T) {
	embedPrice := prices{
		"compat/nomic-embed": {EmbeddingPerMillion: money.MustParse("0.02")},
		"voyageai/voyage-3":  {EmbeddingPerMillion: money.MustParse("0.06")},
	}
	run := func(t *testing.T, mw *middlewares.ProviderCallMiddleware, req *pipeline.Request) *usage.Record {
		t.Helper()
		rec := newRecordingUsage()
		um := middlewares.NewUsage(rec, embedPrice, nil)
		ctx := pipeline.WithRequestID(context.Background(), id.NewRequestID().String())
		_, err := um.Process(ctx, req, func(ctx context.Context) (*pipeline.Response, error) {
			return middlewares.NewRetry(3, time.Millisecond, 1).Process(ctx, req, func(ctx context.Context) (*pipeline.Response, error) {
				return mw.Process(ctx, req, nil)
			})
		})
		if err == nil {
			t.Fatal("want an error")
		}
		if err := um.Flush(context.Background()); err != nil {
			t.Fatalf("flush: %v", err)
		}
		return rec.only(t)
	}

	t.Run("refused is not a server error", func(t *testing.T) {
		voyage := &embedder{name: "voyageai", width: 1024}
		got := run(t, providerCallFor(voyage), embeddingWith("voyageai", "voyage-3", 1024))
		if got.Outcome != usage.OutcomeRefused || got.StatusCode != 400 || got.RefusalCode != pipeline.CodeInvalidRequest ||
			got.Provider != "" || got.PricingStatus != usage.PricingNotCharged {
			t.Fatalf("record = %s %d %q provider %q %s; want refused 400 invalid_request, no provider, not charged",
				got.Outcome, got.StatusCode, got.RefusalCode, got.Provider, got.PricingStatus)
		}
	})

	t.Run("a wrong width keeps the tokens it was charged", func(t *testing.T) {
		compat := sizedEmbedder{&embedder{name: "compat", width: 768}, func(string) bool { return true }}
		got := run(t, providerCallFor(compat), embeddingWith("compat", "nomic-embed", 1024))
		if got.Outcome != usage.OutcomeError || got.Provider != "compat" || got.PromptTokens != 1000 {
			t.Fatalf("record = %s provider %q prompt %d; want an error served by compat with 1000 tokens", got.Outcome, got.Provider, got.PromptTokens)
		}
		if got.PricingStatus != usage.PricingPriced || got.CostUSD == nil || !got.CostUSD.Equal(money.MustParse("0.00002")) {
			t.Fatalf("cost %v (%s), want 0.00002 priced", got.CostUSD, got.PricingStatus)
		}
	})
}
