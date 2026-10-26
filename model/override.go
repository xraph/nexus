package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/xraph/nexus/provider"
)

// ErrUnpricedLongContext rejects a price override whose long-context tier
// lacks an input price or an output price. With neither, Cost would charge a
// long prompt at the base price, which is not what the override says. With
// only one, it would charge the other side at $0 and a budget would
// undercount. SetOverrides wraps it with the provider, the model and the
// missing price; match it with errors.Is.
var ErrUnpricedLongContext = errors.New("nexus: long-context tier has no input or output price")

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
//
// An entry whose LongContext tier lacks an input price or an output price
// fails the whole call with ErrUnpricedLongContext, naming the model and the
// provider, and the overrides already set stay as they were. A Free price,
// or a Free tier, may leave either price at zero. OverridesErr reports the
// latest failure until a call succeeds.
func (b *PriceBook) SetOverrides(overrides []PriceOverride) error {
	set := make(map[string]map[string]provider.Pricing)
	for _, o := range overrides {
		if o.Provider == "" || o.Model == "" {
			continue
		}
		if tier := o.Pricing.LongContext; tier != nil && !o.Pricing.Free && !tier.Free {
			if missing := missingTierPrice(*tier); missing != "" {
				err := fmt.Errorf("%w: model %s at %s has no %s in its long-context tier", ErrUnpricedLongContext, o.Model, o.Provider, missing)
				b.mu.Lock()
				b.rejected = err
				b.mu.Unlock()
				return err
			}
		}
		if set[o.Provider] == nil {
			set[o.Provider] = make(map[string]provider.Pricing)
		}
		set[o.Provider][o.Model] = clonePricing(o.Pricing)
	}
	b.mu.Lock()
	b.overrides = set
	b.rejected = nil
	b.mu.Unlock()
	return nil
}

// OverridesErr is the error of the latest SetOverrides call, or nil when it
// succeeded or none was made. A later successful call clears it.
func (b *PriceBook) OverridesErr() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.rejected
}

// missingTierPrice names the token price a long-context tier lacks, or "".
func missingTierPrice(tier provider.Pricing) string {
	switch {
	case tier.InputPerMillion.IsZero() && tier.OutputPerMillion.IsZero():
		return "input or output price"
	case tier.InputPerMillion.IsZero():
		return "input price"
	case tier.OutputPerMillion.IsZero():
		return "output price"
	default:
		return ""
	}
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
