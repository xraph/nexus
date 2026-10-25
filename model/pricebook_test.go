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

func TestOverrideWinsAndPricesUnlistedModels(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&listedProvider{name: "opencompat-x"})
	reg.Register(&listedProvider{name: "openai", models: []provider.Model{{ID: "gpt-4o", Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10")}}}})
	book := model.NewPriceBook(reg)
	ctx := context.Background()
	if _, ok := book.Price(ctx, "opencompat-x", "llama3"); ok {
		t.Fatalf("an unlisted model is priced before any override")
	}
	book.SetOverrides([]model.PriceOverride{
		{Provider: "opencompat-x", Model: "llama3", Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.2"), OutputPerMillion: money.MustParse("0.4")}},
		{Provider: "openai", Model: "gpt-4o", Pricing: provider.Pricing{InputPerMillion: money.MustParse("2"), OutputPerMillion: money.MustParse("8")}},
	})
	if p, ok := book.Price(ctx, "opencompat-x", "llama3"); !ok || !p.InputPerMillion.Equal(money.MustParse("0.2")) || !p.OutputPerMillion.Equal(money.MustParse("0.4")) {
		t.Fatalf("override = %+v, %v", p, ok)
	}
	if !book.Lists(ctx, "opencompat-x", "llama3") {
		t.Fatalf("an overridden model should be listed")
	}
	if !book.Priced(ctx, "opencompat-x", "llama3") {
		t.Fatalf("an overridden model should be priced")
	}
	if p, ok := book.Price(ctx, "openai", "gpt-4o"); !ok || !p.InputPerMillion.Equal(money.MustParse("2")) {
		t.Fatalf("an override for a listed model should replace the list price, got %+v, %v", p, ok)
	}
}

func TestSetOverridesReplacesTheSet(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&listedProvider{name: "opencompat-x"})
	book := model.NewPriceBook(reg)
	ctx := context.Background()
	llama := model.PriceOverride{Provider: "opencompat-x", Model: "llama3", Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.2")}}
	mistral := model.PriceOverride{Provider: "opencompat-x", Model: "mistral", Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.1")}}
	book.SetOverrides([]model.PriceOverride{llama})
	if _, ok := book.Price(ctx, "opencompat-x", "llama3"); !ok {
		t.Fatalf("first set not applied")
	}
	book.SetOverrides([]model.PriceOverride{mistral})
	if _, ok := book.Price(ctx, "opencompat-x", "llama3"); ok {
		t.Fatalf("an override left out of the new set should be gone")
	}
	if book.Lists(ctx, "opencompat-x", "llama3") || book.Priced(ctx, "opencompat-x", "llama3") {
		t.Fatalf("an override left out of the new set should not be listed or priced")
	}
	if _, ok := book.Price(ctx, "opencompat-x", "mistral"); !ok {
		t.Fatalf("second set not applied")
	}
}

func TestPricedNeedsANonZeroPrice(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&listedProvider{name: "p", models: []provider.Model{
		{ID: "listed-without-price"},
		{ID: "embed", Pricing: provider.Pricing{EmbeddingPerMillion: money.MustParse("0.02")}},
		{ID: "output-only", Pricing: provider.Pricing{OutputPerMillion: money.MustParse("1")}},
		{ID: "free", Pricing: provider.Pricing{Free: true}},
	}})
	reg.Register(&freeProvider{listedProvider{name: "ollama"}})
	book := model.NewPriceBook(reg)
	ctx := context.Background()
	for modelID, want := range map[string]bool{"listed-without-price": false, "embed": true, "output-only": true, "free": true, "unlisted": false} {
		if got := book.Priced(ctx, "p", modelID); got != want {
			t.Errorf("Priced(p, %s) = %v, want %v", modelID, got, want)
		}
	}
	if !book.Priced(ctx, "ollama", "anything") {
		t.Errorf("a free-of-charge provider prices every model")
	}
	if book.Priced(ctx, "nobody", "gpt-4o") {
		t.Errorf("an unknown provider prices nothing")
	}
}
