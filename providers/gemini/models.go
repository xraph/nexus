package gemini

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// geminiModels returns the known Google Gemini model catalog.
func geminiModels() []provider.Model {
	return []provider.Model{
		{
			ID: "gemini-2.0-flash", Provider: "gemini", Name: "Gemini 2.0 Flash",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 1048576, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.10"), OutputPerMillion: money.MustParse("0.40")},
		},
		{
			ID: "gemini-2.0-flash-lite", Provider: "gemini", Name: "Gemini 2.0 Flash Lite",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, JSON: true},
			ContextWindow: 1048576, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.075"), OutputPerMillion: money.MustParse("0.30")},
		},
		{
			ID: "gemini-1.5-pro", Provider: "gemini", Name: "Gemini 1.5 Pro",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 2097152, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("1.25"), OutputPerMillion: money.MustParse("5.00")},
		},
		{
			ID: "gemini-1.5-flash", Provider: "gemini", Name: "Gemini 1.5 Flash",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 1048576, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.075"), OutputPerMillion: money.MustParse("0.30")},
		},
		{
			ID: "text-embedding-004", Provider: "gemini", Name: "Text Embedding 004",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 2048,
			Pricing:       provider.Pricing{EmbeddingPerMillion: money.MustParse("0.025")},
		},
	}
}
