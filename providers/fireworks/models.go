package fireworks

import "github.com/xraph/nexus/provider"

// fireworksModels returns the known Fireworks AI model catalog.
func fireworksModels() []provider.Model {
	return []provider.Model{
		{
			ID: "accounts/fireworks/models/llama-v3p1-405b-instruct", Provider: "fireworks", Name: "Llama 3.1 405B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 3.00, OutputPerMillion: 3.00},
		},
		{
			ID: "accounts/fireworks/models/llama-v3p1-8b-instruct", Provider: "fireworks", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.20, OutputPerMillion: 0.20},
		},
		{
			ID: "accounts/fireworks/models/mixtral-8x22b-instruct", Provider: "fireworks", Name: "Mixtral 8x22B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 65536, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.90, OutputPerMillion: 0.90},
		},
	}
}
