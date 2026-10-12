package bedrock

import (
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
)

// bedrockModels returns the known Bedrock model catalog.
func bedrockModels() []provider.Model {
	return []provider.Model{
		{
			ID: "anthropic.claude-3-5-sonnet-20241022-v2:0", Provider: "bedrock", Name: "Claude 3.5 Sonnet (Bedrock)",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 200000, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("3.00"), OutputPerMillion: money.MustParse("15.00")},
		},
		{
			ID: "meta.llama3-1-70b-instruct-v1:0", Provider: "bedrock", Name: "Llama 3.1 70B Instruct",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.65"), OutputPerMillion: money.MustParse("3.50")},
		},
		{
			ID: "amazon.titan-text-express-v1", Provider: "bedrock", Name: "Titan Text Express",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 8192, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: money.MustParse("0.20"), OutputPerMillion: money.MustParse("0.60")},
		},
	}
}
