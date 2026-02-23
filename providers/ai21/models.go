package ai21

import "github.com/xraph/nexus/provider"

// ai21Models returns the known AI21 model catalog.
func ai21Models() []provider.Model {
	return []provider.Model{
		{
			ID: "jamba-1.5-large", Provider: "ai21", Name: "Jamba 1.5 Large",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 256000, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 2.00, OutputPerMillion: 8.00},
		},
		{
			ID: "jamba-1.5-mini", Provider: "ai21", Name: "Jamba 1.5 Mini",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 256000, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.20, OutputPerMillion: 0.40},
		},
	}
}
