package lmstudio

import (
	"regexp"
	"testing"

	"github.com/xraph/nexus/provider"
)

// extractSample captures a real-world model emission observed via the
// LM Studio provider, with the expected calls + cleaned content.
// Append one row per new failure mode — the table is the source of
// truth for "patterns the upstream extractor must handle".
//
// Spec-gated samples live in twinos/extensions/agent (the runner has
// the bound-tool list); this upstream layer is explicit-framing only
// — wrappers + markdown fences.
type extractSample struct {
	name          string
	content       string
	wantToolNames []string
	wantCleanedRe *regexp.Regexp
}

var observedExtractSamples = []extractSample{
	{
		name:          "iter1: closed tool_call wrapper, single object",
		content:       `<tool_call>{"name":"workspace-info","arguments":{}}</tool_call>`,
		wantToolNames: []string{"workspace-info"},
		wantCleanedRe: regexp.MustCompile(`^$`),
	},
	{
		name:          "iter2: unclosed tool_call wrapper, array payload",
		content:       `<tool_call> [{"name": "workspace-info", "arguments": {}}]`,
		wantToolNames: []string{"workspace-info"},
		wantCleanedRe: regexp.MustCompile(`^$`),
	},
	{
		name: "iter3: markdown json code fence with prose preamble, tool/params keys",
		content: "I'll retrieve the details of your workspace for you.\n\n" +
			"```json\n" +
			"{\n" +
			"  \"tool\": \"workspace-info\",\n" +
			"  \"params\": {}\n" +
			"}\n" +
			"```",
		wantToolNames: []string{"workspace-info"},
		wantCleanedRe: regexp.MustCompile(`I'll retrieve`),
	},
}

func TestObservedExtractSamples(t *testing.T) {
	for _, s := range observedExtractSamples {
		s := s
		t.Run(s.name, func(t *testing.T) {
			calls, cleaned := ExtractTextFormToolCalls(s.content)
			if len(s.wantToolNames) == 0 {
				if len(calls) != 0 {
					t.Fatalf("expected no calls, got %d (%v)", len(calls), extractNames(calls))
				}
				return
			}
			if len(calls) != len(s.wantToolNames) {
				t.Fatalf("expected %d calls (%v), got %d (%v)",
					len(s.wantToolNames), s.wantToolNames, len(calls), extractNames(calls))
			}
			for i, want := range s.wantToolNames {
				if calls[i].Function.Name != want {
					t.Errorf("call[%d]: expected name %q, got %q", i, want, calls[i].Function.Name)
				}
			}
			if s.wantCleanedRe != nil && !s.wantCleanedRe.MatchString(cleaned) {
				t.Errorf("cleaned content %q does not match %s", cleaned, s.wantCleanedRe)
			}
		})
	}
}

func extractNames(calls []provider.ToolCall) []string {
	out := make([]string, len(calls))
	for i := range calls {
		out[i] = calls[i].Function.Name
	}
	return out
}
