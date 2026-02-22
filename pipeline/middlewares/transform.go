package middlewares

import (
	"context"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/transform"
)

// TransformMiddleware applies input/output transforms in the pipeline.
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

	// Apply output transforms
	if resp != nil && resp.Completion != nil && req.Completion != nil {
		if err := m.registry.ApplyOutput(ctx, req.Completion, resp.Completion); err != nil {
			return resp, err
		}
	}

	return resp, nil
}
