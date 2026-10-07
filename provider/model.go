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

// Pricing is a model's list price per million tokens. When Free is set the
// model costs exactly $0. Otherwise a model with both InputPerMillion and
// OutputPerMillion at zero is unpriced (its cost is unknown, never $0); when
// only one of them is set, the other kind of token is charged at $0.
// Embeddings use EmbeddingPerMillion.
type Pricing struct {
	InputPerMillion     money.USD `json:"input_per_million"`
	OutputPerMillion    money.USD `json:"output_per_million"`
	EmbeddingPerMillion money.USD `json:"embedding_per_million,omitzero"`
	Free                bool      `json:"free,omitempty"`
}

// FreeOfCharge is implemented by a provider whose every model, listed or
// not, costs exactly $0, such as local inference.
type FreeOfCharge interface {
	FreeOfCharge() bool
}
