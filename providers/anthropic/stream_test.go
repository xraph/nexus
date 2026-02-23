package anthropic_test

import (
	"context"
	"io"
	"testing"

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
	defer stream.Close()

	chunk, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}

	// The message_start event should set the ID on subsequent chunks
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
	defer stream.Close()

	// Collect all text deltas
	var collected string
	for {
		chunk, err := stream.Next(ctx)
		if err == io.EOF {
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
	defer stream.Close()

	chunk, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
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
			defer stream.Close()

			var lastChunk *provider.StreamChunk
			for {
				chunk, err := stream.Next(ctx)
				if err == io.EOF {
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
	defer stream.Close()

	// Drain the stream
	for {
		_, err := stream.Next(ctx)
		if err == io.EOF {
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
	defer stream.Close()

	// Drain the stream
	for {
		_, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next() error: %v", err)
		}
	}

	// Calling Next() again after EOF should still return EOF
	_, err = stream.Next(ctx)
	if err != io.EOF {
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
	defer stream.Close()

	// Should immediately get EOF since there are no content_block_delta events
	_, err = stream.Next(ctx)
	if err != io.EOF {
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
	defer stream.Close()

	chunk, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
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
	defer stream.Close()

	// Read one content chunk - usage should still be nil at this point
	_, err = stream.Next(ctx)
	if err != nil {
		t.Fatalf("Next() error: %v", err)
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
