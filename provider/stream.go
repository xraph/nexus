package provider

import "context"

// Stream represents a streaming response from a provider.
type Stream interface {
	// Next returns the next chunk. Returns io.EOF when done.
	Next(ctx context.Context) (*StreamChunk, error)

	// Close releases resources.
	Close() error

	// Usage returns final usage after stream completes.
	Usage() *Usage
}

// StreamChunk is a single piece of a streamed response.
type StreamChunk struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

// Delta contains the incremental content in a stream chunk.
type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
