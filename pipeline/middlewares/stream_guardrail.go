package middlewares

import (
	"context"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/pipeline"
)

// WarnLogger receives the stream guard stage's warnings. nexus.Logger
// implements it.
type WarnLogger interface {
	Warn(msg string, args ...any)
}

// StreamGuardrailMiddleware runs output guards over streamed responses with
// guard.NewOutputGuardedStream: the output-phase guards of a guard service
// on the whole message, and stream guards on each chunk. It acts on stream
// requests (pipeline.RequestStream). The check at the end of the stream gets
// a "check guardrails" span like the guardrail stage's.
type StreamGuardrailMiddleware struct {
	guards   []guard.StreamGuard
	strategy guard.StreamStrategy
	svc      guard.Service
	log      WarnLogger
}

// NewStreamGuardrail creates the stage with stream guards that check each
// chunk. An empty or unknown strategy is guard.StrategyBuffer. Add the
// output-phase guards with WithOutputGuards.
func NewStreamGuardrail(guards []guard.StreamGuard, strategy guard.StreamStrategy) *StreamGuardrailMiddleware {
	return &StreamGuardrailMiddleware{
		guards:   guards,
		strategy: strategy,
	}
}

// WithOutputGuards runs svc's output-phase guards on each streamed message.
func (m *StreamGuardrailMiddleware) WithOutputGuards(svc guard.Service) *StreamGuardrailMiddleware {
	m.svc = svc
	return m
}

// WithLogger sets where the stage warns that a guard rewrote a stream it
// could not change any more (guard.StrategyPassthrough and
// guard.StrategyChunkwise).
func (m *StreamGuardrailMiddleware) WithLogger(log WarnLogger) *StreamGuardrailMiddleware {
	m.log = log
	return m
}

func (m *StreamGuardrailMiddleware) Name() string  { return "stream_guardrail" }
func (m *StreamGuardrailMiddleware) Priority() int { return 155 } // just after input guardrail

func (m *StreamGuardrailMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if req.Type != pipeline.RequestStream || req.Completion == nil || (m.svc == nil && len(m.guards) == 0) {
		return next(ctx)
	}

	resp, err := next(ctx)
	if err != nil || resp == nil || resp.Stream == nil {
		return resp, err
	}

	input := &guard.CheckInput{TenantID: pipeline.TenantID(ctx), Metadata: cloneMetadata(req.Completion.Metadata)}
	var svc guard.Service
	if m.svc != nil {
		svc = &tracedOutputGuards{Service: m.svc, mw: m}
	}
	resp.Stream = guard.NewOutputGuardedStream(resp.Stream, svc, input, m.strategy).WithStreamGuards(m.guards...)
	return resp, nil
}

// tracedOutputGuards runs the output check under the guardrail span and
// warns when a rewrite came too late to apply.
type tracedOutputGuards struct {
	guard.Service
	mw *StreamGuardrailMiddleware
}

func (t *tracedOutputGuards) CheckPhase(ctx context.Context, phase guard.Phase, input *guard.CheckInput) (*guard.CheckResult, error) {
	result, err := checkGuards(ctx, t.Service, phase, input)
	if err != nil || result == nil {
		return result, err
	}
	switch t.mw.strategy {
	case guard.StrategyPassthrough, guard.StrategyChunkwise:
		if result.Modified && !result.Blocked && t.mw.log != nil {
			t.mw.log.Warn("nexus: output guard modified a passthrough stream; the change was not applied", "guard", result.Guard)
		}
	}
	return result, nil
}
