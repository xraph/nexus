package deepinfra

import "github.com/xraph/nexus/provider"

// deepinfraModels returns the known Deepinfra model catalog.
func deepinfraModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/Meta-Llama-3.1-405B-Instruct", Provider: "deepinfra", Name: "Llama 3.1 405B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 1.79, OutputPerMillion: 1.79},
		},
		{
			ID: "meta-llama/Meta-Llama-3.1-8B-Instruct", Provider: "deepinfra", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.06, OutputPerMillion: 0.06},
		},
		{
			ID: "BAAI/bge-large-en-v1.5", Provider: "deepinfra", Name: "BGE Large EN v1.5",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 512,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.01},
		},
	}
}
