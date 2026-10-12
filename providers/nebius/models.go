package nebius

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// nebiusModels returns the known Nebius model catalog.
func nebiusModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/Meta-Llama-3.1-8B-Instruct", Provider: "nebius", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.08"), OutputPerMillion: money.MustParse("0.08")},
		},
		{
			ID: "meta-llama/Meta-Llama-3.1-70B-Instruct", Provider: "nebius", Name: "Llama 3.1 70B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.50"), OutputPerMillion: money.MustParse("0.50")},
		},
		{
			ID: "BAAI/bge-m3", Provider: "nebius", Name: "BGE-M3",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: money.MustParse("0.005")},
		},
	}
}
