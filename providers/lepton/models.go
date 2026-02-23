package lepton

import "github.com/xraph/nexus/provider"

// leptonModels returns the known Lepton AI model catalog.
func leptonModels() []provider.Model {
	return []provider.Model{
		{
			ID: "llama3.1-8b", Provider: "lepton", Name: "Llama 3.1 8B",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.07, OutputPerMillion: 0.07},
		},
		{
			ID: "llama3.1-70b", Provider: "lepton", Name: "Llama 3.1 70B",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.80, OutputPerMillion: 0.80},
		},
	}
}
