package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// ---------------------------------------------------------------------------
// Table-driven SSE parsing tests
//
// Each test case defines raw SSE lines that the mock server will write,
// then asserts on the chunks returned by the stream.
// ---------------------------------------------------------------------------

func TestStream_NormalChunks(t *testing.T) {
	// Standard multi-chunk stream with content tokens.
	chunks := []string{
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", "Hello", ""),
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", " there", ""),
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", "", "stop"),
	}

	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	got := drainContent(t, stream)
	if got != "Hello there" {
		t.Errorf("content = %q, want %q", got, "Hello there")
	}
}

func TestStream_DoneSignal(t *testing.T) {
	// After [DONE] the stream must return io.EOF on subsequent calls.
	chunks := []string{
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", "hi", "stop"),
	}

	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	_ = drainContent(t, stream)

	// Further calls must return EOF.
	ctx := context.Background()
	_, err := stream.Next(ctx)
	if err != io.EOF {
		t.Errorf("expected io.EOF after [DONE], got %v", err)
	}
}

func TestStream_MalformedJSON(t *testing.T) {
	// If a data line contains invalid JSON, the stream should return an error.
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// Write a chunk with malformed JSON.
		_, _ = fmt.Fprint(w, "data: {invalid json!}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	_, err = stream.Next(ctx)
	if err == nil {
		t.Fatal("expected error from malformed JSON chunk")
	}
}

func TestStream_UsageInFinalChunk(t *testing.T) {
	// OpenAI sends usage information in the final chunk (with empty choices).
	chunks := []string{
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", "answer", "stop"),
		testutil.OpenAIChunkJSONWithUsage("id-1", "gpt-4o", 10, 5, 15),
	}

	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	// Drain the stream.
	_ = drainContent(t, stream)

	usage := stream.Usage()
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", usage.PromptTokens)
	}
	if usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", usage.CompletionTokens)
	}
	if usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", usage.TotalTokens)
	}
}

func TestStream_EmptyLinesAndComments(t *testing.T) {
	// SSE spec: empty lines and lines beginning with ":" are comments/keep-alives.
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Empty lines and SSE comments before/between data lines.
		_, _ = fmt.Fprint(w, "\n")
		_, _ = fmt.Fprint(w, ": this is an SSE comment\n")
		_, _ = fmt.Fprint(w, "\n")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", testutil.OpenAIChunkJSON("id-1", "gpt-4o", "ok", ""))
		_, _ = fmt.Fprint(w, "\n")
		_, _ = fmt.Fprint(w, ": another comment\n")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", testutil.OpenAIChunkJSON("id-1", "gpt-4o", "", "stop"))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	got := drainContent(t, stream)
	if got != "ok" {
		t.Errorf("content = %q, want %q", got, "ok")
	}
}

func TestStream_ToolCalls(t *testing.T) {
	// Verify that tool_calls in delta are correctly parsed.
	toolCallChunk := map[string]any{
		"id":      "chatcmpl-tc",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "gpt-4o",
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []map[string]any{
						{
							"id":   "call_123",
							"type": "function",
							"function": map[string]any{
								"name":      "get_weather",
								"arguments": `{"city":"NYC"}`,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}
	chunkJSON, _ := json.Marshal(toolCallChunk)

	chunks := []string{string(chunkJSON)}
	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	ctx := context.Background()
	chunk, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if len(chunk.Delta.ToolCalls) == 0 {
		t.Fatal("expected tool_calls in delta")
	}
	tc := chunk.Delta.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("tool call name = %q, want %q", tc.Function.Name, "get_weather")
	}
	if chunk.FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want %q", chunk.FinishReason, "tool_calls")
	}
}

func TestStream_RoleInFirstChunk(t *testing.T) {
	// OpenAI typically sends role in the first chunk delta.
	roleChunk := map[string]any{
		"id":      "chatcmpl-r",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "gpt-4o",
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{
					"role": "assistant",
				},
				"finish_reason": nil,
			},
		},
	}
	chunkJSON, _ := json.Marshal(roleChunk)

	chunks := []string{
		string(chunkJSON),
		testutil.OpenAIChunkJSON("chatcmpl-r", "gpt-4o", "Hi", "stop"),
	}

	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	ctx := context.Background()
	first, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if first.Delta.Role != "assistant" {
		t.Errorf("first chunk role = %q, want %q", first.Delta.Role, "assistant")
	}
}

func TestStream_NoUsageWithoutUsageChunk(t *testing.T) {
	// If no usage chunk is sent, Usage() should return nil.
	chunks := []string{
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", "data", "stop"),
	}

	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	_ = drainContent(t, stream)

	if stream.Usage() != nil {
		t.Error("expected nil usage when no usage chunk was sent")
	}
}

func TestStream_CloseStopsIteration(t *testing.T) {
	// After Close(), Next() should return io.EOF.
	chunks := []string{
		testutil.OpenAIChunkJSON("id-1", "gpt-4o", "data", ""),
	}
	// Note: we use a handler that never sends [DONE] to test close behavior.
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", c)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}

	// Read one chunk, then close.
	_, err = stream.Next(ctx)
	if err != nil {
		t.Fatalf("first Next() error: %v", err)
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// After close, done flag should be set.
	_, err = stream.Next(ctx)
	if err != io.EOF {
		t.Errorf("expected io.EOF after Close(), got %v", err)
	}
}

func TestStream_EmptyChoicesChunkSkipped(t *testing.T) {
	// A chunk with empty choices array should be skipped (no StreamChunk returned).
	emptyChoicesChunk := map[string]any{
		"id":      "chatcmpl-e",
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   "gpt-4o",
		"choices": []map[string]any{},
	}
	emptyJSON, _ := json.Marshal(emptyChoicesChunk)

	chunks := []string{
		string(emptyJSON),
		testutil.OpenAIChunkJSON("chatcmpl-e", "gpt-4o", "real", "stop"),
	}

	stream := startStream(t, chunks)
	defer func() { _ = stream.Close() }()

	ctx := context.Background()
	chunk, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	// The empty-choices chunk should have been skipped; we get "real" directly.
	if chunk.Delta.Content != "real" {
		t.Errorf("content = %q, want %q", chunk.Delta.Content, "real")
	}
}

func TestStream_NonDataLinesIgnored(t *testing.T) {
	// Lines that are not "data: " prefixed should be silently skipped.
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		_, _ = fmt.Fprint(w, "event: ping\n")
		_, _ = fmt.Fprint(w, "id: 42\n")
		_, _ = fmt.Fprint(w, "retry: 5000\n")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", testutil.OpenAIChunkJSON("id-1", "gpt-4o", "ok", "stop"))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	got := drainContent(t, stream)
	if got != "ok" {
		t.Errorf("content = %q, want %q", got, "ok")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// startStream creates a mock server, configures it with the given chunks,
// and returns the resulting provider.Stream.
func startStream(t *testing.T, chunks []string) provider.Stream {
	t.Helper()

	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler(chunks))

	p := New("test-key", WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o",
		Messages: []provider.Message{
			{Role: "user", Content: "test"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	return stream
}

// drainContent reads all chunks from the stream and concatenates Delta.Content.
func drainContent(t *testing.T, stream provider.Stream) string {
	t.Helper()
	ctx := context.Background()
	var out string
	for {
		chunk, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		out += chunk.Delta.Content
	}
	return out
}
