package anyscale

import "github.com/xraph/nexus/provider"

// anyscaleModels returns the known Anyscale model catalog.
func anyscaleModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/Llama-3-8b-chat-hf", Provider: "anyscale", Name: "Llama 3 8B Chat",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 8192, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.15, OutputPerMillion: 0.15},
		},
		{
			ID: "meta-llama/Llama-3-70b-chat-hf", Provider: "anyscale", Name: "Llama 3 70B Chat",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 8192, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 1.00, OutputPerMillion: 1.00},
		},
	}
}
