package nexus

// Re-export convenience types for top-level access.
// These allow users to avoid importing sub-packages for common types.

import (
	"github.com/xraph/nexus/provider"
)

// Common type aliases for convenience.
type (
	// CompletionRequest is an alias for provider.CompletionRequest.
	CompletionRequest = provider.CompletionRequest

	// CompletionResponse is an alias for provider.CompletionResponse.
	CompletionResponse = provider.CompletionResponse

	// Message is an alias for provider.Message.
	Message = provider.Message

	// Stream is an alias for provider.Stream.
	Stream = provider.Stream
)
