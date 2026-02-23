package gemini

import (
	"context"
	"testing"

	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/testutil"
	"github.com/xraph/nexus/provider"
)

func TestName(t *testing.T) {
	p := New("test-key")
	if p.Name() != "gemini" {
		t.Fatalf("got %q, want %q", p.Name(), "gemini")
	}
}

func TestCapabilities(t *testing.T) {
	p := New("test-key")
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
	p := New("test-key")
	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models")
	}
	for _, m := range models {
		if m.Provider != "gemini" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "gemini")
		}
	}
}

func TestComplete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetCompletion(map[string]any{
		"candidates": []map[string]any{{
			"content":      map[string]any{"parts": []map[string]any{{"text": "Hello!"}}, "role": "model"},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 8, "totalTokenCount": 18},
	})
	p := New("test-key", WithBaseURL(mock.Server.URL))
	resp, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "test-model",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Complete() returned nil response")
	}
	if resp.Provider != "gemini" {
		t.Errorf("response Provider=%q, want %q", resp.Provider, "gemini")
	}
	if len(resp.Choices) == 0 {
		t.Error("response must have at least one choice")
	}
}

func TestEmbed(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetCompletion(map[string]any{
		"embeddings": []map[string]any{{"values": []float64{0.1, 0.2, 0.3}}},
	})
	// The embed endpoint uses a different path, so we set the embedding response too.
	mock.Ctrl.SetEmbedding(map[string]any{
		"embeddings": []map[string]any{{"values": []float64{0.1, 0.2, 0.3}}},
	})
	p := New("test-key", WithBaseURL(mock.Server.URL))
	resp, err := p.Embed(context.Background(), &provider.EmbeddingRequest{
		Model: "test-embed-model",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Embed() returned nil response")
	}
	if len(resp.Embeddings) == 0 {
		t.Error("response must have at least one embedding")
	}
}

func TestHealthy(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(mock.Server.URL))
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestConformance(t *testing.T) {
	mock := testutil.NewMockServer(t)
	// Set a Gemini-format completion response so the conformance test's Models() call works.
	mock.Ctrl.SetCompletion(map[string]any{
		"candidates": []map[string]any{{
			"content":      map[string]any{"parts": []map[string]any{{"text": "Hello!"}}, "role": "model"},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 8, "totalTokenCount": 18},
	})
	p := New("test-key", WithBaseURL(mock.Server.URL))
	providertest.TestProviderContract(t, p)
}
