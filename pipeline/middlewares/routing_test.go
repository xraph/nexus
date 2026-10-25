package middlewares_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/router"
	"github.com/xraph/nexus/router/strategies"
)

// routed is a provider that lists fixed models and records every call and
// the model it was called with.
type routed struct {
	name   string
	models []string
	embeds bool

	mu     sync.Mutex
	calls  int
	called []string
}

func newRouted(name string, embeds bool, models ...string) *routed {
	return &routed{name: name, embeds: embeds, models: models}
}

func (p *routed) Name() string { return p.name }
func (p *routed) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true, Streaming: true, Embeddings: p.embeds}
}
func (p *routed) Models(context.Context) ([]provider.Model, error) {
	out := make([]provider.Model, 0, len(p.models))
	for _, id := range p.models {
		out = append(out, provider.Model{ID: id, Provider: p.name})
	}
	return out, nil
}
func (p *routed) record(modelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.called = append(p.called, modelID)
}
func (p *routed) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	p.record(req.Model)
	return &provider.CompletionResponse{Provider: p.name, Model: req.Model}, nil
}
func (p *routed) CompleteStream(_ context.Context, req *provider.CompletionRequest) (provider.Stream, error) {
	p.record(req.Model)
	return nil, nil //nolint:nilnil // the provider call answers a nil stream with an empty response
}
func (p *routed) Embed(_ context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	p.record(req.Model)
	return &provider.EmbeddingResponse{Provider: p.name, Model: req.Model}, nil
}
func (p *routed) Healthy(context.Context) bool { return true }

func (p *routed) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *routed) Served() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.called...)
}

func registryOf(ps ...*routed) provider.Registry {
	reg := provider.NewRegistry()
	for _, p := range ps {
		reg.Register(p)
	}
	return reg
}

func routedCompletion(modelID, providerName string) *pipeline.Request {
	return &pipeline.Request{
		Type:       pipeline.RequestCompletion,
		Completion: &provider.CompletionRequest{Model: modelID, Provider: providerName},
		State:      map[string]any{},
	}
}

func routedEmbedding(modelID, providerName string) *pipeline.Request {
	return &pipeline.Request{
		Type:      pipeline.RequestEmbedding,
		Embedding: &provider.EmbeddingRequest{Model: modelID, Provider: providerName, Input: []string{"x"}},
		State:     map[string]any{},
	}
}

// callOnceThroughRetry runs the provider call behind a retry stage and
// returns how many times the retry stage reached it.
func callOnceThroughRetry(t *testing.T, mw *middlewares.ProviderCallMiddleware, req *pipeline.Request) (int, error) {
	t.Helper()
	attempts := 0
	_, err := middlewares.NewRetry(3, time.Millisecond, 1).Process(context.Background(), req, func(ctx context.Context) (*pipeline.Response, error) {
		attempts++
		return mw.Process(ctx, req, nil)
	})
	return attempts, err
}

func TestExplicitProviderWins(t *testing.T) {
	for _, typ := range []pipeline.RequestType{pipeline.RequestCompletion, pipeline.RequestStream} {
		t.Run(string(typ), func(t *testing.T) {
			openai := newRouted("openai", true, "gpt-4o")
			anthropic := newRouted("anthropic", false, "claude-opus-5-5")
			mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai, anthropic))

			req := routedCompletion("gpt-4o", "anthropic")
			req.Type = typ
			if _, err := mw.Process(context.Background(), req, nil); err != nil {
				t.Fatalf("Process: %v", err)
			}
			if anthropic.Calls() != 1 || openai.Calls() != 0 {
				t.Fatalf("calls: anthropic %d, openai %d; the named provider must serve the request", anthropic.Calls(), openai.Calls())
			}
			if req.State[pipeline.StateProviderName] != "anthropic" {
				t.Fatalf("provider name = %v, want anthropic", req.State[pipeline.StateProviderName])
			}
		})
	}
}

