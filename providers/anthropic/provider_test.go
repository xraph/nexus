package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/testutil"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
)

// --------------------------------------------------------------------
// Constructor
// --------------------------------------------------------------------

func TestNew(t *testing.T) {
	p := anthropic.New("sk-ant-test")
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_WithBaseURL(t *testing.T) {
	p := anthropic.New("sk-ant-test", anthropic.WithBaseURL("http://localhost:9999"))
	if p == nil {
		t.Fatal("New(WithBaseURL) returned nil")
	}
}

// --------------------------------------------------------------------
// Name
// --------------------------------------------------------------------

func TestName(t *testing.T) {
	p := anthropic.New("test-key")
	if got := p.Name(); got != "anthropic" {
		t.Fatalf("Name() = %q, want %q", got, "anthropic")
	}
}

// --------------------------------------------------------------------
// Capabilities
// --------------------------------------------------------------------

func TestCapabilities(t *testing.T) {
	p := anthropic.New("test-key")
	caps := p.Capabilities()

	want := map[string]bool{
		"chat":       true,
		"streaming":  true,
		"vision":     true,
		"tools":      true,
		"json":       true,
		"thinking":   true,
		"batch":      true,
		"embeddings": false,
		"images":     false,
		"audio":      false,
	}

	for name, expected := range want {
		if caps.Supports(name) != expected {
			t.Errorf("Capabilities().Supports(%q) = %v, want %v", name, caps.Supports(name), expected)
		}
	}
}

// --------------------------------------------------------------------
// Complete (mock)
// --------------------------------------------------------------------

func newMockProvider(t *testing.T) (*anthropic.Provider, *testutil.MockServer) {
	t.Helper()
	mock := testutil.NewMockServer(t)
	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))
	return p, mock
}

func anthropicCompletionResp() map[string]any {
	return map[string]any{
		"id":   "msg_test123",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-5-20250514",
		"content": []map[string]any{
			{"type": "text", "text": "Hello! How can I help you?"},
		},
		"stop_reason": "end_turn",
		"usage": map[string]any{
			"input_tokens":  10,
			"output_tokens": 8,
		},
	}
}

