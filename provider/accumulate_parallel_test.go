package provider_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// Guards: two parallel tool calls that arrive with distinct IDs but the same
// slice index must finalize as two calls, each with its own arguments.
// Anthropic, Bedrock, Gemini Live and the OpenAI realtime stream all emit every
// tool delta as a one-element slice, so idx is always 0 for every call. Before
// the fix the second call's ID was unknown, the index fallback matched the
// first slot, the second call's JSON was glued onto the first's arguments and
// the second call itself vanished. The model's own turn then failed to
// round-trip on the next request ("tool_use.input: Input should be an object").
func TestAccumulator_DistinctIDsAtSameIndexStayApart(t *testing.T) {
	t.Parallel()
	one := func(id, name, args string) *provider.StreamChunk {
		return &provider.StreamChunk{Kind: provider.EventToolCallDelta, Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{ID: id, Type: "function", Function: provider.ToolCallFunc{Name: name, Arguments: args}},
		}}}
	}
	chunks := []*provider.StreamChunk{
		one("toolu_1", "read_history", `{"id": "layer_a",`),
		one("toolu_1", "read_history", ` "minutes": 30}`),
		one("toolu_2", "related_resources", `{"id": "mk-1"}`),
		one("toolu_1", "read_history", ``), // a trailing empty delta must not disturb either slot
	}
	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(chunks, nil))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
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

// Guards: an OpenAI-style stream that names the call once and then streams
// ID-less argument fragments at the same index keeps merging by index. The
// distinct-ID rule above must only split slots whose IDs actually differ.
func TestAccumulator_IDLessFragmentsStillMergeByIndex(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{ID: "call_a", Type: "function", Function: provider.ToolCallFunc{Name: "lookup", Arguments: `{"q":`}},
			{ID: "call_b", Type: "function", Function: provider.ToolCallFunc{Name: "fetch", Arguments: `{"u":`}},
		}}},
		{Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{Function: provider.ToolCallFunc{Arguments: `"x"}`}},
			{Function: provider.ToolCallFunc{Arguments: `"y"}`}},
		}}, FinishReason: "tool_calls"},
	}
	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(chunks, nil))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	tools := resp.Choices[0].Message.ToolCalls
	if len(tools) != 2 {
		t.Fatalf("got %d tool calls, want 2: %+v", len(tools), tools)
	}
	if tools[0].ID != "call_a" || tools[0].Function.Arguments != `{"q":"x"}` {
		t.Fatalf("first call = %+v", tools[0])
	}
	if tools[1].ID != "call_b" || tools[1].Function.Arguments != `{"u":"y"}` {
		t.Fatalf("second call = %+v", tools[1])
	}
}
