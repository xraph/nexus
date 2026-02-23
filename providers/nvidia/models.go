package nvidia

import "github.com/xraph/nexus/provider"

// nvidiaModels returns the known NVIDIA NIM model catalog.
func nvidiaModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta/llama-3.1-405b-instruct", Provider: "nvidia", Name: "Llama 3.1 405B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 5.00, OutputPerMillion: 16.00},
		},
		{
			ID: "meta/llama-3.1-8b-instruct", Provider: "nvidia", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.30, OutputPerMillion: 0.50},
		},
		{
			ID: "nvidia/nv-embedqa-e5-v5", Provider: "nvidia", Name: "NV-EmbedQA E5 v5",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 512,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.02},
		},
	}
}
