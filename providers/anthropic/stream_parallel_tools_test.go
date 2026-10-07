package anthropic_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

// Guards: an Anthropic turn with two tool_use blocks (parallel tool calls)
// accumulates into two provider.ToolCalls, each carrying only its own input
// JSON. This replays the exact wire sequence that broke in production: the
// stream tags every input_json_delta with the block's own tool id, but emits
// each delta as a one-element slice, so the accumulator must key on the id
// rather than the slice index or the second call is folded into the first.
func TestStream_ParallelToolUseBlocksAccumulateSeparately(t *testing.T) {
	mock := testutil.NewMockServer(t)

	blockStart := func(index int, id, name string) string {
		return testutil.AnthropicEventJSON("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": index,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    id,
				"name":  name,
				"input": map[string]any{},
			},
		})
	}
	inputDelta := func(index int, partial string) string {
		return testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": index,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": partial,
			},
		})
	}
	blockStop := func(index int) string {
		return testutil.AnthropicEventJSON("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": index,
		})
	}

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_parallel",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		blockStart(0, "toolu_1", "read_history"), "",
		inputDelta(0, `{"id": "layer_a",`), "",
		inputDelta(0, ` "minutes": 30}`), "",
		blockStop(0), "",
		blockStart(1, "toolu_2", "related_resources"), "",
		inputDelta(1, `{"id": "mk-1"}`), "",
		blockStop(1), "",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "tool_use"},
			"usage": map[string]any{"output_tokens": 40},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model:     "claude-sonnet-4-5-20250514",
		Messages:  []provider.Message{{Role: "user", Content: "Two checks, please."}},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	resp, err := provider.Accumulate(ctx, stream)
	if err != nil {
		t.Fatalf("Accumulate() error: %v", err)
	}
	tools := resp.Choices[0].Message.ToolCalls
	if len(tools) != 2 {
		t.Fatalf("got %d tool calls, want 2: %+v", len(tools), tools)
	}
	if tools[0].ID != "toolu_1" || tools[0].Function.Name != "read_history" || tools[0].Function.Arguments != `{"id": "layer_a", "minutes": 30}` {
		t.Fatalf("first call = %+v", tools[0])
	}
	if tools[1].ID != "toolu_2" || tools[1].Function.Name != "related_resources" || tools[1].Function.Arguments != `{"id": "mk-1"}` {
		t.Fatalf("second call = %+v", tools[1])
	}
}
