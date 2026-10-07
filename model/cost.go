package model

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

// Cost prices usage at a model's list prices, exactly: prompt tokens at the
// input price plus completion tokens at the output price, or prompt tokens
// at the embedding price for an embedding. When the model has no price for
// the tokens used it returns nil and PricingUnpricedModel, so an unknown
// cost is never reported as $0. A Free price list costs exactly $0.
//
// The result is only as good as the token counts the provider reported.
// Providers do not report cache or thinking tokens today, so those are not
// priced.
func Cost(u provider.Usage, p provider.Pricing, embedding bool) (*money.USD, usage.PricingStatus) {
	if p.Free {
		zero := money.Zero
		return &zero, usage.PricingPriced
	}
	if embedding {
		if p.EmbeddingPerMillion.IsZero() {
			return nil, usage.PricingUnpricedModel
		}
		c := p.EmbeddingPerMillion.PerMillion(int64(u.PromptTokens))
		return &c, usage.PricingPriced
	}
	if p.InputPerMillion.IsZero() && p.OutputPerMillion.IsZero() {
		return nil, usage.PricingUnpricedModel
	}
	c := p.InputPerMillion.PerMillion(int64(u.PromptTokens)).
		Add(p.OutputPerMillion.PerMillion(int64(u.CompletionTokens)))
	return &c, usage.PricingPriced
}
