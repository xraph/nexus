package mistral

import "github.com/xraph/nexus/provider"

// mistralModels returns the known Mistral model catalog.
func mistralModels() []provider.Model {
	return []provider.Model{
		{
			ID: "mistral-large-latest", Provider: "mistral", Name: "Mistral Large",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 2.00, OutputPerMillion: 6.00},
		},
		{
			ID: "mistral-small-latest", Provider: "mistral", Name: "Mistral Small",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 32000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 0.10, OutputPerMillion: 0.30},
		},
		{
			ID: "codestral-latest", Provider: "mistral", Name: "Codestral",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 32000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 0.30, OutputPerMillion: 0.90},
		},
		{
			ID: "mistral-embed", Provider: "mistral", Name: "Mistral Embed",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.10},
		},
		{
			ID: "pixtral-large-latest", Provider: "mistral", Name: "Pixtral Large",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 2.00, OutputPerMillion: 6.00},
		},
	}
}
