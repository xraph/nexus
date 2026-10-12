package openai

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// openAIModels returns the known OpenAI model catalog.
func openAIModels() []provider.Model {
	return []provider.Model{
		{
			ID: "gpt-4o", Provider: "openai", Name: "GPT-4o",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Embeddings: false, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 16384,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10.00")},
		},
		{
			ID: "gpt-4o-mini", Provider: "openai", Name: "GPT-4o Mini",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Embeddings: false, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 16384,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.15"), OutputPerMillion: money.MustParse("0.60")},
		},
		{
			ID: "gpt-4-turbo", Provider: "openai", Name: "GPT-4 Turbo",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Embeddings: false, Vision: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("10.00"), OutputPerMillion: money.MustParse("30.00")},
		},
		{
			ID: "o1", Provider: "openai", Name: "o1",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Thinking: true, Tools: true, JSON: true},
			ContextWindow: 200000, MaxOutput: 100000,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("15.00"), OutputPerMillion: money.MustParse("60.00")},
		},
		{
			ID: "o1-mini", Provider: "openai", Name: "o1-mini",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Thinking: true},
			ContextWindow: 128000, MaxOutput: 65536,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("3.00"), OutputPerMillion: money.MustParse("12.00")},
		},
		{
			ID: "text-embedding-3-small", Provider: "openai", Name: "Embedding 3 Small",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8191,
			Pricing:       provider.Pricing{EmbeddingPerMillion: money.MustParse("0.02")},
		},
		{
			ID: "text-embedding-3-large", Provider: "openai", Name: "Embedding 3 Large",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8191,
			Pricing:       provider.Pricing{EmbeddingPerMillion: money.MustParse("0.13")},
		},
	}
}
