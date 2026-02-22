package provider

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

// Pricing per million tokens in USD.
type Pricing struct {
	InputPerMillion     float64 `json:"input_per_million"`
	OutputPerMillion    float64 `json:"output_per_million"`
	EmbeddingPerMillion float64 `json:"embedding_per_million,omitempty"`
}
