package opencompat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/opencompat"
	"github.com/xraph/nexus/testutil"
)

func TestNew(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("my-provider", mock.Server.URL, "test-key")

	if p == nil {
		t.Fatal("New() returned nil")
	}
}

func TestName(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("custom-llm", mock.Server.URL, "test-key")

	if got := p.Name(); got != "custom-llm" {
		t.Errorf("Name() = %q, want %q", got, "custom-llm")
	}
}

func TestName_DifferentNames(t *testing.T) {
	mock := testutil.NewMockServer(t)

	tests := []string{"together", "groq", "fireworks", "local-vllm", "my-provider-123"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			p := opencompat.New(name, mock.Server.URL, "key")
			if got := p.Name(); got != name {
				t.Errorf("Name() = %q, want %q", got, name)
			}
		})
	}
}

func TestDefaultCapabilities(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("test", mock.Server.URL, "key")

	caps := p.Capabilities()
	if !caps.Chat {
		t.Error("default capabilities should have Chat=true")
	}
	if !caps.Streaming {
		t.Error("default capabilities should have Streaming=true")
	}
	if caps.Embeddings {
		t.Error("default capabilities should have Embeddings=false")
	}
	if caps.Vision {
		t.Error("default capabilities should have Vision=false")
	}
	if caps.Tools {
		t.Error("default capabilities should have Tools=false")
	}
}

func TestWithCapabilities(t *testing.T) {
	mock := testutil.NewMockServer(t)

	custom := provider.Capabilities{
		Chat:       true,
		Streaming:  true,
		Embeddings: true,
		Vision:     true,
		Tools:      true,
		JSON:       true,
	}

	p := opencompat.New("test", mock.Server.URL, "key",
		opencompat.WithCapabilities(custom),
	)

	caps := p.Capabilities()
	if !caps.Embeddings {
		t.Error("WithCapabilities should set Embeddings=true")
	}
	if !caps.Vision {
		t.Error("WithCapabilities should set Vision=true")
	}
	if !caps.Tools {
		t.Error("WithCapabilities should set Tools=true")
	}
	if !caps.JSON {
		t.Error("WithCapabilities should set JSON=true")
	}
}

func TestCapabilitiesSupports(t *testing.T) {
	mock := testutil.NewMockServer(t)

	caps := provider.Capabilities{
		Chat:       true,
		Streaming:  true,
		Embeddings: true,
	}
	p := opencompat.New("test", mock.Server.URL, "key",
		opencompat.WithCapabilities(caps),
	)

	got := p.Capabilities()
	if !got.Supports("chat") {
		t.Error("Supports(chat) should be true")
	}
	if !got.Supports("streaming") {
		t.Error("Supports(streaming) should be true")
	}
	if !got.Supports("embeddings") {
		t.Error("Supports(embeddings) should be true")
	}
	if got.Supports("vision") {
		t.Error("Supports(vision) should be false")
	}
	if got.Supports("unknown") {
		t.Error("Supports(unknown) should be false")
	}
}

func TestModels_Default(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("test", mock.Server.URL, "key")

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() returned error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Models() without WithModels should return empty slice, got %d", len(models))
	}
}

func TestWithModels(t *testing.T) {
	mock := testutil.NewMockServer(t)

	modelList := []provider.Model{
		{ID: "llama-3-70b", Provider: "together", Name: "Llama 3 70B"},
		{ID: "mixtral-8x7b", Provider: "together", Name: "Mixtral 8x7B"},
	}

	p := opencompat.New("together", mock.Server.URL, "key",
		opencompat.WithModels(modelList),
	)

	models, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() returned error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Models() returned %d models, want 2", len(models))
	}
	if models[0].ID != "llama-3-70b" {
		t.Errorf("models[0].ID = %q, want %q", models[0].ID, "llama-3-70b")
	}
	if models[1].ID != "mixtral-8x7b" {
		t.Errorf("models[1].ID = %q, want %q", models[1].ID, "mixtral-8x7b")
	}
}

