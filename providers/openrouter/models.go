package openrouter

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// openRouterModels returns a representative subset of OpenRouter models.
// OpenRouter provides access to models from many providers; these are the most popular.
func openRouterModels() []provider.Model {
	return []provider.Model{
		{
			ID: "openai/gpt-4o", Provider: "openrouter", Name: "GPT-4o (via OpenRouter)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 16384,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10.00")},
		},
		{
			ID: "anthropic/claude-3.5-sonnet", Provider: "openrouter", Name: "Claude 3.5 Sonnet (via OpenRouter)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 200000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("3.00"), OutputPerMillion: money.MustParse("15.00")},
		},
		{
			ID: "meta-llama/llama-3.1-405b-instruct", Provider: "openrouter", Name: "Llama 3.1 405B (via OpenRouter)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.70"), OutputPerMillion: money.MustParse("2.70")},
		},
		{
			ID: "google/gemini-2.0-flash-001", Provider: "openrouter", Name: "Gemini 2.0 Flash (via OpenRouter)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 1048576, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.10"), OutputPerMillion: money.MustParse("0.40")},
		},
	}
}
