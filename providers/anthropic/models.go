package anthropic

import "github.com/xraph/nexus/provider"

// anthropicModels returns the known Anthropic model catalog.
func anthropicModels() []provider.Model {
	return []provider.Model{
		{
			ID: "claude-sonnet-4-5-20250514", Provider: "anthropic", Name: "Claude Sonnet 4.5",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true, Thinking: true},
			ContextWindow: 200000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 3.00, OutputPerMillion: 15.00},
		},
		{
			ID: "claude-opus-4-5-20250630", Provider: "anthropic", Name: "Claude Opus 4.5",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true, Thinking: true},
			ContextWindow: 200000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 15.00, OutputPerMillion: 75.00},
		},
		{
			ID: "claude-3-5-haiku-20241022", Provider: "anthropic", Name: "Claude 3.5 Haiku",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 200000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 0.80, OutputPerMillion: 4.00},
		},
		{
			ID: "claude-3-5-sonnet-20241022", Provider: "anthropic", Name: "Claude 3.5 Sonnet",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true, Thinking: true},
			ContextWindow: 200000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 3.00, OutputPerMillion: 15.00},
		},
	}
}
