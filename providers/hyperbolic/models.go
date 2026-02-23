package hyperbolic

import "github.com/xraph/nexus/provider"

// hyperbolicModels returns the known Hyperbolic model catalog.
func hyperbolicModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/Llama-3.1-8B-Instruct", Provider: "hyperbolic", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.06, OutputPerMillion: 0.06},
		},
		{
			ID: "meta-llama/Llama-3.1-405B-Instruct", Provider: "hyperbolic", Name: "Llama 3.1 405B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 4.00, OutputPerMillion: 4.00},
		},
	}
}
