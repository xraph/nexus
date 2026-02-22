package guards

import (
	"context"
	"strings"

	"github.com/xraph/nexus/guard"
)

// ContentFilterGuard blocks requests containing specified keywords or phrases.
type ContentFilterGuard struct {
	blocklist []string
	action    guard.Action
}

// NewContentFilter creates a content filter guard.
func NewContentFilter(action guard.Action, blocklist ...string) *ContentFilterGuard {
	lower := make([]string, len(blocklist))
	for i, w := range blocklist {
		lower[i] = strings.ToLower(w)
	}
	return &ContentFilterGuard{
		blocklist: lower,
		action:    action,
	}
}

func (g *ContentFilterGuard) Name() string       { return "content_filter" }
func (g *ContentFilterGuard) Phase() guard.Phase { return guard.PhaseBoth }

func (g *ContentFilterGuard) Check(_ context.Context, input *guard.CheckInput) (*guard.CheckResult, error) {
	for _, msg := range input.Messages {
		content, ok := msg.Content.(string)
		if !ok {
			continue
		}
		lower := strings.ToLower(content)
		for _, word := range g.blocklist {
			if strings.Contains(lower, word) {
				return &guard.CheckResult{
					Passed:  false,
					Blocked: g.action == guard.ActionBlock,
					Action:  g.action,
					Reason:  "blocked content detected",
					Details: map[string]any{"matched": word},
				}, nil
			}
		}
	}

	return &guard.CheckResult{Passed: true, Action: guard.ActionAllow}, nil
}
