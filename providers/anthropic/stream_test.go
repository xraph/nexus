package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

// --------------------------------------------------------------------
// SSE event: message_start
// --------------------------------------------------------------------

func TestStream_MessageStart_CapturesID(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_abc123",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Hi",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Drain until we see a chunk with a populated ID (message_start now
	// emits a discrete EventMessageStart frame that also carries the ID).
	var chunk *provider.StreamChunk
	for {
		c, nextErr := stream.Next(ctx)
		if nextErr != nil {
			t.Fatalf("Next() error: %v", nextErr)
		}
		if c.ID != "" {
			chunk = c
			break
		}
	}

	if chunk.ID != "msg_abc123" {
		t.Errorf("chunk.ID = %q, want %q", chunk.ID, "msg_abc123")
	}
}

// --------------------------------------------------------------------
// SSE event: content_block_delta (text_delta)
// --------------------------------------------------------------------

func TestStream_ContentBlockDelta_TextDelta(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_delta1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "The ",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "answer ",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "is 42.",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 6,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "What is the answer?"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Collect all text deltas
	var collected string
	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
		collected += chunk.Delta.Content
	}

	want := "The answer is 42."
	if collected != want {
		t.Errorf("collected text = %q, want %q", collected, want)
	}
}

func TestStream_ContentBlockDelta_ProviderAndModel(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_pm1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Hi",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Skip the discrete message_start frame and verify provider/model on
	// the first content delta.
	var chunk *provider.StreamChunk
	for {
		c, nextErr := stream.Next(ctx)
		if nextErr != nil {
			t.Fatalf("Next() error: %v", nextErr)
		}
		if c.Kind == provider.EventDelta && c.Delta.Content != "" {
			chunk = c
			break
		}
	}
	if chunk.Provider != "anthropic" {
		t.Errorf("chunk.Provider = %q, want %q", chunk.Provider, "anthropic")
	}
	if chunk.Model != "claude-sonnet-4-5-20250514" {
		t.Errorf("chunk.Model = %q, want %q", chunk.Model, "claude-sonnet-4-5-20250514")
	}
}

// --------------------------------------------------------------------
// SSE event: message_delta (stop_reason + usage)
// --------------------------------------------------------------------

func TestStream_MessageDelta_StopReason(t *testing.T) {
	tests := []struct {
		name       string
		stopReason string
		wantFinish string
	}{
		{"end_turn", "end_turn", "stop"},
		{"max_tokens", "max_tokens", "length"},
		{"tool_use", "tool_use", "tool_calls"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := testutil.NewMockServer(t)

			lines := []string{
				testutil.AnthropicEventJSON("message_start", map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id":    "msg_sr_" + tt.name,
						"model": "claude-sonnet-4-5-20250514",
					},
				}),
				"",
				testutil.AnthropicEventJSON("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": 0,
					"delta": map[string]any{
						"type": "text_delta",
						"text": "Test",
					},
				}),
				"",
				testutil.AnthropicEventJSON("message_delta", map[string]any{
					"type": "message_delta",
					"delta": map[string]any{
						"stop_reason": tt.stopReason,
					},
					"usage": map[string]any{
						"output_tokens": 3,
					},
				}),
				"",
				testutil.AnthropicEventJSON("message_stop", map[string]any{
					"type": "message_stop",
				}),
				"",
			}
			mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

			p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
			ctx := context.Background()

			stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
				Model: "claude-sonnet-4-5-20250514",
				Messages: []provider.Message{
					{Role: "user", Content: "Test"},
				},
				MaxTokens: 100,
				Stream:    true,
			})
			if err != nil {
				t.Fatalf("CompleteStream() error: %v", err)
			}
			defer func() { _ = stream.Close() }()

			var lastChunk *provider.StreamChunk
			for {
				chunk, err := stream.Next(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("Next() error: %v", err)
				}
				lastChunk = chunk
			}

			if lastChunk == nil {
				t.Fatal("no chunks received")
			}
			if lastChunk.FinishReason != tt.wantFinish {
				t.Errorf("FinishReason = %q, want %q", lastChunk.FinishReason, tt.wantFinish)
			}
		})
	}
}

