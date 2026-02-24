package azureopenai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providertest"
	"github.com/xraph/nexus/testutil"
)

func TestName(t *testing.T) {
	p := New("test-key")
	if p.Name() != "azureopenai" {
		t.Fatalf("got %q, want %q", p.Name(), "azureopenai")
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
		if m.Provider != "azureopenai" {
			t.Errorf("model %q provider=%q, want %q", m.ID, m.Provider, "azureopenai")
		}
	}
}

func TestComplete(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key",
		WithBaseURL(mock.Server.URL),
		WithDeploymentID("gpt-4o"),
		WithAPIVersion("2024-08-01-preview"),
	)

	resp, err := p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Provider != "azureopenai" {
		t.Errorf("Provider=%q, want %q", resp.Provider, "azureopenai")
	}
	if len(resp.Choices) == 0 {
		t.Fatal("no choices")
	}
}

func TestComplete_APIKeyHeader(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("my-azure-key",
		WithBaseURL(mock.Server.URL),
		WithDeploymentID("gpt-4o"),
	)

	_, _ = p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})

	headers := mock.Ctrl.GetLastHeader()
	if headers.Get("api-key") != "my-azure-key" {
		t.Errorf("api-key header=%q, want %q", headers.Get("api-key"), "my-azure-key")
	}
	// Azure uses api-key, NOT Authorization Bearer.
	if auth := headers.Get("Authorization"); auth != "" {
		t.Errorf("unexpected Authorization header: %q", auth)
	}
}

func TestComplete_URLFormat(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key",
		WithBaseURL(mock.Server.URL),
		WithDeploymentID("my-deployment"),
		WithAPIVersion("2024-08-01-preview"),
	)

	_, _ = p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 100,
	})

	path := mock.Ctrl.GetLastPath()
	expected := "/openai/deployments/my-deployment/chat/completions"
	if path != expected {
		t.Errorf("path=%q, want %q", path, expected)
	}
}

func TestComplete_RequestBody(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key",
		WithBaseURL(mock.Server.URL),
		WithDeploymentID("gpt-4o"),
	)

	_, _ = p.Complete(context.Background(), &provider.CompletionRequest{
		Model:     "gpt-4o",
		Messages:  []provider.Message{{Role: "user", Content: "Hello"}},
		MaxTokens: 200,
	})

	var body map[string]any
	_ = json.Unmarshal(mock.Ctrl.GetLastBody(), &body)
	if body["model"] != "gpt-4o" {
		t.Errorf("model=%v, want %q", body["model"], "gpt-4o")
	}
	if int(body["max_tokens"].(float64)) != 200 {
		t.Errorf("max_tokens=%v, want 200", body["max_tokens"])
	}
}

func TestEmbed(t *testing.T) {
	// Azure embed uses a custom path: /openai/deployments/{id}/embeddings
	// The testutil mock's isEmbeddingPath only checks /embeddings and /v1/embeddings
	// so we create a dedicated httptest server for this.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}, "index": 0},
			},
			"usage": map[string]any{"prompt_tokens": 3, "total_tokens": 3},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	p := New("test-key",
		WithBaseURL(server.URL),
		WithDeploymentID("text-embedding-ada-002"),
	)

	resp, err := p.Embed(context.Background(), &provider.EmbeddingRequest{
		Model: "text-embedding-ada-002",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if resp.Provider != "azureopenai" {
		t.Errorf("Provider=%q, want %q", resp.Provider, "azureopenai")
	}
	if len(resp.Embeddings) != 1 {
		t.Fatalf("len(Embeddings)=%d, want 1", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0]) != 3 {
		t.Errorf("embedding dimension=%d, want 3", len(resp.Embeddings[0]))
	}
}

func TestHealthy(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := New("test-key",
		WithBaseURL(mock.Server.URL),
		WithDeploymentID("gpt-4o"),
	)
	if !p.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestConformance(t *testing.T) {
	p := New("test-key")
	providertest.TestProviderContract(t, p)
}

func TestOptions(t *testing.T) {
	p := New("key",
		WithResourceName("my-resource"),
		WithDeploymentID("my-deploy"),
		WithAPIVersion("2024-01-01"),
		WithBaseURL("https://custom.endpoint.com"),
	)
	if p.resourceName != "my-resource" {
		t.Errorf("resourceName=%q, want %q", p.resourceName, "my-resource")
	}
	if p.deploymentID != "my-deploy" {
		t.Errorf("deploymentID=%q, want %q", p.deploymentID, "my-deploy")
	}
	if p.apiVersion != "2024-01-01" {
		t.Errorf("apiVersion=%q, want %q", p.apiVersion, "2024-01-01")
	}
	if p.baseURL != "https://custom.endpoint.com" {
		t.Errorf("baseURL=%q, want %q", p.baseURL, "https://custom.endpoint.com")
	}
}
