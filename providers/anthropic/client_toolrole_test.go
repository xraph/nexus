package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xraph/nexus/provider"
)

// Anthropic's Messages API only accepts "user" and "assistant" roles. OpenAI's
// "tool" result message and assistant `tool_calls` array must be translated to
// Anthropic tool_result / tool_use content blocks, otherwise the API rejects
// the request with: messages: Unexpected role "tool".
func TestToAnthropicRequest_toolFlowTranslatesToContentBlocks(t *testing.T) {
	c := newClient("k", "https://example.test")
	req := &provider.CompletionRequest{
		Model: "claude-opus-4-8",
		Messages: []provider.Message{
			{Role: "user", Content: "what's the weather in Paris?"},
			{Role: "assistant", Content: "", ToolCalls: []provider.ToolCall{{
				ID:       "toolu_1",
				Type:     "function",
				Function: provider.ToolCallFunc{Name: "get_weather", Arguments: `{"city":"Paris"}`},
			}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "sunny, 22C"},
		},
	}

	got := c.toAnthropicRequest(req)

	for i, m := range got.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			t.Fatalf("message %d has invalid role %q (Anthropic only accepts user/assistant)", i, m.Role)
		}
	}
	if len(got.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got.Messages))
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	wire := string(raw)

	// assistant tool call → tool_use block with a JSON-object input
	if got.Messages[1].Role != "assistant" {
		t.Fatalf("message 1 role = %q, want assistant", got.Messages[1].Role)
	}
	for _, want := range []string{`"type":"tool_use"`, `"id":"toolu_1"`, `"name":"get_weather"`, `"input":{"city":"Paris"}`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("assistant tool_call not converted (missing %s) in: %s", want, wire)
		}
	}

	// tool result → user message carrying a tool_result block
	if got.Messages[2].Role != "user" {
		t.Fatalf("message 2 role = %q, want user (tool result must map to a user message)", got.Messages[2].Role)
	}
	for _, want := range []string{`"type":"tool_result"`, `"tool_use_id":"toolu_1"`, "sunny, 22C"} {
		if !strings.Contains(wire, want) {
			t.Fatalf("tool message not converted (missing %s) in: %s", want, wire)
		}
	}
}

// Plain user/assistant text messages must still pass through untouched.
func TestToAnthropicRequest_plainMessagesUnchanged(t *testing.T) {
	c := newClient("k", "https://example.test")
	req := &provider.CompletionRequest{
		Model: "claude-opus-4-8",
		Messages: []provider.Message{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	}

	got := c.toAnthropicRequest(req)

	if got.System != "be terse" {
		t.Fatalf("system = %q, want %q", got.System, "be terse")
	}
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages (system extracted), got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "hi" {
		t.Fatalf("user message altered: %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "assistant" || got.Messages[1].Content != "hello" {
		t.Fatalf("assistant message altered: %+v", got.Messages[1])
	}
}
