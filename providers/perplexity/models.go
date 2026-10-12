package perplexity

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// perplexityModels returns the known Perplexity model catalog.
func perplexityModels() []provider.Model {
	return []provider.Model{
		{
			ID: "sonar-pro", Provider: "perplexity", Name: "Sonar Pro",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true},
			ContextWindow: 127072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("3.00"), OutputPerMillion: money.MustParse("15.00")},
		},
		{
			ID: "sonar", Provider: "perplexity", Name: "Sonar",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true},
			ContextWindow: 127072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("1.00"), OutputPerMillion: money.MustParse("1.00")},
		},
		{
			ID: "sonar-reasoning", Provider: "perplexity", Name: "Sonar Reasoning",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true},
			ContextWindow: 127072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("1.00"), OutputPerMillion: money.MustParse("5.00")},
		},
	}
}
