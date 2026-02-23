package lmstudio

import "github.com/xraph/nexus/provider"

// lmStudioModels returns common LM Studio models.
// Models are local so pricing is effectively zero.
func lmStudioModels() []provider.Model {
	return []provider.Model{
		{
			ID: "lmstudio-community/Meta-Llama-3.1-8B-Instruct-GGUF", Provider: "lmstudio", Name: "Llama 3.1 8B (LM Studio)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.00001, OutputPerMillion: 0.00001},
		},
		{
			ID: "lmstudio-community/Mistral-7B-Instruct-v0.3-GGUF", Provider: "lmstudio", Name: "Mistral 7B v0.3 (LM Studio)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 32768, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.00001, OutputPerMillion: 0.00001},
		},
		{
			ID: "nomic-ai/nomic-embed-text-v1.5-GGUF", Provider: "lmstudio", Name: "Nomic Embed Text (LM Studio)",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.00001},
		},
	}
}