func TestComplete_DelegatesToInnerProvider(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("my-provider", mock.Server.URL, "test-key")

	ctx := context.Background()
	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Complete() returned nil response")
	}

	// Verify the request went to the mock server
	if got := mock.Ctrl.GetLastPath(); got != "/chat/completions" {
		t.Errorf("request path = %q, want %q", got, "/chat/completions")
	}
	if got := mock.Ctrl.GetLastHeader().Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer test-key")
	}
}

func TestComplete_OverridesProviderName(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("my-custom-provider", mock.Server.URL, "test-key")

	ctx := context.Background()
	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}

	// The inner openai provider would set Provider="openai", but opencompat
	// must override it with the custom name.
	if resp.Provider != "my-custom-provider" {
		t.Errorf("resp.Provider = %q, want %q", resp.Provider, "my-custom-provider")
	}
}

func TestComplete_ReturnsChoices(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("test", mock.Server.URL, "key")

	ctx := context.Background()
	resp, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("response must have at least one choice")
	}
}

func TestComplete_ForwardsRequestBody(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("test", mock.Server.URL, "key")

	temp := 0.7
	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "gpt-test",
		Messages: []provider.Message{
			{Role: "user", Content: "What is 2+2?"},
		},
		MaxTokens:   50,
		Temperature: &temp,
	})
	if err != nil {
		t.Fatalf("Complete() returned error: %v", err)
	}

	// Verify the request body was forwarded correctly
	body := mock.Ctrl.GetLastBody()
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	if reqBody["model"] != "gpt-test" {
		t.Errorf("request model = %v, want %q", reqBody["model"], "gpt-test")
	}
	if reqBody["max_tokens"] != float64(50) {
		t.Errorf("request max_tokens = %v, want 50", reqBody["max_tokens"])
	}
}

func TestComplete_ErrorFromServer(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusInternalServerError)

	p := opencompat.New("test", mock.Server.URL, "key")
	ctx := context.Background()
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err == nil {
		t.Fatal("Complete() should return error on server error")
	}
}

func TestEmbed_DelegatesToInnerProvider(t *testing.T) {
	mock := testutil.NewMockServer(t)

	// Set up embedding response
	embResp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1, 0.2, 0.3},
			},
		},
		"model": "text-embedding-test",
		"usage": map[string]any{
			"prompt_tokens": 3,
			"total_tokens":  3,
		},
	}
	mock.Ctrl.SetEmbedding(embResp)

	p := opencompat.New("my-embedder", mock.Server.URL, "test-key")

	ctx := context.Background()
	resp, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "text-embedding-test",
		Input: []string{"Hello world"},
	})
	if err != nil {
		t.Fatalf("Embed() returned error: %v", err)
	}
	if resp == nil {
		t.Fatal("Embed() returned nil response")
	}

	// Verify request path
	if got := mock.Ctrl.GetLastPath(); got != "/embeddings" {
		t.Errorf("request path = %q, want %q", got, "/embeddings")
	}
}

func TestEmbed_OverridesProviderName(t *testing.T) {
	mock := testutil.NewMockServer(t)

	embResp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1, 0.2, 0.3},
			},
		},
		"model": "embed-model",
		"usage": map[string]any{
			"prompt_tokens": 2,
			"total_tokens":  2,
		},
	}
	mock.Ctrl.SetEmbedding(embResp)

	p := opencompat.New("my-custom-embedder", mock.Server.URL, "key")

	ctx := context.Background()
	resp, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "embed-model",
		Input: []string{"Test"},
	})
	if err != nil {
		t.Fatalf("Embed() returned error: %v", err)
	}

	// The inner openai provider would set Provider="openai", but opencompat
	// must override it.
	if resp.Provider != "my-custom-embedder" {
		t.Errorf("resp.Provider = %q, want %q", resp.Provider, "my-custom-embedder")
	}
}

