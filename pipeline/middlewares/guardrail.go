package middlewares

import (
	"context"
	"errors"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// GuardrailMiddleware runs guardrails before and after the provider call.
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
	}

	result, err := m.guard.CheckPhase(ctx, guard.PhaseInput, input)
	if err != nil {
		return nil, err
	}
	if result.Blocked {
		return nil, errors.New("nexus: " + result.Reason)
	}
	if result.Modified {
		req.Completion.Messages = result.Messages
	}

	// Continue pipeline (provider call)
	resp, err := next(ctx)
	if err != nil {
		return resp, err
	}

	// Output guardrails (non-streaming only)
	if resp != nil && resp.Completion != nil && len(resp.Completion.Choices) > 0 {
		// Build output check input from response messages
		outputMsgs := make([]provider.Message, len(resp.Completion.Choices))
		for i, c := range resp.Completion.Choices {
			outputMsgs[i] = c.Message
		}

		outputInput := &guard.CheckInput{
			Messages: outputMsgs,
			TenantID: pipeline.TenantID(ctx),
		}

		outputResult, err := m.guard.CheckPhase(ctx, guard.PhaseOutput, outputInput)
		if err != nil {
			return nil, err
		}
		if outputResult.Blocked {
			return nil, errors.New("nexus: output blocked: " + outputResult.Reason)
		}
		if outputResult.Modified && len(outputResult.Messages) > 0 {
			for i := range resp.Completion.Choices {
				if i < len(outputResult.Messages) {
					resp.Completion.Choices[i].Message = outputResult.Messages[i]
				}
			}
		}
	}

	return resp, nil
}
