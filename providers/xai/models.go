package xai

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// xaiModels returns the known xAI model catalog.
func xaiModels() []provider.Model {
	return []provider.Model{
		{
			ID: "grok-2", Provider: "xai", Name: "Grok 2",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.00"), OutputPerMillion: money.MustParse("10.00")},
		},
		{
			ID: "grok-2-mini", Provider: "xai", Name: "Grok 2 Mini",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Vision: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.30"), OutputPerMillion: money.MustParse("0.50")},
		},
	}
}