func TestEmbed_ReturnsEmbeddings(t *testing.T) {
	mock := testutil.NewMockServer(t)

	embResp := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.5, 0.6, 0.7, 0.8},
			},
		},
		"model": "test-model",
		"usage": map[string]any{
			"prompt_tokens": 4,
			"total_tokens":  4,
		},
	}
	mock.Ctrl.SetEmbedding(embResp)

	p := opencompat.New("test", mock.Server.URL, "key")

	ctx := context.Background()
	resp, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "test-model",
		Input: []string{"Embed this"},
	})
	if err != nil {
		t.Fatalf("Embed() returned error: %v", err)
	}
	if len(resp.Embeddings) != 1 {
		t.Fatalf("got %d embeddings, want 1", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0]) != 4 {
		t.Errorf("embedding dimension = %d, want 4", len(resp.Embeddings[0]))
	}
}

func TestEmbed_ErrorFromServer(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusUnauthorized)

	p := opencompat.New("test", mock.Server.URL, "bad-key")

	ctx := context.Background()
	_, err := p.Embed(ctx, &provider.EmbeddingRequest{
		Model: "test-model",
		Input: []string{"Hello"},
	})
	if err == nil {
		t.Fatal("Embed() should return error on server error")
	}
}

func TestHealthy_DelegatesToInnerProvider(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("test", mock.Server.URL, "key")

	ctx := context.Background()

	// Default mock returns 200 on /models, so Healthy should be true
	if !p.Healthy(ctx) {
		t.Error("Healthy() should return true when mock server is up")
	}
}

func TestHealthy_ReturnsFalseOnError(t *testing.T) {
	mock := testutil.NewMockServer(t)
	mock.Ctrl.SetStatusCode(http.StatusServiceUnavailable)

	p := opencompat.New("test", mock.Server.URL, "key")

	ctx := context.Background()
	if p.Healthy(ctx) {
		t.Error("Healthy() should return false when server returns error")
	}
}

func TestCompleteStream_DelegatesToInnerProvider(t *testing.T) {
	mock := testutil.NewMockServer(t)

	chunks := []string{
		testutil.OpenAIChunkJSON("chunk-1", "test-model", "Hello", ""),
		testutil.OpenAIChunkJSON("chunk-2", "test-model", " world", "stop"),
	}
	mock.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler(chunks))

	p := opencompat.New("my-streamer", mock.Server.URL, "key")

	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "test-model",
		Messages: []provider.Message{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("CompleteStream() returned error: %v", err)
	}
	defer stream.Close()

	// Read at least one chunk to verify streaming works
	chunk, err := stream.Next(ctx)
	if err != nil {
		t.Fatalf("stream.Next() returned error: %v", err)
	}
	if chunk == nil {
		t.Fatal("stream.Next() returned nil chunk")
	}
}

func TestProviderInterface(t *testing.T) {
	mock := testutil.NewMockServer(t)
	p := opencompat.New("test", mock.Server.URL, "key")

	// Verify that *Provider satisfies the provider.Provider interface
	var _ provider.Provider = p
}

func TestMultipleOptions(t *testing.T) {
	mock := testutil.NewMockServer(t)

	caps := provider.Capabilities{
		Chat:       true,
		Streaming:  true,
		Embeddings: true,
		Tools:      true,
	}
	models := []provider.Model{
		{ID: "model-a", Provider: "test", Name: "Model A"},
	}

	p := opencompat.New("multi-opt", mock.Server.URL, "key",
		opencompat.WithCapabilities(caps),
		opencompat.WithModels(models),
	)

	if p.Name() != "multi-opt" {
		t.Errorf("Name() = %q, want %q", p.Name(), "multi-opt")
	}

	gotCaps := p.Capabilities()
	if !gotCaps.Tools {
		t.Error("capabilities should have Tools=true")
	}

	gotModels, err := p.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(gotModels) != 1 {
		t.Fatalf("Models() returned %d, want 1", len(gotModels))
	}
	if gotModels[0].ID != "model-a" {
		t.Errorf("model ID = %q, want %q", gotModels[0].ID, "model-a")
	}
}
