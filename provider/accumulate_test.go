package provider_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestAccumulator_TextOnly(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{ID: "id-1", Provider: "test", Model: "m", Delta: provider.Delta{Role: "assistant", Content: "Hello"}},
		{Delta: provider.Delta{Content: ", "}},
		{Delta: provider.Delta{Content: "world!"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, &provider.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8})

	resp, err := provider.Accumulate(context.Background(), stream)
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	if got := resp.Choices[0].Message.Content; got != "Hello, world!" {
		t.Fatalf("content = %q, want %q", got, "Hello, world!")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.ID != "id-1" || resp.Provider != "test" || resp.Model != "m" {
		t.Fatalf("metadata: id=%q provider=%q model=%q", resp.ID, resp.Provider, resp.Model)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Fatalf("usage fallback not applied: %+v", resp.Usage)
	}
}

func TestAccumulator_ReasoningAndContent(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{Kind: provider.EventReasoning, Delta: provider.Delta{Reasoning: "Let me think... "}},
		{Kind: provider.EventReasoning, Delta: provider.Delta{Reasoning: "1+1=2."}},
		{Delta: provider.Delta{Content: "The answer is 2."}, FinishReason: "stop"},
	}
	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(chunks, nil))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	if resp.ThinkingContent != "Let me think... 1+1=2." {
		t.Fatalf("thinking content = %q", resp.ThinkingContent)
	}
	if resp.Choices[0].Message.Content != "The answer is 2." {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
}

func TestAccumulator_ToolCallsMergedByIndex(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{ID: "call_1", Type: "function", Function: provider.ToolCallFunc{Name: "lookup", Arguments: `{"q":"`}},
		}}},
		{Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{Function: provider.ToolCallFunc{Arguments: `weather`}},
		}}},
		{Delta: provider.Delta{ToolCalls: []provider.ToolCall{
			{Function: provider.ToolCallFunc{Arguments: `"}`}},
		}}, FinishReason: "tool_calls"},
	}
	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(chunks, nil))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	tools := resp.Choices[0].Message.ToolCalls
	if len(tools) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(tools))
	}
	if tools[0].ID != "call_1" || tools[0].Function.Name != "lookup" {
		t.Fatalf("first slot lost identifying info: %+v", tools[0])
	}
	if tools[0].Function.Arguments != `{"q":"weather"}` {
		t.Fatalf("arguments = %q", tools[0].Function.Arguments)
	}
}

func TestAccumulator_FinalUsageChunkPreferredOverStream(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "hi"}},
		{Kind: provider.EventUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11}},
	}
	streamFallback := &provider.Usage{PromptTokens: 99, CompletionTokens: 99, TotalTokens: 99}
	resp, err := provider.Accumulate(context.Background(), testutil.NewFakeStream(chunks, streamFallback))
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	if resp.Usage.TotalTokens != 11 {
		t.Fatalf("expected final-chunk usage to win, got %+v", resp.Usage)
	}
}

func TestAccumulator_IncrementalAddAndFinalize(t *testing.T) {
	t.Parallel()
	acc := provider.NewAccumulator()
	acc.Add(&provider.StreamChunk{Delta: provider.Delta{Content: "a"}})
	acc.Add(&provider.StreamChunk{Delta: provider.Delta{Content: "b"}})
	acc.Add(&provider.StreamChunk{FinishReason: "stop"})

	resp := acc.Finalize(func() *provider.Usage {
		return &provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	})
	if resp.Choices[0].Message.Content != "ab" {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason = %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 3 {
		t.Fatalf("usage fallback not used: %+v", resp.Usage)
	}
}

func TestAccumulator_ContextCanceled(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a"}},
		{Delta: provider.Delta{Content: "b"}},
	}
	stream := testutil.NewFakeStream(chunks, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := provider.Accumulate(ctx, stream)
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	// Partial response is still returned for inspection.
	if resp == nil {
		t.Fatal("expected partial response, got nil")
	}
}
