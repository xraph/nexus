package deepseek

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// deepseekModels returns the known DeepSeek model catalog.
func deepseekModels() []provider.Model {
	return []provider.Model{
		{
			ID: "deepseek-chat", Provider: "deepseek", Name: "DeepSeek Chat (V3)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 65536, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.27"), OutputPerMillion: money.MustParse("1.10")},
		},
		{
			ID: "deepseek-reasoner", Provider: "deepseek", Name: "DeepSeek Reasoner (R1)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Thinking: true},
			ContextWindow: 65536, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.55"), OutputPerMillion: money.MustParse("2.19")},
		},
	}
}
