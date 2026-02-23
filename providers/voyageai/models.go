package voyageai

import "github.com/xraph/nexus/provider"

// voyageAIModels returns the known Voyage AI model catalog.
func voyageAIModels() []provider.Model {
	return []provider.Model{
		{
			ID: "voyage-3", Provider: "voyageai", Name: "Voyage 3",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 32000,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.06},
		},
		{
			ID: "voyage-3-lite", Provider: "voyageai", Name: "Voyage 3 Lite",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 32000,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.02},
		},
		{
			ID: "voyage-code-3", Provider: "voyageai", Name: "Voyage Code 3",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 32000,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.06},
		},
		{
			ID: "voyage-finance-2", Provider: "voyageai", Name: "Voyage Finance 2",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 32000,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.06},
		},
	}
}
