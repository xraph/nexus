package model

import "github.com/xraph/nexus/provider"

// CostEstimate represents the estimated cost of a request.
type CostEstimate struct {
	InputCost  float64 `json:"input_cost"`  // USD
	OutputCost float64 `json:"output_cost"` // USD
	TotalCost  float64 `json:"total_cost"`  // USD
	Currency   string  `json:"currency"`    // always "USD"
}

// EstimateCost calculates the estimated cost for a request based on
// token usage and model pricing.
func EstimateCost(usage provider.Usage, pricing provider.Pricing) *CostEstimate {
	inputCost := float64(usage.PromptTokens) / 1_000_000 * pricing.InputPerMillion
	outputCost := float64(usage.CompletionTokens) / 1_000_000 * pricing.OutputPerMillion

	return &CostEstimate{
		InputCost:  inputCost,
		OutputCost: outputCost,
		TotalCost:  inputCost + outputCost,
		Currency:   "USD",
	}
}

// EstimateCostFromTokens calculates the estimated cost from raw token counts.
func EstimateCostFromTokens(inputTokens, outputTokens int, pricing provider.Pricing) *CostEstimate {
	inputCost := float64(inputTokens) / 1_000_000 * pricing.InputPerMillion
	outputCost := float64(outputTokens) / 1_000_000 * pricing.OutputPerMillion

	return &CostEstimate{
		InputCost:  inputCost,
		OutputCost: outputCost,
		TotalCost:  inputCost + outputCost,
		Currency:   "USD",
	}
}
