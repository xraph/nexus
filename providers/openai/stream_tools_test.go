package openai_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/testutil"
)

// TestStream_ToolCallDeltaAccumulates verifies that an OpenAI tool_calls
// stream — name only on first chunk, arguments concatenated across N chunks
// keyed by index — merges into a single ToolCall on Accumulate.
func TestStream_ToolCallDeltaAccumulates(t *testing.T) {
	t.Parallel()
	mock := testutil.NewMockServer(t)

	chunks := []string{
		// First chunk: role + tool_call slot 0 with id and function name.
		mustJSON(map[string]any{
			"id":     "chatcmpl-tools",
			"object": "chat.completion.chunk",
			"model":  "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{{
						"index": 0,
						"id":    "call_a",
						"type":  "function",
						"function": map[string]any{
							"name":      "lookup",
							"arguments": "",
						},
					}},
				},
				"finish_reason": nil,
			}},
		}),
		// Second chunk: incremental arguments.
		mustJSON(map[string]any{
			"id":     "chatcmpl-tools",
			"object": "chat.completion.chunk",
			"model":  "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index": 0,
						"function": map[string]any{
							"arguments": `{"q":"`,
						},
					}},
				},
				"finish_reason": nil,
			}},
		}),
		// Third chunk: rest of arguments + finish_reason.
		mustJSON(map[string]any{
			"id":     "chatcmpl-tools",
			"object": "chat.completion.chunk",
			"model":  "gpt-4o",
			"choices": []map[string]any{{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{{
						"index": 0,
						"function": map[string]any{
							"arguments": `weather"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		}),
	}
	mock.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler(chunks))

	p := openai.New("k", openai.WithBaseURL(mock.Server.URL))
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "gpt-4o",
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
		t.Fatalf("Accumulate: %v", err)
	}
	tools := resp.Choices[0].Message.ToolCalls
	if len(tools) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(tools))
	}
	if tools[0].ID != "call_a" || tools[0].Function.Name != "lookup" {
		t.Fatalf("missing id/name: %+v", tools[0])
	}
	if tools[0].Function.Arguments != `{"q":"weather"}` {
		t.Fatalf("arguments = %q", tools[0].Function.Arguments)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason = %q", resp.Choices[0].FinishReason)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
