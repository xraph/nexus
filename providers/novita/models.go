package novita

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// novitaModels returns the known Novita AI model catalog.
func novitaModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/llama-3.1-8b-instruct", Provider: "novita", Name: "Llama 3.1 8B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.08"), OutputPerMillion: money.MustParse("0.08")},
		},
		{
			ID: "meta-llama/llama-3.1-70b-instruct", Provider: "novita", Name: "Llama 3.1 70B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.59"), OutputPerMillion: money.MustParse("0.79")},
		},
	}
}
