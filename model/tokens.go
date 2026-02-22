package model

import "context"

// TokenCounter estimates token counts for requests.
type TokenCounter interface {
	// Count returns the estimated token count for a text.
	Count(ctx context.Context, text string, model string) (int, error)

	// CountMessages returns the estimated token count for a message list.
	CountMessages(ctx context.Context, messages []Message, model string) (int, error)
}

// RequestTokenCounter extends TokenCounter with the ability to estimate
// tokens for a full completion request (used by the pipeline middleware).
type RequestTokenCounter interface {
	TokenCounter

	// EstimateRequest returns a full estimate for a completion request,
	// including context window limits and overflow strategy.
	EstimateRequest(ctx context.Context, messages []Message, model string, maxTokens int) (*TokenEstimate, error)
}

// Message is a minimal message type for token counting.
// It mirrors the essential fields from provider.Message.
type Message struct {
	Role    string
	Content string
}

// TokenEstimate is the result of a token estimation.
type TokenEstimate struct {
	InputTokens      int              `json:"input_tokens"`
	OutputMax        int              `json:"output_max"`
	Total            int              `json:"total"`
	ContextWindow    int              `json:"context_window"`
	Overflow         bool             `json:"overflow"` // true if InputTokens > ContextWindow
	OverflowStrategy OverflowStrategy `json:"overflow_strategy,omitempty"`
}

// OverflowStrategy defines how to handle context overflow.
type OverflowStrategy string

const (
	// OverflowError returns an error when context overflows.
	OverflowError OverflowStrategy = "error"

	// OverflowTruncateOldest truncates oldest messages to fit.
	OverflowTruncateOldest OverflowStrategy = "truncate_oldest"

	// OverflowTruncateMiddle removes middle messages, keeping first and last.
	OverflowTruncateMiddle OverflowStrategy = "truncate_middle"
)