func TestStream_MessageDelta_Usage(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_usage1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "OK",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 42,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Count"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Drain the stream
	for {
		_, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
	}

	usage := stream.Usage()
	if usage == nil {
		t.Fatal("Usage() returned nil after stream completed")
	}
	if usage.CompletionTokens != 42 {
		t.Errorf("Usage.CompletionTokens = %d, want 42", usage.CompletionTokens)
	}
}

// --------------------------------------------------------------------
// SSE event: message_stop (EOF)
// --------------------------------------------------------------------

func TestStream_MessageStop_ReturnsEOF(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_eof1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Done",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 1,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Done?"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Drain the stream
	for {
		_, err = stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
	}

	// Calling Next() again after EOF should still return EOF
	_, err = stream.Next(ctx)
	if !errors.Is(err, io.EOF) {
		t.Errorf("Next() after EOF = %v, want io.EOF", err)
	}
}

// --------------------------------------------------------------------
// Stream Close
// --------------------------------------------------------------------

func TestStream_Close(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_close1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Hello",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": " there",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 2,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}

	// Read one chunk then close early
	_, err = stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

// --------------------------------------------------------------------
// Empty stream (only message_start + message_stop, no content)
// --------------------------------------------------------------------

func TestStream_EmptyContent(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_empty1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// First call now returns the discrete message_start frame. Drain past
	// it and verify the next call hits io.EOF since no content was emitted.
	_, err = stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error on message_start frame: %v", err)
	}
	_, err = stream.Next(ctx)
	if !errors.Is(err, io.EOF) {
		t.Errorf("Next() = %v, want io.EOF for empty stream", err)
	}
}

// --------------------------------------------------------------------
// SSE comment lines (starting with :) should be ignored
// --------------------------------------------------------------------

func TestStream_IgnoresSSEComments(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		": this is a comment",
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_comment1",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		": another comment",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "OK",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Drain past the discrete message_start frame to find the text delta.
	var chunk *provider.StreamChunk
	for {
		c, nextErr := stream.Next(ctx)
		if nextErr != nil {
			t.Fatalf("Next() error: %v", nextErr)
		}
		if c.Kind == provider.EventDelta && c.Delta.Content != "" {
			chunk = c
			break
		}
	}
	if chunk.Delta.Content != "OK" {
		t.Errorf("chunk.Delta.Content = %q, want %q", chunk.Delta.Content, "OK")
	}
}

// --------------------------------------------------------------------
// Usage is nil before message_delta is received
// --------------------------------------------------------------------

func TestStream_UsageNilBeforeMessageDelta(t *testing.T) {
	mock := testutil.NewMockServer(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_usage_nil",
				"model": "claude-sonnet-4-5-20250514",
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type": "text_delta",
				"text": "Partial",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 10,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Drain frames before message_delta — Usage() should still be nil
	// while we're reading message_start + the content delta.
	for {
		c, nextErr := stream.Next(ctx)
		if nextErr != nil {
			t.Fatalf("Next() error: %v", nextErr)
		}
		if c.Kind == provider.EventDelta && c.Delta.Content != "" {
			break
		}
	}
	if stream.Usage() != nil {
		t.Error("Usage() should be nil before message_delta is received")
	}

	// Read message_delta chunk - usage should now be set
	_, err = stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}

	usage := stream.Usage()
	if usage == nil {
		t.Fatal("Usage() should not be nil after message_delta")
	}
	if usage.CompletionTokens != 10 {
		t.Errorf("Usage.CompletionTokens = %d, want 10", usage.CompletionTokens)
	}
}

// --------------------------------------------------------------------
// Cache usage and timeouts
// --------------------------------------------------------------------

