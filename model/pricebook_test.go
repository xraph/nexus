package model_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

type listedProvider struct {
	name   string
	models []provider.Model
	calls  int
}

func (p *listedProvider) Name() string { return p.name }
func (p *listedProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true}
}
func (p *listedProvider) Models(context.Context) ([]provider.Model, error) {
	p.calls++
	return p.models, nil
}
func (p *listedProvider) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, nil //nolint:nilnil // unused
}
func (p *listedProvider) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, nil //nolint:nilnil // unused
}
func (p *listedProvider) Embed(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil //nolint:nilnil // unused
}
func (p *listedProvider) Healthy(context.Context) bool { return true }

type freeProvider struct{ listedProvider }

func (*freeProvider) FreeOfCharge() bool { return true }

func TestPriceBookPricesAtTheProviderThatServed(t *testing.T) {
	reg := provider.NewRegistry()
	openai := &listedProvider{name: "openai", models: []provider.Model{{ID: "gpt-4o", Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10")}}}}
	router := &listedProvider{name: "openrouter", models: []provider.Model{{ID: "gpt-4o", Pricing: provider.Pricing{InputPerMillion: money.MustParse("3"), OutputPerMillion: money.MustParse("12")}}}}
	local := &freeProvider{listedProvider{name: "ollama"}}
	reg.Register(openai)
	reg.Register(router)
	reg.Register(local)
	book := model.NewPriceBook(reg)
	ctx := context.Background()

	if p, ok := book.Price(ctx, "openrouter", "gpt-4o"); !ok || p.InputPerMillion.String() != "3" {
		t.Fatalf("openrouter gpt-4o = %+v, %v", p, ok)
	}
	if p, ok := book.Price(ctx, "openai", "gpt-4o"); !ok || p.InputPerMillion.String() != "2.5" {
		t.Fatalf("openai gpt-4o = %+v, %v", p, ok)
	}
	// The first listed ID wins; a dated response model falls back to the
	// requested one.
	if p, ok := book.Price(ctx, "openai", "gpt-4o-2024-08-06", "gpt-4o"); !ok || p.OutputPerMillion.String() != "10" {
		t.Fatalf("fallback ID = %+v, %v", p, ok)
	}
	if _, ok := book.Price(ctx, "openai", "o9"); ok {
		t.Fatalf("a model the provider does not list must not be priced")
	}
	if _, ok := book.Price(ctx, "nobody", "gpt-4o"); ok {
		t.Fatalf("an unknown provider must not be priced")
	}
	if p, ok := book.Price(ctx, "ollama", "llama3:70b"); !ok || !p.Free {
		t.Fatalf("a free-of-charge provider prices any model at $0, got %+v, %v", p, ok)
	}
	book.Price(ctx, "openai", "gpt-4o")
	if openai.calls != 1 {
		t.Fatalf("Models called %d times; the price list should be read once", openai.calls)
	}
}

func TestPriceBookListsOnlyWhatTheProviderLists(t *testing.T) {
	reg := provider.NewRegistry()
	openai := &listedProvider{name: "openai", models: []provider.Model{{ID: "gpt-4o"}}}
	local := &freeProvider{listedProvider{name: "ollama", models: []provider.Model{{ID: "llama3"}}}}
	reg.Register(openai)
	reg.Register(local)
	book := model.NewPriceBook(reg)
	ctx := context.Background()

	for _, tc := range []struct {
		provider, model string
		want            bool
	}{
		{"openai", "gpt-4o", true},
		{"openai", "alice@example.com", false},
		{"openai", "", false},
		{"nobody", "gpt-4o", false},
		// Free of charge prices any model, but lists only its own.
		{"ollama", "llama3", true},
		{"ollama", "qwen3:8b", false},
	} {
		if got := book.Lists(ctx, tc.provider, tc.model); got != tc.want {
			t.Errorf("Lists(%q, %q) = %v, want %v", tc.provider, tc.model, got, tc.want)
		}
	}
	book.Price(ctx, "openai", "gpt-4o")
	if openai.calls != 1 {
		t.Fatalf("Models called %d times; Lists and Price share one cached list", openai.calls)
	}
}
