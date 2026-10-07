package pipeline

import (
	"context"
	"errors"

	"github.com/xraph/nexus/provider"
)

// Middleware processes requests in the pipeline.
// Call next(ctx) to continue the chain, or return early to short-circuit.
type Middleware interface {
	// Name returns a unique identifier for this middleware.
	Name() string

	// Priority returns execution order among non-terminal middleware
	// (lower = earlier = further out). The terminal always runs last.
	// Built-in bands:
	//   0-19:    request id, tracing, usage (they wrap everything)
	//   20-99:   timeout, identity, access, quota, stream lifecycle
	//   100-199: guardrails
	//   200-299: transform, alias, cache
	//   300-399: retry; custom middleware here runs once per attempt
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

// Terminal marks the middleware that ends the chain by calling a provider.
// A pipeline has exactly one, and it always runs last whatever its
// priority, so a stage that wraps the call (usage, tracing, retry, custom
// middleware) can never be sorted behind it and silently skipped.
type Terminal interface {
	Terminal()
}

// Stage describes one stage of a built pipeline.
type Stage struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Terminal bool   `json:"terminal"`
}

// Inspector is implemented by a pipeline that can list its stages in the
// order they run.
type Inspector interface {
	Stages() []Stage
}

var (
	// ErrNoTerminal reports a pipeline with nothing that calls a provider.
	ErrNoTerminal = errors.New("nexus: pipeline has no terminal stage")
	// ErrManyTerminals reports a pipeline with more than one terminal stage.
	ErrManyTerminals = errors.New("nexus: pipeline has more than one terminal stage")
)