func TestComplete(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())

	ctx := context.Background()
	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp == nil {
		t.Fatal("Complete() returned nil response")
	}
	if resp.ID != "msg_test123" {
		t.Errorf("resp.ID = %q, want %q", resp.ID, "msg_test123")
	}
	if resp.Provider != "anthropic" {
		t.Errorf("resp.Provider = %q, want %q", resp.Provider, "anthropic")
	}
	if resp.Model != "claude-sonnet-4-5-20250514" {
		t.Errorf("resp.Model = %q, want %q", resp.Model, "claude-sonnet-4-5-20250514")
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	choice := resp.Choices[0]
	if choice.Message.Role != "assistant" {
		t.Errorf("choice.Message.Role = %q, want %q", choice.Message.Role, "assistant")
	}
	if choice.Message.Content != "Hello! How can I help you?" {
		t.Errorf("choice.Message.Content = %q, want %q", choice.Message.Content, "Hello! How can I help you?")
	}
	if choice.FinishReason != "stop" {
		t.Errorf("choice.FinishReason = %q, want %q", choice.FinishReason, "stop")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("Usage.PromptTokens = %d, want 10", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 8 {
		t.Errorf("Usage.CompletionTokens = %d, want 8", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 18 {
		t.Errorf("Usage.TotalTokens = %d, want 18", resp.Usage.TotalTokens)
	}
}

func TestComplete_RequestFormat(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model:  "claude-sonnet-4-5-20250514",
		System: "You are a helpful assistant.",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	// Verify request was sent to correct path
	if path := mock.Ctrl.GetLastPath(); path != "/v1/messages" {
		t.Errorf("request path = %q, want %q", path, "/v1/messages")
	}

	// Verify headers
	headers := mock.Ctrl.GetLastHeader()
	if got := headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := headers.Get("x-api-key"); got != "test-key" {
		t.Errorf("x-api-key = %q, want %q", got, "test-key")
	}
	if got := headers.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
	}

	// Verify request body format
	body := mock.Ctrl.GetLastBody()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	if req["model"] != "claude-sonnet-4-5-20250514" {
		t.Errorf("request model = %v, want %q", req["model"], "claude-sonnet-4-5-20250514")
	}
	if req["system"] != "You are a helpful assistant." {
		t.Errorf("request system = %v, want %q", req["system"], "You are a helpful assistant.")
	}
	// stream should be false or absent (omitempty may omit it)
	if stream, ok := req["stream"].(bool); ok && stream {
		t.Errorf("request stream = %v, want false or absent", req["stream"])
	}
	if maxTokens, ok := req["max_tokens"].(float64); !ok || int(maxTokens) != 256 {
		t.Errorf("request max_tokens = %v, want 256", req["max_tokens"])
	}

	// Verify messages are present and system messages are extracted
	msgs, ok := req["messages"].([]any)
	if !ok {
		t.Fatal("request messages is not an array")
	}
	if len(msgs) != 1 {
		t.Fatalf("request messages length = %d, want 1", len(msgs))
	}
}

func TestComplete_DefaultMaxTokens(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		// MaxTokens intentionally omitted
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	body := mock.Ctrl.GetLastBody()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	if maxTokens, ok := req["max_tokens"].(float64); !ok || int(maxTokens) != 4096 {
		t.Errorf("default max_tokens = %v, want 4096", req["max_tokens"])
	}
}

func TestComplete_SystemMessageExtraction(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}

	body := mock.Ctrl.GetLastBody()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	// System message should be extracted to the top-level "system" field
	if req["system"] != "Be concise." {
		t.Errorf("request system = %v, want %q", req["system"], "Be concise.")
	}

	// Messages array should NOT include the system message
	msgs, ok := req["messages"].([]any)
	if !ok {
		t.Fatal("request messages is not an array")
	}
	if len(msgs) != 1 {
		t.Errorf("messages length = %d, want 1 (system should be extracted)", len(msgs))
	}
}

func TestComplete_APIError(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetStatusCode(http.StatusBadRequest)

	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("Complete() should return error on 400 status")
	}
}

func TestComplete_StopReasonMapping(t *testing.T) {
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
			p, mock := newMockProvider(t)
			resp := anthropicCompletionResp()
			resp["stop_reason"] = tt.stopReason
			mock.Ctrl.SetCompletion(resp)

			ctx := context.Background()
			got, err := p.Complete(ctx, &provider.CompletionRequest{
				Model: "claude-sonnet-4-5-20250514",
				Messages: []provider.Message{
					{Role: "user", Content: "Hello"},
				},
				MaxTokens: 100,
			})
			if err != nil {
				t.Fatalf("Complete() error: %v", err)
			}
			if got.Choices[0].FinishReason != tt.wantFinish {
				t.Errorf("FinishReason = %q, want %q", got.Choices[0].FinishReason, tt.wantFinish)
			}
		})
	}
}

// --------------------------------------------------------------------
// CompleteStream (mock)
// --------------------------------------------------------------------

func TestCompleteStream(t *testing.T) {
	p, mock := newMockProvider(t)

	lines := []string{
		testutil.AnthropicEventJSON("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":    "msg_stream1",
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
				"text": " world",
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": "end_turn",
			},
			"usage": map[string]any{
				"output_tokens": 5,
			},
		}),
		"",
		testutil.AnthropicEventJSON("message_stop", map[string]any{
			"type": "message_stop",
		}),
		"",
	}

	mock.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler(lines))

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

	var chunks []*provider.StreamChunk
	for {
		chunk, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next() error: %v", err)
		}
		chunks = append(chunks, chunk)
	}

	// Expect: content_block_delta("Hello"), content_block_delta(" world"), message_delta(stop)
	if len(chunks) < 3 {
		t.Fatalf("got %d chunks, want at least 3", len(chunks))
	}

	// First text chunk
	if chunks[0].Delta.Content != "Hello" {
		t.Errorf("chunk[0].Delta.Content = %q, want %q", chunks[0].Delta.Content, "Hello")
	}
	if chunks[0].ID != "msg_stream1" {
		t.Errorf("chunk[0].ID = %q, want %q", chunks[0].ID, "msg_stream1")
	}
	if chunks[0].Provider != "anthropic" {
		t.Errorf("chunk[0].Provider = %q, want %q", chunks[0].Provider, "anthropic")
	}

	// Second text chunk
	if chunks[1].Delta.Content != " world" {
		t.Errorf("chunk[1].Delta.Content = %q, want %q", chunks[1].Delta.Content, " world")
	}

	// Final delta (message_delta) with stop reason
	if chunks[2].FinishReason != "stop" {
		t.Errorf("final chunk FinishReason = %q, want %q", chunks[2].FinishReason, "stop")
	}

	// Usage should be captured
	usage := stream.Usage()
	if usage == nil {
		t.Fatal("stream.Usage() returned nil")
	}
	if usage.CompletionTokens != 5 {
		t.Errorf("Usage.CompletionTokens = %d, want 5", usage.CompletionTokens)
	}
}

