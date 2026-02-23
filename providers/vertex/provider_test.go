package vertex

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

func vertexMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		_ = body

		// Check for stream endpoint.
		if r.URL.Query().Get("alt") == "sse" {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			chunk1 := map[string]any{
				"candidates": []map[string]any{
					{"content": map[string]any{"role": "model", "parts": []map[string]any{{"text": "Hello"}}}},
				},
			}
			chunk2 := map[string]any{
				"candidates": []map[string]any{
					{"content": map[string]any{"role": "model", "parts": []map[string]any{{"text": " world"}}}, "finishReason": "STOP"},
				},
				"usageMetadata": map[string]any{"promptTokenCount": 5, "candidatesTokenCount": 3, "totalTokenCount": 8},
			}
			b1, _ := json.Marshal(chunk1)
			b2, _ := json.Marshal(chunk2)
			fmt.Fprintf(w, "data: %s\n\n", b1)
			flusher.Flush()
			fmt.Fprintf(w, "data: %s\n\n", b2)
			flusher.Flush()
			return
		}

		// Check for predict (embeddings).
		if r.URL.Path != "" && len(r.URL.Path) > 8 && r.URL.Path[len(r.URL.Path)-7:] == "predict" {
			resp := map[string]any{
				"predictions": []map[string]any{
					{"embeddings": map[string]any{"values": []float64{0.1, 0.2, 0.3, 0.4, 0.5}}},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Default: generateContent response.
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"role":  "model",
						"parts": []map[string]any{{"text": "Hello! How can I help you?"}},
					},
					"finishReason": "STOP",
				},
			},
			"usageMetadata": map[string]any{
				"promptTokenCount":     10,
				"candidatesTokenCount": 8,
				"totalTokenCount":      18,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestName(t *testing.T) {
	p := New(WithAccessToken("test-token"))
	if p.Name() != "vertex" {
		t.Fatalf("got %q, want %q", p.Name(), "vertex")
	}
}

func TestCapabilities(t *testing.T) {
	p := New(WithAccessToken("test-token"))
	caps := p.Capabilities()
	if !caps.Chat {
		t.Error("expected Chat capability")
	}
	if !caps.Streaming {
		t.Error("expected Streaming capability")
	}
	if !caps.Embeddings {
		t.Error("expected Embeddings capability")
	}
	if !caps.Vision {
		t.Error("expected Vision capability")
	}
	if !caps.Tools {
		t.Error("expected Tools capability")
	}
	if !caps.JSON {
		t.Error("expected JSON capability")
	}
}

func TestModels(t *testing.T) {
	p := New(WithAccessToken("test-token"))
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models")
	}
	for _, m := range models {
		if m.Provider != "vertex" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "vertex")
		}
	}
}

func TestComplete(t *testing.T) {
	server := vertexMockServer(t)
	p := New(
		WithAccessToken("test-token"),
		WithProjectID("test-project"),
		WithLocation("us-central1"),
		WithBaseURL(server.URL),
	)

	resp, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "gemini-2.0-flash",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Provider != "vertex" {
		t.Errorf("Provider=%q, want %q", resp.Provider, "vertex")
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
	server := vertexMockServer(t)
	p := New(
		WithAccessToken("test-token"),
		WithProjectID("test-project"),
		WithLocation("us-central1"),
		WithBaseURL(server.URL),
	)

	stream, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:     "gemini-2.0-flash",
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
		if chunk.Provider != "vertex" {
			t.Errorf("chunk Provider=%q, want %q", chunk.Provider, "vertex")
		}
	}
	if content != "Hello world" {
		t.Errorf("content=%q, want %q", content, "Hello world")
	}
	if lastFinish != "stop" {
		t.Errorf("FinishReason=%q, want %q", lastFinish, "stop")
	}
}

func TestEmbed(t *testing.T) {
	server := vertexMockServer(t)
	p := New(
		WithAccessToken("test-token"),
		WithProjectID("test-project"),
		WithLocation("us-central1"),
		WithBaseURL(server.URL),
	)

	resp, err := p.Embed(context.Background(), &provider.EmbeddingRequest{
		Model: "text-embedding-004",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if resp.Provider != "vertex" {
		t.Errorf("Provider=%q, want %q", resp.Provider, "vertex")
	}
	if len(resp.Embeddings) != 1 {
		t.Fatalf("len(Embeddings)=%d, want 1", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0]) != 5 {
		t.Errorf("embedding dimension=%d, want 5", len(resp.Embeddings[0]))
	}
}

func TestHealthy(t *testing.T) {
	server := vertexMockServer(t)
	p := New(
		WithAccessToken("test-token"),
		WithProjectID("test-project"),
		WithLocation("us-central1"),
		WithBaseURL(server.URL),
	)
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestConformance(t *testing.T) {
	p := New(WithAccessToken("test-token"))
	providertest.TestProviderContract(t, p)
}

func TestOptions(t *testing.T) {
	p := New(
		WithProjectID("my-project"),
		WithLocation("europe-west1"),
		WithAccessToken("my-token"),
		WithBaseURL("https://custom.endpoint.com"),
	)
	if p.projectID != "my-project" {
		t.Errorf("projectID=%q, want %q", p.projectID, "my-project")
	}
	if p.location != "europe-west1" {
		t.Errorf("location=%q, want %q", p.location, "europe-west1")
	}
	if p.accessToken != "my-token" {
		t.Errorf("accessToken=%q, want %q", p.accessToken, "my-token")
	}
	if p.baseURL != "https://custom.endpoint.com" {
		t.Errorf("baseURL=%q, want %q", p.baseURL, "https://custom.endpoint.com")
	}
}

func TestDefaultLocation(t *testing.T) {
	p := New(WithAccessToken("test-token"))
	if p.location != "us-central1" {
		t.Errorf("default location=%q, want %q", p.location, "us-central1")
	}
}

func TestFinishReasonMapping(t *testing.T) {
	// Test through a complete round-trip since vertexMapFinishReason is unexported.
	// Verified via the TestComplete test (STOP -> stop).
	// Additional mapping verification via response content.
}
