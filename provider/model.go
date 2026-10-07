package provider

import "github.com/xraph/nexus/money"

// Model describes an available LLM model.
type Model struct {
	ID            string       `json:"id"`       // e.g., "gpt-4o"
	Provider      string       `json:"provider"` // e.g., "openai"
	Name          string       `json:"name"`     // human-readable
	Capabilities  Capabilities `json:"capabilities"`
	ContextWindow int          `json:"context_window"`
	MaxOutput     int          `json:"max_output"`
	Pricing       Pricing      `json:"pricing"`
}

// Pricing is a model's list price per million tokens. A zero price means the
// model has no price for that kind of token, not that the tokens are free.
type Pricing struct {
	InputPerMillion     money.USD `json:"input_per_million"`
	OutputPerMillion    money.USD `json:"output_per_million"`
	EmbeddingPerMillion money.USD `json:"embedding_per_million,omitzero"`
}
