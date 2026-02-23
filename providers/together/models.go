package together

import "github.com/xraph/nexus/provider"

// togetherModels returns the known Together AI model catalog.
func togetherModels() []provider.Model {
	return []provider.Model{
		{
			ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", Provider: "together", Name: "Llama 3.3 70B Instruct Turbo",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.88, OutputPerMillion: 0.88},
		},
		{
			ID: "meta-llama/Llama-3.1-405B-Instruct-Turbo", Provider: "together", Name: "Llama 3.1 405B Instruct Turbo",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 130815, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 3.50, OutputPerMillion: 3.50},
		},
		{
			ID: "meta-llama/Llama-3.1-8B-Instruct-Turbo", Provider: "together", Name: "Llama 3.1 8B Instruct Turbo",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 4096,
			Pricing: provider.Pricing{InputPerMillion: 0.18, OutputPerMillion: 0.18},
		},
		{
			ID: "togethercomputer/m2-bert-80M-8k-retrieval", Provider: "together", Name: "M2 BERT 80M 8K Retrieval",
			Capabilities:  provider.Capabilities{Embeddings: true},
			ContextWindow: 8192,
			Pricing:       provider.Pricing{EmbeddingPerMillion: 0.008},
		},
	}
}
