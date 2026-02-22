package guards

import (
	"context"
	"regexp"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/provider"
)

// RegexGuard applies custom regex rules to content.
type RegexGuard struct {
	name  string
	rules []RegexRule
	phase guard.Phase
}

// RegexRule is a single regex-based rule.
type RegexRule struct {
	Name        string
	Pattern     *regexp.Regexp
	Action      guard.Action
	Reason      string
	Replacement string // for redact action
}

// NewRegex creates a regex-based guard.
func NewRegex(name string, phase guard.Phase) *RegexGuard {
	return &RegexGuard{
		name:  name,
		phase: phase,
	}
}

// AddRule adds a regex rule to the guard.
func (g *RegexGuard) AddRule(name, pattern string, action guard.Action, reason string) *RegexGuard {
	g.rules = append(g.rules, RegexRule{
		Name:    name,
		Pattern: regexp.MustCompile(pattern),
		Action:  action,
		Reason:  reason,
	})
	return g
}

// AddRedactRule adds a rule that redacts matching content.
func (g *RegexGuard) AddRedactRule(name, pattern, replacement string) *RegexGuard {
	g.rules = append(g.rules, RegexRule{
		Name:        name,
		Pattern:     regexp.MustCompile(pattern),
		Action:      guard.ActionRedact,
		Replacement: replacement,
	})
	return g
}

func (g *RegexGuard) Name() string       { return g.name }
func (g *RegexGuard) Phase() guard.Phase { return g.phase }

func (g *RegexGuard) Check(_ context.Context, input *guard.CheckInput) (*guard.CheckResult, error) {
	var modified bool
	messages := input.Messages

	for _, rule := range g.rules {
		for i, msg := range messages {
			content, ok := msg.Content.(string)
			if !ok {
				continue
			}

			if !rule.Pattern.MatchString(content) {
				continue
			}

			switch rule.Action {
			case guard.ActionBlock:
				return &guard.CheckResult{
					Passed:  false,
					Blocked: true,
					Action:  guard.ActionBlock,
					Reason:  rule.Reason,
					Details: map[string]any{"rule": rule.Name},
				}, nil

			case guard.ActionRedact:
				if !modified {
					// Copy on first modification
					messages = make([]provider.Message, len(input.Messages))
					copy(messages, input.Messages)
					modified = true
				}
				messages[i].Content = rule.Pattern.ReplaceAllString(content, rule.Replacement)

			case guard.ActionWarn:
				// Allow but flag
				return &guard.CheckResult{
					Passed:  true,
					Action:  guard.ActionWarn,
					Reason:  rule.Reason,
					Details: map[string]any{"rule": rule.Name},
				}, nil
			}
		}
	}

	if modified {
		return &guard.CheckResult{
			Passed:   true,
			Action:   guard.ActionRedact,
			Modified: true,
			Messages: messages,
		}, nil
	}

	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}
