package anthropic_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

func TestStream_ThinkingDeltaEmitsReasoning(t *testing.T) {
	t.Parallel()
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "msg_thinking", "model": "claude-3-7-sonnet"},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "thinking_delta", "thinking": "Let me think... "},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "thinking_delta", "thinking": "1+1=2."},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 1,
			"delta": map[string]any{"type": "text_delta", "text": "The answer is 2."},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:     "claude-3-7-sonnet",
		Messages:  []provider.Message{{Role: "user", Content: "1+1?"}},
		MaxTokens: 100,
		Stream:    true,
		Thinking:  &provider.ThinkingConfig{Enabled: true, BudgetTokens: 1024},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var reasoning, content string
	for {
		c, e := stream.Next(context.Background())
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatalf("Next: %v", e)
		}
		reasoning += c.Delta.Reasoning
		content += c.Delta.Content
	}

	if reasoning != "Let me think... 1+1=2." {
		t.Fatalf("reasoning = %q", reasoning)
	}
	if content != "The answer is 2." {
		t.Fatalf("content = %q", content)
	}
}

func TestStream_InputJSONDeltaAccumulatesToolCall(t *testing.T) {
	t.Parallel()
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "msg_tools", "model": "claude-sonnet"},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "toolu_1",
				"name": "lookup",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"q":"`},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `weather"}`},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "claude-sonnet",
		Messages: []provider.Message{{Role: "user", Content: "weather?"}},
		Stream:   true,
		Tools: []provider.Tool{{
			Type:     "function",
			Function: provider.ToolFunction{Name: "lookup"},
		}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	tools := resp.Choices[0].Message.ToolCalls
	if len(tools) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(tools))
	}
	if tools[0].ID != "toolu_1" || tools[0].Function.Name != "lookup" {
		t.Fatalf("tool call missing id/name: %+v", tools[0])
	}
	if tools[0].Function.Arguments != `{"q":"weather"}` {
		t.Fatalf("arguments = %q", tools[0].Function.Arguments)
	}
}
