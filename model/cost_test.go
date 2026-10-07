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
