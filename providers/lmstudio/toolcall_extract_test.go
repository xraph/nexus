package lmstudio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/xraph/nexus/provider"
)

func TestExtractTextFormToolCalls_ToolCallWrapper(t *testing.T) {
	in := `<tool_call>{"name":"workspace-info","arguments":{"limit":5}}</tool_call>`
	calls, cleaned := ExtractTextFormToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "workspace-info" {
		t.Fatalf("unexpected name: %q", calls[0].Function.Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("args invalid JSON: %v", err)
	}
	if args["limit"] != float64(5) {
		t.Fatalf("expected limit=5, got %v", args["limit"])
	}
	if cleaned != "" {
		t.Fatalf("expected fully-consumed input, got %q", cleaned)
	}
}

func TestExtractTextFormToolCalls_FunctionCallWithProse(t *testing.T) {
	in := "Sure, calling now.\n<function_call>{\"name\":\"openView\",\"arguments\":{\"href\":\"/foo\"}}</function_call>\nDone."
	calls, cleaned := ExtractTextFormToolCalls(in)
	if len(calls) != 1 || calls[0].Function.Name != "openView" {
		t.Fatalf("expected openView, got %+v", calls)
	}
	// Surrounding prose should survive.
	if cleaned == in {
		t.Fatalf("expected wrapper stripped, got unchanged input")
	}
	if cleaned == "" {
		t.Fatalf("expected prose to remain, got empty cleaned")
	}
}

func TestExtractTextFormToolCalls_PipeMarker(t *testing.T) {
	in := `<|tool_call|>{"name":"openSheet","arguments":{"sheetId":"abc"}}<|/tool_call|>`
	calls, _ := ExtractTextFormToolCalls(in)
	if len(calls) != 1 || calls[0].Function.Name != "openSheet" {
		t.Fatalf("expected openSheet, got %+v", calls)
	}
}

func TestExtractTextFormToolCalls_MultipleMarkers(t *testing.T) {
	in := "first\n<tool_call>{\"name\":\"a\"}</tool_call>\nmiddle\n<tool_call>{\"name\":\"b\",\"args\":{\"x\":1}}</tool_call>\nlast"
	calls, cleaned := ExtractTextFormToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "a" || calls[1].Function.Name != "b" {
		t.Fatalf("calls out of order or wrong: %+v", calls)
	}
	if cleaned == in {
		t.Fatalf("expected markers stripped")
	}
}

func TestExtractTextFormToolCalls_NoMarkers(t *testing.T) {
	in := "Just plain content with no tool call."
	calls, cleaned := ExtractTextFormToolCalls(in)
	if calls != nil {
		t.Fatalf("expected nil, got %+v", calls)
	}
	if cleaned != in {
		t.Fatalf("expected input untouched, got %q", cleaned)
	}
}

func TestExtractTextFormToolCalls_MalformedJSON(t *testing.T) {
	in := `<tool_call>{"name":"oops"`
	calls, cleaned := ExtractTextFormToolCalls(in)
	if calls != nil {
		t.Fatalf("expected no calls for unterminated marker, got %d", len(calls))
	}
	if cleaned != in {
		t.Fatalf("expected input untouched, got %q", cleaned)
	}
}

func TestExtractTextFormToolCalls_AlternateKeys(t *testing.T) {
	in := `<tool_call>{"tool":"workspace-info","parameters":{"limit":3}}</tool_call>`
	calls, _ := ExtractTextFormToolCalls(in)
	if len(calls) != 1 || calls[0].Function.Name != "workspace-info" {
		t.Fatalf("expected workspace-info via tool/parameters aliases, got %+v", calls)
	}
}

// --- extractingStream tests ---

// fakeStream emits a fixed sequence of chunks then EOF.
type fakeStream struct {
	chunks []*provider.StreamChunk
	idx    int
	usage  *provider.Usage
}

func (f *fakeStream) Next(_ context.Context) (*provider.StreamChunk, error) {
	if f.idx >= len(f.chunks) {
		return nil, io.EOF
	}
	c := f.chunks[f.idx]
	f.idx++
	return c, nil
}
func (f *fakeStream) Close() error           { return nil }
func (f *fakeStream) Usage() *provider.Usage { return f.usage }

func TestExtractingStream_EmitsSyntheticToolCallChunk(t *testing.T) {
	// Model dribbles content with a complete marker.
	inner := &fakeStream{chunks: []*provider.StreamChunk{
		{Kind: provider.EventDelta, Delta: provider.Delta{Content: "Sure, "}},
		{Kind: provider.EventDelta, Delta: provider.Delta{Content: "<tool_call>"}},
		{Kind: provider.EventDelta, Delta: provider.Delta{Content: "{\"name\":\"workspace-info\"}"}},
		{Kind: provider.EventDelta, Delta: provider.Delta{Content: "</tool_call>"}},
	}}
	wrap := &extractingStream{inner: inner}

	// Drain.
	var got []*provider.StreamChunk
	for {
		c, err := wrap.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		got = append(got, c)
	}

	// Original 4 content chunks + 1 synthesised tool-call chunk.
	if len(got) != 5 {
		t.Fatalf("expected 5 chunks (4 content + 1 synthesised), got %d", len(got))
	}
	last := got[len(got)-1]
	if last.Kind != provider.EventToolCallDelta {
		t.Fatalf("expected last chunk to be EventToolCallDelta, got %v", last.Kind)
	}
	if len(last.Delta.ToolCalls) != 1 || last.Delta.ToolCalls[0].Function.Name != "workspace-info" {
		t.Fatalf("expected synthesised workspace-info, got %+v", last.Delta.ToolCalls)
	}
}

func TestExtractingStream_NoMarkers_PassesThrough(t *testing.T) {
	inner := &fakeStream{chunks: []*provider.StreamChunk{
		{Kind: provider.EventDelta, Delta: provider.Delta{Content: "Hello there!"}},
	}}
	wrap := &extractingStream{inner: inner}

	var got []*provider.StreamChunk
	for {
		c, err := wrap.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		got = append(got, c)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
}

// --- LM Studio observed failure modes ---

func TestExtractTextFormToolCalls_UnclosedWrapperWithArrayPayload(t *testing.T) {
	in := `<tool_call> [{"name": "workspace-info", "arguments": {}}]`
	calls, cleaned := ExtractTextFormToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call from unclosed wrapper + array, got %d", len(calls))
	}
	if calls[0].Function.Name != "workspace-info" {
		t.Fatalf("expected workspace-info, got %q", calls[0].Function.Name)
	}
	if cleaned != "" {
		t.Fatalf("expected fully-consumed input, got %q", cleaned)
	}
}

func TestExtractTextFormToolCalls_UnclosedWrapperSingleObject(t *testing.T) {
	in := `<tool_call>{"name":"workspace-info","arguments":{}}`
	calls, cleaned := ExtractTextFormToolCalls(in)
	if len(calls) != 1 || calls[0].Function.Name != "workspace-info" {
		t.Fatalf("expected workspace-info, got %+v", calls)
	}
	if cleaned != "" {
		t.Fatalf("expected fully-consumed input, got %q", cleaned)
	}
}

func TestExtractTextFormToolCalls_ClosedWrapperArrayMultiCall(t *testing.T) {
	in := `<tool_call>[{"name":"a","args":{"x":1}},{"name":"b"}]</tool_call>`
	calls, _ := ExtractTextFormToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "a" || calls[1].Function.Name != "b" {
		t.Fatalf("calls out of order or wrong: %+v", calls)
	}
}

func TestExtractTextFormToolCalls_UnclosedDoesntFireWhenClosedExists(t *testing.T) {
	// Mixed: one properly-closed marker followed by a partial opener.
	// The closed-form pass must claim the first call; the unclosed
	// fallback must NOT fire (since pass 1 already produced output).
	in := "<tool_call>{\"name\":\"a\"}</tool_call>\n<tool_call>{\"name\":\"b\""
	calls, _ := ExtractTextFormToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call (the closed one only), got %d", len(calls))
	}
	if calls[0].Function.Name != "a" {
		t.Fatalf("expected the closed call to win, got %q", calls[0].Function.Name)
	}
}

func TestExtractTextFormToolCalls_BareJSONArray(t *testing.T) {
	// Some models emit just the array, no wrapper at all.
	// ExtractTextFormToolCalls scans for markers only; bare JSON is
	// handled by callers (the runner has its own JSON-on-own-line
	// path). Here we just confirm the extractor doesn't get tripped
	// up — it should return (nil, in).
	in := `[{"name":"workspace-info"}]`
	calls, cleaned := ExtractTextFormToolCalls(in)
	if calls != nil {
		t.Fatalf("expected nil from bare JSON (no marker), got %d", len(calls))
	}
	if cleaned != in {
		t.Fatalf("expected input untouched, got %q", cleaned)
	}
}
