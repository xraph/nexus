// Package provider defines the unified interface for LLM providers.
package provider

import "context"

// Provider abstracts an LLM provider (OpenAI, Anthropic, Ollama, etc.)
type Provider interface {
	// Name returns the provider identifier (e.g., "openai", "anthropic").
	Name() string

	// Capabilities returns what this provider supports.
	Capabilities() Capabilities

	// Models returns the list of available models.
	Models(ctx context.Context) ([]Model, error)

	// Complete sends a chat completion request.
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// CompleteStream sends a streaming chat completion request.
	CompleteStream(ctx context.Context, req *CompletionRequest) (Stream, error)

	// Embed sends an embedding request (if supported).
	Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)

	// Healthy returns true if the provider is reachable.
	Healthy(ctx context.Context) bool
}
