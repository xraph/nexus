package middlewares

import (
	"context"

	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// TokenCountingMiddleware estimates token usage and enforces context window limits.
type TokenCountingMiddleware struct {
	counter model.RequestTokenCounter
}

// NewTokenCounting creates a token counting middleware.
func NewTokenCounting(counter model.RequestTokenCounter) *TokenCountingMiddleware {
	return &TokenCountingMiddleware{counter: counter}
}

func (m *TokenCountingMiddleware) Name() string  { return "token_counting" }
func (m *TokenCountingMiddleware) Priority() int { return 220 } // After alias, before routing

func (m *TokenCountingMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.counter == nil || req.Completion == nil {
		return next(ctx)
	}

	// Convert provider messages to model messages for estimation
	msgs := toModelMessages(req.Completion.Messages)

	// Estimate tokens
	estimate, err := m.counter.EstimateRequest(ctx, msgs, req.Completion.Model, req.Completion.MaxTokens)
	if err != nil {
		// Token estimation is non-fatal — continue
		return next(ctx)
	}

	// Store estimate in state for usage tracking
	req.State["token_estimate_input"] = estimate.InputTokens
	req.State["token_estimate_context_window"] = estimate.ContextWindow

	// Check overflow
	if estimate.Overflow {
		switch estimate.OverflowStrategy {
		case model.OverflowTruncateOldest:
			req.Completion.Messages = truncateOldest(req.Completion.Messages)
		case model.OverflowTruncateMiddle:
			req.Completion.Messages = truncateMiddle(req.Completion.Messages)
		case model.OverflowError:
			return nil, contextOverflowError(estimate)
		default:
			// Default: let it through and let the provider handle it
		}
	}

	return next(ctx)
}

func toModelMessages(msgs []provider.Message) []model.Message {
	result := make([]model.Message, len(msgs))
	for i, m := range msgs {
		content, _ := m.Content.(string)
		result[i] = model.Message{
			Role:    m.Role,
			Content: content,
		}
	}
	return result
}

// truncateOldest keeps the first (system) message and the last N messages.
func truncateOldest(msgs []provider.Message) []provider.Message {
	if len(msgs) <= 2 {
		return msgs
	}

	// Keep first system message + last 2 messages
	result := make([]provider.Message, 0, 3)
	result = append(result, msgs[0])
	result = append(result, msgs[len(msgs)-2:]...)
	return result
}

// truncateMiddle keeps the first 2 and last 2 messages, removing the middle.
func truncateMiddle(msgs []provider.Message) []provider.Message {
	if len(msgs) <= 4 {
		return msgs
	}

	result := make([]provider.Message, 0, 4)
	result = append(result, msgs[:2]...)
	result = append(result, msgs[len(msgs)-2:]...)
	return result
}

type contextOverflowErr struct {
	inputTokens   int
	contextWindow int
}

func (e *contextOverflowErr) Error() string {
	return "nexus: request exceeds context window"
}

func contextOverflowError(est *model.TokenEstimate) error {
	return &contextOverflowErr{
		inputTokens:   est.InputTokens,
		contextWindow: est.ContextWindow,
	}
}
