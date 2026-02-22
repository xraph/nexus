// Package guards provides built-in guardrail implementations.
package guards

import (
	"context"
	"regexp"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/provider"
)

// PIIGuard detects and redacts personally identifiable information.
type PIIGuard struct {
	action   guard.Action
	patterns []piiPattern
}

type piiPattern struct {
	name        string
	regex       *regexp.Regexp
	replacement string
}

// NewPII creates a PII detection/redaction guard.
func NewPII(action guard.Action) *PIIGuard {
	g := &PIIGuard{action: action}

	// Default PII patterns
	g.addPattern("email", `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`, "[EMAIL_REDACTED]")
	g.addPattern("phone", `\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`, "[PHONE_REDACTED]")
	g.addPattern("ssn", `\b\d{3}-\d{2}-\d{4}\b`, "[SSN_REDACTED]")
	g.addPattern("credit_card", `\b(?:\d{4}[-\s]?){3}\d{4}\b`, "[CC_REDACTED]")

	return g
}

func (g *PIIGuard) addPattern(name, pattern, replacement string) {
	g.patterns = append(g.patterns, piiPattern{
		name:        name,
		regex:       regexp.MustCompile(pattern),
		replacement: replacement,
	})
}

func (g *PIIGuard) Name() string       { return "pii" }
func (g *PIIGuard) Phase() guard.Phase { return guard.PhaseBoth }

func (g *PIIGuard) Check(_ context.Context, input *guard.CheckInput) (*guard.CheckResult, error) {
	detected := false
	var findings []string

	for _, msg := range input.Messages {
		content, ok := msg.Content.(string)
		if !ok {
			continue
		}
		for _, p := range g.patterns {
			if p.regex.MatchString(content) {
				detected = true
				findings = append(findings, p.name)
			}
		}
	}

	if !detected {
		return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
	}

	switch g.action {
	case guard.ActionBlock:
		return &guard.CheckResult{
			Passed:  false,
			Blocked: true,
			Action:  guard.ActionBlock,
			Reason:  "PII detected in request",
			Details: map[string]any{"types": findings},
		}, nil

	case guard.ActionRedact:
		modified := make([]provider.Message, len(input.Messages))
		copy(modified, input.Messages)
		for i := range modified {
			content, ok := modified[i].Content.(string)
			if !ok {
				continue
			}
			for _, p := range g.patterns {
				content = p.regex.ReplaceAllString(content, p.replacement)
			}
			modified[i].Content = content
		}
		return &guard.CheckResult{
			Passed:   true,
			Action:   guard.ActionRedact,
			Reason:   "PII redacted",
			Modified: true,
			Messages: modified,
			Details:  map[string]any{"types": findings},
		}, nil

	case guard.ActionWarn:
		return &guard.CheckResult{
			Passed:  true,
			Action:  guard.ActionWarn,
			Reason:  "PII detected but allowed",
			Details: map[string]any{"types": findings},
		}, nil

	default:
		return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
	}
}

// AddPattern adds a custom PII pattern.
func (g *PIIGuard) AddPattern(name, pattern, replacement string) {
	g.addPattern(name, pattern, replacement)
}
