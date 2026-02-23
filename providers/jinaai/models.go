package jinaai

import "github.com/xraph/nexus/provider"

// jinaAIModels returns the known Jina AI model catalog.
func jinaAIModels() []provider.Model {
	return []provider.Model{
		{
			ID: "jina-embeddings-v3", Provider: "jinaai", Name: "Jina Embeddings v3",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.02},
		},
		{
			ID: "jina-clip-v2", Provider: "jinaai", Name: "Jina CLIP v2",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.02},
		},
	}
}
