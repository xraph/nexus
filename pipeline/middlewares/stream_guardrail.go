package middlewares

import (
	"context"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/pipeline"
)

// StreamGuardrailMiddleware applies guardrails to streaming responses.
type StreamGuardrailMiddleware struct {
	guards   []guard.StreamGuard
	strategy guard.StreamStrategy
}

// NewStreamGuardrail creates middleware that guards streaming responses.
func NewStreamGuardrail(guards []guard.StreamGuard, strategy guard.StreamStrategy) *StreamGuardrailMiddleware {
	return &StreamGuardrailMiddleware{
		guards:   guards,
		strategy: strategy,
	}
}

func (m *StreamGuardrailMiddleware) Name() string  { return "stream_guardrail" }
func (m *StreamGuardrailMiddleware) Priority() int { return 155 } // just after input guardrail

func (m *StreamGuardrailMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	// For non-streaming requests, pass through.
	if req.Completion == nil || !req.Completion.Stream {
		return next(ctx)
	}

	// Let the request proceed and wrap the stream in the response.
	resp, err := next(ctx)
	if err != nil {
		return nil, err
	}

	// If there's a stream, wrap it with guardrails.
	if resp.Stream != nil && len(m.guards) > 0 {
		resp.Stream = guard.NewGuardedStream(resp.Stream, m.guards, m.strategy)
	}

	return resp, nil
}
