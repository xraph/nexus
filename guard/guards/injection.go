package guards

import (
	"context"
	"regexp"

	"github.com/xraph/nexus/guard"
)

// InjectionGuard detects prompt injection attempts.
type InjectionGuard struct {
	patterns []*regexp.Regexp
}

// NewInjection creates a prompt injection detection guard.
func NewInjection() *InjectionGuard {
	g := &InjectionGuard{}

	// Common injection patterns
	patterns := []string{
		`(?i)ignore\s+(all\s+)?previous\s+instructions`,
		`(?i)forget\s+(all\s+)?previous\s+(instructions|prompts|context)`,
		`(?i)disregard\s+(all\s+)?(previous|prior|above)\s+(instructions|rules)`,
		`(?i)you\s+are\s+now\s+(a|an)\s+`,
		`(?i)system\s*:\s*you\s+are`,
		`(?i)override\s+(system|safety|security)\s+(prompt|instructions|rules)`,
		`(?i)\[SYSTEM\]`,
		`(?i)act\s+as\s+if\s+(you\s+have\s+)?no\s+(restrictions|rules)`,
		`(?i)jailbreak`,
		`(?i)DAN\s+mode`,
	}

	for _, p := range patterns {
		g.patterns = append(g.patterns, regexp.MustCompile(p))
	}

	return g
}

func (g *InjectionGuard) Name() string       { return "injection" }
func (g *InjectionGuard) Phase() guard.Phase { return guard.PhaseInput }

func (g *InjectionGuard) Check(_ context.Context, input *guard.CheckInput) (*guard.CheckResult, error) {
	for _, msg := range input.Messages {
		// Only check user messages for injection
		if msg.Role != "user" {
			continue
		}
		content, ok := msg.Content.(string)
		if !ok {
			continue
		}

		for _, p := range g.patterns {
			if p.MatchString(content) {
				return &guard.CheckResult{
					Passed:  false,
					Blocked: true,
					Action:  guard.ActionBlock,
					Reason:  "prompt injection detected",
					Details: map[string]any{
						"pattern": p.String(),
					},
				}, nil
			}
		}
	}

	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}

// AddPattern adds a custom injection detection regex pattern.
func (g *InjectionGuard) AddPattern(pattern string) {
	g.patterns = append(g.patterns, regexp.MustCompile(pattern))
}
