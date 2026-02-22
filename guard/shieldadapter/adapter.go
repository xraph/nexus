// Package shieldadapter provides a guardrail adapter that bridges an external
// Shield content safety engine to the Nexus guard.Guard interface.
package shieldadapter

import (
	"context"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/provider"
)

// Engine represents an external Shield content safety engine.
// This is the minimal interface that the adapter requires.
type Engine interface {
	// Check evaluates content for safety violations.
	Check(ctx context.Context, content string) (*Result, error)
}

// Result is the Shield check result.
type Result struct {
	Allowed bool    `json:"allowed"`
	Reason  string  `json:"reason,omitempty"`
	Score   float64 `json:"score"`
}

// Adapter bridges Shield to the Nexus guard.Guard interface.
type Adapter struct {
	engine Engine
	action guard.Action
}

// Compile-time check.
var _ guard.Guard = (*Adapter)(nil)

// New creates a Shield guard adapter.
func New(engine Engine) *Adapter {
	return &Adapter{
		engine: engine,
		action: guard.ActionBlock,
	}
}

// NewWithAction creates a Shield guard adapter with a specific action.
func NewWithAction(engine Engine, action guard.Action) *Adapter {
	return &Adapter{
		engine: engine,
		action: action,
	}
}

func (a *Adapter) Name() string       { return "shield" }
func (a *Adapter) Phase() guard.Phase { return guard.PhaseBoth }

func (a *Adapter) Check(ctx context.Context, input *guard.CheckInput) (*guard.CheckResult, error) {
	// Extract text content from messages
	for _, msg := range input.Messages {
		content, ok := msg.Content.(string)
		if !ok {
			continue
		}

		result, err := a.engine.Check(ctx, content)
		if err != nil {
			return nil, err
		}

		if !result.Allowed {
			return &guard.CheckResult{
				Passed:  false,
				Blocked: a.action == guard.ActionBlock,
				Action:  a.action,
				Reason:  result.Reason,
				Details: map[string]any{
					"shield_score": result.Score,
				},
			}, nil
		}
	}

	return &guard.CheckResult{
		Passed: true,
		Action: guard.ActionAllow,
	}, nil
}

// WrapMessages extracts text from provider messages for Shield checking.
func WrapMessages(messages []provider.Message) string {
	var text string
	for _, msg := range messages {
		if content, ok := msg.Content.(string); ok {
			text += content + "\n"
		}
	}
	return text
}