func TestCompleteStream_APIError(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetStatusCode(http.StatusInternalServerError)

	ctx := context.Background()
	_, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "claude-sonnet-4-5-20250514",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
		Stream:    true,
	})
	if err == nil {
		t.Fatal("CompleteStream() should return error on 500 status")
	}
}

// --------------------------------------------------------------------
// Embed -> ErrNotSupported
// --------------------------------------------------------------------

func TestEmbed_NotSupported(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()
	_, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "any-model",
		Input: []string{"Hello"},
	})
	if err != provider.ErrNotSupported {
		t.Fatalf("Embed() = %v, want ErrNotSupported", err)
	}
}

// --------------------------------------------------------------------
// Healthy
// --------------------------------------------------------------------

func TestHealthy_405(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetStatusCode(http.StatusMethodNotAllowed) // 405

	ctx := context.Background()
	if !p.Healthy(ctx) {
		t.Error("Healthy() = false, want true for 405 Method Not Allowed")
	}
}

func TestHealthy_200(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetStatusCode(http.StatusOK) // 200

	ctx := context.Background()
	if !p.Healthy(ctx) {
		t.Error("Healthy() = false, want true for 200 OK")
	}
}

func TestHealthy_401(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetStatusCode(http.StatusUnauthorized) // 401

	ctx := context.Background()
	if !p.Healthy(ctx) {
		t.Error("Healthy() = false, want true for 401 Unauthorized (server reachable)")
	}
}

func TestHealthy_500(t *testing.T) {
	p, mock := newMockProvider(t)
	mock.Ctrl.SetStatusCode(http.StatusInternalServerError) // 500

	ctx := context.Background()
	if p.Healthy(ctx) {
		t.Error("Healthy() = true, want false for 500 Internal Server Error")
	}
}

func TestHealthy_Unreachable(t *testing.T) {
	p := anthropic.New("test-key", anthropic.WithBaseURL("http://127.0.0.1:1"))
	ctx := context.Background()
	if p.Healthy(ctx) {
		t.Error("Healthy() = true, want false for unreachable server")
	}
}

// --------------------------------------------------------------------
// Models
// --------------------------------------------------------------------

func TestModels(t *testing.T) {
	p := anthropic.New("test-key")
	ctx := context.Background()
	models, err := p.Models(ctx)
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("Models() returned empty list")
	}
}

// --------------------------------------------------------------------
// Provider interface conformance
// --------------------------------------------------------------------

func TestConformance(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))

	providertest.TestProviderContract(t, p)
}

func TestConformance_EmbedNotSupported(t *testing.T) {
	p := anthropic.New("test-key")
	providertest.TestProviderEmbedNotSupported(t, p)
}

func TestConformance_Complete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetCompletion(anthropicCompletionResp())

	p := anthropic.New("test-key", anthropic.WithBaseURL(mock.Server.URL))

	providertest.TestProviderComplete(t, p)
}

// --------------------------------------------------------------------
// Compile-time interface check
// --------------------------------------------------------------------

var _ provider.Provider = (*anthropic.Provider)(nil)
