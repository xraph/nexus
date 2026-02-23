package cohere

import "github.com/xraph/nexus/provider"

// cohereModels returns the known Cohere model catalog.
func cohereModels() []provider.Model {
	return []provider.Model{
		{
			ID: "command-r-plus", Provider: "cohere", Name: "Command R+",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 2.50, OutputPerMillion: 10.00},
		},
		{
			ID: "command-r", Provider: "cohere", Name: "Command R",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.15, OutputPerMillion: 0.60},
		},
		{
			ID: "command-light", Provider: "cohere", Name: "Command Light",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true},
			ContextWindow: 4096, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.30, OutputPerMillion: 0.60},
		},
		{
			ID: "embed-v4.0", Provider: "cohere", Name: "Embed v4.0",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 512,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.10},
		},
		{
			ID: "embed-multilingual-v3.0", Provider: "cohere", Name: "Embed Multilingual v3.0",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 512,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.10},
		},
	}
}
