package transform

import (
	"context"
	"regexp"

	"github.com/xraph/nexus/provider"
)

// AnonymizerTransform redacts sensitive data from requests and responses.
type AnonymizerTransform struct {
	patterns []anonymizerPattern
}

type anonymizerPattern struct {
	name        string
	regex       *regexp.Regexp
	replacement string
}

// NewAnonymizer creates a data anonymizer with common PII patterns.
func NewAnonymizer() *AnonymizerTransform {
	a := &AnonymizerTransform{}

	// Common PII patterns
	a.AddPattern("email", `\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`, "[EMAIL]")
	a.AddPattern("phone", `\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`, "[PHONE]")
	a.AddPattern("ssn", `\b\d{3}-\d{2}-\d{4}\b`, "[SSN]")
	a.AddPattern("credit_card", `\b(?:\d{4}[-\s]?){3}\d{4}\b`, "[CREDIT_CARD]")
	a.AddPattern("ip_address", `\b(?:\d{1,3}\.){3}\d{1,3}\b`, "[IP_ADDRESS]")

	return a
}

// AddPattern adds a named regex pattern for anonymization.
func (a *AnonymizerTransform) AddPattern(name, pattern, replacement string) {
	re := regexp.MustCompile(pattern)
	a.patterns = append(a.patterns, anonymizerPattern{
		name:        name,
		regex:       re,
		replacement: replacement,
	})
}

func (a *AnonymizerTransform) Name() string { return "anonymizer" }
func (a *AnonymizerTransform) Phase() Phase { return PhaseInput }

func (a *AnonymizerTransform) TransformInput(_ context.Context, req *provider.CompletionRequest) error {
	for i := range req.Messages {
		if s, ok := req.Messages[i].Content.(string); ok {
			req.Messages[i].Content = a.redact(s)
		}
	}
	return nil
}

func (a *AnonymizerTransform) redact(text string) string {
	for _, p := range a.patterns {
		text = p.regex.ReplaceAllString(text, p.replacement)
	}
	return text
}

// NewAnonymizerOutput creates an output anonymizer that redacts PII from responses.
func NewAnonymizerOutput() *OutputAnonymizerTransform {
	a := NewAnonymizer()
	return &OutputAnonymizerTransform{base: a}
}

// OutputAnonymizerTransform redacts PII from provider responses.
type OutputAnonymizerTransform struct {
	base *AnonymizerTransform
}

func (t *OutputAnonymizerTransform) Name() string { return "anonymizer_output" }
func (t *OutputAnonymizerTransform) Phase() Phase { return PhaseOutput }

func (t *OutputAnonymizerTransform) TransformOutput(_ context.Context, _ *provider.CompletionRequest, resp *provider.CompletionResponse) error {
	for i := range resp.Choices {
		if s, ok := resp.Choices[i].Message.Content.(string); ok {
			resp.Choices[i].Message.Content = t.base.redact(s)
		}
	}
	return nil
}

// AddPattern delegates to the base anonymizer.
func (t *OutputAnonymizerTransform) AddPattern(name, pattern, replacement string) {
	t.base.AddPattern(name, pattern, replacement)
}

// Ensure compile-time interface compliance.
var (
	_ InputTransform  = (*AnonymizerTransform)(nil)
	_ OutputTransform = (*NormalizerTransform)(nil)
	_ OutputTransform = (*OutputAnonymizerTransform)(nil)
	_ InputTransform  = (*SystemPromptTransform)(nil)
	_ InputTransform  = (*RAGTransform)(nil)
)
