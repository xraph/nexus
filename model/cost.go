package model

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

// Cost prices usage at a model's list prices, exactly. A completion charges
// its uncached prompt tokens at the input price, cache reads and writes at
// their own prices (see provider.Pricing for the fallbacks) and completion
// tokens at the output price. A request whose prompt is longer than the
// model's LongContextAbove is charged entirely at the LongContext tier. A
// tier with neither an input nor an output price is ignored and the request
// is charged at the base price, so a missing tier never turns a priced model
// into an unknown cost.
//
// An embedding charges prompt tokens at the embedding price. An embedding
// provider that reports only a total (Voyage does) is priced from the
// total, since every token of an embedding is input.
//
// When the model has no price for the tokens used it returns nil and
// PricingUnpricedModel, so an unknown cost is never reported as $0. A Free
// price list costs exactly $0.
//
// The result is only as good as the token counts the provider reported: a
// provider that does not report its cache tokens has them charged as
// uncached input.
func Cost(u provider.Usage, p provider.Pricing, embedding bool) (*money.USD, usage.PricingStatus) {
	if p.Free {
		zero := money.Zero
		return &zero, usage.PricingPriced
	}
	if embedding {
		if p.EmbeddingPerMillion.IsZero() {
			return nil, usage.PricingUnpricedModel
		}
		tokens := u.PromptTokens
		if tokens == 0 {
			tokens = u.TotalTokens
		}
		c := p.EmbeddingPerMillion.PerMillion(int64(tokens))
		return &c, usage.PricingPriced
	}
	if p.LongContextAbove > 0 && u.PromptTokens > p.LongContextAbove && p.LongContext != nil && hasTokenPrice(*p.LongContext) {
		p = *p.LongContext
	}
	if !hasTokenPrice(p) {
		return nil, usage.PricingUnpricedModel
	}

	// The cache counts are parts of the prompt. A 1-hour write is also a
	// write, so the writes are at least the 1-hour ones.
	writes := max(u.CacheWriteTokens, u.CacheWriteLongTokens)
	longWrites := u.CacheWriteLongTokens
	uncached := max(0, u.PromptTokens-u.CacheReadTokens-writes)

	readPrice := orElse(p.CacheReadPerMillion, p.InputPerMillion)
	writePrice := orElse(p.CacheWritePerMillion, p.InputPerMillion)
	longWritePrice := orElse(p.CacheWriteLongPerMillion, writePrice)

	c := money.Sum(
		p.InputPerMillion.PerMillion(int64(uncached)),
		readPrice.PerMillion(int64(u.CacheReadTokens)),
		writePrice.PerMillion(int64(writes-longWrites)),
		longWritePrice.PerMillion(int64(longWrites)),
		p.OutputPerMillion.PerMillion(int64(u.CompletionTokens)),
	)
	return &c, usage.PricingPriced
}

// hasTokenPrice reports whether p has an input or an output price, the
// prices a completion is charged at.
func hasTokenPrice(p provider.Pricing) bool {
	return !p.InputPerMillion.IsZero() || !p.OutputPerMillion.IsZero()
}

// orElse is price, or fallback when price is zero.
func orElse(price, fallback money.USD) money.USD {
	if price.IsZero() {
		return fallback
	}
	return price
}
