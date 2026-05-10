package middlewares

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/transform"
)

// TransformMiddleware applies input/output transforms in the pipeline.
//
// For streaming responses, registered StreamingOutputTransform implementations
// are applied per-chunk by wrapping resp.Stream. The same transforms also
// run TransformAccumulated once when the stream closes, so post-hoc audit
// signal still gets the merged response. Non-streaming responses go through
// TransformOutput on the merged CompletionResponse as before.
type TransformMiddleware struct {
	registry *transform.Registry
}

// NewTransform creates a transform execution middleware.
func NewTransform(registry *transform.Registry) *TransformMiddleware {
	return &TransformMiddleware{registry: registry}
}

func (m *TransformMiddleware) Name() string  { return "transform" }
func (m *TransformMiddleware) Priority() int { return 200 } // Before routing, after guardrails

func (m *TransformMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.registry == nil {
		return next(ctx)
	}

	// Apply input transforms
	if req.Completion != nil {
		if err := m.registry.ApplyInput(ctx, req.Completion); err != nil {
			return nil, err
		}
	}

	// Continue pipeline
	resp, err := next(ctx)
	if err != nil {
		return resp, err
	}

	// Apply output transforms — branch on stream vs completion.
	if resp != nil && resp.Stream != nil && req.Completion != nil {
		streaming := m.registry.StreamingOutputs()
		if len(streaming) > 0 {
			resp.Stream = &streamingTransformer{
				inner:      resp.Stream,
				transforms: streaming,
				ctx:        ctx,
				req:        req.Completion,
				acc:        provider.NewAccumulator(),
			}
		}
		return resp, nil
	}

	if resp != nil && resp.Completion != nil && req.Completion != nil {
		if err := m.registry.ApplyOutput(ctx, req.Completion, resp.Completion); err != nil {
			return resp, err
		}
	}

	return resp, nil
}

// streamingTransformer applies StreamingOutputTransform.TransformChunk to
// every chunk passing through, and TransformAccumulated once on Close.
type streamingTransformer struct {
	inner      provider.Stream
	transforms []transform.StreamingOutputTransform
	ctx        context.Context
	req        *provider.CompletionRequest
	acc        *provider.Accumulator

	once sync.Once
}

func (s *streamingTransformer) Next(ctx context.Context) (*provider.StreamChunk, error) {
	for {
		chunk, err := s.inner.Next(ctx)
		if errors.Is(err, io.EOF) {
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			continue
		}

		current := chunk
		dropped := false
		for _, t := range s.transforms {
			next, err := t.TransformChunk(s.ctx, s.req, current)
			if err != nil {
				return nil, err
			}
			if next == nil {
				dropped = true
				break
			}
			current = next
		}
		if dropped {
			continue
		}
		s.acc.Add(current)
		return current, nil
	}
}

func (s *streamingTransformer) Close() error {
	s.once.Do(func() {
		final := s.acc.Finalize(s.inner.Usage)
		for _, t := range s.transforms {
			if err := t.TransformAccumulated(s.ctx, s.req, final); err != nil {
				_ = err // post-hoc errors don't propagate; deliberately swallowed
			}
		}
	})
	return s.inner.Close()
}

func (s *streamingTransformer) Usage() *provider.Usage { return s.inner.Usage() }
