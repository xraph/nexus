package novita

import "github.com/xraph/nexus/provider"

// novitaModels returns the known Novita AI model catalog.
func novitaModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/llama-3.1-8b-instruct", Provider: "novita", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.08, OutputPerMillion: 0.08},
		},
		{
			ID: "meta-llama/llama-3.1-70b-instruct", Provider: "novita", Name: "Llama 3.1 70B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.59, OutputPerMillion: 0.79},
		},
	}
}