func TestStreamUsageReadsCacheTokens(t *testing.T) {
	mock := testutil.NewMockServer(t)
	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_cache",
				"model": "claude-opus-5-5",
				"usage": map[string]any{
					"input_tokens":                100,
					"cache_read_input_tokens":     600,
					"cache_creation_input_tokens": 300,
					"cache_creation": map[string]any{
						"ephemeral_5m_input_tokens": 200,
						"ephemeral_1h_input_tokens": 100,
					},
					"output_tokens": 1,
				},
			},
		}),
		"",
		testutil.AnthropicEventJSON("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "OK"},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{
				"input_tokens":                100,
				"cache_read_input_tokens":     600,
				"cache_creation_input_tokens": 300,
				"output_tokens":               50,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model:    "claude-opus-5-5",
		Messages: []provider.Message{{Role: "user", Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	for {
		_, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
	}

	want := provider.Usage{
		PromptTokens:         1000,
		CompletionTokens:     50,
		CacheReadTokens:      600,
		CacheWriteTokens:     300,
		CacheWriteLongTokens: 100,
		TotalTokens:          1050,
	}
	got := stream.Usage()
	if got == nil {
		t.Fatal("Usage() = nil")
	}
	if *got != want {
		t.Errorf("Usage = %+v, want %+v", *got, want)
	}
}

func TestStreamOutlivesTheResponseHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := range 5 {
			time.Sleep(40 * time.Millisecond)
			_, _ = fmt.Fprintf(w, "%s\n\n", testutil.AnthropicEventJSON("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": fmt.Sprintf("chunk%d", i)},
			}))
			flusher.Flush()
		}
		_, _ = fmt.Fprintf(w, "%s\n\n", testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}))
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	p := anthropic.New("test-key", anthropic.WithBaseURL(srv.URL), anthropic.WithTimeout(50*time.Millisecond))
	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model:    "claude-opus-5-5",
		Messages: []provider.Message{{Role: "user", Content: "Hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var got []string
	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() error after %d chunks: %v", len(got), err)
		}
		got = append(got, chunk.Delta.Content)
	}
	if len(got) != 5 {
		t.Fatalf("got %d chunks %q, want 5", len(got), got)
	}
	for i, c := range got {
		if want := fmt.Sprintf("chunk%d", i); c != want {
			t.Errorf("chunk %d = %q, want %q", i, c, want)
		}
	}
}

func TestStreamRebuildsNativeContent(t *testing.T) {
	mock := testutil.NewMockServer(t)
	ev := testutil.AnthropicEventJSON
	delta := func(idx int, d map[string]any) string {
		return ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": d})
	}
	start := func(idx int, b map[string]any) string {
		return ev("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": b})
	}
	stop := func(idx int) string {
		return ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
	}
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler([]string{
		ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_n", "model": nativeModel}}), "",
		start(0, map[string]any{"type": "thinking", "thinking": "", "signature": ""}), "",
		delta(0, map[string]any{"type": "thinking_delta", "thinking": "x"}), "",
		delta(0, map[string]any{"type": "signature_delta", "signature": "sig1"}), "",
		stop(0), "",
		start(1, map[string]any{"type": "redacted_thinking", "data": "d"}), "",
		stop(1), "",
		start(2, map[string]any{"type": "text", "text": ""}), "",
		delta(2, map[string]any{"type": "text_delta", "text": "hi"}), "",
		stop(2), "",
		start(3, map[string]any{"type": "tool_use", "id": "t1", "name": "f", "input": map[string]any{}}), "",
		delta(3, map[string]any{"type": "input_json_delta", "partial_json": `{"a":`}), "",
		delta(3, map[string]any{"type": "input_json_delta", "partial_json": `1}`}), "",
		stop(3), "",
		ev("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 9}}), "",
		ev("message_stop", map[string]any{"type": "message_stop"}), "",
	}))

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model:    nativeModel,
		Messages: []provider.Message{{Role: "user", Content: "q"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var chunks []*provider.StreamChunk
	for {
		c, nextErr := stream.Next(ctx)
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatalf("Next() error: %v", nextErr)
		}
		chunks = append(chunks, c)
	}
	last := chunks[len(chunks)-1]
	if last.Kind != provider.EventMessageStop || last.Delta.Native == nil {
		t.Fatalf("last chunk = %+v, want an EventMessageStop chunk carrying Native", last)
	}
	for _, c := range chunks[:len(chunks)-1] {
		if c.Delta.Native != nil {
			t.Errorf("chunk %+v carries Native before message_stop", c)
		}
	}
	native := last.Delta.Native
	if native.Provider != "anthropic" || native.Model != nativeModel {
		t.Errorf("Native provider/model = %q/%q, want anthropic/%s", native.Provider, native.Model, nativeModel)
	}

	var blocks []map[string]any
	if err := json.Unmarshal(native.Content, &blocks); err != nil {
		t.Fatalf("decode native content %s: %v", native.Content, err)
	}
	if len(blocks) != 4 {
		t.Fatalf("native content = %s, want 4 blocks", native.Content)
	}
	wantTypes := []string{"thinking", "redacted_thinking", "text", "tool_use"}
	for i, want := range wantTypes {
		if blocks[i]["type"] != want {
			t.Errorf("blocks[%d].type = %v, want %s", i, blocks[i]["type"], want)
		}
	}
	if blocks[0]["thinking"] != "x" || blocks[0]["signature"] != "sig1" {
		t.Errorf("thinking block = %v, want thinking x with signature sig1", blocks[0])
	}
	if blocks[1]["data"] != "d" {
		t.Errorf("redacted_thinking block = %v, want data d", blocks[1])
	}
	if blocks[2]["text"] != "hi" {
		t.Errorf("text block = %v, want text hi", blocks[2])
	}
	if blocks[3]["id"] != "t1" || blocks[3]["name"] != "f" || !reflect.DeepEqual(blocks[3]["input"], map[string]any{"a": float64(1)}) {
		t.Errorf("tool_use block = %v, want t1 f {\"a\":1}", blocks[3])
	}

	if _, err := stream.Next(ctx); !errors.Is(err, io.EOF) {
		t.Errorf("Next() after the native chunk = %v, want io.EOF", err)
	}
}

