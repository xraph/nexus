// Package transform provides request/response transformation middleware for Nexus.
// Transforms modify requests before they reach providers and responses after
// they come back — system prompt injection, RAG context, output normalization,
// data anonymization, etc.
package transform

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Phase indicates when a transform runs.
type Phase string

const (
	// PhaseInput transforms run before the provider call.
	PhaseInput Phase = "input"

	// PhaseOutput transforms run after the provider call.
	PhaseOutput Phase = "output"
)

// Transform modifies requests or responses.
type Transform interface {
	// Name returns a unique identifier for this transform.
	Name() string

	// Phase returns whether this runs on input, output, or both.
	Phase() Phase
}

// InputTransform modifies a request before it reaches a provider.
type InputTransform interface {
	Transform
	// TransformInput modifies the request in place.
	TransformInput(ctx context.Context, req *provider.CompletionRequest) error
}

// OutputTransform modifies a response after it comes back from a provider.
type OutputTransform interface {
	Transform
	// TransformOutput modifies the response in place.
	TransformOutput(ctx context.Context, req *provider.CompletionRequest, resp *provider.CompletionResponse) error
}

// StreamingOutputTransform extends OutputTransform with per-chunk and
// post-stream hooks. The middleware applies streaming transforms only when
// resp.Stream is non-nil; non-streaming responses still go through
// TransformOutput on the merged response.
//
// TransformChunk runs for every chunk that arrives. It may:
//   - Return the chunk unchanged.
//   - Return a modified copy.
//   - Return nil to drop the chunk silently.
//   - Return an error to abort the stream.
//
// TransformAccumulated runs once on Close with the merged final response.
// Use it for post-hoc audit signal or final-state mutation.
type StreamingOutputTransform interface {
	OutputTransform
	TransformChunk(ctx context.Context, req *provider.CompletionRequest, chunk *provider.StreamChunk) (*provider.StreamChunk, error)
	TransformAccumulated(ctx context.Context, req *provider.CompletionRequest, resp *provider.CompletionResponse) error
}

// Registry manages transforms.
type Registry struct {
	input            []InputTransform
	output           []OutputTransform
	streamingOutputs []StreamingOutputTransform
}

// NewRegistry creates a new transform registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a transform. It is type-asserted into input/output and
// (optionally) streaming-output lists.
func (r *Registry) Register(t Transform) {
	if it, ok := t.(InputTransform); ok {
		r.input = append(r.input, it)
	}
	if ot, ok := t.(OutputTransform); ok {
		r.output = append(r.output, ot)
	}
	if st, ok := t.(StreamingOutputTransform); ok {
		r.streamingOutputs = append(r.streamingOutputs, st)
	}
}

// ApplyInput runs all input transforms in order.
func (r *Registry) ApplyInput(ctx context.Context, req *provider.CompletionRequest) error {
	for _, t := range r.input {
		if err := t.TransformInput(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// ApplyOutput runs all output transforms in order.
func (r *Registry) ApplyOutput(ctx context.Context, req *provider.CompletionRequest, resp *provider.CompletionResponse) error {
	for _, t := range r.output {
		if err := t.TransformOutput(ctx, req, resp); err != nil {
			return err
		}
	}
	return nil
}

// InputTransforms returns registered input transforms.
func (r *Registry) InputTransforms() []InputTransform { return r.input }

// OutputTransforms returns registered output transforms.
func (r *Registry) OutputTransforms() []OutputTransform { return r.output }

// StreamingOutputs returns registered streaming output transforms.
func (r *Registry) StreamingOutputs() []StreamingOutputTransform { return r.streamingOutputs }

// Count returns total number of registered transforms.
func (r *Registry) Count() int { return len(r.input) + len(r.output) }
