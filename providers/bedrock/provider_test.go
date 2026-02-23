package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providertest"
)

func bedrockMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		// Check for stream endpoint.
		if r.URL.Path != "" && len(r.URL.Path) > 20 {
			if r.URL.Path[len(r.URL.Path)-len("converse-with-response-stream"):] == "converse-with-response-stream" {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, _ := w.(http.Flusher)
				fmt.Fprintf(w, "data: %s\n\n", `{"messageStart":{"role":"assistant"}}`)
				flusher.Flush()
				fmt.Fprintf(w, "data: %s\n\n", `{"contentBlockDelta":{"delta":{"text":"Hello"},"contentBlockIndex":0}}`)
				flusher.Flush()
				fmt.Fprintf(w, "data: %s\n\n", `{"contentBlockDelta":{"delta":{"text":" world"},"contentBlockIndex":0}}`)
				flusher.Flush()
				fmt.Fprintf(w, "data: %s\n\n", `{"messageStop":{"stopReason":"end_turn"}}`)
				flusher.Flush()
				fmt.Fprintf(w, "data: %s\n\n", `{"metadata":{"usage":{"inputTokens":5,"outputTokens":3,"totalTokens":8}}}`)
				flusher.Flush()
				return
			}
		}

		// Non-stream converse endpoint.
		_ = body
		resp := converseResponse{
			Output: converseOutput{
				Message: &converseMessage{
					Role: "assistant",
					Content: []contentBlock{
						{Text: "Hello! How can I help you?"},
					},
				},
			},
			StopReason: "end_turn",
			Usage: converseUsage{
				InputTokens:  10,
				OutputTokens: 8,
				TotalTokens:  18,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestName(t *testing.T) {
	p := New("key", "secret", "us-east-1")
	if p.Name() != "bedrock" {
		t.Fatalf("got %q, want %q", p.Name(), "bedrock")
	}
}

func TestCapabilities(t *testing.T) {
	p := New("key", "secret", "us-east-1")
	caps := p.Capabilities()
	if !caps.Chat {
		t.Error("expected Chat capability")
	}
	if !caps.Streaming {
		t.Error("expected Streaming capability")
	}
	if !caps.Tools {
		t.Error("expected Tools capability")
	}
	if !caps.JSON {
		t.Error("expected JSON capability")
	}
	if caps.Embeddings {
		t.Error("expected Embeddings to be false")
	}
}

func TestModels(t *testing.T) {
	p := New("key", "secret", "us-east-1")
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models")
	}
	for _, m := range models {
		if m.Provider != "bedrock" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "bedrock")
		}
	}
}

func TestComplete(t *testing.T) {
	server := bedrockMockServer(t)
	p := New("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1",
		WithBaseURL(server.URL))

	resp, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Provider != "bedrock" {
		t.Errorf("Provider=%q, want %q", resp.Provider, "bedrock")
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
	if resp.Choices[0].Message.Content == "" {
		t.Error("expected non-empty content")
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason=%q, want %q", resp.Choices[0].FinishReason, "stop")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens=%d, want 10", resp.Usage.PromptTokens)
	}
}

func TestCompleteStream(t *testing.T) {
	server := bedrockMockServer(t)
	p := New("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1",
		WithBaseURL(server.URL))

	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:     "anthropic.claude-3-5-sonnet-20241022-v2:0",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("CompleteStream() error: %v", err)
	}
	defer stream.Close()

	var content string
	var lastFinish string
	for {
		chunk, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		content += chunk.Delta.Content
		if chunk.FinishReason != "" {
			lastFinish = chunk.FinishReason
		}
		if chunk.Provider != "bedrock" {
			t.Errorf("chunk Provider=%q, want %q", chunk.Provider, "bedrock")
		}
	}
	if content != "Hello world" {
		t.Errorf("content=%q, want %q", content, "Hello world")
	}
	if lastFinish != "stop" {
		t.Errorf("FinishReason=%q, want %q", lastFinish, "stop")
	}

	usage := stream.Usage()
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PromptTokens != 5 {
		t.Errorf("PromptTokens=%d, want 5", usage.PromptTokens)
	}
}

func TestHealthy(t *testing.T) {
	server := bedrockMockServer(t)
	p := New("key", "secret", "us-east-1", WithBaseURL(server.URL))
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestEmbedNotSupported(t *testing.T) {
	p := New("key", "secret", "us-east-1")
	providertest.TestProviderEmbedNotSupported(t, p)
}

func TestConformance(t *testing.T) {
	p := New("key", "secret", "us-east-1")
	providertest.TestProviderContract(t, p)
}

func TestStopReasonMapping(t *testing.T) {
	tests := []struct {
		bedrock string
		want    string
	}{
		{"end_turn", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"stop_sequence", "stop"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := mapStopReason(tt.bedrock)
		if got != tt.want {
			t.Errorf("mapStopReason(%q)=%q, want %q", tt.bedrock, got, tt.want)
		}
	}
}

func TestWithSessionToken(t *testing.T) {
	p := New("key", "secret", "us-east-1", WithSessionToken("session-tok"))
	if p.sessionToken != "session-tok" {
		t.Errorf("sessionToken=%q, want %q", p.sessionToken, "session-tok")
	}
}
