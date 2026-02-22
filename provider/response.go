package provider

import "time"

// CompletionResponse is the unified response type.
type CompletionResponse struct {
	ID       string    `json:"id"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Created  time.Time `json:"created"`

	// Content
	Choices []Choice `json:"choices"`

	// Usage
	Usage Usage `json:"usage"`

	// Nexus metadata
	Cached  bool          `json:"cached,omitempty"`
	Latency time.Duration `json:"latency,omitempty"`
	Cost    float64       `json:"cost,omitempty"` // estimated cost in USD

	// Extended thinking
	ThinkingContent string `json:"thinking_content,omitempty"`
	ThinkingTokens  int    `json:"thinking_tokens,omitempty"`

	// State is used to pass metadata between middleware layers.
	State map[string]any `json:"-"`
}

// Choice represents one completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	ThinkingTokens   int `json:"thinking_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens"`
}

// EmbeddingResponse for embeddings.
type EmbeddingResponse struct {
	Provider   string      `json:"provider"`
	Model      string      `json:"model"`
	Embeddings [][]float64 `json:"embeddings"`
	Usage      Usage       `json:"usage"`
}
