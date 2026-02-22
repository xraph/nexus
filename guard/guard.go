// Package guard defines the guardrails subsystem for content safety.
package guard

import (
	"context"

	"github.com/xraph/nexus/provider"
)

// Guard is a single content safety rule.
type Guard interface {
	// Name returns the guard identifier.
	Name() string

	// Phase returns when this guard runs: "input", "output", or "both".
	Phase() Phase

	// Check evaluates content against this guard's rules.
	Check(ctx context.Context, input *CheckInput) (*CheckResult, error)
}

// Phase indicates when a guard runs.
type Phase string

const (
	PhaseInput  Phase = "input"
	PhaseOutput Phase = "output"
	PhaseBoth   Phase = "both"
)

// CheckInput is the content to evaluate.
type CheckInput struct {
	Messages []provider.Message
	TenantID string
	Metadata map[string]string
}

// CheckResult is the guard's verdict.
type CheckResult struct {
	Passed   bool               // true if content is OK
	Blocked  bool               // true if content should be rejected
	Action   Action             // what to do
	Reason   string             // human-readable explanation
	Modified bool               // true if messages were altered (e.g., PII redacted)
	Messages []provider.Message // modified messages (if Modified)
	Details  map[string]any     // guard-specific details
}

// Action describes the guard's response.
type Action string

const (
	ActionAllow  Action = "allow"
	ActionBlock  Action = "block"
	ActionRedact Action = "redact" // remove sensitive content
	ActionWarn   Action = "warn"   // allow but flag
)

// Service manages and executes guards.
type Service interface {
	// Register adds a guard.
	Register(g Guard)

	// Check runs all applicable guards for the given phase.
	// Returns on first block; aggregates all redactions.
	Check(ctx context.Context, input *CheckInput) (*CheckResult, error)

	// CheckPhase runs guards for a specific phase only.
	CheckPhase(ctx context.Context, phase Phase, input *CheckInput) (*CheckResult, error)

	// List returns all registered guards.
	List() []Guard
}

// NewService creates a new guard service.
func NewService() Service {
	return &guardService{}
}

type guardService struct {
	guards []Guard
}

func (s *guardService) Register(g Guard) {
	s.guards = append(s.guards, g)
}

func (s *guardService) Check(ctx context.Context, input *CheckInput) (*CheckResult, error) {
	result := &CheckResult{Passed: true, Action: ActionAllow}
	for _, g := range s.guards {
		r, err := g.Check(ctx, input)
		if err != nil {
			return nil, err
		}
		if r.Blocked {
			return r, nil
		}
		if r.Modified {
			input.Messages = r.Messages
			result.Modified = true
			result.Messages = r.Messages
		}
	}
	return result, nil
}

func (s *guardService) CheckPhase(ctx context.Context, phase Phase, input *CheckInput) (*CheckResult, error) {
	result := &CheckResult{Passed: true, Action: ActionAllow}
	for _, g := range s.guards {
		if g.Phase() != phase && g.Phase() != PhaseBoth {
			continue
		}
		r, err := g.Check(ctx, input)
		if err != nil {
			return nil, err
		}
		if r.Blocked {
			return r, nil
		}
		if r.Modified {
			input.Messages = r.Messages
			result.Modified = true
			result.Messages = r.Messages
		}
	}
	return result, nil
}

func (s *guardService) List() []Guard {
	return s.guards
}
