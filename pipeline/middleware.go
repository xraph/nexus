package pipeline

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Middleware processes requests in the pipeline.
// Call next(ctx) to continue the chain, or return early to short-circuit.
type Middleware interface {
	// Name returns a unique identifier for this middleware.
	Name() string

	// Priority returns execution order (lower = earlier). Suggested ranges:
	//   0-99:    Auth, rate limiting, budget
	//   100-199: Guardrails (input)
	//   200-299: Cache, transform
	//   300-399: Routing, provider call (core)
	//   400-499: Guardrails (output), transform
	//   500-599: Usage, audit, metrics
	Priority() int

	// Process handles the request. Call next(ctx) to continue.
	Process(ctx context.Context, req *Request, next NextFunc) (*Response, error)
}

// NextFunc calls the next middleware in the chain.
type NextFunc func(ctx context.Context) (*Response, error)

// Request wraps the unified request with pipeline metadata.
type Request struct {
	Completion *provider.CompletionRequest
	Embedding  *provider.EmbeddingRequest
	Type       RequestType // "completion", "stream", "embedding"

	// Mutable state middleware can read/write.
	State map[string]any
}

// RequestType identifies the type of request being processed.
type RequestType string

const (
	RequestCompletion RequestType = "completion"
	RequestStream     RequestType = "stream"
	RequestEmbedding  RequestType = "embedding"
)

// Response wraps the unified response.
type Response struct {
	Completion *provider.CompletionResponse
	Stream     provider.Stream
	Embedding  *provider.EmbeddingResponse
}
