package azureopenai

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// azureOpenAIModels returns the known Azure OpenAI model catalog.
// Azure deployments vary, but these are common defaults.
func azureOpenAIModels() []provider.Model {
	return []provider.Model{
		{
			ID: "gpt-4o", Provider: "azureopenai", Name: "GPT-4o",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 16384,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10.00")},
		},
		{
			ID: "gpt-4o-mini", Provider: "azureopenai", Name: "GPT-4o Mini",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 16384,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.15"), OutputPerMillion: money.MustParse("0.60")},
		},
		{
			ID: "text-embedding-3-small", Provider: "azureopenai", Name: "Embedding 3 Small",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8191,
			Pricing:       provider.Pricing{EmbeddingPerMillion: money.MustParse("0.02")},
		},
	}
}