func TestUnknownExplicitProviderIsRefused(t *testing.T) {
	openai := newRouted("openai", true, "gpt-4o")
	mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai))

	req := routedCompletion("gpt-4o", "nope")
	attempts, err := callOnceThroughRetry(t, mw, req)
	if !errors.Is(err, middlewares.ErrUnknownProvider) {
		t.Fatalf("err = %v, want ErrUnknownProvider", err)
	}
	if status, code := pipeline.HTTPStatus(err); status != 400 || code != pipeline.CodeInvalidRequest {
		t.Fatalf("refusal = %d %s, want 400 invalid_request", status, code)
	}
	if !pipeline.IsPermanent(err) {
		t.Fatalf("an unknown provider must be permanent")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, an unknown provider must not be retried", attempts)
	}
	if openai.Calls() != 0 {
		t.Fatalf("openai called %d times, no provider may be called", openai.Calls())
	}
	if _, ok := req.State[pipeline.StateProviderName]; ok {
		t.Fatalf("no provider name may be recorded for a refused request")
	}
}

func TestAliasTargetProviderIsHonoured(t *testing.T) {
	aliases := model.NewAliasRegistry()
	if err := aliases.Register(&model.Alias{Name: "smart", Targets: []model.AliasTarget{{Provider: "anthropic", Model: "claude-sonnet-5-5"}}}); err != nil {
		t.Fatal(err)
	}
	openai := newRouted("openai", true, "gpt-4o")
	anthropic := newRouted("anthropic", false)
	mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai, anthropic))

	req := routedCompletion("smart", "")
	_, err := middlewares.NewAlias(aliases).Process(context.Background(), req, func(ctx context.Context) (*pipeline.Response, error) {
		return mw.Process(ctx, req, nil)
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if openai.Calls() != 0 {
		t.Fatalf("openai called %d times, the alias names anthropic", openai.Calls())
	}
	if got := anthropic.Served(); len(got) != 1 || got[0] != "claude-sonnet-5-5" {
		t.Fatalf("anthropic called with %v, want [claude-sonnet-5-5]", got)
	}
}

func TestUnknownAliasTargetProviderIsRefused(t *testing.T) {
	openai := newRouted("openai", true, "gpt-4o")
	mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai))

	req := routedCompletion("claude-sonnet-5-5", "")
	req.State[pipeline.StateAliasProvider] = "anthropic"
	req.State[pipeline.StateAliasModel] = "claude-sonnet-5-5"
	attempts, err := callOnceThroughRetry(t, mw, req)
	if !errors.Is(err, middlewares.ErrUnknownProvider) || attempts != 1 || openai.Calls() != 0 {
		t.Fatalf("err = %v, attempts %d, openai calls %d; want one refused attempt", err, attempts, openai.Calls())
	}
}

func TestRoutesToTheProviderThatListsTheModel(t *testing.T) {
	for _, typ := range []pipeline.RequestType{pipeline.RequestCompletion, pipeline.RequestStream} {
		t.Run(string(typ), func(t *testing.T) {
			openai := newRouted("openai", true, "gpt-4o")
			anthropic := newRouted("anthropic", false, "claude-opus-5-5")
			mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai, anthropic))

			req := routedCompletion("claude-opus-5-5", "")
			req.Type = typ
			if _, err := mw.Process(context.Background(), req, nil); err != nil {
				t.Fatalf("Process: %v", err)
			}
			if anthropic.Calls() != 1 || openai.Calls() != 0 {
				t.Fatalf("calls: anthropic %d, openai %d; want the provider that lists the model", anthropic.Calls(), openai.Calls())
			}
		})
	}
}

func TestRoutesByModelWithoutARouter(t *testing.T) {
	openai := newRouted("openai", true, "gpt-4o")
	anthropic := newRouted("anthropic", false, "claude-opus-5-5")
	mw := middlewares.NewProviderCall(nil, registryOf(openai, anthropic))

	if _, err := mw.Process(context.Background(), routedCompletion("claude-opus-5-5", ""), nil); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if anthropic.Calls() != 1 || openai.Calls() != 0 {
		t.Fatalf("calls: anthropic %d, openai %d; want the first provider that lists the model", anthropic.Calls(), openai.Calls())
	}
}

