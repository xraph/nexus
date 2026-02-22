package transform

import (
	"context"
	"strings"

	"github.com/xraph/nexus/provider"
)

// NormalizerTransform standardizes output format across providers.
type NormalizerTransform struct {
	// TrimWhitespace trims leading/trailing whitespace from content.
	TrimWhitespace bool

	// StripProviderMeta removes provider-specific metadata.
	StripProviderMeta bool

	// NormalizeFinishReason maps provider-specific finish reasons to standard ones.
	NormalizeFinishReason bool
}

// NewNormalizer creates an output normalizer transform.
func NewNormalizer() *NormalizerTransform {
	return &NormalizerTransform{
		TrimWhitespace:        true,
		StripProviderMeta:     false,
		NormalizeFinishReason: true,
	}
}

func (t *NormalizerTransform) Name() string { return "normalizer" }
func (t *NormalizerTransform) Phase() Phase { return PhaseOutput }

func (t *NormalizerTransform) TransformOutput(ctx context.Context, req *provider.CompletionRequest, resp *provider.CompletionResponse) error {
	for i := range resp.Choices {
		c := &resp.Choices[i]

		if t.TrimWhitespace {
			if s, ok := c.Message.Content.(string); ok {
				c.Message.Content = strings.TrimSpace(s)
			}
		}

		if t.NormalizeFinishReason {
			c.FinishReason = normalizeFinishReason(c.FinishReason)
		}
	}

	return nil
}

// normalizeFinishReason maps various provider finish reasons to standard values.
func normalizeFinishReason(reason string) string {
	switch strings.ToLower(reason) {
	case "stop", "end_turn", "complete":
		return "stop"
	case "length", "max_tokens":
		return "length"
	case "tool_calls", "tool_use":
		return "tool_calls"
	case "content_filter", "content_filtered":
		return "content_filter"
	default:
		return reason
	}
}
