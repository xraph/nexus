package cerebras

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// cerebrasModels returns the known Cerebras model catalog.
func cerebrasModels() []provider.Model {
	return []provider.Model{
		{
			ID: "llama3.1-8b", Provider: "cerebras", Name: "Llama 3.1 8B",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 8192, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.10"), OutputPerMillion: money.MustParse("0.10")},
		},
		{
			ID: "llama3.1-70b", Provider: "cerebras", Name: "Llama 3.1 70B",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 8192, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.60"), OutputPerMillion: money.MustParse("0.60")},
		},
	}
}
