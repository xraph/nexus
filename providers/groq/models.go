package groq

import "github.com/xraph/nexus/provider"

// groqModels returns the known Groq model catalog.
func groqModels() []provider.Model {
	return []provider.Model{
		{
			ID: "llama-3.3-70b-versatile", Provider: "groq", Name: "Llama 3.3 70B Versatile",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 128000, MaxOutput: 32768,
			Pricing: provider.Pricing{InputPerMillion: 0.59, OutputPerMillion: 0.79},
		},
		{
			ID: "llama-3.1-8b-instant", Provider: "groq", Name: "Llama 3.1 8B Instant",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 131072, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 0.05, OutputPerMillion: 0.08},
		},
		{
			ID: "mixtral-8x7b-32768", Provider: "groq", Name: "Mixtral 8x7B",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, Tools: true, JSON: true},
			ContextWindow: 32768, MaxOutput: 32768,
			Pricing: provider.Pricing{InputPerMillion: 0.24, OutputPerMillion: 0.24},
		},
		{
			ID: "gemma2-9b-it", Provider: "groq", Name: "Gemma 2 9B IT",
			Capabilities:  provider.Capabilities{Chat: true, Streaming: true, JSON: true},
			ContextWindow: 8192, MaxOutput: 8192,
			Pricing: provider.Pricing{InputPerMillion: 0.20, OutputPerMillion: 0.20},
		},
	}
}