func TestStreamKeepsUnknownNativeBlocks(t *testing.T) {
	mock := testutil.NewMockServer(t)
	ev := testutil.AnthropicEventJSON
	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler([]string{
		ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_u", "model": nativeModel}}), "",
		ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "future_block", "payload": map[string]any{"k": "v"}}}), "",
		ev("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}), "",
		ev("message_stop", map[string]any{"type": "message_stop"}), "",
	}))
	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	resp, err := streamAndAccumulate(t, p)
	if err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	native := resp.Choices[0].Message.Native
	if native == nil {
		t.Fatal("accumulated Message.Native is nil")
	}
	var blocks []map[string]any
	if err := json.Unmarshal(native.Content, &blocks); err != nil {
		t.Fatalf("decode native content: %v", err)
	}
	want := []map[string]any{{"type": "future_block", "payload": map[string]any{"k": "v"}}}
	if !reflect.DeepEqual(blocks, want) {
		t.Errorf("native blocks = %v, want %v", blocks, want)
	}
}

func streamAndAccumulate(t *testing.T, p *anthropic.Provider) (*provider.CompletionResponse, error) {
	t.Helper()
	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    nativeModel,
		Messages: []provider.Message{{Role: "user", Content: "q"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	return provider.Accumulate(context.Background(), stream)
}

func TestStreamWithAnUnknownDeltaCarriesNoNative(t *testing.T) {
	ev := testutil.AnthropicEventJSON
	cases := map[string][]string{
		"unknown delta type": {
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}), "",
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": "hi"}}), "",
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "future_delta", "x": 1}}), "",
		},
		"delta for a block that never started": {
			ev("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}), "",
			ev("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "text_delta", "text": "lost"}}), "",
		},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			mock := testutil.NewMockServer(t)
			lines := make([]string, 0, len(body)+4)
			lines = append(lines, ev("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_x", "model": nativeModel}}), "")
			lines = append(lines, body...)
			lines = append(lines, ev("message_stop", map[string]any{"type": "message_stop"}), "")
			mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))
			p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
			resp, err := streamAndAccumulate(t, p)
			if err != nil {
				t.Fatalf("accumulate: %v", err)
			}
			if native := resp.Choices[0].Message.Native; native != nil {
				t.Fatalf("Native = %s, want none: the stream had content the rebuild cannot carry", native.Content)
			}
		})
	}
}
