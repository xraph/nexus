package model

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// PriceOverride sets the price of Model at Provider, the registered name of
// the provider that serves it. It wins over the provider's own list price
// and prices a model the provider does not list, such as one served through
// an OpenAI-compatible endpoint.
type PriceOverride struct {
	Provider string
	Model    string
	Pricing  provider.Pricing
}

// SetOverrides replaces every override with overrides. An entry with no
// provider or no model is ignored; of two entries for the same model at the
// same provider the later wins. It is safe to call while requests are being
// priced, so a host can reload its prices at runtime.
func (b *PriceBook) SetOverrides(overrides []PriceOverride) {
	set := make(map[string]map[string]provider.Pricing)
	for _, o := range overrides {
		if o.Provider == "" || o.Model == "" {
			continue
		}
		if set[o.Provider] == nil {
			set[o.Provider] = make(map[string]provider.Pricing)
		}
		set[o.Provider][o.Model] = clonePricing(o.Pricing)
	}
	b.mu.Lock()
	b.overrides = set
	b.mu.Unlock()
}

// Priced reports whether modelID has a price at providerName: Free, or any
// of an input, output or embedding price that is not zero. A model the
// provider lists with no price is not priced.
func (b *PriceBook) Priced(ctx context.Context, providerName, modelID string) bool {
	p, ok := b.Price(ctx, providerName, modelID)
	if !ok {
		return false
	}
	return p.Free || !p.InputPerMillion.IsZero() || !p.OutputPerMillion.IsZero() || !p.EmbeddingPerMillion.IsZero()
}

// override returns the override for the first of modelIDs that has one at
// providerName. The set it reads is never changed after SetOverrides
// stores it, so it is read without holding the lock.
func (b *PriceBook) override(providerName string, modelIDs ...string) (provider.Pricing, bool) {
	b.mu.RLock()
	set := b.overrides[providerName]
	b.mu.RUnlock()
	for _, id := range modelIDs {
		if id == "" {
			continue
		}
		if price, ok := set[id]; ok {
			return price, true
		}
	}
	return provider.Pricing{}, false
}

// clonePricing copies p and its long-context tier, so a caller that changes
// its PriceOverride later does not change the stored price.
func clonePricing(p provider.Pricing) provider.Pricing {
	if p.LongContext != nil {
		tier := clonePricing(*p.LongContext)
		p.LongContext = &tier
	}
	return p
}
