package anthropic

import (
	"fmt"
	"slices"

	"github.com/xraph/nexus/provider"
)

const (
	// completeMaxTokens is the max_tokens default for a non-streamed call,
	// low enough to finish well inside a response-header timeout.
	completeMaxTokens = 16000
	// streamMaxTokens is the max_tokens default for a stream.
	streamMaxTokens = 64000
	// unlistedMaxTokens is the max_tokens default for a model not in the
	// catalog, whose output limit is unknown.
	unlistedMaxTokens = 8192
	// minThinkingBudget is the smallest budget_tokens the API accepts.
	minThinkingBudget = 1024
	// budgetHeadroom is the room left for the answer above a thinking budget
	// when max_tokens has to be raised past it.
	budgetHeadroom = 4096
)

type anthropicThinking struct {
	Type         string `json:"type"` // adaptive, enabled, disabled, between_tools
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`

	BlockBinding *anthropicBlockBinding `json:"block_binding,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// thinkingFor maps the request's thinking config onto the model's thinking
// field. nil means the field is left out.
func thinkingFor(tr traits, listed bool, cfg *provider.ThinkingConfig) *anthropicThinking {
	if cfg == nil {
		return nil
	}
	mode := tr.thinking
	if !listed && cfg.BudgetTokens > 0 {
		mode = thinkingBudget
	}

	switch mode {
	case thinkingBudget:
		if !cfg.Enabled {
			return nil
		}
		return &anthropicThinking{Type: "enabled", BudgetTokens: max(cfg.BudgetTokens, minThinkingBudget)}
	case thinkingAlwaysOn:
		// The model thinks either way; asking to see the thinking is enough
		// to send the adaptive field with a display.
		if !cfg.Enabled && !cfg.IncludeThinking {
			return nil
		}
		return adaptiveThinking(cfg)
	case thinkingAdaptiveBetweenTools:
		if !cfg.Enabled {
			return &anthropicThinking{Type: "between_tools"}
		}
		return adaptiveThinking(cfg)
	default:
		if !cfg.Enabled {
			return &anthropicThinking{Type: "disabled"}
		}
		return adaptiveThinking(cfg)
	}
}

func adaptiveThinking(cfg *provider.ThinkingConfig) *anthropicThinking {
	t := &anthropicThinking{Type: "adaptive"}
	if cfg.IncludeThinking {
		// Current models leave thinking text empty unless asked for a summary.
		t.Display = "summarized"
	}
	return t
}

// effortFor returns the effort to send, or "" for none. A listed model that
// does not accept the level fails with provider.ErrNotSupported before any
// call; an unlisted model gets the level unchecked.
func effortFor(model string, tr traits, listed bool, cfg *provider.ThinkingConfig) (string, error) {
	if cfg == nil || cfg.Effort == "" {
		return "", nil
	}
	if listed && !slices.Contains(tr.efforts, cfg.Effort) {
		return "", fmt.Errorf("anthropic: effort %q is not supported by %s: %w", cfg.Effort, model, provider.ErrNotSupported)
	}
	return cfg.Effort, nil
}

// maxTokensFor picks max_tokens. An explicit value is kept; otherwise a
// listed model gets min(16000, MaxOutput) for a call and min(64000,
// MaxOutput) for a stream, and an unlisted one 8192. With budget thinking,
// max_tokens must exceed the budget, so it is raised to budget + 4096 when
// it is not already above it, capped at the model's MaxOutput.
func maxTokensFor(req *provider.CompletionRequest, stream bool, thinking *anthropicThinking) int {
	m, listed := catalogModel(req.Model)
	capped := listed && m.MaxOutput > 0

	n := req.MaxTokens
	if n <= 0 {
		switch {
		case !capped:
			n = unlistedMaxTokens
		case stream:
			n = min(streamMaxTokens, m.MaxOutput)
		default:
			n = min(completeMaxTokens, m.MaxOutput)
		}
	}

	if thinking != nil && thinking.Type == "enabled" && n <= thinking.BudgetTokens {
		n = thinking.BudgetTokens + budgetHeadroom
		if capped {
			n = min(n, m.MaxOutput)
		}
	}
	return n
}
