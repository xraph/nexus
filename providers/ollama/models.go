package ollama

import "github.com/xraph/nexus/provider"

// ollamaModels returns common default Ollama models.
// Ollama models are local so pricing is effectively zero.
func ollamaModels() []provider.Model {
	return []provider.Model{
		{
			ID: "llama3.1:8b", Provider: "ollama", Name: "Llama 3.1 8B (local)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.00001, OutputPerMillion: 0.00001},
		},
		{
			ID: "llama3.1:70b", Provider: "ollama", Name: "Llama 3.1 70B (local)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.00001, OutputPerMillion: 0.00001},
		},
		{
			ID: "mistral:7b", Provider: "ollama", Name: "Mistral 7B (local)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 32768, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.00001, OutputPerMillion: 0.00001},
		},
		{
			ID: "nomic-embed-text", Provider: "ollama", Name: "Nomic Embed Text (local)",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.00001},
		},
	}
}
