package model

import (
	"context"
	"sync"

	"github.com/xraph/nexus/provider"
)

// PriceBook looks up list prices by provider and model. The same model ID
// can be sold by several providers at different prices (gpt-4o through
// openai, azureopenai and openrouter), so a price is only meaningful at the
// provider that served the request. Each provider's price list is read once
// and kept. Overrides (SetOverrides) are consulted before the lists.
type PriceBook struct {
	providers provider.Registry

	mu        sync.RWMutex
	lists     map[string]map[string]provider.Pricing
	overrides map[string]map[string]provider.Pricing // provider, then model
}

// NewPriceBook creates a price book over the given providers.
func NewPriceBook(providers provider.Registry) *PriceBook {
	return &PriceBook{providers: providers, lists: make(map[string]map[string]provider.Pricing)}
}

// Price returns the price at providerName of the first of modelIDs with an
// override there, else the list price of the first of modelIDs the provider
// lists. A provider that implements provider.FreeOfCharge prices every model
// it has no override for at exactly $0. ok is false when no override
// matches and the provider is unknown, its list cannot be read, or it lists
// none of the models.
func (b *PriceBook) Price(ctx context.Context, providerName string, modelIDs ...string) (provider.Pricing, bool) {
	if price, ok := b.override(providerName, modelIDs...); ok {
		return price, true
	}
	p, ok := b.providers.Get(providerName)
	if !ok {
		return provider.Pricing{}, false
	}
	if f, isFree := p.(provider.FreeOfCharge); isFree && f.FreeOfCharge() {
		return provider.Pricing{Free: true}, true
	}
	list, ok := b.list(ctx, p)
	if !ok {
		return provider.Pricing{}, false
	}
	for _, id := range modelIDs {
		if id == "" {
			continue
		}
		if price, ok := list[id]; ok {
			return price, true
		}
	}
	return provider.Pricing{}, false
}

// Lists reports whether the provider named providerName lists modelID, or
// has an override for it. It reads the same cached list as Price. Routing
// uses it to find the providers that serve a model, and telemetry to name
// only the models a provider lists, so a client's arbitrary model string
// never becomes a metric series or a span attribute.
func (b *PriceBook) Lists(ctx context.Context, providerName, modelID string) bool {
	if modelID == "" {
		return false
	}
	if _, ok := b.override(providerName, modelID); ok {
		return true
	}
	p, ok := b.providers.Get(providerName)
	if !ok {
		return false
	}
	list, ok := b.list(ctx, p)
	if !ok {
		return false
	}
	_, listed := list[modelID]
	return listed
}

func (b *PriceBook) list(ctx context.Context, p provider.Provider) (map[string]provider.Pricing, bool) {
	b.mu.RLock()
	list, ok := b.lists[p.Name()]
	b.mu.RUnlock()
	if ok {
		return list, true
	}
	models, err := p.Models(ctx)
	if err != nil {
		return nil, false
	}
	list = make(map[string]provider.Pricing, len(models))
	for _, m := range models {
		list[m.ID] = m.Pricing
	}
	b.mu.Lock()
	b.lists[p.Name()] = list
	b.mu.Unlock()
	return list, true
}
