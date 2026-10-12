package sambanova

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// sambanovaModels returns the known SambaNova model catalog.
func sambanovaModels() []provider.Model {
	return []provider.Model{
		{
			ID: "Meta-Llama-3.1-8B-Instruct", Provider: "sambanova", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 4096, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.10"), OutputPerMillion: money.MustParse("0.10")},
		},
		{
			ID: "Meta-Llama-3.1-405B-Instruct", Provider: "sambanova", Name: "Llama 3.1 405B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 4096, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("5.00"), OutputPerMillion: money.MustParse("10.00")},
		},
	}
}
