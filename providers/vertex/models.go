package vertex

import "github.com/xraph/nexus/provider"

// vertexModels returns the known Vertex AI model catalog.
func vertexModels() []provider.Model {
	return []provider.Model{
		{
			ID: "gemini-2.0-flash", Provider: "vertex", Name: "Gemini 2.0 Flash",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 1048576, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 0.10, OutputPerMillion: 0.40},
		},
		{
			ID: "gemini-1.5-pro", Provider: "vertex", Name: "Gemini 1.5 Pro",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 2097152, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 1.25, OutputPerMillion: 5.00},
		},
		{
			ID: "text-embedding-004", Provider: "vertex", Name: "Text Embedding 004",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 2048,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.025},
		},
	}
}