func TestStrategyChoosesAmongProvidersListingTheModel(t *testing.T) {
	openai := newRouted("openai", true, "gpt-4o")
	anthropic := newRouted("anthropic", false, "claude-opus-5-5")
	openrouter := newRouted("openrouter", true, "gpt-4o", "claude-opus-5-5")
	mw := middlewares.NewProviderCall(router.NewService(strategies.NewRoundRobin()), registryOf(openai, anthropic, openrouter))

	for range 4 {
		if _, err := mw.Process(context.Background(), routedCompletion("gpt-4o", ""), nil); err != nil {
			t.Fatalf("Process: %v", err)
		}
	}
	if anthropic.Calls() != 0 {
		t.Fatalf("anthropic called %d times, it does not list gpt-4o", anthropic.Calls())
	}
	if openai.Calls() != 2 || openrouter.Calls() != 2 {
		t.Fatalf("calls: openai %d, openrouter %d; want round robin between them", openai.Calls(), openrouter.Calls())
	}
}

func TestUnlistedModelFallsBackToTheStrategy(t *testing.T) {
	openai := newRouted("openai", true, "gpt-4o")
	anthropic := newRouted("anthropic", false, "claude-opus-5-5")
	mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai, anthropic))

	req := routedCompletion("llama3", "")
	if _, err := mw.Process(context.Background(), req, nil); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if openai.Calls() != 1 || anthropic.Calls() != 0 {
		t.Fatalf("calls: openai %d, anthropic %d; want the strategy's pick over every provider", openai.Calls(), anthropic.Calls())
	}
}

func TestEmbeddingRoutesByProviderThenModel(t *testing.T) {
	cases := []struct {
		name, provider, model, want string
	}{
		{"explicit provider wins over the model", "b", "embed-a", "b"},
		{"the provider that lists the model", "", "embed-b", "b"},
		{"neither, the first embeddings provider", "", "embed-none", "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newRouted("a", true, "embed-a")
			b := newRouted("b", true, "embed-b")
			mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(a, b))

			req := routedEmbedding(tc.model, tc.provider)
			resp, err := mw.Process(context.Background(), req, nil)
			if err != nil || resp == nil || resp.Embedding == nil {
				t.Fatalf("Process = %+v, %v", resp, err)
			}
			if resp.Embedding.Provider != tc.want || req.State[pipeline.StateProviderName] != tc.want {
				t.Fatalf("served by %q (state %v), want %q", resp.Embedding.Provider, req.State[pipeline.StateProviderName], tc.want)
			}
		})
	}
}

func TestEmbeddingExplicitProviderIsNeverSwapped(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		openai := newRouted("openai", true, "text-embedding-3-small")
		mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai))

		req := routedEmbedding("text-embedding-3-small", "nope")
		attempts, err := callOnceThroughRetry(t, mw, req)
		if !errors.Is(err, middlewares.ErrUnknownProvider) {
			t.Fatalf("err = %v, want ErrUnknownProvider", err)
		}
		if status, code := pipeline.HTTPStatus(err); status != 400 || code != pipeline.CodeInvalidRequest {
			t.Fatalf("refusal = %d %s, want 400 invalid_request", status, code)
		}
		if attempts != 1 || openai.Calls() != 0 {
			t.Fatalf("attempts %d, openai calls %d; want one refused attempt", attempts, openai.Calls())
		}
		if _, ok := req.State[pipeline.StateProviderName]; ok {
			t.Fatalf("no provider name may be recorded for a refused request")
		}
	})

	t.Run("does not embed", func(t *testing.T) {
		openai := newRouted("openai", true, "text-embedding-3-small")
		anthropic := newRouted("anthropic", false)
		mw := middlewares.NewProviderCall(router.NewService(strategies.NewPriority()), registryOf(openai, anthropic))

		req := routedEmbedding("text-embedding-3-small", "anthropic")
		attempts, err := callOnceThroughRetry(t, mw, req)
		if !errors.Is(err, provider.ErrNotSupported) {
			t.Fatalf("err = %v, want provider.ErrNotSupported", err)
		}
		if status, code := pipeline.HTTPStatus(err); status != 400 || code != pipeline.CodeInvalidRequest {
			t.Fatalf("refusal = %d %s, want 400 invalid_request", status, code)
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, a provider that cannot embed must not be retried", attempts)
		}
		if openai.Calls() != 0 || anthropic.Calls() != 0 {
			t.Fatalf("calls: openai %d, anthropic %d; no provider may be called", openai.Calls(), anthropic.Calls())
		}
		if _, ok := req.State[pipeline.StateProviderName]; ok {
			t.Fatalf("no provider name may be recorded for a refused request")
		}
	})
}
