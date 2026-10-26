package middlewares

import (
	"context"
	"maps"

	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/internal/telemetry"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// GuardrailMiddleware runs guardrails before and after the provider call.
// Each phase gets a "check guardrails" span with the phase, whether it
// blocked or modified the messages, and the name of the guard that blocked.
// The guard's reason and the messages never go on the span.
type GuardrailMiddleware struct {
	guard guard.Service
}

// NewGuardrail creates a guardrail middleware.
func NewGuardrail(g guard.Service) *GuardrailMiddleware {
	return &GuardrailMiddleware{guard: g}
}

func (m *GuardrailMiddleware) Name() string  { return "guardrail" }
func (m *GuardrailMiddleware) Priority() int { return 150 } // After auth, before transforms

func (m *GuardrailMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.guard == nil || req.Completion == nil {
		return next(ctx)
	}

	// Input guardrails
	input := &guard.CheckInput{
		Messages: req.Completion.Messages,
		TenantID: pipeline.TenantID(ctx),
		Metadata: cloneMetadata(req.Completion.Metadata),
	}

	result, err := checkGuards(ctx, m.guard, guard.PhaseInput, input)
	if err != nil {
		return nil, err
	}
	if result.Blocked {
		return nil, &guard.BlockedError{Guard: result.Guard, Phase: guard.PhaseInput, Reason: result.Reason}
	}
	if result.Modified {
		req.Completion.Messages = result.Messages
	}

	// Continue pipeline (provider call)
	resp, err := next(ctx)
	if err != nil {
		return resp, err
	}

	// Output guardrails. Streamed output is the stream guardrail stage's.
	if resp != nil && resp.Completion != nil && len(resp.Completion.Choices) > 0 {
		// Build output check input from response messages
		outputMsgs := make([]provider.Message, len(resp.Completion.Choices))
		for i, c := range resp.Completion.Choices {
			outputMsgs[i] = c.Message
		}

		outputInput := &guard.CheckInput{
			Messages: outputMsgs,
			TenantID: pipeline.TenantID(ctx),
			Metadata: cloneMetadata(req.Completion.Metadata),
		}

		outputResult, err := checkGuards(ctx, m.guard, guard.PhaseOutput, outputInput)
		if err != nil {
			return nil, err
		}
		if outputResult.Blocked {
			u := resp.Completion.Usage
			return nil, &guard.BlockedError{
				Guard: outputResult.Guard, Phase: guard.PhaseOutput, Reason: outputResult.Reason,
				Usage: &u, Model: resp.Completion.Model, Provider: resp.Completion.Provider,
			}
		}
		if outputResult.Modified && len(outputResult.Messages) > 0 {
			for i := range resp.Completion.Choices {
				if i < len(outputResult.Messages) {
					resp.Completion.Choices[i].Message = outputResult.Messages[i]
					// Native still holds the text and thinking the guard
					// rewrote. Dropping it keeps the original out of what
					// consumers store; the turn is rebuilt from the
					// rewritten fields, without thinking, on replay.
					resp.Completion.Choices[i].Message.Native = nil
				}
			}
		}
	}

	return resp, nil
}

// cloneMetadata gives each guard check its own copy of the request's
// metadata, so a guard that edits it changes neither the request nor what
// the next check sees.
func cloneMetadata(m map[string]string) map[string]string {
	return maps.Clone(m)
}

// checkGuards runs one phase of svc's guards under a "check guardrails" span
// with the phase, whether it blocked or modified the messages, and the name
// of the guard that blocked. The reason and the messages never go on it.
func checkGuards(ctx context.Context, svc guard.Service, phase guard.Phase, input *guard.CheckInput) (*guard.CheckResult, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "check guardrails",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(telemetry.GuardrailPhase.String(string(phase))))
	defer span.End()
	result, err := svc.CheckPhase(ctx, phase, input)
	if err != nil {
		telemetry.Fail(span, err)
		return nil, err
	}
	if result != nil && span.IsRecording() {
		span.SetAttributes(telemetry.GuardrailBlock.Bool(result.Blocked), telemetry.GuardrailEdited.Bool(result.Modified))
		if result.Blocked && result.Guard != "" {
			span.SetAttributes(telemetry.GuardrailName.String(result.Guard))
		}
	}
	return result, nil
}
