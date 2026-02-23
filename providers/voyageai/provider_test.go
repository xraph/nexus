package voyageai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/provider"
)

func voyageMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5}, "index": 0},
			},
			"usage": map[string]any{"total_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestName(t *testing.T) {
	p := New("test-key")
	if p.Name() != "voyageai" {
		t.Fatalf("got %q, want %q", p.Name(), "voyageai")
	}
}

func TestCapabilities(t *testing.T) {
	p := New("test-key")
	caps := p.Capabilities()
	if !caps.Embeddings {
		t.Error("expected Embeddings capability")
	}
	if caps.Chat {
		t.Error("expected Chat to be false")
	}
	if caps.Streaming {
		t.Error("expected Streaming to be false")
	}
}

func TestModels(t *testing.T) {
	p := New("test-key")
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models")
	}
	for _, m := range models {
		if m.Provider != "voyageai" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "voyageai")
		}
		if !m.Capabilities.Embeddings {
			t.Errorf("model %q should have Embeddings capability", m.ID)
		}
	}
}

func TestEmbed(t *testing.T) {
	server := voyageMockServer(t)
	p := New("test-key", WithBaseURL(server.URL))

	resp, err := p.Embed(context.Background(), &provider.EmbeddingRequest{
		Model: "voyage-3",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if resp.Provider != "voyageai" {
		t.Errorf("Provider=%q, want %q", resp.Provider, "voyageai")
	}
	if len(resp.Embeddings) != 1 {
		t.Fatalf("len(Embeddings)=%d, want 1", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0]) != 5 {
		t.Errorf("embedding dimension=%d, want 5", len(resp.Embeddings[0]))
	}
	if resp.Usage.TotalTokens != 5 {
		t.Errorf("TotalTokens=%d, want 5", resp.Usage.TotalTokens)
	}
}

func TestCompleteNotSupported(t *testing.T) {
	p := New("test-key")
	_, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:    "any-model",
		Messages: []provider.Message{{Role: "user", Content: "Hello"}},
	})
	if err != provider.ErrNotSupported {
		t.Fatalf("Complete() should return ErrNotSupported, got: %v", err)
	}
}

func TestCompleteStreamNotSupported(t *testing.T) {
	p := New("test-key")
	_, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model:    "any-model",
		Messages: []provider.Message{{Role: "user", Content: "Hello"}},
	})
	if err != provider.ErrNotSupported {
		t.Fatalf("CompleteStream() should return ErrNotSupported, got: %v", err)
	}
}

func TestHealthy(t *testing.T) {
	server := voyageMockServer(t)
	p := New("test-key", WithBaseURL(server.URL))
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestConformance(t *testing.T) {
	p := New("test-key")
	providertest.TestProviderContract(t, p)
}

func TestEmbed_AuthHeader(t *testing.T) {
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2}, "index": 0},
			},
			"usage": map[string]any{"total_tokens": 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	p := New("my-voyage-key", WithBaseURL(server.URL))
	_, _ = p.Embed(context.Background(), &provider.EmbeddingRequest{
		Model: "voyage-3",
		Input: []string{"test"},
	})

	if capturedAuth != "Bearer my-voyage-key" {
		t.Errorf("Authorization=%q, want %q", capturedAuth, "Bearer my-voyage-key")
	}
}
