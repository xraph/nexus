package model_test

import (
	"testing"

	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

func TestCostIsExactListPriceOverReportedTokens(t *testing.T) {
	p := provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10.00")}
	cost, status := model.Cost(provider.Usage{PromptTokens: 1234, CompletionTokens: 567}, p, false)
	if status != usage.PricingPriced || cost == nil {
		t.Fatalf("status = %s, cost = %v", status, cost)
	}
	// 1234 × 2.50 / 1e6 + 567 × 10 / 1e6 = 0.003085 + 0.00567
	if cost.String() != "0.008755" {
		t.Fatalf("cost = %s, want 0.008755", cost)
	}
}

func TestCostOfAModelWithNoPriceIsUnknownNotFree(t *testing.T) {
	cost, status := model.Cost(provider.Usage{PromptTokens: 10, CompletionTokens: 10}, provider.Pricing{}, false)
	if cost != nil || status != usage.PricingUnpricedModel {
		t.Fatalf("cost = %v, status = %s; want nil and unpriced_model", cost, status)
	}
}

func TestEmbeddingCostUsesTheEmbeddingPrice(t *testing.T) {
	p := provider.Pricing{EmbeddingPerMillion: money.MustParse("0.02")}
	cost, status := model.Cost(provider.Usage{PromptTokens: 5000}, p, true)
	if status != usage.PricingPriced || cost == nil || cost.String() != "0.0001" {
		t.Fatalf("cost = %v, status = %s; want 0.0001 priced", cost, status)
	}
	if cost, status := model.Cost(provider.Usage{PromptTokens: 5000}, provider.Pricing{InputPerMillion: money.MustParse("1")}, true); cost != nil || status != usage.PricingUnpricedModel {
		t.Fatalf("an embedding on a chat-only price list should be unpriced, got %v %s", cost, status)
	}
}

func TestFreeModelsCostExactlyZero(t *testing.T) {
	for _, embedding := range []bool{false, true} {
		cost, status := model.Cost(provider.Usage{PromptTokens: 5000, CompletionTokens: 7000}, provider.Pricing{Free: true}, embedding)
		if status != usage.PricingPriced || cost == nil || !cost.IsZero() {
			t.Fatalf("embedding=%v: cost %v, status %s; want exactly 0, priced", embedding, cost, status)
		}
	}
}

func TestAnEmbeddingThatReportsOnlyATotalIsPricedFromIt(t *testing.T) {
	p := provider.Pricing{EmbeddingPerMillion: money.MustParse("0.02")}
	cost, status := model.Cost(provider.Usage{TotalTokens: 5000}, p, true)
	if status != usage.PricingPriced || cost == nil || cost.String() != "0.0001" {
		t.Fatalf("cost = %v, status = %s; want 0.0001 priced", cost, status)
	}
}

func TestCostPricesCacheReadsAndWrites(t *testing.T) {
	p := provider.Pricing{
		InputPerMillion:          money.MustParse("4"),
		OutputPerMillion:         money.MustParse("20"),
		CacheReadPerMillion:      money.MustParse("0.20"),
		CacheWritePerMillion:     money.MustParse("5"),
		CacheWriteLongPerMillion: money.MustParse("8"),
	}
	u := provider.Usage{PromptTokens: 1_000_000, CacheReadTokens: 600_000, CacheWriteTokens: 300_000, CacheWriteLongTokens: 100_000, CompletionTokens: 10_000}
	cost, status := model.Cost(u, p, false)
	// 0.1M uncached at 4, 0.6M reads at 0.20, 0.2M 5-minute writes at 5,
	// 0.1M 1-hour writes at 8, 0.01M output at 20.
	if status != usage.PricingPriced || cost == nil || !cost.Equal(money.MustParse("2.52")) {
		t.Fatalf("cost = %v, status = %s; want 2.52 priced", cost, status)
	}
}

func TestCostChargesCacheTokensAtInputWhenNoCachePrice(t *testing.T) {
	p := provider.Pricing{InputPerMillion: money.MustParse("2"), OutputPerMillion: money.MustParse("10")}
	cost, status := model.Cost(provider.Usage{PromptTokens: 1_000_000, CacheReadTokens: 500_000}, p, false)
	if status != usage.PricingPriced || cost == nil || !cost.Equal(money.MustParse("2")) {
		t.Fatalf("cost = %v, status = %s; want 2 priced", cost, status)
	}
	// A 1-hour write with no price of its own falls back to the 5-minute
	// write price, then to the input price.
	p.CacheWritePerMillion = money.MustParse("3")
	cost, _ = model.Cost(provider.Usage{PromptTokens: 1_000_000, CacheWriteTokens: 1_000_000, CacheWriteLongTokens: 1_000_000}, p, false)
	if cost == nil || !cost.Equal(money.MustParse("3")) {
		t.Fatalf("1-hour write without its own price = %v, want 3", cost)
	}
}

func TestCostUsesTheLongContextTier(t *testing.T) {
	p := provider.Pricing{
		InputPerMillion:  money.MustParse("0.10"),
		OutputPerMillion: money.MustParse("0.50"),
		LongContextAbove: 100_000,
		LongContext: &provider.Pricing{
			InputPerMillion:     money.MustParse("0.50"),
			OutputPerMillion:    money.MustParse("2.50"),
			CacheReadPerMillion: money.MustParse("0.05"),
		},
	}
	// At the threshold: the base price. 100_000 × 0.10 + 1_000 × 0.50.
	cost, status := model.Cost(provider.Usage{PromptTokens: 100_000, CompletionTokens: 1_000}, p, false)
	if status != usage.PricingPriced || cost == nil || !cost.Equal(money.MustParse("0.0105")) {
		t.Fatalf("at the threshold: cost = %v, status = %s; want 0.0105", cost, status)
	}
	// One token over: every token at the tier, cache reads included.
	// 50_001 × 0.50 + 50_000 × 0.05 + 1_000 × 2.50.
	cost, status = model.Cost(provider.Usage{PromptTokens: 100_001, CacheReadTokens: 50_000, CompletionTokens: 1_000}, p, false)
	if status != usage.PricingPriced || cost == nil || !cost.Equal(money.MustParse("0.0300005")) {
		t.Fatalf("over the threshold: cost = %v, status = %s; want 0.0300005", cost, status)
	}
}

func TestCostFallsBackToTheBaseTierWhenTheTierHasNoPrice(t *testing.T) {
	p := provider.Pricing{
		InputPerMillion:  money.MustParse("0.10"),
		OutputPerMillion: money.MustParse("0.50"),
		LongContextAbove: 100_000,
		LongContext:      &provider.Pricing{CacheReadPerMillion: money.MustParse("0.05")},
	}
	// Over the line, but the tier has no input or output price: the base
	// price. 200_000 × 0.10 + 1_000 × 0.50.
	cost, status := model.Cost(provider.Usage{PromptTokens: 200_000, CompletionTokens: 1_000}, p, false)
	if status != usage.PricingPriced || cost == nil || !cost.Equal(money.MustParse("0.0205")) {
		t.Fatalf("over the line with an unpriced tier: cost = %v, status = %s; want 0.0205 priced", cost, status)
	}
}
