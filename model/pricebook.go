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
// and kept.
type PriceBook struct {
	providers provider.Registry

	mu    sync.RWMutex
	lists map[string]map[string]provider.Pricing
}

// NewPriceBook creates a price book over the given providers.
func NewPriceBook(providers provider.Registry) *PriceBook {
	return &PriceBook{providers: providers, lists: make(map[string]map[string]provider.Pricing)}
}

// Price returns the list price at providerName of the first of modelIDs the
// provider lists. A provider that implements provider.FreeOfCharge prices
// every model at exactly $0. ok is false when the provider is unknown, its
// list cannot be read, or it lists none of the models.
func (b *PriceBook) Price(ctx context.Context, providerName string, modelIDs ...string) (provider.Pricing, bool) {
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
